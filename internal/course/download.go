package course

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Niclassslua/MoodleMCP/internal/forum"
	"github.com/Niclassslua/MoodleMCP/internal/fsname"
	"github.com/Niclassslua/MoodleMCP/internal/moodle"
	"github.com/Niclassslua/MoodleMCP/internal/syncstate"
)

type Mode string

const (
	ModeAll           Mode = "all"
	ModeResourcesOnly Mode = "resources-only"
	ModeForumsOnly    Mode = "forums-only"
)

type Options struct {
	CourseID     int
	OutDir       string
	Mode         Mode
	FilenameMode fsname.Mode
	Concurrency  int
	SkipExisting bool
	Force        bool
	SkipLocked   bool  // leave out forum discussions Moodle marks as locked (Gesperrt)
	Flat         bool  // write into OutDir itself instead of OutDir/<course name>
	MaxBytes     int64 // files larger than this are listed but not downloaded; 0 = no limit
	State        *syncstate.State
}

type Result struct {
	CourseID   int        `json:"courseId"`
	CourseName string     `json:"courseName"`
	Downloaded int        `json:"downloaded"`
	Skipped    int        `json:"skipped"`
	CourseDir  string     `json:"courseDir"`
	Files      []FileInfo `json:"files"` // every file Moodle lists now, oldest first
}

