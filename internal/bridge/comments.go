package bridge

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// CommentCap is the longest comment the house rules allow, in lines. Two: at
// three an agent writes three on every declaration and passes.
const CommentCap = 2

var hunkRE = regexp.MustCompile(`^@@ -\S+ \+(\d+)`)

// LongComments is every run of added `//` lines in a unified diff longer than
// cap, each as "file:line — n lines". A removal inside a run does not break it.
func LongComments(diff []string, cap int) []string {
	var file string
	var run, at, startedAt int
	var over []string

	flush := func() {
		if run > cap {
			over = append(over, fmt.Sprintf("%s:%d — %d lines", file, startedAt, run))
		}
		run = 0
	}
	for _, line := range diff {
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			flush()
			file, at = strings.TrimPrefix(line, "+++ b/"), 1
		case strings.HasPrefix(line, "@@"):
			flush()
			if m := hunkRE.FindStringSubmatch(line); m != nil {
				at, _ = strconv.Atoi(m[1])
			}
		case strings.HasPrefix(line, "-"):
		case strings.HasPrefix(line, "+"):
			body := strings.TrimSpace(line[1:])
			if strings.HasPrefix(body, "//") && !strings.HasPrefix(body, "//go:") {
				if run == 0 {
					startedAt = at
				}
				run++
			} else {
				flush()
			}
			at++
		default:
			flush()
		}
	}
	flush()
	return over
}
