package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Niclassslua/MoodleMCP/internal/app"
	"github.com/Niclassslua/MoodleMCP/internal/config"
	"github.com/Niclassslua/MoodleMCP/internal/moodle"
	mcpserver "github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeMoodle serves just enough of the Web Services API for the MCP tools:
// 25 courses ("Course 01".."Course 25"), an empty forum in course 2, a
// permission error on course 3's grades, a forum with discussions in course 4,
// conversation 5 with 30 messages, and in course 4 (the only current course)
// a text resource, a page, a two-file folder, and assignment "Essay".
func fakeMoodle(t *testing.T) *mcpserver.ClientSession {
	return connectClient(t, newServer("test", func() (*app.App, error) { return fakeApp(t), nil }))
}

// fakeApp is an App wired to the fake Moodle server.
func fakeApp(t *testing.T) *app.App {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if strings.Contains(r.URL.Path, "pluginfile.php") {
			w.Write([]byte(notesText))
			return
		}
		site := "http://" + r.Host
		file := func(name string) map[string]any {
			return map[string]any{"type": "file", "filename": name, "fileurl": site + "/webservice/pluginfile.php/1/" + name, "mimetype": "text/plain", "filesize": len(notesText)}
		}
		var out any
		switch q.Get("wsfunction") {
		case "core_webservice_get_site_info":
			out = map[string]any{"sitename": "Fake", "username": "u", "userid": 7, "release": "4.1", "downloadfiles": 1, "siteurl": "http://" + r.Host}
		case "core_enrol_get_users_courses":
			var cs []moodle.Course
			for i := 1; i <= 25; i++ {
				cs = append(cs, moodle.Course{ID: i, FullName: fmt.Sprintf("Course %02d", i), ShortName: fmt.Sprintf("c%02d", i)})
			}
			out = cs
		case "core_course_get_contents":
			out = []any{}
			// course 2: empty forum 8; course 4: forum 9 with two discussions
			if forumID, ok := map[string]int{"2": 8, "4": 9}[q.Get("courseid")]; ok {
				modules := []any{map[string]any{"id": 1, "name": "News", "modname": "forum", "instance": forumID}}
				if forumID == 9 {
					modules = append(modules,
						map[string]any{"id": 2, "name": "Notes", "modname": "resource", "url": site + "/mod/resource/view.php?id=2", "contents": []any{file("notes.txt")}},
						map[string]any{"id": 3, "name": "Week 1", "modname": "page"},
						map[string]any{"id": 4, "name": "Slides", "modname": "folder", "contents": []any{file("a.txt"), file("b.txt")}},
						map[string]any{"id": 5, "name": "Essay", "modname": "assign"})
				}
				out = []any{map[string]any{"name": "General", "modules": modules}}
			}
		case "core_course_get_enrolled_courses_by_timeline_classification":
			out = map[string]any{"courses": []any{map[string]any{"id": 4, "fullname": "Course 04", "shortname": "c04"}}}
		case "mod_label_get_labels_by_courses":
			out = map[string]any{"labels": []any{}}
		case "mod_page_get_pages_by_courses":
			out = map[string]any{"pages": []any{map[string]any{"coursemodule": 3, "name": "Week 1", "intro": "", "content": "<p>Read chapter 1</p>"}}}
		case "mod_assign_get_assignments":
			out = map[string]any{"courses": []any{map[string]any{"id": 4, "assignments": []any{map[string]any{
				"id": 50, "cmid": 5, "course": 4, "name": "Essay", "intro": "<p>Write 500 words</p>", "duedate": 1700000000,
				"introattachments": []any{map[string]any{"filename": "rubric.pdf"}}}}}}}
		case "mod_assign_get_submission_status":
			out = map[string]any{
				"lastattempt": map[string]any{"gradingstatus": "graded", "submission": map[string]any{"status": "submitted", "timemodified": 1699990000,
					"plugins": []any{map[string]any{"type": "file", "fileareas": []any{map[string]any{"area": "submission_files", "files": []any{map[string]any{"filename": "essay.pdf"}}}}}}}},
				"feedback": map[string]any{"gradefordisplay": "85,00 / 100,00", "gradeddate": 1700100000,
					"plugins": []any{map[string]any{"editorfields": []any{map[string]any{"text": "<p>Good work</p>"}}}}},
			}
		case "core_course_get_updates_since":
			out = map[string]any{"instances": []any{}}
			if q.Get("courseid") == "4" {
				out = map[string]any{"instances": []any{map[string]any{"contextlevel": "module", "id": 2, "updates": []any{map[string]any{"name": "contentfiles", "timeupdated": 1700000000}}}}}
			}
		case "message_popup_get_popup_notifications":
			out = map[string]any{"unreadcount": 1, "notifications": []any{
				map[string]any{"id": 1, "subject": "New grade", "smallmessage": "Essay graded", "timecreated": 1700100000, "read": false, "contexturl": site + "/mod/assign/view.php?id=5"},
				map[string]any{"id": 2, "subject": "Old post", "smallmessage": "Hello", "timecreated": 1700000000, "read": true},
			}}
		case "gradereport_overview_get_course_grades":
			out = map[string]any{"grades": []any{map[string]any{"courseid": 1, "grade": "1,7"}}}
		case "core_calendar_get_action_events_by_timesort":
			yesterday := time.Now().Add(-24 * time.Hour).Unix()
			out = map[string]any{"events": []any{map[string]any{"id": 9, "name": "Essay is due", "modulename": "assign", "timesort": yesterday,
				"url": site + "/mod/assign/view.php?id=5", "course": map[string]any{"id": 4, "fullname": "Course 04"}}}}
		case "core_course_get_courses_by_field":
			out = map[string]any{"courses": []any{}}
		case "core_calendar_get_action_events_by_course", "core_calendar_get_calendar_upcoming_view":
			out = map[string]any{"events": []any{}}
		case "mod_forum_get_forum_discussions":
			out = map[string]any{"discussions": []any{}}
			if q.Get("forumid") == "9" {
				out = map[string]any{"discussions": []any{
					map[string]any{"id": 1, "discussion": 100, "name": "Old &amp; Gone", "userfullname": "Prof", "created": 1700000000, "timemodified": 1700000000, "numreplies": 2, "locked": true},
					map[string]any{"id": 2, "discussion": 200, "name": "New", "userfullname": "Prof", "created": 1700090000, "timemodified": 1700090000},
				}}
			}
		case "mod_forum_get_discussion_posts":
			if id := q.Get("discussionid"); id != "100" && id != "200" {
				out = map[string]any{"exception": "dml_missing_record_exception", "errorcode": "invalidrecord", "message": "Can't find data record"}
				break
			}
			out = map[string]any{"posts": []any{ // Moodle returns newest first
				map[string]any{"id": 3, "subject": "Re: Old", "message": "<p>third</p>", "timecreated": 1700000300, "author": map[string]any{"fullname": "C"}},
				map[string]any{"id": 2, "parentid": 1, "subject": "Re: Old", "message": "second", "timecreated": 1700000200,
					"author": map[string]any{"fullname": "B", "urls": map[string]any{"profileimage": site + "/pluginfile.php/9/user/icon/f1"}}},
				map[string]any{"id": 1, "subject": "Old", "message": "first", "timecreated": 1700000100, "author": map[string]any{"fullname": "Prof"},
					"attachments": []any{map[string]any{"filename": "slides.pdf"}}},
			}}
		case "gradereport_user_get_grade_items":
			out = map[string]any{"exception": "required_capability_exception", "errorcode": "nopermissions", "message": "No permission"}
		case "core_enrol_get_enrolled_users":
			if q.Get("options[0][value]") != "id,fullname,roles" {
				t.Errorf("enrolled users requested without userfields restriction")
			}
			out = []any{
				map[string]any{"id": 1, "fullname": "Zoe Student", "roles": []any{map[string]any{"shortname": "student"}}},
				map[string]any{"id": 2, "fullname": "Prof Zed", "roles": []any{map[string]any{"shortname": "editingteacher"}}},
				map[string]any{"id": 3, "fullname": "Amy Student", "roles": []any{map[string]any{"shortname": "student"}}},
			}
		case "core_message_get_conversation":
			if q.Get("conversationid") != "5" {
				out = map[string]any{"exception": "moodle_exception", "errorcode": "invalidconversation", "message": "Conversation does not exist"}
				break
			}
			out = map[string]any{"id": 5, "type": 1, "members": []any{map[string]any{"id": 8, "fullname": "Alice"}}}
		case "core_message_get_conversation_messages":
			from, _ := strconv.Atoi(q.Get("limitfrom"))
			num, _ := strconv.Atoi(q.Get("limitnum"))
			var msgs []any
			for i := from; i < 30 && (num == 0 || i < from+num); i++ {
				msgs = append(msgs, map[string]any{"id": i, "useridfrom": 8, "text": fmt.Sprintf("msg %d", i), "timecreated": 1700000000 + i})
			}
			out = map[string]any{"id": 5, "messages": msgs}
		default:
			t.Errorf("unexpected wsfunction %q", q.Get("wsfunction"))
			out = map[string]any{"exception": "x", "errorcode": "x", "message": "unexpected"}
		}
		json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(ts.Close)
	c, err := moodle.New(config.Config{BaseURL: ts.URL, Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	return &app.App{Client: c}
}

type callResult struct {
	isError    bool
	text       string
	structured map[string]any
}

// call invokes a tool and checks that the text block and structuredContent
// carry the same data whenever the call succeeds.
func call(t *testing.T, cs *mcpserver.ClientSession, name string, args map[string]any) callResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcpserver.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error %v", name, err)
	}
	out := callResult{isError: res.IsError, text: res.Content[0].(*mcpserver.TextContent).Text}
	if res.IsError {
		return out
	}
	b, _ := json.Marshal(res.StructuredContent)
	json.Unmarshal(b, &out.structured)
	var fromText map[string]any
	if err := json.Unmarshal([]byte(out.text), &fromText); err != nil || !reflect.DeepEqual(fromText, out.structured) {
		t.Errorf("%s: text and structuredContent differ (%v)", name, err)
	}
	return out
}

