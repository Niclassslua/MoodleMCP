package message

import "testing"

func TestTitle(t *testing.T) {
	cases := []struct {
		name   string
		c      Conversation
		selfID int
		want   string
	}{
		{"named", Conversation{Name: "Team A"}, 1, "Team A"},
		{"dm", Conversation{Members: []Member{{ID: 1, FullName: "Me"}, {ID: 2, FullName: "Alice"}}}, 1, "Alice"},
		{"self", Conversation{Type: TypeSelf}, 1, "Self"},
		{"fallback", Conversation{ID: 99}, 1, "conversation-99"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Title(tc.c, tc.selfID); got != tc.want {
				t.Fatalf("Title() = %q, want %q", got, tc.want)
			}
		})
	}
}
