package moodle

import (
	"fmt"
	"net/url"
)

func courseIDsParam(ids ...int) url.Values {
	v := url.Values{}
	for i, id := range ids {
		v.Set(fmt.Sprintf("courseids[%d]", i), fmt.Sprintf("%d", id))
	}
	return v
}

type CourseMeta struct {
	ID        int    `json:"id"`
	FullName  string `json:"fullname"`
	ShortName string `json:"shortname"`
	Summary   string `json:"summary"`
	StartDate int64  `json:"startdate"`
	EndDate   int64  `json:"enddate"`
	Format    string `json:"format"`
	Category  int    `json:"categoryid"`
}

type coursesByFieldResp struct {
	Courses []CourseMeta `json:"courses"`
}

func (c *Client) CourseMeta(courseID int) (CourseMeta, error) {
	params := url.Values{
		"field": {"id"},
		"value": {fmt.Sprintf("%d", courseID)},
	}
	var resp coursesByFieldResp
	if err := c.Call("core_course_get_courses_by_field", params, &resp); err != nil {
		return CourseMeta{}, err
	}
	if len(resp.Courses) == 0 {
		return CourseMeta{}, fmt.Errorf("course %d not found", courseID)
	}
	return resp.Courses[0], nil
}

type CalendarEvent struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	EventType string `json:"eventtype"`
	TimeStart int64  `json:"timestart"`
	TimeSort  int64  `json:"timesort"`
	Course    struct {
		ID       int    `json:"id"`
		FullName string `json:"fullname"`
	} `json:"course"`
	Description string `json:"description"`
	URL         string `json:"url"`
	ModuleName  string `json:"modulename"`
	Overdue     bool   `json:"overdue"`
	Action      *struct {
		URL string `json:"url"`
	} `json:"action"`
}

type calendarResp struct {
	Events []CalendarEvent `json:"events"`
}

func (c *Client) UpcomingEvents(courseID int) ([]CalendarEvent, error) {
	params := url.Values{}
	if courseID > 0 {
		params.Set("courseid", fmt.Sprintf("%d", courseID))
	}
	var resp calendarResp
	if err := c.Call("core_calendar_get_calendar_upcoming_view", params, &resp); err != nil {
		return nil, err
	}
	return resp.Events, nil
}

// ActionEventsSince returns still-actionable events (e.g. unsubmitted
// assignments) with a sort time between from and to, across all courses.
func (c *Client) ActionEventsSince(from, to int64) ([]CalendarEvent, error) {
	params := url.Values{
		"timesortfrom": {fmt.Sprintf("%d", from)},
		"timesortto":   {fmt.Sprintf("%d", to)},
	}
	var resp calendarResp
	if err := c.Call("core_calendar_get_action_events_by_timesort", params, &resp); err != nil {
		return nil, err
	}
	return resp.Events, nil
}

func (c *Client) ActionEvents(courseID int) ([]CalendarEvent, error) {
	params := url.Values{}
	if courseID > 0 {
		params.Set("courseid", fmt.Sprintf("%d", courseID))
	}
	var resp calendarResp
	if err := c.Call("core_calendar_get_action_events_by_course", params, &resp); err != nil {
		return nil, err
	}
	return resp.Events, nil
}

type GradeItem struct {
	ItemName            string   `json:"itemname"`
	GradeRaw            *float64 `json:"graderaw"`
	GradeFormatted      string   `json:"gradeformatted"`
	GradeMax            float64  `json:"grademax"`
	PercentageFormatted string   `json:"percentageformatted"`
	Feedback            string   `json:"feedback"`
}

type userGrade struct {
	CourseID   int         `json:"courseid"`
	GradeItems []GradeItem `json:"gradeitems"`
}

type gradesResp struct {
	UserGrades []userGrade `json:"usergrades"`
}

