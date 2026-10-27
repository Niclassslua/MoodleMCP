package moodle

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Niclassslua/MoodleMCP/internal/config"
)

type Client struct {
	Base   string
	Token  string
	HTTP   *http.Client
	UserID int
}

func New(cfg config.Config) (*Client, error) {
	c := &Client{
		Base: cfg.BaseURL,
		HTTP: &http.Client{Timeout: 120 * time.Second},
	}
	token := cfg.Token
	if token == "" {
		var err error
		token, err = fetchToken(c.HTTP, cfg.BaseURL, cfg.Username, cfg.Password, cfg.Service)
		if err != nil {
			return nil, err
		}
	}
	c.Token = token
	info, err := c.SiteInfo()
	if err != nil {
		return nil, err
	}
	c.UserID = info.UserID
	return c, nil
}

func fetchToken(httpClient *http.Client, base, user, pass, service string) (string, error) {
	form := url.Values{
		"username": {user},
		"password": {pass},
		"service":  {service},
	}
	resp, err := httpClient.PostForm(base+"/login/token.php", form)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var out struct {
		Token string `json:"token"`
		Error string `json:"error"`
		Code  string `json:"errorcode"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("token: non-JSON (HTTP %d)", resp.StatusCode)
	}
	if out.Token == "" {
		msg := out.Error
		if out.Code != "" {
			msg = out.Code + ": " + msg
		}
		return "", fmt.Errorf("token: %s", msg)
	}
	return out.Token, nil
}

type SiteInfo struct {
	SiteName      string `json:"sitename"`
	Username      string `json:"username"`
	UserID        int    `json:"userid"`
	Release       string `json:"release"`
	DownloadFiles int    `json:"downloadfiles"`
	SiteURL       string `json:"siteurl"`
}

func (c *Client) SiteInfo() (SiteInfo, error) {
	var info SiteInfo
	if err := c.Call("core_webservice_get_site_info", nil, &info); err != nil {
		return info, err
	}
	return info, nil
}

type Course struct {
	ID        int    `json:"id"`
	FullName  string `json:"fullname"`
	ShortName string `json:"shortname"`
	URL       string `json:"url,omitempty"`
}

// CourseURL is the course's page in the Moodle web UI.
func (c *Client) CourseURL(id int) string {
	return fmt.Sprintf("%s/course/view.php?id=%d", c.Base, id)
}

func (c *Client) Courses() ([]Course, error) {
	params := url.Values{"userid": {fmt.Sprintf("%d", c.UserID)}}
	var courses []Course
	if err := c.Call("core_enrol_get_users_courses", params, &courses); err != nil {
		return nil, err
	}
	for i := range courses {
		courses[i].URL = c.CourseURL(courses[i].ID)
	}
	return courses, nil
}

type Section struct {
	ID      int      `json:"id"`
	Name    string   `json:"name"`
	Summary string   `json:"summary"`
	Modules []Module `json:"modules"`
}

type Module struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	ModName     string    `json:"modname"`
	Instance    int       `json:"instance"`
	URL         string    `json:"url"`
	Contents    []Content `json:"contents"`
	Visible     any       `json:"visible"`
	UserVisible *bool     `json:"uservisible"`
}

type Content struct {
	Type         string `json:"type"`
	Filename     string `json:"filename"`
	Filepath     string `json:"filepath"`
	Filesize     int64  `json:"filesize"`
	FileURL      string `json:"fileurl"`
	TimeModified int64  `json:"timemodified"`
	MIMEType     string `json:"mimetype"`
}

func (c *Client) CourseContents(courseID int) ([]Section, error) {
	params := url.Values{"courseid": {fmt.Sprintf("%d", courseID)}}
	var sections []Section
	if err := c.Call("core_course_get_contents", params, &sections); err != nil {
		return nil, err
	}
	return sections, nil
}

func (c *Client) Call(fn string, extra url.Values, dest any) error {
	u, err := url.Parse(c.Base + "/webservice/rest/server.php")
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("wstoken", c.Token)
	q.Set("wsfunction", fn)
	q.Set("moodlewsrestformat", "json")
	for k, vs := range extra {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	u.RawQuery = q.Encode()

	resp, err := c.HTTP.Get(u.String())
	if err != nil {
		return fmt.Errorf("Moodle connection failed: %w", redactURL(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	var exc struct {
		Exception string `json:"exception"`
		ErrorCode string `json:"errorcode"`
		Message   string `json:"message"`
	}
	if json.Unmarshal(body, &exc) == nil && exc.Exception != "" {
		return fmt.Errorf("Moodle error: %s: %s (%s)", fn, exc.Message, exc.ErrorCode)
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(body, dest)
}

// FileURLWithToken ensures webservice/pluginfile URLs carry the token.
func (c *Client) FileURLWithToken(fileURL string) string {
	if fileURL == "" {
		return ""
	}
	u, err := url.Parse(fileURL)
	if err != nil {
		return fileURL
	}
	// Rewrite /pluginfile.php → /webservice/pluginfile.php when needed
	if strings.Contains(u.Path, "/pluginfile.php") && !strings.Contains(u.Path, "/webservice/pluginfile.php") {
		u.Path = strings.Replace(u.Path, "/pluginfile.php", "/webservice/pluginfile.php", 1)
	}
	q := u.Query()
	q.Set("token", c.Token)
	u.RawQuery = q.Encode()
	return u.String()
}

func (c *Client) Download(fileURL string, w io.Writer) (int64, error) {
	resp, err := c.HTTP.Get(c.FileURLWithToken(fileURL))
	if err != nil {
		return 0, redactURL(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	return io.Copy(w, io.LimitReader(resp.Body, 500<<20)) // 500 MB cap
}

// redactURL drops the query (which carries the token) from transport errors.
func redactURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		if u, perr := url.Parse(ue.URL); perr == nil {
			u.RawQuery = ""
			ue.URL = u.String()
		} else {
			ue.URL = "(redacted)"
		}
	}
	return err
}
