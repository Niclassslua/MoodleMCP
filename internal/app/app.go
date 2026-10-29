package app

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Niclassslua/MoodleMCP/internal/config"
	"github.com/Niclassslua/MoodleMCP/internal/course"
	"github.com/Niclassslua/MoodleMCP/internal/forum"
	"github.com/Niclassslua/MoodleMCP/internal/fsname"
	"github.com/Niclassslua/MoodleMCP/internal/message"
	"github.com/Niclassslua/MoodleMCP/internal/moodle"
	"github.com/Niclassslua/MoodleMCP/internal/textx"
)

type App struct {
	Client *moodle.Client
}

func New() (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	c, err := moodle.New(cfg)
	if err != nil {
		return nil, err
	}
	return &App{Client: c}, nil
}

func (a *App) ListCourses() ([]moodle.Course, error) {
	return a.Client.Courses()
}

// CoursesFiltered narrows courses to Moodle's timeline classification:
// "all", "inprogress", "future", or "past".
func (a *App) CoursesFiltered(filter string) ([]moodle.Course, error) {
	if filter == "" || filter == "all" {
		return a.Client.Courses()
	}
	return a.Client.CoursesByTimeline(filter)
}

// CurrentCourses returns the courses Moodle classifies as in progress.
func (a *App) CurrentCourses() ([]moodle.Course, error) {
	return a.Client.CoursesByTimeline("inprogress")
}

// coursesFor resolves one course, or all current courses when ref is empty.
func (a *App) coursesFor(ref string) ([]moodle.Course, error) {
	if strings.TrimSpace(ref) == "" {
		return a.CurrentCourses()
	}
	co, err := a.ResolveCourse(ref)
	if err != nil {
		return nil, err
	}
	return []moodle.Course{co}, nil
}

// moduleIDFromURL reads the course-module id from a /mod/<x>/view.php?id=N link.
func moduleIDFromURL(link string) int {
	u, err := url.Parse(link)
	if err != nil || !strings.Contains(u.Path, "/mod/") {
		return 0
	}
	id, _ := strconv.Atoi(u.Query().Get("id"))
	return id
}

// ResolveCourse accepts id, URL, shortname, or unique fullname substring.
func (a *App) ResolveCourse(ref string) (moodle.Course, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return moodle.Course{}, fmt.Errorf("invalid input: course is required")
	}

	if id, err := course.ResolveCourseID(0, ref); err == nil && strings.Contains(ref, "id=") {
		return a.courseByID(id)
	}
	if id, err := strconv.Atoi(ref); err == nil && id > 0 {
		return a.courseByID(id)
	}

	courses, err := a.Client.Courses()
	if err != nil {
		return moodle.Course{}, err
	}
	refLower := strings.ToLower(ref)
	var matches []moodle.Course
	for _, co := range courses {
		if strings.EqualFold(co.ShortName, ref) || strings.EqualFold(co.FullName, ref) {
			return co, nil
		}
		if strings.Contains(strings.ToLower(co.ShortName), refLower) ||
			strings.Contains(strings.ToLower(co.FullName), refLower) {
			matches = append(matches, co)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return moodle.Course{}, fmt.Errorf("course not found: no enrolled course matches %q; use list_courses to see course IDs", ref)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("ambiguous course: %q matches several courses; retry with one of these IDs:", ref))
	for _, m := range matches {
		b.WriteString(fmt.Sprintf("\n  %d  %s  (%s)", m.ID, m.ShortName, m.FullName))
	}
	return moodle.Course{}, fmt.Errorf("%s", b.String())
}

func (a *App) courseByID(id int) (moodle.Course, error) {
	courses, err := a.Client.Courses()
	if err != nil {
		return moodle.Course{}, err
	}
	for _, co := range courses {
		if co.ID == id {
			return co, nil
		}
	}
	return moodle.Course{ID: id, FullName: fmt.Sprintf("course-%d", id), URL: a.Client.CourseURL(id)}, nil
}

type InspectModule struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	ModName string `json:"modname"`
	URL     string `json:"url,omitempty"`
}

type InspectSection struct {
	Name    string          `json:"name"`
	Modules []InspectModule `json:"modules"`
}

type InspectResult struct {
	Course       moodle.Course     `json:"course"`
	Meta         moodle.CourseMeta `json:"meta,omitempty"`
	ModuleCounts map[string]int    `json:"moduleCounts"`
	FileCount    int               `json:"fileCount"`
	FileBytes    int64             `json:"fileBytes"`
	Sections     []InspectSection  `json:"sections"`
}

func (a *App) Inspect(ref string) (InspectResult, error) {
	co, err := a.ResolveCourse(ref)
	if err != nil {
		return InspectResult{}, err
	}
	meta, _ := a.Client.CourseMeta(co.ID)
	sections, err := a.Client.CourseContents(co.ID)
	if err != nil {
		return InspectResult{}, err
	}
	out := InspectResult{
		Course:       co,
		Meta:         meta,
		ModuleCounts: map[string]int{},
	}
	for _, sec := range sections {
		is := InspectSection{Name: sec.Name}
		for _, m := range sec.Modules {
			if m.UserVisible != nil && !*m.UserVisible {
				continue
			}
			out.ModuleCounts[m.ModName]++
			is.Modules = append(is.Modules, InspectModule{ID: m.ID, Name: m.Name, ModName: m.ModName, URL: m.URL})
			for _, ct := range m.Contents {
				if ct.Type == "file" {
					out.FileCount++
					out.FileBytes += ct.Filesize
				}
			}
		}
		if len(is.Modules) > 0 {
			out.Sections = append(out.Sections, is)
		}
	}
	return out, nil
}