func (c *Client) Grades(courseID int) ([]GradeItem, error) {
	params := url.Values{
		"courseid": {fmt.Sprintf("%d", courseID)},
		"userid":   {fmt.Sprintf("%d", c.UserID)},
	}
	var resp gradesResp
	if err := c.Call("gradereport_user_get_grade_items", params, &resp); err != nil {
		return nil, err
	}
	if len(resp.UserGrades) == 0 {
		return nil, nil
	}
	return resp.UserGrades[0].GradeItems, nil
}

type UpdateInstance struct {
	ContextLevel string `json:"contextlevel"`
	ID           int    `json:"id"`
	Updates      []struct {
		Name        string `json:"name"`
		TimeUpdated int64  `json:"timeupdated"`
	} `json:"updates"`
}

type updatesResp struct {
	Instances []UpdateInstance `json:"instances"`
}

func (c *Client) UpdatesSince(courseID int, since int64) ([]UpdateInstance, error) {
	params := url.Values{
		"courseid": {fmt.Sprintf("%d", courseID)},
		"since":    {fmt.Sprintf("%d", since)},
	}
	var resp updatesResp
	if err := c.Call("core_course_get_updates_since", params, &resp); err != nil {
		return nil, err
	}
	return resp.Instances, nil
}

type Label struct {
	ID     int    `json:"id"`
	Course int    `json:"course"`
	Name   string `json:"name"`
	Intro  string `json:"intro"`
	CMID   int    `json:"coursemodule"`
}

type labelsResp struct {
	Labels []Label `json:"labels"`
}

func (c *Client) Labels(courseID int) ([]Label, error) {
	var resp labelsResp
	if err := c.Call("mod_label_get_labels_by_courses", courseIDsParam(courseID), &resp); err != nil {
		return nil, err
	}
	return resp.Labels, nil
}