func TestPagingCoversEveryCourseOnce(t *testing.T) {
	cs := fakeMoodle(t)
	seen := map[float64]int{}
	for offset := 0; ; offset += 10 {
		r := call(t, cs, "list_courses", map[string]any{"offset": offset, "limit": 10})
		if r.isError {
			t.Fatal(r.text)
		}
		for _, c := range r.structured["courses"].([]any) {
			seen[c.(map[string]any)["id"].(float64)]++
		}
		page := r.structured["page"].(map[string]any)
		if page["total"].(float64) != 25 {
			t.Fatalf("total = %v", page["total"])
		}
		if !page["hasMore"].(bool) {
			break
		}
	}
	for id := 1; id <= 25; id++ {
		if seen[float64(id)] != 1 {
			t.Errorf("course %d seen %d times", id, seen[float64(id)])
		}
	}
	if r := call(t, cs, "list_courses", map[string]any{"offset": 100}); r.isError || len(r.structured["courses"].([]any)) != 0 {
		t.Errorf("past-the-end page: %+v", r)
	}
}

func TestConversationPages(t *testing.T) {
	cs := fakeMoodle(t)
	var got []string
	for offset := 0; ; offset += 20 {
		r := call(t, cs, "get_conversation", map[string]any{"conversation_id": 5, "offset": offset})
		if r.isError {
			t.Fatal(r.text)
		}
		for _, m := range r.structured["messages"].([]any) {
			got = append(got, m.(map[string]any)["text"].(string))
		}
		if !r.structured["hasMore"].(bool) {
			break
		}
	}
	if len(got) != 30 || got[0] != "msg 0" || got[29] != "msg 29" {
		t.Errorf("messages = %d, first/last %q/%q", len(got), got[0], got[len(got)-1])
	}
}

