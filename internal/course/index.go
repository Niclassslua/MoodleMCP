package course

import (
	"encoding/json"
	"os"
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
	Section     string       `json:"section"`
	GeneratedAt string       `json:"generatedAt"`
	Files       []IndexEntry `json:"files"`
}

// IndexEntry is one downloadable file as Moodle names it.
type IndexEntry struct {
	DisplayName string `json:"displayName"`
	File        string `json:"file"`
	Modified    string `json:"modified"`
	SizeBytes   int64  `json:"sizeBytes"`
	URL         string `json:"url"`
	mtime       int64
}

// writeSectionIndex writes INDEX.json into secDir, listing every file of the section's
// resource and folder modules, oldest first. Modified is Moodle's last-modified time.
func writeSectionIndex(secDir, sectionName string, mods []moodle.Module, mode fsname.Mode) error {
	var entries []IndexEntry
	for _, mod := range mods {
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
			entries = append(entries, IndexEntry{
				DisplayName: mod.Name,
				File:        prefix + strings.TrimPrefix(ct.Filepath, "/") + ct.Filename,
				Modified:    time.Unix(ct.TimeModified, 0).Local().Format(time.RFC3339),
				SizeBytes:   ct.Filesize,
				URL:         mod.URL,
				mtime:       ct.TimeModified,
			})
		}
	}
	if len(entries) == 0 {
		return nil
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].mtime != entries[j].mtime {
			return entries[i].mtime < entries[j].mtime
		}
		return entries[i].DisplayName < entries[j].DisplayName
	})

	idx := SectionIndex{
		Section:     sectionName,
		GeneratedAt: time.Now().Format(time.RFC3339),
		Files:       entries,
	}
	b, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(secDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(secDir, "INDEX.json"), append(b, '\n'), 0o644)
}
