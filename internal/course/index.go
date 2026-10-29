package course

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Niclassslua/MoodleMCP/internal/fsname"
	"github.com/Niclassslua/MoodleMCP/internal/moodle"
)

// SectionIndex is the per-section INDEX.json: plain data, no presentation text,
// so the caller decides language and layout (e.g. an agent rendering markdown).
type SectionIndex struct {
	SchemaVersion int        `json:"schemaVersion"`
	Section       string     `json:"section"`
	GeneratedAt   string     `json:"generatedAt"`
	Files         []FileInfo `json:"files"`
}

// FileInfo is one downloadable file as Moodle names it. ID stays the same across
// syncs and renames of the display name: "<course module id>:<path in module>".
type FileInfo struct {
	ID          string `json:"id"`
	Section     string `json:"section"`
	DisplayName string `json:"displayName"`
	File        string `json:"file"` // relative to the section folder
	Path        string `json:"path"` // relative to the course folder, slash-separated
	Modified    string `json:"modified"`
	SizeBytes   int64  `json:"sizeBytes"`
	URL         string `json:"url"`
	TooLarge    bool   `json:"tooLarge,omitempty"`
	mtime       int64
}

// sectionDirName is the folder name Download uses for a section.
func sectionDirName(sec moodle.Section, mode fsname.Mode) string {
	name := sec.Name
	if name == "" {
		name = "section"
	}
	return fsname.Sanitize(name, mode)
}

// collectFiles lists every file of the visible resource and folder modules, oldest
// first, with the paths Download writes them to. Files over maxBytes (>0) are TooLarge.
func collectFiles(sections []moodle.Section, mode fsname.Mode, maxBytes int64) []FileInfo {
	var files []FileInfo
	for _, sec := range sections {
		secName := sec.Name
		if secName == "" {
			secName = "section"
		}
		secDir := sectionDirName(sec, mode)
		for _, mod := range sec.Modules {
			if mod.UserVisible != nil && !*mod.UserVisible {
				continue
			}
			if mod.ModName != "resource" && mod.ModName != "folder" {
				continue
			}
			// folder modules are downloaded into their own subfolder, see Download
			prefix := ""
			if mod.ModName == "folder" {
				prefix = fsname.Sanitize(mod.Name, mode) + "/"
			}
			for _, ct := range mod.Contents {
				if ct.Type != "file" || ct.Filename == "" {
					continue
				}
				rel := strings.TrimPrefix(ct.Filepath, "/")
				file := prefix + rel + fsname.Sanitize(ct.Filename, mode)
				files = append(files, FileInfo{
					ID:          fmt.Sprintf("%d:%s%s", mod.ID, rel, ct.Filename),
					Section:     secName,
					DisplayName: mod.Name,
					File:        file,
					Path:        path.Join(secDir, file),
					Modified:    time.Unix(ct.TimeModified, 0).Local().Format(time.RFC3339),
					SizeBytes:   ct.Filesize,
					URL:         mod.URL,
					TooLarge:    maxBytes > 0 && ct.Filesize > maxBytes,
					mtime:       ct.TimeModified,
				})
			}
		}
	}
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].mtime != files[j].mtime {
			return files[i].mtime < files[j].mtime
		}
		return files[i].DisplayName < files[j].DisplayName
	})
	return files
}

// writeSectionIndexes writes INDEX.json into each section folder that has files.
func writeSectionIndexes(courseDir string, sections []moodle.Section, files []FileInfo, mode fsname.Mode) error {
	for _, sec := range sections {
		secName := sec.Name
		if secName == "" {
			secName = "section"
		}
		var entries []FileInfo
		for _, f := range files {
			if f.Section == secName {
				entries = append(entries, f)
			}
		}
		if len(entries) == 0 {
			continue
		}
		idx := SectionIndex{
			SchemaVersion: 1,
			Section:       secName,
			GeneratedAt:   time.Now().Format(time.RFC3339),
			Files:         entries,
		}
		b, err := json.MarshalIndent(idx, "", "  ")
		if err != nil {
			return err
		}
		secDir := filepath.Join(courseDir, sectionDirName(sec, mode))
		if err := os.MkdirAll(secDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(secDir, "INDEX.json"), append(b, '\n'), 0o644); err != nil {
			return fmt.Errorf("index %q: %w", secName, err)
		}
	}
	return nil
}
