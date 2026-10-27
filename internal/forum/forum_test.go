package forum

import (
	"encoding/json"
	"testing"
)

// Moodle marks forum discussions as locked ("Gesperrt" in the UI); the JSON key must map onto Locked.
func TestDiscussionDecodesLocked(t *testing.T) {
	var resp DiscussionsResponse
	if err := json.Unmarshal([]byte(`{"discussions":[{"discussion":1,"locked":true},{"discussion":2,"locked":false}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Discussions[0].Locked || resp.Discussions[1].Locked {
		t.Fatalf("locked not decoded: %+v", resp.Discussions)
	}
}

func TestDiscussionDecodesExtraFields(t *testing.T) {
	var resp DiscussionsResponse
	in := `{"discussions":[{"discussion":1,"userid":7,"timestart":100,"timeend":200,"starred":true,"numunread":3,"attachment":true,"canreply":true,"canlock":false}]}`
	if err := json.Unmarshal([]byte(in), &resp); err != nil {
		t.Fatal(err)
	}
	d := resp.Discussions[0]
	if d.UserID != 7 || d.TimeStart != 100 || d.TimeEnd != 200 || !d.Starred || d.NumUnread != 3 || !d.Attachment || !d.CanReply || d.CanLock {
		t.Fatalf("extra fields not decoded: %+v", d)
	}
}
