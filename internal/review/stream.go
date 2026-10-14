package review

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// streamFence tags the per-finding blocks the reviewer emits as it works, so
// they cannot be confused with the final summary array.
const streamFence = "ade-finding"

var streamRE = regexp.MustCompile("(?s)```" + streamFence + "\\s*\\n(.*?)```")

// Streamer pulls findings out of a reply while it is still being written.
type Streamer struct {
	buf  strings.Builder
	seen map[string]bool
}

func NewStreamer() *Streamer {
	return &Streamer{seen: map[string]bool{}}
}

// Key identifies a finding across the streamed blocks and the final array, so
// the same finding reported twice is only filed once.
func Key(f Finding) string {
	return strings.TrimSpace(f.File) + ":" + strconv.Itoa(f.Line) + ":" + strings.TrimSpace(summaryOf(f))
}

// Push adds the next chunk of reply text and returns findings not seen before.
// The whole buffer is rescanned, so a block split across events still arrives.
func (s *Streamer) Push(text string) []Finding {
	if text == "" {
		return nil
	}
	s.buf.WriteString(text)

	var out []Finding
	for _, match := range streamRE.FindAllStringSubmatch(s.buf.String(), -1) {
		var f Finding
		if err := json.Unmarshal([]byte(match[1]), &f); err != nil {
			continue
		}
		for _, found := range valid([]Finding{f}) {
			if s.seen[Key(found)] {
				continue
			}
			s.seen[Key(found)] = true
			out = append(out, found)
		}
	}
	return out
}

// Has reports whether a finding has already been streamed.
func (s *Streamer) Has(f Finding) bool { return s.seen[Key(f)] }

// StripStream removes the finding blocks from a reply, so what is shown in
// the conversation is the prose and the findings are the comments they
// became.
func StripStream(text string) string {
	return strings.TrimSpace(streamRE.ReplaceAllString(text, ""))
}