func Download(c *moodle.Client, opt Options) (Result, error) {
	if opt.Concurrency < 1 {
		opt.Concurrency = 4
	}
	if opt.Mode == "" {
		opt.Mode = ModeAll
	}
	if opt.FilenameMode == "" {
		opt.FilenameMode = fsname.UTF8
	}
	if opt.Force {
		opt.SkipExisting = false
	}

	courses, err := c.Courses()
	if err != nil {
		return Result{}, err
	}
	var courseName string
	var shortName string
	for _, co := range courses {
		if co.ID == opt.CourseID {
			courseName = co.FullName
			shortName = co.ShortName
			break
		}
	}
	if courseName == "" {
		courseName = fmt.Sprintf("course-%d", opt.CourseID)
	}

	sections, err := c.CourseContents(opt.CourseID)
	if err != nil {
		return Result{}, err
	}

	courseDir := filepath.Join(opt.OutDir, fsname.Sanitize(courseName, opt.FilenameMode))
	if opt.Flat {
		courseDir = opt.OutDir
	}
	if err := os.MkdirAll(courseDir, 0o755); err != nil {
		return Result{}, err
	}
	if opt.State == nil {
		st, err := syncstate.Load(courseDir)
		if err != nil {
			return Result{}, err
		}
		opt.State = st
	}

	// updates_since: dirty course-module ids (0 = treat all dirty on first sync / force)
	dirtyAll := opt.Force || !opt.SkipExisting
	dirtyCM := map[int]bool{}
	if opt.SkipExisting && !opt.Force {
		since := opt.State.LastSync
		if since > 0 {
			updates, err := c.UpdatesSince(opt.CourseID, since)
			if err == nil {
				for _, u := range updates {
					if u.ContextLevel == "module" {
						dirtyCM[u.ID] = true
					}
				}
			} else {
				dirtyAll = true // fall back to per-file state
			}
		} else {
			dirtyAll = true
		}
	}

	type job struct {
		fileURL string
		dest    string
		key     string
		mtime   int64
		size    int64
	}
	var jobs []job
	var downloaded, skipped atomic.Int64

	wantResources := opt.Mode == ModeAll || opt.Mode == ModeResourcesOnly
	wantForums := opt.Mode == ModeAll || opt.Mode == ModeForumsOnly
	wantRich := wantResources // labels/pages/assigns are course content, not forums

	cmDirty := func(cmid int) bool {
		if dirtyAll || opt.Force {
			return true
		}
		if len(dirtyCM) == 0 && opt.SkipExisting {
			// updates empty → still check per-file state
			return true
		}
		return dirtyCM[cmid]
	}

	for _, sec := range sections {
		secName := sec.Name
		if secName == "" {
			secName = "section"
		}
		secDir := filepath.Join(courseDir, fsname.Sanitize(secName, opt.FilenameMode))

		for _, mod := range sec.Modules {
			if mod.UserVisible != nil && !*mod.UserVisible {
				continue
			}
			switch mod.ModName {
			case "resource", "folder":
				if !wantResources {
					continue
				}
				if !cmDirty(mod.ID) {
					skipped.Add(1)
					continue
				}
				modDir := secDir
				if mod.ModName == "folder" {
					modDir = filepath.Join(secDir, fsname.Sanitize(mod.Name, opt.FilenameMode))
				}
				for _, ct := range mod.Contents {
					if ct.Type != "file" || ct.FileURL == "" {
						continue
					}
					if opt.MaxBytes > 0 && ct.Filesize > opt.MaxBytes {
						skipped.Add(1)
						continue
					}
					rel := strings.TrimPrefix(ct.Filepath, "/")
					dest := filepath.Join(modDir, rel, fsname.Sanitize(ct.Filename, opt.FilenameMode))
					key := fmt.Sprintf("%d:%s", mod.ID, ct.FileURL)
					if opt.SkipExisting && opt.State != nil && opt.State.Unchanged(key, ct.TimeModified, ct.Filesize) {
						skipped.Add(1)
						continue
					}
					jobs = append(jobs, job{fileURL: ct.FileURL, dest: dest, key: key, mtime: ct.TimeModified, size: ct.Filesize})
				}
			case "url":
				if !wantResources {
					continue
				}
				if err := os.MkdirAll(secDir, 0o755); err != nil {
					return Result{}, err
				}
				link := ""
				for _, ct := range mod.Contents {
					if ct.Type == "url" && ct.FileURL != "" {
						link = ct.FileURL
						break
					}
				}
				if link == "" && mod.URL != "" {
					link = mod.URL
				}
				if link == "" {
					continue
				}
				dest := filepath.Join(secDir, fsname.Sanitize(mod.Name, opt.FilenameMode)+".txt")
				key := fmt.Sprintf("%d:url", mod.ID)
				if opt.SkipExisting && opt.State != nil && opt.State.Unchanged(key, 0, int64(len(link))) {
					skipped.Add(1)
					continue
				}
				if err := os.WriteFile(dest, []byte(link+"\n"), 0o644); err != nil {
					return Result{}, err
				}
				downloaded.Add(1)
				if opt.State != nil {
					opt.State.Set(key, 0, int64(len(link)), dest)
				}
			case "forum":
				if !wantForums {
					continue
				}
				forumDir := filepath.Join(secDir, fsname.Sanitize(mod.Name, opt.FilenameMode))
				key := fmt.Sprintf("%d:forum", mod.ID)
				mtime, count, err := forum.Stamp(c, mod.Instance)
				if err != nil {
					return Result{}, fmt.Errorf("forum %q: %w", mod.Name, err)
				}
				if opt.SkipExisting && opt.State != nil && opt.State.Unchanged(key, mtime, count) {
					skipped.Add(1)
					continue
				}
				n, err := forum.Export(c, mod.Instance, mod.Name, forumDir, opt.FilenameMode, opt.SkipLocked)
				if err != nil {
					return Result{}, fmt.Errorf("forum %q: %w", mod.Name, err)
				}
				downloaded.Add(int64(n))
				if opt.State != nil {
					opt.State.Set(key, mtime, count, forumDir)
				}
			case "label", "page", "assign":
				// exported in bulk when ModeAll
			case "choicegroup", "quiz", "book", "lesson", "scheduler", "feedback":
				skipped.Add(1)
			default:
				skipped.Add(1)
			}
		}
	}

	var notes []FileInfo
	if wantRich {
		lp, err := exportLabelsPages(c, opt, courseDir, &downloaded, &skipped)
		if err != nil {
			return Result{}, err
		}
		as, err := exportAssignsInner(c, opt, courseDir, &downloaded, &skipped)
		if err != nil {
			return Result{}, err
		}
		notes = append(lp, as...)
	}

	sem := make(chan struct{}, opt.Concurrency)
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex

	for _, j := range jobs {
		j := j
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := os.MkdirAll(filepath.Dir(j.dest), 0o755); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				return
			}
			f, err := os.Create(j.dest)
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				return
			}
			_, err = c.Download(j.fileURL, f)
			_ = f.Close()
			if err != nil {
				_ = os.Remove(j.dest)
				errMu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", j.dest, err)
				}
				errMu.Unlock()
				return
			}
			downloaded.Add(1)
			if opt.State != nil {
				opt.State.Set(j.key, j.mtime, j.size, j.dest)
			}
			fmt.Fprintf(os.Stderr, "  ↓ %s\n", j.dest)
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return Result{}, firstErr
	}

	files := []FileInfo{}
	if wantResources {
		files = sortFiles(append(collectFiles(sections, opt.FilenameMode, opt.MaxBytes), notes...))
		if err := writeSectionIndexes(courseDir, sections, files, opt.FilenameMode); err != nil {
			return Result{}, err
		}
	}

	// course.json metadata
	meta, _ := c.CourseMeta(opt.CourseID)
	metaDoc := map[string]any{
		"id": opt.CourseID, "fullname": courseName, "shortname": shortName,
		"meta": meta, "downloadedAt": time.Now().UTC().Format(time.RFC3339),
	}
	if b, err := json.MarshalIndent(metaDoc, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(courseDir, "course.json"), b, 0o644)
	}

	if opt.State != nil {
		opt.State.LastSync = time.Now().Unix()
		if err := opt.State.Save(); err != nil {
			return Result{}, fmt.Errorf("save state: %w", err)
		}
	}

	return Result{
		CourseID:   opt.CourseID,
		CourseName: courseName,
		Downloaded: int(downloaded.Load()),
		Skipped:    int(skipped.Load()),
		CourseDir:  courseDir,
		Files:      files,
	}, nil
}