type DoctorResult struct {
	OK            bool   `json:"ok"`
	SiteName      string `json:"sitename"`
	Username      string `json:"username"`
	UserID        int    `json:"userid"`
	SiteURL       string `json:"siteUrl,omitempty"`
	Release       string `json:"release"`
	DownloadFiles int    `json:"downloadfiles"`
	CourseCount   int    `json:"courseCount"`
	SampleCourse  string `json:"sampleCourse,omitempty"`
	SampleFiles   int    `json:"sampleFiles,omitempty"`
	Error         string `json:"error,omitempty"`
}

func (a *App) Doctor() DoctorResult {
	info, err := a.Client.SiteInfo()
	if err != nil {
		return DoctorResult{OK: false, Error: err.Error()}
	}
	courses, err := a.Client.Courses()
	if err != nil {
		return DoctorResult{OK: false, Error: err.Error(), SiteName: info.SiteName, Username: info.Username}
	}
	res := DoctorResult{
		OK:            true,
		SiteName:      info.SiteName,
		Username:      info.Username,
		UserID:        info.UserID,
		SiteURL:       info.SiteURL,
		Release:       info.Release,
		DownloadFiles: info.DownloadFiles,
		CourseCount:   len(courses),
	}
	if info.DownloadFiles == 0 {
		res.OK = false
		res.Error = "downloadfiles=0; pluginfile downloads may fail"
	}
	if len(courses) > 0 {
		sec, err := a.Client.CourseContents(courses[0].ID)
		if err == nil {
			res.SampleCourse = courses[0].FullName
			n := 0
			for _, s := range sec {
				for _, m := range s.Modules {
					for _, ct := range m.Contents {
						if ct.Type == "file" {
							n++
						}
					}
				}
			}
			res.SampleFiles = n
		}
	}
	return res
}

type DueEvent struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	EventType string `json:"eventtype"`
	When      int64  `json:"when"`
	Due       string `json:"due"`
	CourseID  int    `json:"courseId,omitempty"`
	Course    string `json:"course,omitempty"`
	URL       string `json:"url,omitempty"`
	SubmitURL string `json:"submitUrl,omitempty"`
	ModuleID  int    `json:"moduleId,omitempty"`
	Overdue   bool   `json:"overdue,omitempty"`
}

type DueResult struct {
	Days   int        `json:"days"`
	Events []DueEvent `json:"events"`
}

// Due lists events from pastDays ago up to days ahead. Past events come from
// Moodle's action events, which only include items still needing action.
func (a *App) Due(ref string, days, pastDays int) (DueResult, error) {
	if days <= 0 {
		days = 14
	}
	var courseID int
	if ref != "" {
		co, err := a.ResolveCourse(ref)
		if err != nil {
			return DueResult{}, err
		}
		courseID = co.ID
	}
	cutoff := time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
	now := time.Now().Unix()
	from := now - int64(pastDays)*24*3600

	seen := map[int]bool{}
	var events []DueEvent
	add := func(list []moodle.CalendarEvent) {
		for _, e := range list {
			when := e.TimeSort
			if when == 0 {
				when = e.TimeStart
			}
			if when < from || when > cutoff {
				continue
			}
			if seen[e.ID] {
				continue
			}
			seen[e.ID] = true
			cid := e.Course.ID
			if courseID > 0 && cid != 0 && cid != courseID {
				continue
			}
			if cid == 0 {
				cid = courseID
			}
			events = append(events, DueEvent{
				ID: e.ID, Name: e.Name, EventType: e.EventType, When: when, Due: localTime(when), CourseID: cid,
				Course: e.Course.FullName, URL: e.URL, SubmitURL: submitURL(e),
				ModuleID: moduleIDFromURL(e.URL), Overdue: e.Overdue || (when < now && e.ModuleName != ""),
			})
		}
	}

	if pastDays > 0 {
		past, err := a.Client.ActionEventsSince(from, now)
		if err != nil {
			return DueResult{}, err
		}
		add(past)
	}
	if courseID > 0 {
		ae, err := a.Client.ActionEvents(courseID)
		if err == nil {
			add(ae)
		}
		up, err := a.Client.UpcomingEvents(courseID)
		if err == nil {
			add(up)
		}
	} else {
		courses, err := a.Client.Courses()
		if err != nil {
			return DueResult{}, err
		}
		for _, co := range courses {
			ae, err := a.Client.ActionEvents(co.ID)
			if err == nil {
				add(ae)
			}
		}
		up, err := a.Client.UpcomingEvents(0)
		if err == nil {
			add(up)
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].When < events[j].When })
	return DueResult{Days: days, Events: events}, nil
}

func submitURL(e moodle.CalendarEvent) string {
	if e.Action == nil {
		return ""
	}
	return e.Action.URL
}

type GradesResult struct {
	Course moodle.Course      `json:"course"`
	Items  []moodle.GradeItem `json:"items"`
}

func (a *App) Grades(ref string) (GradesResult, error) {
	co, err := a.ResolveCourse(ref)
	if err != nil {
		return GradesResult{}, err
	}
	items, err := a.Client.Grades(co.ID)
	if err != nil {
		return GradesResult{}, err
	}
	return GradesResult{Course: co, Items: items}, nil
}