type Page struct {
	ID      int    `json:"id"`
	Course  int    `json:"course"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Intro   string `json:"intro"`
	CMID    int    `json:"coursemodule"`
}

type pagesResp struct {
	Pages []Page `json:"pages"`
}

func (c *Client) Pages(courseID int) ([]Page, error) {
	var resp pagesResp
	if err := c.Call("mod_page_get_pages_by_courses", courseIDsParam(courseID), &resp); err != nil {
		return nil, err
	}
	return resp.Pages, nil
}

type AssignFile struct {
	Filename string `json:"filename"`
	Filepath string `json:"filepath"`
	Filesize int64  `json:"filesize"`
	FileURL  string `json:"fileurl"`
	MIMEType string `json:"mimetype"`
}

type Assignment struct {
	ID                       int          `json:"id"`
	CMID                     int          `json:"cmid"`
	Course                   int          `json:"course"`
	Name                     string       `json:"name"`
	Intro                    string       `json:"intro"`
	DueDate                  int64        `json:"duedate"`
	AllowSubmissionsFromDate int64        `json:"allowsubmissionsfromdate"`
	CutoffDate               int64        `json:"cutoffdate"`
	IntroAttachments         []AssignFile `json:"introattachments"`
	IntroFiles               []AssignFile `json:"introfiles"`
}

type assignCourse struct {
	ID          int          `json:"id"`
	Assignments []Assignment `json:"assignments"`
}

type assignsResp struct {
	Courses []assignCourse `json:"courses"`
}

// AllAssignments returns assignments from every course the user can see.
func (c *Client) AllAssignments() ([]Assignment, error) {
	var resp assignsResp
	if err := c.Call("mod_assign_get_assignments", nil, &resp); err != nil {
		return nil, err
	}
	var all []Assignment
	for _, co := range resp.Courses {
		all = append(all, co.Assignments...)
	}
	return all, nil
}

func (c *Client) Assignments(courseID int) ([]Assignment, error) {
	var resp assignsResp
	if err := c.Call("mod_assign_get_assignments", courseIDsParam(courseID), &resp); err != nil {
		return nil, err
	}
	for _, co := range resp.Courses {
		if co.ID == courseID {
			return co.Assignments, nil
		}
	}
	if len(resp.Courses) == 1 {
		return resp.Courses[0].Assignments, nil
	}
	return nil, nil
}

type EditorField struct {
	Text string `json:"text"`
}

type SubmissionStatus struct {
	LastAttempt *struct {
		Submission *struct {
			ID           int    `json:"id"`
			Status       string `json:"status"`
			TimeModified int64  `json:"timemodified"`
			Plugins      []struct {
				Type      string `json:"type"`
				FileAreas []struct {
					Area  string       `json:"area"`
					Files []AssignFile `json:"files"`
				} `json:"fileareas"`
			} `json:"plugins"`
		} `json:"submission"`
		GradingStatus string `json:"gradingstatus"`
	} `json:"lastattempt"`
	Feedback *struct {
		GradeForDisplay string `json:"gradefordisplay"`
		GradedDate      int64  `json:"gradeddate"`
		Plugins         []struct {
			EditorFields []EditorField `json:"editorfields"`
		} `json:"plugins"`
	} `json:"feedback"`
}

func (c *Client) SubmissionStatus(assignID int) (SubmissionStatus, error) {
	params := url.Values{"assignid": {fmt.Sprintf("%d", assignID)}}
	var resp SubmissionStatus
	if err := c.Call("mod_assign_get_submission_status", params, &resp); err != nil {
		return resp, err
	}
	return resp, nil
}

type Role struct {
	ShortName string `json:"shortname"`
}

type EnrolledUser struct {
	ID       int    `json:"id"`
	FullName string `json:"fullname"`
	Roles    []Role `json:"roles"`
}

// EnrolledUsers lists course participants with only id, name, and roles
// (userfields keeps emails and other profile data out of the response).
func (c *Client) EnrolledUsers(courseID int) ([]EnrolledUser, error) {
	params := url.Values{
		"courseid":          {fmt.Sprintf("%d", courseID)},
		"options[0][name]":  {"userfields"},
		"options[0][value]": {"id,fullname,roles"},
	}
	var users []EnrolledUser
	if err := c.Call("core_enrol_get_enrolled_users", params, &users); err != nil {
		return nil, err
	}
	return users, nil
}

// CoursesByTimeline lists enrolled courses by Moodle's dashboard
// classification: "inprogress", "future", or "past".
func (c *Client) CoursesByTimeline(classification string) ([]Course, error) {
	params := url.Values{"classification": {classification}}
	var resp struct {
		Courses []Course `json:"courses"`
	}
	if err := c.Call("core_course_get_enrolled_courses_by_timeline_classification", params, &resp); err != nil {
		return nil, err
	}
	for i := range resp.Courses {
		resp.Courses[i].URL = c.CourseURL(resp.Courses[i].ID)
	}
	return resp.Courses, nil
}

type CourseGrade struct {
	CourseID int    `json:"courseid"`
	Grade    string `json:"grade"`
}

func (c *Client) OverviewGrades() ([]CourseGrade, error) {
	params := url.Values{"userid": {fmt.Sprintf("%d", c.UserID)}}
	var resp struct {
		Grades []CourseGrade `json:"grades"`
	}
	if err := c.Call("gradereport_overview_get_course_grades", params, &resp); err != nil {
		return nil, err
	}
	return resp.Grades, nil
}

type Notification struct {
	ID             int    `json:"id"`
	Subject        string `json:"subject"`
	SmallMessage   string `json:"smallmessage"`
	TimeCreated    int64  `json:"timecreated"`
	Read           bool   `json:"read"`
	ContextURL     string `json:"contexturl"`
	ContextURLName string `json:"contexturlname"`
}

type NotificationsPage struct {
	Notifications []Notification `json:"notifications"`
	UnreadCount   int            `json:"unreadcount"`
}

// Notifications returns popup notifications, newest first.
func (c *Client) Notifications(offset, limit int) (NotificationsPage, error) {
	params := url.Values{
		"useridto":    {fmt.Sprintf("%d", c.UserID)},
		"newestfirst": {"1"},
		"offset":      {fmt.Sprintf("%d", offset)},
		"limit":       {fmt.Sprintf("%d", limit)},
	}
	var resp NotificationsPage
	err := c.Call("message_popup_get_popup_notifications", params, &resp)
	return resp, err
}
