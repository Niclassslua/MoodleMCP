package textx

import (
	"html"
	"regexp"
	"strings"
)

var tableRow = regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr\s*>`)
var tableCell = regexp.MustCompile(`(?is)<t[dh]\b[^>]*>(.*?)</t[dh]\s*>`)
var htmlTag = regexp.MustCompile(`(?s)<[^>]*>`)

// HTMLText keeps table rows together and separates cells, so schedules retain
// the association between dates and topics. Other HTML becomes plain text.
func HTMLText(s string) string {
	s = tableRow.ReplaceAllStringFunc(s, func(row string) string {
		var cells []string
		for _, cell := range tableCell.FindAllStringSubmatch(row, -1) {
			cells = append(cells, strings.Join(strings.Fields(html.UnescapeString(htmlTag.ReplaceAllString(cell[1], "\n"))), " "))
		}
		return "\n" + html.EscapeString(strings.Join(cells, " | ")) + "\n"
	})
	var lines []string
	for _, line := range strings.Split(html.UnescapeString(htmlTag.ReplaceAllString(s, "\n")), "\n") {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
