package course

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Niclassslua/MoodleMCP/internal/fsname"
	"github.com/Niclassslua/MoodleMCP/internal/moodle"
)

func TestWriteSectionIndexIsOldestFirstJSON(t *testing.T) {
	mods := []moodle.Module{
		{Name: "Neu", ModName: "resource", URL: "https://x/new", Contents: []moodle.Content{
			{Type: "file", Filename: "new.pdf", TimeModified: 1700090000, Filesize: 1 << 20},
		}},
		{Name: "Alt", ModName: "resource", URL: "https://x/old", Contents: []moodle.Content{
			{Type: "file", Filename: "old.pdf", Filepath: "/sub/", TimeModified: 1700000000, Filesize: 2 << 20},
		}},
		{Name: "Slides", ModName: "folder", URL: "https://x/folder", Contents: []moodle.Content{
			{Type: "file", Filename: "s.pdf", TimeModified: 1700050000},
		}},
		{Name: "Forum", ModName: "forum", Contents: []moodle.Content{
			{Type: "file", Filename: "ignored.pdf", TimeModified: 1}, // forums are not indexed
		}},
	}
	dir := t.TempDir()
	if err := writeSectionIndex(dir, "Materialien", mods, fsname.UTF8); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "INDEX.json"))
	if err != nil {
		t.Fatal(err)
	}
	var idx SectionIndex
	if err := json.Unmarshal(b, &idx); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, b)
	}
	if idx.Section != "Materialien" || len(idx.Files) != 3 {
		t.Fatalf("unexpected index: %+v", idx)
	}
	if idx.Files[0].File != "sub/old.pdf" || idx.Files[1].File != "Slides/s.pdf" || idx.Files[2].File != "new.pdf" {
		t.Fatalf("not oldest-first or wrong paths: %+v", idx.Files)
	}
	if idx.Files[0].DisplayName != "Alt" || idx.Files[0].SizeBytes != 2<<20 || idx.Files[0].URL != "https://x/old" {
		t.Fatalf("fields wrong: %+v", idx.Files[0])
	}
}
