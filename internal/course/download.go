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

	"github.com/Niclassslua/mcd/internal/forum"
	"github.com/Niclassslua/mcd/internal/fsname"
	"github.com/Niclassslua/mcd/internal/moodle"
	"github.com/Niclassslua/mcd/internal/syncstate"
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
	State        *syncstate.State
}

type Result struct {
	CourseName string `json:"courseName"`
	Downloaded int    `json:"downloaded"`
	Skipped    int    `json:"skipped"`
	CourseDir  string `json:"courseDir"`
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
	wantRich := opt.Mode == ModeAll // labels/pages/assigns with "all"

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
				n, err := forum.Export(c, mod.Instance, mod.Name, forumDir, opt.FilenameMode)
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

	if wantRich {
		if _, err := exportLabelsPages(c, opt, courseDir, &downloaded, &skipped); err != nil {
			return Result{}, err
		}
		if err := exportAssignsInner(c, opt, courseDir, &downloaded, &skipped); err != nil {
			return Result{}, err
		}
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
		CourseName: courseName,
		Downloaded: int(downloaded.Load()),
		Skipped:    int(skipped.Load()),
		CourseDir:  courseDir,
	}, nil
}

func exportLabelsPages(c *moodle.Client, opt Options, courseDir string, downloaded, skipped *atomic.Int64) (int, error) {
	sections, err := c.CourseContents(opt.CourseID)
	if err != nil {
		return 0, err
	}
	// map cmid → section dir
	secByCM := map[int]string{}
	for _, sec := range sections {
		secName := sec.Name
		if secName == "" {
			secName = "section"
		}
		secDir := filepath.Join(courseDir, fsname.Sanitize(secName, opt.FilenameMode))
		for _, m := range sec.Modules {
			secByCM[m.ID] = secDir
		}
	}

	labels, err := c.Labels(opt.CourseID)
	if err != nil {
		return 0, err
	}
	for _, lb := range labels {
		secDir := secByCM[lb.CMID]
		if secDir == "" {
			secDir = filepath.Join(courseDir, "labels")
		}
		key := fmt.Sprintf("label:%d", lb.ID)
		size := int64(len(lb.Intro))
		if opt.SkipExisting && opt.State != nil && opt.State.Unchanged(key, 0, size) {
			skipped.Add(1)
			continue
		}
		if err := os.MkdirAll(secDir, 0o755); err != nil {
			return 0, err
		}
		dest := filepath.Join(secDir, fsname.Sanitize(lb.Name, opt.FilenameMode)+".html")
		html := "<!DOCTYPE html><meta charset=\"utf-8\"><title>" + lb.Name + "</title>\n" + lb.Intro
		if err := os.WriteFile(dest, []byte(html), 0o644); err != nil {
			return 0, err
		}
		downloaded.Add(1)
		if opt.State != nil {
			opt.State.Set(key, 0, size, dest)
		}
	}

	pages, err := c.Pages(opt.CourseID)
	if err != nil {
		return 0, err
	}
	for _, p := range pages {
		secDir := secByCM[p.CMID]
		if secDir == "" {
			secDir = filepath.Join(courseDir, "pages")
		}
		body := p.Content
		if body == "" {
			body = p.Intro
		}
		key := fmt.Sprintf("page:%d", p.ID)
		size := int64(len(body))
		if opt.SkipExisting && opt.State != nil && opt.State.Unchanged(key, 0, size) {
			skipped.Add(1)
			continue
		}
		if err := os.MkdirAll(secDir, 0o755); err != nil {
			return 0, err
		}
		dest := filepath.Join(secDir, fsname.Sanitize(p.Name, opt.FilenameMode)+".html")
		html := "<!DOCTYPE html><meta charset=\"utf-8\"><title>" + p.Name + "</title>\n" + body
		if err := os.WriteFile(dest, []byte(html), 0o644); err != nil {
			return 0, err
		}
		downloaded.Add(1)
		if opt.State != nil {
			opt.State.Set(key, 0, size, dest)
		}
	}
	return 0, nil
}

func exportAssignsInner(c *moodle.Client, opt Options, courseDir string, downloaded, skipped *atomic.Int64) error {
	assigns, err := c.Assignments(opt.CourseID)
	if err != nil {
		return err
	}
	base := filepath.Join(courseDir, "assigns")
	for _, a := range assigns {
		dir := filepath.Join(base, fsname.Sanitize(a.Name, opt.FilenameMode))
		key := fmt.Sprintf("assign:%d", a.ID)
		intro := a.Intro
		if opt.SkipExisting && opt.State != nil && opt.State.Unchanged(key, a.DueDate, int64(len(intro))) {
			skipped.Add(1)
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		meta := map[string]any{
			"id": a.ID, "name": a.Name, "duedate": a.DueDate,
			"allowsubmissionsfromdate": a.AllowSubmissionsFromDate, "cutoffdate": a.CutoffDate,
		}
		mb, _ := json.MarshalIndent(meta, "", "  ")
		_ = os.WriteFile(filepath.Join(dir, "assign.json"), mb, 0o644)
		if intro != "" {
			html := "<!DOCTYPE html><meta charset=\"utf-8\"><title>" + a.Name + "</title>\n" + intro
			_ = os.WriteFile(filepath.Join(dir, "intro.html"), []byte(html), 0o644)
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
				return err
			}
			if err := saveURL(c, f.FileURL, dest); err != nil {
				return err
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
							return err
						}
						downloaded.Add(1)
					}
				}
			}
		}
		if opt.State != nil {
			opt.State.Set(key, a.DueDate, int64(len(intro)), dir)
		}
	}
	return nil
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