// noteMarkdown wraps Moodle HTML in a Markdown note. Lines are trimmed and blank
// lines dropped so Markdown readers (Obsidian) keep it one raw HTML block instead
// of turning indented lines into code blocks.
// ponytail: whitespace inside <pre> is lost; convert HTML to Markdown if that matters.
func noteMarkdown(title, html string) []byte {
	var b strings.Builder
	b.WriteString("# " + title + "\n\n")
	for _, ln := range strings.Split(strings.ReplaceAll(html, "\r", ""), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			b.WriteString(ln + "\n")
		}
	}
	return []byte(b.String())
}

// noteInfo lists a written (or unchanged) note like a resource file, so callers
// see labels, pages and assignment intros in Result.Files.
func noteInfo(courseDir, dest, id, section, name, url string, mtime, size int64) FileInfo {
	rel, _ := filepath.Rel(courseDir, dest)
	return FileInfo{
		ID: id, Section: section, DisplayName: name, File: filepath.Base(dest),
		Path: filepath.ToSlash(rel), Modified: time.Unix(mtime, 0).Local().Format(time.RFC3339),
		SizeBytes: size, URL: url, mtime: mtime,
	}
}

func exportLabelsPages(c *moodle.Client, opt Options, courseDir string, downloaded, skipped *atomic.Int64) ([]FileInfo, error) {
	sections, err := c.CourseContents(opt.CourseID)
	if err != nil {
		return nil, err
	}
	// map cmid → section dir, name and link
	secByCM := map[int]string{}
	secNameByCM := map[int]string{}
	urlByCM := map[int]string{}
	for _, sec := range sections {
		secName := sec.Name
		if secName == "" {
			secName = "section"
		}
		secDir := filepath.Join(courseDir, sectionDirName(sec, opt.FilenameMode))
		for _, m := range sec.Modules {
			secByCM[m.ID] = secDir
			secNameByCM[m.ID] = secName
			urlByCM[m.ID] = m.URL
		}
	}
	var infos []FileInfo
	// write saves one note unless the state says it is unchanged; it is listed either way
	write := func(cmid int, key, fallbackDir, name, html string, mtime int64) error {
		secDir := secByCM[cmid]
		section := secNameByCM[cmid]
		if secDir == "" {
			secDir = filepath.Join(courseDir, fallbackDir)
			section = fallbackDir
		}
		url := urlByCM[cmid]
		if url == "" { // labels have no view page
			url = fmt.Sprintf("%s/course/view.php?id=%d#module-%d", c.Base, opt.CourseID, cmid)
		}
		dest := filepath.Join(secDir, fsname.Sanitize(name, opt.FilenameMode)+".md")
		body := noteMarkdown(name, html)
		size := int64(len(body))
		infos = append(infos, noteInfo(courseDir, dest, fmt.Sprintf("%d:%s", cmid, filepath.Base(dest)), section, name, url, mtime, size))
		if opt.SkipExisting && opt.State != nil && opt.State.Unchanged(key, mtime, size) {
			skipped.Add(1)
			return nil
		}
		if err := os.MkdirAll(secDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, body, 0o644); err != nil {
			return err
		}
		downloaded.Add(1)
		if opt.State != nil {
			opt.State.Set(key, mtime, size, dest)
		}
		return nil
	}

	labels, err := c.Labels(opt.CourseID)
	if err != nil {
		return nil, err
	}
	for _, lb := range labels {
		if err := write(lb.CMID, fmt.Sprintf("label:%d", lb.ID), "labels", lb.Name, lb.Intro, lb.TimeModified); err != nil {
			return nil, err
		}
	}

	pages, err := c.Pages(opt.CourseID)
	if err != nil {
		return nil, err
	}
	for _, p := range pages {
		body := p.Content
		if body == "" {
			body = p.Intro
		}
		if err := write(p.CMID, fmt.Sprintf("page:%d", p.ID), "pages", p.Name, body, p.TimeModified); err != nil {
			return nil, err
		}
	}
	return infos, nil
}