type DownloadOpts struct {
	Course       string
	OutDir       string
	Mode         string
	FilenameMode string
	Concurrency  int
	SkipExisting bool
	Force        bool
	SkipLocked   bool
	Flat         bool
	MaxBytes     int64
}

func (a *App) Download(opt DownloadOpts) (course.Result, error) {
	co, err := a.ResolveCourse(opt.Course)
	if err != nil {
		return course.Result{}, err
	}
	mode := course.Mode(opt.Mode)
	if mode == "" {
		mode = course.ModeAll
	}
	fm := fsname.Mode(opt.FilenameMode)
	if fm == "" {
		fm = fsname.UTF8
	}
	skip := opt.SkipExisting && !opt.Force
	return course.Download(a.Client, course.Options{
		CourseID:     co.ID,
		OutDir:       opt.OutDir,
		Mode:         mode,
		FilenameMode: fm,
		Concurrency:  opt.Concurrency,
		SkipExisting: skip,
		Force:        opt.Force,
		SkipLocked:   opt.SkipLocked,
		Flat:         opt.Flat,
		MaxBytes:     opt.MaxBytes,
	})
}

type SyncItem struct {
	Course string `json:"course"`
	Out    string `json:"out"`
	Mode   string `json:"mode"`
}

type SyncOpts struct {
	Items        []SyncItem
	OutDir       string
	Mode         string
	FilenameMode string
	Concurrency  int
	Force        bool
	SkipLocked   bool
	Flat         bool
	MaxBytes     int64
	Interval     time.Duration
}

