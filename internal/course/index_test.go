package course

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Niclassslua/MoodleMCP/internal/fsname"
	"github.com/Niclassslua/MoodleMCP/internal/moodle"
)

func TestSectionIndexIsOldestFirstJSONWithStableIDs(t *testing.T) {
	sections := []moodle.Section{{Name: "Materialien", Modules: []moodle.Module{
		{ID: 1, Name: "Neu", ModName: "resource", URL: "https://x/new", Contents: []moodle.Content{
			{Type: "file", Filename: "new.pdf", TimeModified: 1700090000, Filesize: 1 << 20},
		}},
		{ID: 2, Name: "Alt", ModName: "resource", URL: "https://x/old", Contents: []moodle.Content{
			{Type: "file", Filename: "old.pdf", Filepath: "/sub/", TimeModified: 1700000000, Filesize: 2 << 20},
		}},
		{ID: 3, Name: "Slides", ModName: "folder", URL: "https://x/folder", Contents: []moodle.Content{
			{Type: "file", Filename: "s.pdf", TimeModified: 1700050000, Filesize: 300 << 20},
		}},
		{ID: 4, Name: "Forum", ModName: "forum", Contents: []moodle.Content{
			{Type: "file", Filename: "ignored.pdf", TimeModified: 1}, // forums are not indexed
		}},
	}}}
	files := collectFiles(sections, fsname.UTF8, 200<<20)
	dir := t.TempDir()
	if err := writeSectionIndexes(dir, sections, files, fsname.UTF8); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "Materialien", "INDEX.json"))
	if err != nil {
		t.Fatal(err)
	}
	var idx SectionIndex
	if err := json.Unmarshal(b, &idx); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, b)
	}
	if idx.SchemaVersion != 1 || idx.Section != "Materialien" || len(idx.Files) != 3 {
		t.Fatalf("unexpected index: %+v", idx)
	}
	if idx.Files[0].File != "sub/old.pdf" || idx.Files[1].File != "Slides/s.pdf" || idx.Files[2].File != "new.pdf" {
		t.Fatalf("not oldest-first or wrong paths: %+v", idx.Files)
	}
	old := idx.Files[0]
	if old.ID != "2:sub/old.pdf" || old.Path != "Materialien/sub/old.pdf" || old.DisplayName != "Alt" || old.SizeBytes != 2<<20 || old.URL != "https://x/old" {
		t.Fatalf("fields wrong: %+v", old)
	}
	if !idx.Files[1].TooLarge || idx.Files[0].TooLarge {
		t.Fatalf("tooLarge wrong: %+v", idx.Files)
	}
}