func TestEmptyResult(t *testing.T) {
	r := call(t, fakeMoodle(t), "search_forums", map[string]any{"course": "c02"})
	if r.isError || r.structured["page"].(map[string]any)["total"].(float64) != 0 || len(r.structured["hits"].([]any)) != 0 {
		t.Errorf("empty forum: %+v", r)
	}
}

func TestErrorsAreCategorized(t *testing.T) {
	cs := fakeMoodle(t)
	for _, tc := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"inspect_course", map[string]any{"course": "Physics"}, "course not found"},
		{"inspect_course", map[string]any{"course": "Course 1"}, "ambiguous course"},
		{"get_grades", map[string]any{"course": "c03"}, "Moodle error"},
		{"get_conversation", map[string]any{"conversation_id": 99}, "conversation not found"},
		{"list_courses", map[string]any{"limit": 500}, "limit"},
		{"list_courses", map[string]any{"offset": -1}, "offset"},
		{"download_course", map[string]any{"course": "c01", "mode": "bogus"}, "mode"},
		{"list_messages", map[string]any{"export": true}, "export"},
		{"get_discussion", map[string]any{"discussion_id": 999}, "discussion not found"},
	} {
		r := call(t, cs, tc.tool, tc.args)
		if !r.isError || !strings.Contains(r.text, tc.want) {
			t.Errorf("%s %v: isError=%v text=%q, want %q", tc.tool, tc.args, r.isError, r.text, tc.want)
		}
	}
}

