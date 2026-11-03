package review

import (
	"encoding/json"
	"regexp"
	"strings"
)

// NoteFence tags the per-point blocks a reviewer emits about a design
// document, so they cannot be read as findings on a diff.
const NoteFence = "ade-note"

var noteRE = regexp.MustCompile("(?s)```" + NoteFence + "\\s*\\n(.*?)```")

// Note is one point about a design document, pinned to text quoted from it.
type Note struct {
	Quote string `json:"quote"`
	Note  string `json:"note"`
}

// NoteKey identifies a note across the streamed blocks, so the same point
// written twice is filed once.
func NoteKey(n Note) string {
	return strings.TrimSpace(n.Quote) + ":" + strings.TrimSpace(n.Note)
}

// NoteStreamer pulls notes out of a reply while it is still being written.
type NoteStreamer struct {
	buf  strings.Builder
	seen map[string]bool
}

func NewNoteStreamer() *NoteStreamer {
	return &NoteStreamer{seen: map[string]bool{}}
}

// Push adds the next chunk of reply text and returns notes not seen before.
// The whole buffer is rescanned, so a block split across events still arrives.
func (s *NoteStreamer) Push(text string) []Note {
	if text == "" {
		return nil
	}
	s.buf.WriteString(text)

	var out []Note
	for _, match := range noteRE.FindAllStringSubmatch(s.buf.String(), -1) {
		var n Note
		if err := json.Unmarshal([]byte(match[1]), &n); err != nil {
			continue
		}
		n.Quote, n.Note = strings.TrimSpace(n.Quote), strings.TrimSpace(n.Note)
		if n.Quote == "" || n.Note == "" {
			continue
		}
		if s.seen[NoteKey(n)] {
			continue
		}
		s.seen[NoteKey(n)] = true
		out = append(out, n)
	}
	return out
}