func exportAssignsInner(c *moodle.Client, opt Options, courseDir string, downloaded, skipped *atomic.Int64) ([]FileInfo, error) {
	assigns, err := c.Assignments(opt.CourseID)
	if err != nil {
		return nil, err
	}
	var infos []FileInfo
	base := filepath.Join(courseDir, "assigns")
	for _, a := range assigns {
		dir := filepath.Join(base, fsname.Sanitize(a.Name, opt.FilenameMode))
		key := fmt.Sprintf("assign:%d", a.ID)
		intro := a.Intro
		var note []byte
		if intro != "" {
			note = noteMarkdown(a.Name, intro)
			url := fmt.Sprintf("%s/mod/assign/view.php?id=%d", c.Base, a.CMID)
			infos = append(infos, noteInfo(courseDir, filepath.Join(dir, "intro.md"), fmt.Sprintf("%d:intro.md", a.CMID), "assigns", a.Name, url, a.TimeModified, int64(len(note))))
		}
		if opt.SkipExisting && opt.State != nil && opt.State.Unchanged(key, a.TimeModified, int64(len(intro))) {
			skipped.Add(1)
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		meta := map[string]any{
			"id": a.ID, "name": a.Name, "duedate": a.DueDate,
			"allowsubmissionsfromdate": a.AllowSubmissionsFromDate, "cutoffdate": a.CutoffDate,
		}
		mb, _ := json.MarshalIndent(meta, "", "  ")
		_ = os.WriteFile(filepath.Join(dir, "assign.json"), mb, 0o644)
		if note != nil {
			_ = os.WriteFile(filepath.Join(dir, "intro.md"), note, 0o644)
			downloaded.Add(1)
		}
		files := a.IntroAttachments
		if len(files) == 0 {
			files = a.IntroFiles
		}
		for _, f := range files {
			if f.FileURL == "" || f.Filename == "" {
				continue
			}
			dest := filepath.Join(dir, "intro", fsname.Sanitize(f.Filename, opt.FilenameMode))
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return nil, err
			}
			if err := saveURL(c, f.FileURL, dest); err != nil {
				return nil, err
			}
			downloaded.Add(1)
		}
		st, err := c.SubmissionStatus(a.ID)
		if err == nil && st.LastAttempt != nil && st.LastAttempt.Submission != nil {
			subDir := filepath.Join(dir, "submission")
			_ = os.MkdirAll(subDir, 0o755)
			for _, pl := range st.LastAttempt.Submission.Plugins {
				for _, area := range pl.FileAreas {
					for _, f := range area.Files {
						if f.FileURL == "" || f.Filename == "" {
							continue
						}
						dest := filepath.Join(subDir, fsname.Sanitize(f.Filename, opt.FilenameMode))
						if err := saveURL(c, f.FileURL, dest); err != nil {
							return nil, err
						}
						downloaded.Add(1)
					}
				}
			}
		}
		if opt.State != nil {
			opt.State.Set(key, a.TimeModified, int64(len(intro)), dir)
		}
	}
	return infos, nil
}

func saveURL(c *moodle.Client, fileURL, dest string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = c.Download(fileURL, f)
	return err
}

// ResolveCourseID from numeric id or course view URL.
func ResolveCourseID(id int, courseURL string) (int, error) {
	if id > 0 {
		return id, nil
	}
	if courseURL == "" {
		return 0, fmt.Errorf("need --id or --url")
	}
	for _, part := range strings.Split(courseURL, "?") {
		if !strings.Contains(part, "id=") {
			continue
		}
		for _, kv := range strings.Split(part, "&") {
			k, v, ok := strings.Cut(kv, "=")
			if ok && k == "id" {
				var n int
				_, err := fmt.Sscanf(v, "%d", &n)
				if err == nil && n > 0 {
					return n, nil
				}
			}
		}
	}
	if i := strings.Index(courseURL, "id="); i >= 0 {
		var n int
		_, err := fmt.Sscanf(courseURL[i+3:], "%d", &n)
		if err == nil && n > 0 {
			return n, nil
		}
	}
	return 0, fmt.Errorf("could not parse course id from %q", courseURL)
}