func TestForumDiscussionsAndPosts(t *testing.T) {
	cs := fakeMoodle(t)
	r := call(t, cs, "search_forums", map[string]any{"course": "c04"})
	if r.isError {
		t.Fatal(r.text)
	}
	hits := r.structured["hits"].([]any)
	first := hits[0].(map[string]any)
	if len(hits) != 2 || first["discussion"] != "New" || first["author"] != "Prof" || first["created"] == "" {
		t.Fatalf("hits = %v", hits)
	}
	if got := hits[1].(map[string]any)["discussion"]; got != "Old & Gone" {
		t.Errorf("discussion name = %q, want HTML entities decoded", got)
	}
	res, err := fakeApp(t).SearchForums("c04", "")
	if err != nil || len(res.Hits) != 2 || res.Hits[1].Snippet != "first" {
		t.Errorf("snippet = %+v (%v), want the opening post", res.Hits, err)
	}
	var texts []string
	for offset := 0; ; offset += 2 {
		r := call(t, cs, "get_discussion", map[string]any{"discussion_id": 100, "offset": offset, "limit": 2})
		if r.isError {
			t.Fatal(r.text)
		}
		for _, p := range r.structured["posts"].([]any) {
			texts = append(texts, p.(map[string]any)["text"].(string))
		}
		if !r.structured["hasMore"].(bool) {
			break
		}
	}
	if strings.Join(texts, ",") != "first,second,third" {
		t.Errorf("posts = %v, want oldest first without HTML", texts)
	}
}

// Discussion "Old" is locked in the fixture, "New" is not.
func TestDownloadSkipLockedLeavesOutLockedDiscussions(t *testing.T) {
	cs := fakeMoodle(t)
	exported := func(skipLocked bool) (old, fresh bool) {
		r := call(t, cs, "download_course", map[string]any{"course": "c04", "mode": "forums-only", "out": t.TempDir(), "skip_locked": skipLocked})
		if r.isError {
			t.Fatal(r.text)
		}
		forumDir := filepath.Join(r.structured["courseDir"].(string), "General", "News")
		_, errOld := os.Stat(filepath.Join(forumDir, "Old & Gone"))
		_, errNew := os.Stat(filepath.Join(forumDir, "New", "New.md"))
		return errOld == nil, errNew == nil
	}
	if old, fresh := exported(false); !old || !fresh {
		t.Errorf("without skip_locked: Old=%v New=%v, want both exported", old, fresh)
	}
	if old, fresh := exported(true); old || !fresh {
		t.Errorf("with skip_locked: Old=%v New=%v, want only New", old, fresh)
	}
}

func TestDownloadListsFilesWithStableIDsAndSkipsTooLarge(t *testing.T) {
	cs := fakeMoodle(t)
	r := call(t, cs, "download_course", map[string]any{"course": "c04", "mode": "resources-only", "out": t.TempDir(), "max_size_mb": 1})
	if r.isError {
		t.Fatal(r.text)
	}
	files := r.structured["files"].([]any)
	if len(files) != 5 {
		t.Fatalf("files = %v", files)
	}
	ids := map[string]bool{}
	for _, f := range files {
		ids[f.(map[string]any)["id"].(string)] = true
	}
	if !ids["2:notes.txt"] || !ids["4:a.txt"] || !ids["4:b.txt"] || !ids["3:Week 1.md"] || !ids["5:intro.md"] {
		t.Errorf("ids = %v", ids)
	}
	if _, err := os.Stat(filepath.Join(r.structured["courseDir"].(string), "General", "Slides", "a.txt")); err != nil {
		t.Errorf("folder file missing: %v", err)
	}
	page, err := os.ReadFile(filepath.Join(r.structured["courseDir"].(string), "General", "Week 1.md"))
	if err != nil || string(page) != "# Week 1\n\n<p>Read chapter 1</p>\n" {
		t.Errorf("resources-only must export pages as notes: %q %v", page, err)
	}
}

