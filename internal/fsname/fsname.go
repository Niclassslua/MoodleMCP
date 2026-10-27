package fsname

import (
	"regexp"
	"strings"
	"unicode"
)

type Mode string

const (
	UTF8  Mode = "utf8"
	ASCII Mode = "ascii"
)

var asciiRe = regexp.MustCompile(`[^a-z0-9._-]+`)

func Sanitize(name string, mode Mode) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unnamed"
	}
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "\\", "-")
	name = strings.Map(func(r rune) rune {
		if r == 0 || r == '\n' || r == '\r' {
			return -1
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)

	if mode == ASCII {
		repl := strings.NewReplacer(
			"ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss",
			"Ä", "ae", "Ö", "oe", "Ü", "ue",
		)
		name = repl.Replace(name)
		name = strings.ToLower(name)
		name = asciiRe.ReplaceAllString(name, "-")
		for strings.Contains(name, "--") {
			name = strings.ReplaceAll(name, "--", "-")
		}
		name = strings.Trim(name, "-._")
		if name == "" {
			return "unnamed"
		}
		return name
	}

	// utf8: collapse whitespace, strip path junk
	name = strings.Join(strings.Fields(name), " ")
	name = strings.Trim(name, ". ")
	if name == "" {
		return "unnamed"
	}
	return name
}
