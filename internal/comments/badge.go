package comments

import (
	"fmt"
	"strings"
)

// The badge each severity wears on a comment GitHub shows. shields.io
// renders the image; the colours are the ones the reader already knows.
var badgeColour = map[string]string{
	SeverityCritical: "8B0000",
	SeverityHigh:     "red",
	SeverityMedium:   "orange",
	SeverityLow:      "yellow",
}

// badgeHost is the only image this ever points at, and what Badged looks
// for before adding a second one.
const badgeHost = "https://img.shields.io/badge/"

// Badge is a severity as the line a comment opens with: how bad this is,
// said in the width of a word, before the words that say why.
func Badge(severity string) string {
	named := Severity(severity)
	label := strings.ToUpper(named[:1]) + named[1:]
	return fmt.Sprintf("![%s](%s%s-%s)", label, badgeHost, label, badgeColour[named])
}

// Badged is the body as GitHub receives it. A body already carrying a badge
// is left alone: the words are the author's and this is only the heading.
func Badged(severity, body string) string {
	if strings.TrimSpace(body) == "" || strings.Contains(body, badgeHost) {
		return body
	}
	return Badge(severity) + "\n\n" + body
}
