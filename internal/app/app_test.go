package app

import "testing"

func TestPreviewText(t *testing.T) {
	if got, truncated := previewText("short", 5); got != "short" || truncated {
		t.Fatalf("short preview = (%q, %v)", got, truncated)
	}
	if got, truncated := previewText("abcdef", 3); got != "abc…" || !truncated {
		t.Fatalf("truncated preview = (%q, %v)", got, truncated)
	}
	if got, truncated := previewText("äöü", 2); got != "äö…" || !truncated {
		t.Fatalf("Unicode preview = (%q, %v)", got, truncated)
	}
}
