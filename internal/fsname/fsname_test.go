package fsname

import "testing"

func TestSanitizeASCII(t *testing.T) {
	got := Sanitize("Übung / Test.pdf", ASCII)
	if got != "uebung-test.pdf" {
		t.Fatalf("got %q", got)
	}
}

func TestSanitizeUTF8(t *testing.T) {
	got := Sanitize("Übung  Test", UTF8)
	if got != "Übung Test" {
		t.Fatalf("got %q", got)
	}
}
