// wscheck probes whether this Moodle site allows Web Services for your account.
//
//	cd mcd && go run ./cmd/wscheck
//
// Reads MOODLE_* from the environment, or from ../.env / .env (never prints secrets).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	loadDotEnv()

	base := strings.TrimRight(strings.TrimSpace(os.Getenv("MOODLE_URL")), "/")
	user := strings.TrimSpace(os.Getenv("MOODLE_USERNAME"))
	pass := os.Getenv("MOODLE_PASSWORD")
	service := strings.TrimSpace(os.Getenv("MOODLE_SERVICE"))
	if service == "" {
		service = "moodle_mobile_app"
	}
	token := strings.TrimSpace(os.Getenv("MOODLE_TOKEN"))

	if base == "" {
		fail("MOODLE_URL is empty")
	}
	fmt.Printf("Moodle URL: %s\n", base)
	fmt.Printf("Service:    %s\n", service)

	client := &http.Client{Timeout: 30 * time.Second}

	if token == "" {
		if user == "" || pass == "" {
			fail("need MOODLE_TOKEN or MOODLE_USERNAME + MOODLE_PASSWORD")
		}
		fmt.Println("\n[1/3] Requesting token via login/token.php …")
		var err error
		token, err = fetchToken(client, base, user, pass, service)
		if err != nil {
			fail("token: %v\n\nWeb Services may be disabled, the service shortname wrong, or credentials rejected.\nTry MOODLE_SERVICE=… if your site uses a custom service.", err)
		}
		fmt.Println("     OK — token received (not shown)")
	} else {
		fmt.Println("\n[1/3] Using MOODLE_TOKEN from environment")
	}

	fmt.Println("[2/3] Calling core_webservice_get_site_info …")
	site, err := callWS(client, base, token, "core_webservice_get_site_info", nil)
	if err != nil {
		fail("site_info: %v", err)
	}
	var info struct {
		Sitename      string `json:"sitename"`
		Username      string `json:"username"`
		Release       string `json:"release"`
		Version       string `json:"version"`
		DownloadFiles int    `json:"downloadfiles"`
	}
	if err := json.Unmarshal(site, &info); err != nil {
		fail("site_info JSON: %v\nraw: %s", err, truncate(string(site), 400))
	}
	fmt.Printf("     OK — site=%q user=%q release=%q downloadfiles=%d\n",
		info.Sitename, info.Username, info.Release, info.DownloadFiles)
	if info.DownloadFiles == 0 {
		fmt.Println("     WARN — downloadfiles=0; file downloads via pluginfile may be blocked for this service")
	}

	fmt.Println("[3/3] Calling core_enrol_get_users_courses …")
	// userid 0 often means “current user” is not accepted; site_info includes userid
	var siteFull struct {
		Userid int `json:"userid"`
	}
	_ = json.Unmarshal(site, &siteFull)
	params := url.Values{}
	if siteFull.Userid > 0 {
		params.Set("userid", fmt.Sprintf("%d", siteFull.Userid))
	}
	coursesRaw, err := callWS(client, base, token, "core_enrol_get_users_courses", params)
	if err != nil {
		fail("courses: %v", err)
	}
	var courses []map[string]any
	if err := json.Unmarshal(coursesRaw, &courses); err != nil {
		// some sites wrap errors as object
		fail("courses JSON: %v\nraw: %s", err, truncate(string(coursesRaw), 400))
	}
	fmt.Printf("     OK — %d course(s)\n", len(courses))
	for i, c := range courses {
		if i >= 5 {
			fmt.Printf("     … and %d more\n", len(courses)-5)
			break
		}
		fmt.Printf("     - id=%v  %v\n", c["id"], c["fullname"])
	}

	fmt.Println("\nPASS — Web Services work for this account. Safe to build the Go CLI on WS.")
}

func fetchToken(client *http.Client, base, user, pass, service string) (string, error) {
	form := url.Values{
		"username": {user},
		"password": {pass},
		"service":  {service},
	}
	resp, err := client.PostForm(base+"/login/token.php", form)
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
		return "", fmt.Errorf("non-JSON response (HTTP %d): %s", resp.StatusCode, truncate(string(body), 200))
	}
	if out.Token == "" {
		msg := out.Error
		if out.Code != "" {
			msg = out.Code + ": " + msg
		}
		if msg == "" {
			msg = truncate(string(body), 200)
		}
		return "", fmt.Errorf("%s", msg)
	}
	return out.Token, nil
}

func callWS(client *http.Client, base, token, fn string, extra url.Values) ([]byte, error) {
	u, err := url.Parse(base + "/webservice/rest/server.php")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("wstoken", token)
	q.Set("wsfunction", fn)
	q.Set("moodlewsrestformat", "json")
	for k, vs := range extra {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	u.RawQuery = q.Encode()

	resp, err := client.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	// Moodle exception shape
	var exc struct {
		Exception string `json:"exception"`
		ErrorCode string `json:"errorcode"`
		Message   string `json:"message"`
	}
	if json.Unmarshal(body, &exc) == nil && exc.Exception != "" {
		return nil, fmt.Errorf("%s (%s): %s", exc.ErrorCode, exc.Exception, exc.Message)
	}
	return body, nil
}

func loadDotEnv() {
	candidates := []string{
		".env",
		filepath.Join("..", ".env"),
		filepath.Join("..", "..", ".env"),
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, ".env"),
			filepath.Join(wd, "..", ".env"),
			filepath.Join(wd, "..", "..", ".env"),
		)
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			v = strings.Trim(v, `"'`)
			if os.Getenv(k) == "" {
				_ = os.Setenv(k, v)
			}
		}
		fmt.Printf("Loaded env file: %s\n", p)
		return
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "\nFAIL — "+format+"\n", args...)
	os.Exit(1)
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
