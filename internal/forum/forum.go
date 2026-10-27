package forum

import (
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Niclassslua/MoodleMCP/internal/fsname"
	"github.com/Niclassslua/MoodleMCP/internal/moodle"
)

type DiscussionsResponse struct {
	Discussions []Discussion `json:"discussions"`
}

type Discussion struct {
	ID           int    `json:"id"`
	Discussion   int    `json:"discussion"`
	Name         string `json:"name"`
	Subject      string `json:"subject"`
	TimeModified int64  `json:"timemodified"`
	Created      int64  `json:"created"`
	UserFullName string `json:"userfullname"`
	NumReplies   int    `json:"numreplies"`
	Pinned       bool   `json:"pinned"`
}

func (d Discussion) DiscussionID() int {
	if d.Discussion != 0 {
		return d.Discussion
	}
	return d.ID
}

func ListDiscussions(c *moodle.Client, forumInstanceID int) ([]Discussion, error) {
	params := url.Values{"forumid": {fmt.Sprintf("%d", forumInstanceID)}}
	var disc DiscussionsResponse
	if err := c.Call("mod_forum_get_forum_discussions", params, &disc); err != nil {
		return nil, err
	}
	return disc.Discussions, nil
}

func ListPosts(c *moodle.Client, discussionID int) ([]Post, error) {
	pq := url.Values{"discussionid": {fmt.Sprintf("%d", discussionID)}}
	var posts PostsResponse
	if err := c.Call("mod_forum_get_discussion_posts", pq, &posts); err != nil {
		return nil, err
	}
	return posts.Posts, nil
}

type PostsResponse struct {
	Posts []Post `json:"posts"`
}

type Post struct {
	ID          int       `json:"id"`
	Subject     string    `json:"subject"`
	Message     string    `json:"message"`
	Author      Author    `json:"author"`
	TimeCreated int64     `json:"timecreated"`
	Attachments []FileRef `json:"attachments"`
	InlineFiles []FileRef `json:"messageinlinefiles"`
}

type Author struct {
	Name string `json:"fullname"`
}

type FileRef struct {
	Filename string `json:"filename"`
	Filepath string `json:"filepath"`
	FileURL  string `json:"fileurl"`
	MIMEType string `json:"mimetype"`
}

// Stamp returns a cheap change marker (count + max timemodified) for sync skips.
func Stamp(c *moodle.Client, forumInstanceID int) (mtime int64, count int64, err error) {
	params := url.Values{"forumid": {fmt.Sprintf("%d", forumInstanceID)}}
	var disc DiscussionsResponse
	if err := c.Call("mod_forum_get_forum_discussions", params, &disc); err != nil {
		return 0, 0, err
	}
	count = int64(len(disc.Discussions))
	for _, d := range disc.Discussions {
		if d.TimeModified > mtime {
			mtime = d.TimeModified
		}
	}
	return mtime, count, nil
}

// Export writes a forum into dir as markdown threads + attachments.
func Export(c *moodle.Client, forumInstanceID int, forumName, dir string, mode fsname.Mode) (int, error) {
	params := url.Values{"forumid": {fmt.Sprintf("%d", forumInstanceID)}}
	var disc DiscussionsResponse
	if err := c.Call("mod_forum_get_forum_discussions", params, &disc); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}

	written := 0
	for _, d := range disc.Discussions {
		did := d.Discussion
		if did == 0 {
			did = d.ID
		}
		title := d.Name
		if title == "" {
			title = d.Subject
		}
		threadDir := filepath.Join(dir, fsname.Sanitize(title, mode))
		if err := os.MkdirAll(threadDir, 0o755); err != nil {
			return written, err
		}

		var posts PostsResponse
		pq := url.Values{"discussionid": {fmt.Sprintf("%d", did)}}
		if err := c.Call("mod_forum_get_discussion_posts", pq, &posts); err != nil {
			return written, fmt.Errorf("discussion %d: %w", did, err)
		}

		var b strings.Builder
		b.WriteString("# " + title + "\n\n")
		for _, p := range posts.Posts {
			author := p.Author.Name
			if author == "" {
				author = "unknown"
			}
			b.WriteString(fmt.Sprintf("## %s — %s\n\n", author, p.Subject))
			b.WriteString(StripHTML(p.Message))
			b.WriteString("\n\n")

			for _, f := range append(append([]FileRef{}, p.Attachments...), p.InlineFiles...) {
				if f.FileURL == "" || f.Filename == "" {
					continue
				}
				path := filepath.Join(threadDir, fsname.Sanitize(f.Filename, mode))
				if err := saveFile(c, f.FileURL, path); err != nil {
					return written, err
				}
				written++
				b.WriteString(fmt.Sprintf("- attachment: %s\n", f.Filename))
			}
		}
		mdPath := filepath.Join(threadDir, fsname.Sanitize(title, mode)+".md")
		if err := os.WriteFile(mdPath, []byte(b.String()), 0o644); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

func saveFile(c *moodle.Client, fileURL, dest string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = c.Download(fileURL, f)
	return err
}

func StripHTML(s string) string {
	s = html.UnescapeString(s)
	// ponytail: naive strip; good enough for offline notes
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			b.WriteByte('\n')
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
