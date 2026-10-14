package transcripts

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

// Wrapper markers from design.TurnPrompt, as they read once cleanPrompt has
// collapsed the turn onto one line.
const (
	turnOpening = "We are working on this repository together in ade."
	heardOpen   = "Since you last spoke, @"
	heardSaid   = " said in the conversation: "
	theySaid    = "They said: "
	foldSaid    = " said this in our conversation: <said>"
	seedMark    = "It is the first draft of a design"
)

// launchPrefixes open the prompts ade writes to start a run. Only the first
// sentence is the user's intent; the rest is ade's instructions.
var launchPrefixes = []string{
	"Implement the change described in ",
	"Read ticket ",
	"Draft the design for ",
}

// machinePrefixes open one-shots whose whole turn is ade's, carrying a copy of
// a conversation or of an agent's reply.
var machinePrefixes = []string{
	"Name this conversation in at most six words",
	"Below is a conversation between a user and an agent",
	"You are @",
	"Several reviewers looked at the change below",
	"Rewrite these code review notes as comments to leave on a pull request",
	"You are applying review comments left on a diff",
}

// Machine is a transcript ade wrote the whole of — a title to generate, review
// notes to rewrite. It has a cwd like any other and is not a conversation.
func Machine(s *Session) bool {
	if s == nil {
		return false
	}
	text := strings.TrimSpace(s.FirstPrompt)
	for _, prefix := range machinePrefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

// Prose is the user, named agent and agent text from offset from, stopping
// before limit bytes (zero: none); end is where the next read resumes.
func Prose(s *Session, from int64, limit int) (said []Text, end int64, err error) {
	var read func([]byte) []Text
	switch s.Agent {
	case AgentClaude:
		read = claudeProse
	case AgentCodex:
		read = codexProse
	default:
		return nil, from, nil
	}
	spent := 0
	end, err = scanLinesFrom(s.Path, from, func(raw []byte) bool {
		texts := read(raw)
		size := 0
		for _, t := range texts {
			size += ProseSize(t)
		}
		if limit > 0 && spent > 0 && spent+size > limit {
			return false
		}
		spent += size
		said = append(said, texts...)
		return true
	})
	return said, end, err
}

// ProseSize is what one line of prose costs a prompt, as "role: body".
func ProseSize(t Text) int { return len(t.Role) + len(t.Body) + 3 }

func claudeProse(raw []byte) []Text {
	var line claudeLine
	if json.Unmarshal(raw, &line) != nil || line.Message == nil || line.IsMeta || line.IsSidechain {
		return nil
	}
	switch line.Type {
	case "user":
		return userProse(line.Timestamp, claudeUserText(line.Message))
	case "assistant":
		var out []Text
		for _, block := range claudeBlocks(line.Message.Content) {
			if body := strings.TrimSpace(block.Text); block.Type == "text" && body != "" {
				out = append(out, Text{Role: RoleAgent, At: line.Timestamp, Body: body})
			}
		}
		return out
	}
	return nil
}

// codexProse reads the event stream only: response_item repeats every message.
func codexProse(raw []byte) []Text {
	var line codexLine
	if json.Unmarshal(raw, &line) != nil || line.Payload == nil || line.Type != "event_msg" {
		return nil
	}
	switch line.Payload.Type {
	case "user_message":
		return userProse(line.Timestamp, cleanPrompt(line.Payload.Message))
	case "agent_message":
		if body := strings.TrimSpace(line.Payload.Message); body != "" {
			return []Text{{Role: RoleAgent, At: line.Timestamp, Body: body}}
		}
	}
	return nil
}

// userProse unwraps a cleaned user turn into what was actually said in it.
func userProse(at time.Time, text string) []Text {
	var out []Text
	for _, part := range Unwrap(text) {
		out = append(out, Text{Role: part.Role, At: at, Body: part.Body})
	}
	return out
}

// Unwrap strips ade's turn wrapper from one collapsed user turn: the rules go,
// each named agent's words become its own line, and what the user typed stays.
func Unwrap(text string) []Text {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	for _, prefix := range machinePrefixes {
		if strings.HasPrefix(text, prefix) {
			return nil
		}
	}
	if launched(text) {
		return []Text{{Role: RoleUser, Body: firstSentence(text)}}
	}
	if name, said, ok := folded(text); ok {
		return []Text{{Role: Role("@" + name), Body: said}}
	}
	if !strings.HasPrefix(text, turnOpening) {
		return []Text{{Role: RoleUser, Body: text}}
	}

	var out []Text
	rest := text
	for {
		at := strings.Index(rest, heardOpen)
		if at < 0 {
			break
		}
		after := rest[at+len(heardOpen):]
		name, body, ok := strings.Cut(after, heardSaid)
		if !ok || strings.ContainsAny(name, " <>") {
			break
		}
		open, closing := "<"+name+">", "</"+name+">"
		if !strings.HasPrefix(body, open) {
			break
		}
		end := strings.Index(body, closing)
		if end < 0 {
			break
		}
		heard := strings.TrimSpace(strings.ReplaceAll(body[len(open):end], `<\/`, "</"))
		if heard != "" {
			out = append(out, Text{Role: Role("@" + name), Body: heard})
		}
		rest = body[end+len(closing):]
	}
	if _, said, ok := strings.Cut(rest, theySaid); ok {
		if said = strings.TrimSpace(said); said != "" {
			out = append(out, Text{Role: RoleUser, Body: said})
		}
	}
	return out
}

func launched(text string) bool {
	for _, prefix := range launchPrefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return strings.HasPrefix(text, "Read ") && strings.Contains(text, seedMark)
}

// folded reads design.FoldPrompt: a named agent's reply, handed back to be
// written into the document. The reply is the part worth keeping.
func folded(text string) (name, said string, ok bool) {
	rest, found := strings.CutPrefix(text, "@")
	if !found {
		return "", "", false
	}
	name, rest, found = strings.Cut(rest, foldSaid)
	if !found || strings.ContainsAny(name, " <>") {
		return "", "", false
	}
	said, _, found = strings.Cut(rest, "</said>")
	if !found {
		return "", "", false
	}
	return name, strings.TrimSpace(strings.ReplaceAll(said, `<\/`, "</")), true
}

// firstSentence is text up to the first full stop followed by a space, or the
// first collapsed bullet, whichever comes first.
func firstSentence(text string) string {
	cut := len(text)
	for _, stop := range []string{". ", " - "} {
		if i := strings.Index(text, stop); i >= 0 && i < cut {
			cut = i + 1
		}
	}
	return strings.TrimSpace(text[:cut])
}

// scanLinesFrom calls fn per whole line from offset from. A half-written last
// line is left for the next read; fn returning false stops before its line.
func scanLinesFrom(path string, from int64, fn func([]byte) bool) (int64, error) {
	file, err := os.Open(path) // #nosec G304 -- a transcript from the agents' own session dirs
	if err != nil {
		if os.IsNotExist(err) {
			return from, nil
		}
		return from, err
	}
	defer func() { _ = file.Close() }()

	pos := from
	reader := bufio.NewReaderSize(file, 64<<10)
	if from > 0 {
		// Reading from the byte before: a newline there means from starts a line.
		if _, err := file.Seek(from-1, io.SeekStart); err != nil {
			return from, err
		}
		reader.Reset(file)
		_, n, err := readLine(reader)
		if err != nil {
			return from, nil
		}
		pos = from - 1 + n
	}
	for {
		line, n, err := readLine(reader)
		if errors.Is(err, io.EOF) {
			return pos, nil
		}
		if err != nil {
			return pos, err
		}
		if line = bytes.TrimSpace(line); len(line) > 0 && !fn(line) {
			return pos, nil
		}
		pos += n
	}
}

// readLine is one whole line and how many bytes it took, or io.EOF when what
// is left has no newline. A line past maxLine is consumed but not returned.
func readLine(reader *bufio.Reader) ([]byte, int64, error) {
	var line []byte
	var n int64
	for {
		chunk, err := reader.ReadSlice('\n')
		n += int64(len(chunk))
		if n <= maxLine {
			line = append(line, chunk...)
		} else {
			line = nil
		}
		switch {
		case err == nil:
			return line, n, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		default:
			return nil, n, err
		}
	}
}