func (a *App) SyncOnce(opt SyncOpts) ([]course.Result, error) {
	var results []course.Result
	for _, it := range opt.Items {
		outdir := it.Out
		if outdir == "" {
			outdir = opt.OutDir
		}
		mode := it.Mode
		if mode == "" {
			mode = opt.Mode
		}
		res, err := a.Download(DownloadOpts{
			Course:       it.Course,
			OutDir:       outdir,
			Mode:         mode,
			FilenameMode: opt.FilenameMode,
			Concurrency:  opt.Concurrency,
			SkipExisting: true,
			Force:        opt.Force,
			SkipLocked:   opt.SkipLocked,
			Flat:         opt.Flat,
			MaxBytes:     opt.MaxBytes,
		})
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}

type ForumHit struct {
	Course       string `json:"course,omitempty"`
	Forum        string `json:"forum"`
	Discussion   string `json:"discussion"`
	DiscussionID int    `json:"discussionId"`
	URL          string `json:"url"`
	Author       string `json:"author,omitempty"`
	Created      string `json:"created,omitempty"`
	LastActivity string `json:"lastActivity,omitempty"`
	Replies      int    `json:"replies"`
	Pinned       bool   `json:"pinned,omitempty"`
	Locked       bool   `json:"locked,omitempty"`
	UserID       int    `json:"userId,omitempty"`
	TimeStart    string `json:"timeStart,omitempty"`
	TimeEnd      string `json:"timeEnd,omitempty"`
	Starred      bool   `json:"starred,omitempty"`
	Unread       int    `json:"unread,omitempty"`
	Attachment   bool   `json:"attachment,omitempty"`
	CanReply     bool   `json:"canReply"`
	CanLock      bool   `json:"canLock"`
	Posts        int    `json:"posts,omitempty"`
	Snippet      string `json:"snippet,omitempty"`
}

func localTime(unix int64) string {
	if unix == 0 {
		return ""
	}
	return time.Unix(unix, 0).Local().Format("2006-01-02 15:04")
}

type ForumsResult struct {
	Course moodle.Course `json:"course"`
	Hits   []ForumHit    `json:"hits"`
}

func (a *App) SearchForums(ref, query string) (ForumsResult, error) {
	return a.searchForums(ref, query, true)
}

// SearchForumSummaries lists discussions without fetching every thread's posts.
func (a *App) SearchForumSummaries(ref, query string) (ForumsResult, error) {
	return a.searchForums(ref, query, false)
}

// searchForums searches one course, or all current courses when ref is empty.
func (a *App) searchForums(ref, query string, includePosts bool) (ForumsResult, error) {
	courses, err := a.coursesFor(ref)
	if err != nil {
		return ForumsResult{}, err
	}
	var res ForumsResult
	if ref != "" {
		res.Course = courses[0]
	}
	q := strings.ToLower(strings.TrimSpace(query))
	for _, co := range courses {
		hits, err := a.courseForumHits(co, q, includePosts)
		if err != nil {
			return ForumsResult{}, err
		}
		res.Hits = append(res.Hits, hits...)
	}
	// Newest activity first across all forums; the timestamp format sorts lexically.
	sort.SliceStable(res.Hits, func(i, j int) bool { return res.Hits[i].LastActivity > res.Hits[j].LastActivity })
	return res, nil
}

func (a *App) courseForumHits(co moodle.Course, q string, includePosts bool) ([]ForumHit, error) {
	sections, err := a.Client.CourseContents(co.ID)
	if err != nil {
		return nil, err
	}
	var hits []ForumHit
	for _, sec := range sections {
		for _, mod := range sec.Modules {
			if mod.ModName != "forum" {
				continue
			}
			disc, err := forum.ListDiscussions(a.Client, mod.Instance)
			if err != nil {
				return nil, err
			}
			for _, d := range disc {
				name := d.Name
				if name == "" {
					name = d.Subject
				}
				hay := strings.ToLower(name + " " + d.Subject)
				if q != "" && !strings.Contains(hay, q) {
					continue
				}
				hit := ForumHit{Course: co.FullName, Forum: mod.Name, Discussion: name, DiscussionID: d.DiscussionID(), URL: a.discussionURL(d.DiscussionID()), Author: d.UserFullName,
					Created: localTime(d.Created), LastActivity: localTime(d.TimeModified), Replies: d.NumReplies, Pinned: d.Pinned, Locked: d.Locked,
					UserID: d.UserID, TimeStart: localTime(d.TimeStart), TimeEnd: localTime(d.TimeEnd), Starred: d.Starred,
					Unread: d.NumUnread, Attachment: d.Attachment, CanReply: d.CanReply, CanLock: d.CanLock}
				if includePosts {
					posts, _ := forum.ListPosts(a.Client, d.DiscussionID())
					hit.Posts = len(posts)
					if len(posts) > 0 {
						hit.Snippet = forum.StripHTML(posts[0].Message)
						if len(hit.Snippet) > 160 {
							hit.Snippet = hit.Snippet[:160] + "…"
						}
					}
				}
				hits = append(hits, hit)
			}
		}
	}
	return hits, nil
}

type Participant struct {
	ID    int      `json:"id"`
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

type ParticipantsResult struct {
	Course       moodle.Course `json:"course"`
	Participants []Participant `json:"participants"`
}

// Participants lists course members with their role shortnames, teaching
// roles first, optionally filtered by name substring and role.
func (a *App) Participants(ref, query, role string) (ParticipantsResult, error) {
	co, err := a.ResolveCourse(ref)
	if err != nil {
		return ParticipantsResult{}, err
	}
	users, err := a.Client.EnrolledUsers(co.ID)
	if err != nil {
		return ParticipantsResult{}, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := ParticipantsResult{Course: co, Participants: []Participant{}}
	for _, u := range users {
		p := Participant{ID: u.ID, Name: u.FullName, Roles: []string{}}
		hasRole := role == ""
		for _, r := range u.Roles {
			p.Roles = append(p.Roles, r.ShortName)
			hasRole = hasRole || r.ShortName == role
		}
		if hasRole && strings.Contains(strings.ToLower(p.Name), q) {
			out.Participants = append(out.Participants, p)
		}
	}
	teaches := func(p Participant) bool {
		for _, r := range p.Roles {
			if r != "student" && r != "guest" {
				return true
			}
		}
		return false
	}
	sort.SliceStable(out.Participants, func(i, j int) bool {
		ti, tj := teaches(out.Participants[i]), teaches(out.Participants[j])
		if ti != tj {
			return ti
		}
		return out.Participants[i].Name < out.Participants[j].Name
	})
	return out, nil
}

type ForumPost struct {
	ID          int      `json:"id"`
	Author      string   `json:"author"`
	Time        string   `json:"time"`
	Subject     string   `json:"subject"`
	Text        string   `json:"text"`
	Truncated   bool     `json:"truncated,omitempty"`
	Attachments []string `json:"attachments,omitempty"`
	// filled only by DiscussionThread, for UIs
	ParentID    int    `json:"parentId,omitempty"`
	Created     string `json:"created,omitempty"`     // RFC3339
	HTML        string `json:"html,omitempty"`        // Moodle's message HTML, inline images as data URIs
	AuthorImage string `json:"authorImage,omitempty"` // profile picture as data URI
}

func (a *App) discussionURL(id int) string {
	return fmt.Sprintf("%s/mod/forum/discuss.php?d=%d", a.Client.Base, id)
}

type DiscussionPage struct {
	DiscussionID int         `json:"discussionId"`
	URL          string      `json:"url"`
	Offset       int         `json:"offset"`
	Limit        int         `json:"limit"`
	Total        int         `json:"total"`
	HasMore      bool        `json:"hasMore"`
	Posts        []ForumPost `json:"posts"`
}

// DiscussionPosts returns one bounded, oldest-first page of a forum discussion.
func (a *App) DiscussionPosts(id, offset, limit int) (DiscussionPage, error) {
	return a.discussionPage(id, offset, limit, false)
}

// DiscussionThread returns the whole discussion with HTML, reply structure and
// images inlined, so a UI can render it without the Moodle token.
func (a *App) DiscussionThread(id int) (DiscussionPage, error) {
	return a.discussionPage(id, 0, 1000, true)
}

// maxInlineImage caps each image embedded as a data URI.
const maxInlineImage = 2 << 20

func (a *App) dataURI(fileURL string) string {
	var buf bytes.Buffer
	n, err := a.Client.Download(fileURL, &limitWriter{w: &buf, left: maxInlineImage + 1})
	if err != nil || n == 0 || buf.Len() > maxInlineImage {
		return ""
	}
	return "data:" + http.DetectContentType(buf.Bytes()) + ";base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

type limitWriter struct {
	w    io.Writer
	left int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if len(p) > l.left {
		return 0, io.ErrShortWrite
	}
	l.left -= len(p)
	return l.w.Write(p)
}

func (a *App) discussionPage(id, offset, limit int, rich bool) (DiscussionPage, error) {
	if id <= 0 || offset < 0 || limit < 1 {
		return DiscussionPage{}, fmt.Errorf("invalid input: discussion id must be positive, offset non-negative, and limit positive")
	}
	posts, err := forum.ListPosts(a.Client, id)
	if err != nil {
		return DiscussionPage{}, fmt.Errorf("discussion not found: %d is not available (%w); use search_forums to see IDs", id, err)
	}
	sort.SliceStable(posts, func(i, j int) bool { return posts[i].TimeCreated < posts[j].TimeCreated })
	end := min(offset+limit, len(posts))
	out := DiscussionPage{DiscussionID: id, URL: a.discussionURL(id), Offset: offset, Limit: limit, Total: len(posts), HasMore: end < len(posts), Posts: []ForumPost{}}
	if offset >= len(posts) {
		return out, nil
	}
	avatars := map[string]string{}
	for _, p := range posts[offset:end] {
		text, truncated := previewText(forum.StripHTML(p.Message), 4000)
		fp := ForumPost{ID: p.ID, Author: p.Author.Name, Time: localTime(p.TimeCreated), Subject: p.Subject, Text: text, Truncated: truncated}
		for _, f := range p.Attachments {
			fp.Attachments = append(fp.Attachments, f.Filename)
		}
		if rich {
			fp.Text, fp.Truncated = forum.StripHTML(p.Message), false
			fp.ParentID = p.ParentID
			fp.Created = time.Unix(p.TimeCreated, 0).Format(time.RFC3339)
			fp.HTML = p.Message
			for _, f := range p.InlineFiles {
				if f.FileURL != "" && strings.Contains(fp.HTML, f.FileURL) {
					if d := a.dataURI(f.FileURL); d != "" {
						fp.HTML = strings.ReplaceAll(fp.HTML, f.FileURL, d)
					}
				}
			}
			if img := p.Author.URLs.ProfileImage; img != "" {
				if _, ok := avatars[img]; !ok {
					avatars[img] = a.dataURI(img)
				}
				fp.AuthorImage = avatars[img]
			}
		}
		out.Posts = append(out.Posts, fp)
	}
	return out, nil
}

type ChatMessage struct {
	From      string `json:"from"`
	Time      string `json:"time"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

func (a *App) conversationURL(id int) string {
	return fmt.Sprintf("%s/message/index.php?convid=%d", a.Client.Base, id)
}

type ConversationSummary struct {
	ID              int    `json:"id"`
	URL             string `json:"url"`
	Title           string `json:"title"`
	Members         string `json:"members,omitempty"`
	Unread          int    `json:"unread"`
	Latest          string `json:"latest,omitempty"`
	LatestTruncated bool   `json:"latestTruncated,omitempty"`
}

type ConversationPage struct {
	ID       int           `json:"id"`
	URL      string        `json:"url"`
	Title    string        `json:"title"`
	Offset   int           `json:"offset"`
	Limit    int           `json:"limit"`
	HasMore  bool          `json:"hasMore"`
	Messages []ChatMessage `json:"messages"`
}

func previewText(s string, maxRunes int) (string, bool) {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s, false
	}
	return string(runes[:maxRunes]) + "…", true
}

// ListMessageSummaries avoids fetching entire conversation histories.
func (a *App) ListMessageSummaries(query string) ([]ConversationSummary, error) {
	convs, err := message.ListConversations(a.Client)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	var summaries []ConversationSummary
	for _, c := range convs {
		title := message.Title(c, a.Client.UserID)
		var members []string
		for _, m := range c.Members {
			if m.ID != a.Client.UserID && m.FullName != "" {
				members = append(members, m.FullName)
			}
		}
		latest := ""
		latestTruncated := false
		if n := len(c.Messages); n > 0 {
			latest, latestTruncated = previewText(forum.StripHTML(c.Messages[n-1].Text), 240)
		}
		memberNames := strings.Join(members, ", ")
		if q != "" && !strings.Contains(strings.ToLower(title+" "+memberNames+" "+latest), q) {
			continue
		}
		summaries = append(summaries, ConversationSummary{
			ID: c.ID, URL: a.conversationURL(c.ID), Title: title, Members: memberNames, Unread: c.UnreadCount, Latest: latest, LatestTruncated: latestTruncated,
		})
	}
	return summaries, nil
}

// ConversationMessages returns one bounded, oldest-first page of a conversation.
func (a *App) ConversationMessages(id, offset, limit int) (ConversationPage, error) {
	if id <= 0 || offset < 0 || limit < 1 {
		return ConversationPage{}, fmt.Errorf("invalid input: conversation id must be positive, offset non-negative, and limit positive")
	}
	conv, err := message.GetConversation(a.Client, id)
	if err != nil {
		return ConversationPage{}, fmt.Errorf("conversation not found: %d is not available (%w); use list_messages to see IDs", id, err)
	}
	msgs, members, err := message.ListMessagesPage(a.Client, id, offset, limit+1)
	if err != nil {
		return ConversationPage{}, fmt.Errorf("conversation %d: %w", id, err)
	}
	hasMore := len(msgs) > limit
	if hasMore {
		msgs = msgs[:limit]
	}
	names := map[int]string{}
	for _, m := range append(append([]message.Member{}, conv.Members...), members...) {
		if m.FullName != "" {
			names[m.ID] = m.FullName
		}
	}
	out := ConversationPage{ID: id, URL: a.conversationURL(id), Title: message.Title(conv, a.Client.UserID), Offset: offset, Limit: limit, HasMore: hasMore, Messages: []ChatMessage{}}
	for _, m := range msgs {
		who := names[m.UserIDFrom]
		if who == "" {
			who = fmt.Sprintf("user-%d", m.UserIDFrom)
		}
		text, truncated := previewText(forum.StripHTML(m.Text), 4000)
		out.Messages = append(out.Messages, ChatMessage{From: who, Time: time.Unix(m.TimeCreated, 0).Local().Format("2006-01-02 15:04"), Text: text, Truncated: truncated})
	}
	return out, nil
}

type MessageHit struct {
	ID       int           `json:"id"`
	Title    string        `json:"title"`
	Type     int           `json:"type"`
	Unread   int           `json:"unread"`
	Members  string        `json:"members,omitempty"`
	Count    int           `json:"count"`
	Messages []ChatMessage `json:"messages"`
}

type MessagesResult struct {
	Hits []MessageHit `json:"hits"`
}

func (a *App) ListMessages(query string) (MessagesResult, error) {
	convs, err := message.ListConversations(a.Client)
	if err != nil {
		return MessagesResult{}, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	var hits []MessageHit
	for _, c := range convs {
		title := message.Title(c, a.Client.UserID)
		var memberNames []string
		for _, m := range c.Members {
			if m.ID != a.Client.UserID && m.FullName != "" {
				memberNames = append(memberNames, m.FullName)
			}
		}
		hay := strings.ToLower(title + " " + strings.Join(memberNames, " "))
		if q != "" && !strings.Contains(hay, q) {
			continue
		}

		msgs, mems, err := message.ListMessages(a.Client, c.ID)
		if err != nil {
			return MessagesResult{}, fmt.Errorf("conversation %d: %w", c.ID, err)
		}
		names := map[int]string{}
		for _, m := range append(append([]message.Member{}, c.Members...), mems...) {
			if m.FullName != "" {
				names[m.ID] = m.FullName
			}
		}
		bodies := make([]ChatMessage, 0, len(msgs))
		for _, m := range msgs {
			who := names[m.UserIDFrom]
			if who == "" {
				who = fmt.Sprintf("user-%d", m.UserIDFrom)
			}
			bodies = append(bodies, ChatMessage{
				From: who,
				Time: time.Unix(m.TimeCreated, 0).Local().Format("2006-01-02 15:04"),
				Text: forum.StripHTML(m.Text),
			})
		}
		hits = append(hits, MessageHit{
			ID: c.ID, Title: title, Type: c.Type, Unread: c.UnreadCount,
			Members: strings.Join(memberNames, ", "), Count: len(bodies), Messages: bodies,
		})
	}
	return MessagesResult{Hits: hits}, nil
}

type MessagesExportResult struct {
	Written int    `json:"written"`
	Dir     string `json:"dir"`
}

func (a *App) ExportMessages(outDir, query, filenameMode string) (MessagesExportResult, error) {
	if outDir == "" {
		outDir = "./downloads"
	}
	mode := fsname.UTF8
	if filenameMode == "ascii" {
		mode = fsname.ASCII
	}
	n, err := message.Export(a.Client, outDir, query, mode)
	if err != nil {
		return MessagesExportResult{}, err
	}
	return MessagesExportResult{Written: n, Dir: filepath.Join(outDir, "Nachrichten")}, nil
}

type AssignmentDetail struct {
	ModuleID       int      `json:"moduleId"`
	AssignmentID   int      `json:"assignmentId"`
	CourseID       int      `json:"courseId"`
	Name           string   `json:"name"`
	URL            string   `json:"url"`
	Intro          string   `json:"intro"`
	IntroTruncated bool     `json:"introTruncated,omitempty"`
	Opens          string   `json:"opens,omitempty"`
	Due            string   `json:"due,omitempty"`
	Cutoff         string   `json:"cutoff,omitempty"`
	Attachments    []string `json:"attachments,omitempty"`
	Submission     string   `json:"submissionStatus,omitempty"`
	GradingStatus  string   `json:"gradingStatus,omitempty"`
	LastModified   string   `json:"lastModified,omitempty"`
	SubmittedFiles []string `json:"submittedFiles,omitempty"`
	Grade          string   `json:"grade,omitempty"`
	GradedAt       string   `json:"gradedAt,omitempty"`
	Feedback       string   `json:"feedback,omitempty"`
}

// Assignment returns an assignment's instructions, dates, and the user's
// submission and feedback, looked up by course-module id.
func (a *App) Assignment(moduleID int) (AssignmentDetail, error) {
	all, err := a.Client.AllAssignments()
	if err != nil {
		return AssignmentDetail{}, err
	}
	var as *moodle.Assignment
	for i := range all {
		if all[i].CMID == moduleID {
			as = &all[i]
			break
		}
	}
	if as == nil {
		return AssignmentDetail{}, fmt.Errorf("module not found: no assignment with module id %d; use list_due or inspect_course to find module IDs", moduleID)
	}
	intro, truncated := previewText(forum.StripHTML(as.Intro), 8000)
	out := AssignmentDetail{
		ModuleID: moduleID, AssignmentID: as.ID, CourseID: as.Course, Name: as.Name,
		URL:   fmt.Sprintf("%s/mod/assign/view.php?id=%d", a.Client.Base, moduleID),
		Intro: intro, IntroTruncated: truncated,
		Opens: localTime(as.AllowSubmissionsFromDate), Due: localTime(as.DueDate), Cutoff: localTime(as.CutoffDate),
	}
	for _, f := range append(append([]moodle.AssignFile{}, as.IntroAttachments...), as.IntroFiles...) {
		out.Attachments = append(out.Attachments, f.Filename)
	}
	st, err := a.Client.SubmissionStatus(as.ID)
	if err != nil {
		return AssignmentDetail{}, err
	}
	if la := st.LastAttempt; la != nil {
		out.GradingStatus = la.GradingStatus
		if sub := la.Submission; sub != nil {
			out.Submission = sub.Status
			out.LastModified = localTime(sub.TimeModified)
			for _, pl := range sub.Plugins {
				for _, fa := range pl.FileAreas {
					for _, f := range fa.Files {
						out.SubmittedFiles = append(out.SubmittedFiles, f.Filename)
					}
				}
			}
		}
	}
	if fb := st.Feedback; fb != nil {
		out.Grade = forum.StripHTML(fb.GradeForDisplay)
		out.GradedAt = localTime(fb.GradedDate)
		var parts []string
		for _, pl := range fb.Plugins {
			for _, f := range pl.EditorFields {
				if t := forum.StripHTML(f.Text); t != "" {
					parts = append(parts, t)
				}
			}
		}
		out.Feedback, _ = previewText(strings.Join(parts, "\n\n"), 8000)
	}
	return out, nil
}

type CourseUpdate struct {
	Course      string   `json:"course"`
	CourseID    int      `json:"courseId"`
	ModuleID    int      `json:"moduleId"`
	Module      string   `json:"module"`
	ModName     string   `json:"modname"`
	URL         string   `json:"url,omitempty"`
	Changes     []string `json:"changes"`
	LastUpdated string   `json:"lastUpdated,omitempty"`
}

// CourseUpdates lists modules changed in the last sinceDays days, newest
// first, for one course or all current courses.
func (a *App) CourseUpdates(ref string, sinceDays int) ([]CourseUpdate, error) {
	courses, err := a.coursesFor(ref)
	if err != nil {
		return nil, err
	}
	since := time.Now().Add(-time.Duration(sinceDays) * 24 * time.Hour).Unix()
	var out []CourseUpdate
	for _, co := range courses {
		insts, err := a.Client.UpdatesSince(co.ID, since)
		if err != nil {
			return nil, err
		}
		if len(insts) == 0 {
			continue
		}
		sections, err := a.Client.CourseContents(co.ID)
		if err != nil {
			return nil, err
		}
		mods := map[int]moodle.Module{}
		for _, sec := range sections {
			for _, m := range sec.Modules {
				mods[m.ID] = m
			}
		}
		for _, in := range insts {
			if in.ContextLevel != "module" {
				continue
			}
			m := mods[in.ID]
			u := CourseUpdate{Course: co.FullName, CourseID: co.ID, ModuleID: in.ID, Module: m.Name, ModName: m.ModName, URL: m.URL, Changes: []string{}}
			var last int64
			for _, up := range in.Updates {
				u.Changes = append(u.Changes, up.Name)
				last = max(last, up.TimeUpdated)
			}
			u.LastUpdated = localTime(last)
			out = append(out, u)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastUpdated > out[j].LastUpdated })
	return out, nil
}

type ModuleText struct {
	ModuleID   int      `json:"moduleId"`
	Name       string   `json:"name"`
	ModName    string   `json:"modname"`
	URL        string   `json:"url,omitempty"`
	Files      []string `json:"files,omitempty"`
	File       string   `json:"file,omitempty"`
	MIMEType   string   `json:"mimeType,omitempty"`
	Text       string   `json:"text"`
	Start      int      `json:"start"`
	TotalChars int      `json:"totalChars"`
	HasMore    bool     `json:"hasMore"`
	NextStart  int      `json:"nextStart,omitempty"`
}

const maxReadBytes = 50 << 20

// ReadModule returns a window of a module's text: page content, label text,
// a URL module's target, or the extracted text of one of its files.
func (a *App) ReadModule(ref string, moduleID int, file string, start, maxChars int) (ModuleText, error) {
	co, err := a.ResolveCourse(ref)
	if err != nil {
		return ModuleText{}, err
	}
	sections, err := a.Client.CourseContents(co.ID)
	if err != nil {
		return ModuleText{}, err
	}
	var mod *moodle.Module
	for _, sec := range sections {
		for i := range sec.Modules {
			if sec.Modules[i].ID == moduleID {
				mod = &sec.Modules[i]
			}
		}
	}
	if mod == nil {
		return ModuleText{}, fmt.Errorf("module not found: module %d is not in %s; use inspect_course to list module IDs", moduleID, co.FullName)
	}
	out := ModuleText{ModuleID: mod.ID, Name: mod.Name, ModName: mod.ModName, URL: mod.URL}
	var text string
	switch mod.ModName {
	case "assign":
		return ModuleText{}, fmt.Errorf("invalid input: module %d is an assignment; use get_assignment", moduleID)
	case "page":
		pages, err := a.Client.Pages(co.ID)
		if err != nil {
			return ModuleText{}, err
		}
		for _, p := range pages {
			if p.CMID == moduleID {
				text = forum.StripHTML(p.Intro + "\n" + p.Content)
			}
		}
	case "label":
		labels, err := a.Client.Labels(co.ID)
		if err != nil {
			return ModuleText{}, err
		}
		for _, l := range labels {
			if l.CMID == moduleID {
				text = forum.StripHTML(l.Intro)
			}
		}
	case "url":
		for _, c := range mod.Contents {
			if c.Type == "url" {
				text = c.FileURL
			}
		}
	default:
		var files []moodle.Content
		for _, c := range mod.Contents {
			if c.Type == "file" {
				files = append(files, c)
				out.Files = append(out.Files, c.Filename)
			}
		}
		chosen, err := pickFile(files, file)
		if err != nil {
			return ModuleText{}, err
		}
		if chosen.Filesize > maxReadBytes {
			return ModuleText{}, fmt.Errorf("unsupported file type: %s is %d MB, over the %d MB read limit; open it in Moodle: %s", chosen.Filename, chosen.Filesize>>20, maxReadBytes>>20, mod.URL)
		}
		// ponytail: every page of a file re-downloads it; add a per-session cache if paging large files gets slow.
		var buf bytes.Buffer
		if _, err := a.Client.Download(chosen.FileURL, &buf); err != nil {
			return ModuleText{}, err
		}
		text, err = textx.Extract(chosen.Filename, chosen.MIMEType, buf.Bytes())
		if errors.Is(err, textx.ErrUnsupported) {
			return ModuleText{}, fmt.Errorf("%w; open it in Moodle: %s", err, mod.URL)
		}
		if err != nil {
			return ModuleText{}, err
		}
		out.File, out.MIMEType = chosen.Filename, chosen.MIMEType
	}
	runes := []rune(text)
	out.Start, out.TotalChars = start, len(runes)
	if start < len(runes) {
		end := min(start+maxChars, len(runes))
		out.Text = string(runes[start:end])
		out.HasMore = end < len(runes)
		if out.HasMore {
			out.NextStart = end
		}
	}
	return out, nil
}

func pickFile(files []moodle.Content, name string) (moodle.Content, error) {
	if len(files) == 0 {
		return moodle.Content{}, fmt.Errorf("unsupported file type: this module has no files or readable text")
	}
	if name == "" {
		if len(files) == 1 {
			return files[0], nil
		}
		var names []string
		for _, f := range files {
			names = append(names, f.Filename)
		}
		return moodle.Content{}, fmt.Errorf("invalid input: module has %d files; pass file as one of: %s", len(files), strings.Join(names, ", "))
	}
	for _, f := range files {
		if strings.EqualFold(f.Filename, name) {
			return f, nil
		}
	}
	return moodle.Content{}, fmt.Errorf("invalid input: no file named %q in this module", name)
}

type NotificationItem struct {
	ID      int    `json:"id"`
	Subject string `json:"subject"`
	Text    string `json:"text,omitempty"`
	Time    string `json:"time"`
	Read    bool   `json:"read"`
	URL     string `json:"url,omitempty"`
	URLName string `json:"urlName,omitempty"`
}

type NotificationsResult struct {
	Offset        int                `json:"offset"`
	Limit         int                `json:"limit"`
	HasMore       bool               `json:"hasMore"`
	UnreadCount   int                `json:"unreadCount"`
	Notifications []NotificationItem `json:"notifications"`
}

// Notifications returns one newest-first page of Moodle notifications;
// unreadOnly filters that page.
func (a *App) Notifications(offset, limit int, unreadOnly bool) (NotificationsResult, error) {
	page, err := a.Client.Notifications(offset, limit+1)
	if err != nil {
		return NotificationsResult{}, err
	}
	out := NotificationsResult{Offset: offset, Limit: limit, UnreadCount: page.UnreadCount, Notifications: []NotificationItem{}}
	ns := page.Notifications
	if len(ns) > limit {
		ns, out.HasMore = ns[:limit], true
	}
	for _, n := range ns {
		if unreadOnly && n.Read {
			continue
		}
		text, _ := previewText(forum.StripHTML(n.SmallMessage), 500)
		out.Notifications = append(out.Notifications, NotificationItem{
			ID: n.ID, Subject: n.Subject, Text: text, Time: localTime(n.TimeCreated), Read: n.Read, URL: n.ContextURL, URLName: n.ContextURLName,
		})
	}
	return out, nil
}

type CourseGradeSummary struct {
	CourseID int    `json:"courseId"`
	Course   string `json:"course"`
	URL      string `json:"url"`
	Grade    string `json:"grade"`
}

// GradesOverview returns the overall grade of every enrolled course.
func (a *App) GradesOverview() ([]CourseGradeSummary, error) {
	grades, err := a.Client.OverviewGrades()
	if err != nil {
		return nil, err
	}
	courses, err := a.Client.Courses()
	if err != nil {
		return nil, err
	}
	names := map[int]string{}
	for _, c := range courses {
		names[c.ID] = c.FullName
	}
	var out []CourseGradeSummary
	for _, g := range grades {
		name := names[g.CourseID]
		if name == "" {
			name = fmt.Sprintf("course-%d", g.CourseID)
		}
		out = append(out, CourseGradeSummary{CourseID: g.CourseID, Course: name, URL: a.Client.CourseURL(g.CourseID), Grade: g.Grade})
	}
	return out, nil
}