func TestDiscussionThreadInlinesHTMLRepliesAndAvatars(t *testing.T) {
	page, err := fakeApp(t).DiscussionThread(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 3 {
		t.Fatalf("posts = %+v", page.Posts)
	}
	reply := page.Posts[1]
	if reply.ParentID != 1 || reply.Created == "" || !strings.HasPrefix(reply.AuthorImage, "data:text/plain") {
		t.Errorf("reply = %+v", reply)
	}
	if page.Posts[2].HTML != "<p>third</p>" || page.Posts[2].Text != "third" {
		t.Errorf("html/text = %q / %q", page.Posts[2].HTML, page.Posts[2].Text)
	}
	if plain, _ := fakeApp(t).DiscussionPosts(100, 0, 10); plain.Posts[1].HTML != "" || plain.Posts[1].AuthorImage != "" {
		t.Errorf("DiscussionPosts must stay lean for agents: %+v", plain.Posts[1])
	}
}

func TestParticipantsTeachersFirst(t *testing.T) {
	cs := fakeMoodle(t)
	r := call(t, cs, "list_participants", map[string]any{"course": "c04"})
	if r.isError {
		t.Fatal(r.text)
	}
	var names []string
	for _, p := range r.structured["participants"].([]any) {
		names = append(names, p.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "Prof Zed,Amy Student,Zoe Student" {
		t.Errorf("order = %v", names)
	}
	r = call(t, cs, "list_participants", map[string]any{"course": "c04", "query": "zed"})
	if r.isError || r.structured["page"].(map[string]any)["total"].(float64) != 1 {
		t.Errorf("query filter: %+v", r)
	}
	r = call(t, cs, "list_participants", map[string]any{"course": "c04", "role": "student"})
	if r.isError || r.structured["page"].(map[string]any)["total"].(float64) != 2 {
		t.Errorf("role filter: %+v", r)
	}
}

func TestResultsCarryWebLinks(t *testing.T) {
	cs := fakeMoodle(t)
	doc := call(t, cs, "doctor", nil)
	site, _ := doc.structured["siteUrl"].(string)
	if !strings.HasPrefix(site, "http://127.0.0.1") {
		t.Fatalf("doctor siteUrl = %q", site)
	}
	course := call(t, cs, "list_courses", map[string]any{"limit": 1}).structured["courses"].([]any)[0].(map[string]any)
	if course["url"] != site+"/course/view.php?id=1" {
		t.Errorf("course url = %v", course["url"])
	}
	hit := call(t, cs, "search_forums", map[string]any{"course": "c04"}).structured["hits"].([]any)[0].(map[string]any)
	if hit["url"] != site+"/mod/forum/discuss.php?d=200" {
		t.Errorf("discussion url = %v", hit["url"])
	}
}

// notesText is long enough to need two read_module windows of 500 characters.
var notesText = strings.Repeat("0123456789", 70)

func TestReadModule(t *testing.T) {
	cs := fakeMoodle(t)
	var text string
	for start := 0; ; {
		r := call(t, cs, "read_module", map[string]any{"course": 4, "module_id": 2, "start": start, "max_chars": 500})
		if r.isError {
			t.Fatal(r.text)
		}
		text += r.structured["text"].(string)
		if !r.structured["hasMore"].(bool) {
			break
		}
		start = int(r.structured["nextStart"].(float64))
	}
	if text != notesText {
		t.Errorf("paged text has %d chars, want %d", len(text), len(notesText))
	}
	if r := call(t, cs, "read_module", map[string]any{"course": "c04", "module_id": 3}); r.isError || r.structured["text"] != "Read chapter 1" {
		t.Errorf("page: %+v", r)
	}
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"course": "c04", "module_id": 4}, "pass file as one of: a.txt, b.txt"},
		{map[string]any{"course": "c04", "module_id": 5}, "use get_assignment"},
		{map[string]any{"course": "c04", "module_id": 99}, "module not found"},
	} {
		if r := call(t, cs, "read_module", tc.args); !r.isError || !strings.Contains(r.text, tc.want) {
			t.Errorf("%v: %q, want %q", tc.args, r.text, tc.want)
		}
	}
	if r := call(t, cs, "read_module", map[string]any{"course": "c04", "module_id": 4, "file": "B.TXT"}); r.isError || r.structured["file"] != "b.txt" {
		t.Errorf("folder file pick: %+v", r)
	}
}

