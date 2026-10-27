package message

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Niclassslua/MoodleMCP/internal/forum"
	"github.com/Niclassslua/MoodleMCP/internal/fsname"
	"github.com/Niclassslua/MoodleMCP/internal/moodle"
)

// Conversation types from core_message.
const (
	TypeIndividual = 1
	TypeGroup      = 2
	TypeSelf       = 3
)

type ConversationsResponse struct {
	Conversations []Conversation `json:"conversations"`
}

type Conversation struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Subname     string    `json:"subname"`
	Type        int       `json:"type"`
	MemberCount int       `json:"membercount"`
	IsRead      bool      `json:"isread"`
	UnreadCount int       `json:"unreadcount"`
	Members     []Member  `json:"members"`
	Messages    []Message `json:"messages"` // preview from list call
}

type Member struct {
	ID       int    `json:"id"`
	FullName string `json:"fullname"`
}

type Message struct {
	ID          int    `json:"id"`
	UserIDFrom  int    `json:"useridfrom"`
	Text        string `json:"text"`
	TimeCreated int64  `json:"timecreated"`
}

type ConversationMessagesResponse struct {
	ID       int       `json:"id"`
	Members  []Member  `json:"members"`
	Messages []Message `json:"messages"`
}

// Title picks a human folder/thread name.
func Title(c Conversation, selfID int) string {
	if strings.TrimSpace(c.Name) != "" {
		return c.Name
	}
	var others []string
	for _, m := range c.Members {
		if m.ID != selfID && m.FullName != "" {
			others = append(others, m.FullName)
		}
	}
	if len(others) > 0 {
		return strings.Join(others, ", ")
	}
	if c.Type == TypeSelf {
		return "Self"
	}
	return fmt.Sprintf("conversation-%d", c.ID)
}

// ListConversations pages through all conversations for the authenticated user.
func ListConversations(c *moodle.Client) ([]Conversation, error) {
	const page = 50
	var all []Conversation
	for offset := 0; ; offset += page {
		params := url.Values{
			"userid":    {fmt.Sprintf("%d", c.UserID)},
			"limitfrom": {fmt.Sprintf("%d", offset)},
			"limitnum":  {fmt.Sprintf("%d", page)},
			"mergeself": {"1"},
		}
		var resp ConversationsResponse
		if err := c.Call("core_message_get_conversations", params, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Conversations...)
		if len(resp.Conversations) < page {
			break
		}
	}
	return all, nil
}

// GetConversation fetches one conversation's metadata and members.
func GetConversation(c *moodle.Client, convID int) (Conversation, error) {
	params := url.Values{
		"userid":                 {fmt.Sprintf("%d", c.UserID)},
		"conversationid":         {fmt.Sprintf("%d", convID)},
		"includecontactrequests": {"0"},
		"includeprivacyinfo":     {"0"},
		"messagelimit":           {"1"},
	}
	var conv Conversation
	err := c.Call("core_message_get_conversation", params, &conv)
	return conv, err
}

// ListMessages returns all messages in a conversation (oldest first).
// Moodle's conversation list only embeds the latest message; this always
// calls core_message_get_conversation_messages (limitnum=0 = no limit).
func ListMessages(c *moodle.Client, convID int) (msgs []Message, members []Member, err error) {
	return ListMessagesPage(c, convID, 0, 0)
}

// ListMessagesPage returns a stable oldest-first slice of a conversation.
func ListMessagesPage(c *moodle.Client, convID, offset, limit int) (msgs []Message, members []Member, err error) {
	if offset < 0 || limit < 0 {
		return nil, nil, fmt.Errorf("offset and limit must not be negative")
	}
	params := url.Values{
		"currentuserid": {fmt.Sprintf("%d", c.UserID)},
		"convid":        {fmt.Sprintf("%d", convID)},
		"limitfrom":     {fmt.Sprintf("%d", offset)},
		"limitnum":      {fmt.Sprintf("%d", limit)},
		"newest":        {"0"},
	}
	var resp ConversationMessagesResponse
	if err := c.Call("core_message_get_conversation_messages", params, &resp); err != nil {
		return nil, nil, err
	}
	return resp.Messages, resp.Members, nil
}

// Export writes conversations as markdown under dir/Nachrichten/.
// If query is non-empty, only matching conversations are written.
func Export(c *moodle.Client, dir, query string, mode fsname.Mode) (written int, err error) {
	convs, err := ListConversations(c)
	if err != nil {
		return 0, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	root := filepath.Join(dir, "Nachrichten")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return 0, err
	}

	for _, conv := range convs {
		title := Title(conv, c.UserID)
		if q != "" && !match(conv, title, q) {
			continue
		}
		msgs, members, err := ListMessages(c, conv.ID)
		if err != nil {
			return written, fmt.Errorf("conversation %d: %w", conv.ID, err)
		}
		names := nameMap(conv.Members, members)

		threadDir := filepath.Join(root, fsname.Sanitize(title, mode))
		if err := os.MkdirAll(threadDir, 0o755); err != nil {
			return written, err
		}

		var b strings.Builder
		b.WriteString("# " + title + "\n\n")
		for _, m := range msgs {
			who := names[m.UserIDFrom]
			if who == "" {
				who = fmt.Sprintf("user-%d", m.UserIDFrom)
			}
			when := time.Unix(m.TimeCreated, 0).Local().Format("2006-01-02 15:04")
			b.WriteString(fmt.Sprintf("## %s — %s\n\n", who, when))
			b.WriteString(forum.StripHTML(m.Text))
			b.WriteString("\n\n")
		}
		mdPath := filepath.Join(threadDir, fsname.Sanitize(title, mode)+".md")
		if err := os.WriteFile(mdPath, []byte(b.String()), 0o644); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

func match(c Conversation, title, q string) bool {
	hay := strings.ToLower(title + " " + c.Name + " " + c.Subname)
	for _, m := range c.Members {
		hay += " " + strings.ToLower(m.FullName)
	}
	for _, m := range c.Messages {
		hay += " " + strings.ToLower(forum.StripHTML(m.Text))
	}
	return strings.Contains(hay, q)
}

func nameMap(parts ...[]Member) map[int]string {
	out := map[int]string{}
	for _, ms := range parts {
		for _, m := range ms {
			if m.FullName != "" {
				out[m.ID] = m.FullName
			}
		}
	}
	return out
}