func TestGetAssignment(t *testing.T) {
	cs := fakeMoodle(t)
	r := call(t, cs, "get_assignment", map[string]any{"module_id": 5})
	if r.isError {
		t.Fatal(r.text)
	}
	got := r.structured
	if got["intro"] != "Write 500 words" || got["submissionStatus"] != "submitted" || got["grade"] != "85,00 / 100,00" ||
		got["feedback"] != "Good work" || got["submittedFiles"].([]any)[0] != "essay.pdf" || got["attachments"].([]any)[0] != "rubric.pdf" {
		t.Errorf("assignment = %v", got)
	}
	if r := call(t, cs, "get_assignment", map[string]any{"module_id": 77}); !r.isError || !strings.Contains(r.text, "module not found") {
		t.Errorf("unknown module: %+v", r)
	}
}

func TestCurrentCourseScopes(t *testing.T) {
	cs := fakeMoodle(t)
	if r := call(t, cs, "list_courses", map[string]any{"filter": "inprogress"}); r.isError || r.structured["page"].(map[string]any)["total"].(float64) != 1 {
		t.Errorf("inprogress filter: %+v", r)
	}
	r := call(t, cs, "course_updates", nil)
	if r.isError {
		t.Fatal(r.text)
	}
	ups := r.structured["updates"].([]any)
	if len(ups) != 1 || ups[0].(map[string]any)["module"] != "Notes" || ups[0].(map[string]any)["changes"].([]any)[0] != "contentfiles" {
		t.Errorf("updates = %v", ups)
	}
	r = call(t, cs, "search_forums", nil)
	if r.isError || len(r.structured["hits"].([]any)) != 2 || r.structured["hits"].([]any)[0].(map[string]any)["course"] != "Course 04" {
		t.Errorf("all-course forum search: %+v", r)
	}
	if r := call(t, cs, "inspect_course", map[string]any{"course": 4}); r.isError {
		t.Errorf("numeric course id: %s", r.text)
	}
}

func TestNotificationsAndGradesOverview(t *testing.T) {
	cs := fakeMoodle(t)
	r := call(t, cs, "list_notifications", map[string]any{"limit": 1})
	if r.isError || !r.structured["hasMore"].(bool) || r.structured["unreadCount"].(float64) != 1 || len(r.structured["notifications"].([]any)) != 1 {
		t.Errorf("notifications page: %+v", r)
	}
	r = call(t, cs, "list_notifications", map[string]any{"unread_only": true})
	if r.isError || len(r.structured["notifications"].([]any)) != 1 {
		t.Errorf("unread only: %+v", r)
	}
	r = call(t, cs, "grades_overview", nil)
	if r.isError || r.structured["courses"].([]any)[0].(map[string]any)["course"] != "Course 01" {
		t.Errorf("overview: %+v", r)
	}
}

func TestDuePastDays(t *testing.T) {
	cs := fakeMoodle(t)
	if r := call(t, cs, "list_due", nil); r.isError || len(r.structured["events"].([]any)) != 0 {
		t.Errorf("default should hide past items: %+v", r)
	}
	r := call(t, cs, "list_due", map[string]any{"past_days": 7})
	if r.isError {
		t.Fatal(r.text)
	}
	ev := r.structured["events"].([]any)[0].(map[string]any)
	if ev["overdue"] != true || ev["moduleId"].(float64) != 5 {
		t.Errorf("past event = %v", ev)
	}
}
