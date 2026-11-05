// Package claudelog reads what claude keeps on this machine: its transcripts,
// and its record of the conversations a running claude has open.
package claudelog

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/session"
)

// Conversation is one conversation claude keeps.
type Conversation struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Dir is the folder it was started in, the only one claude resumes it from.
	Dir  string    `json:"dir"`
	Used time.Time `json:"used"`
	// Running is a living claude that has it open.
	Running bool `json:"running"`

	path string
}

const (
	shown = 30
	// A title is written again every few turns, so the end of the file has one.
	titleTail = 128 << 10
	titleHead = 256 << 10
	titleMax  = 80
)

var (
	idShaped         = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)
	notInProjectName = regexp.MustCompile(`[^A-Za-z0-9]`)
	entrypoint       = regexp.MustCompile(`"entrypoint":"([A-Za-z-]+)"`)
)

// IsID is whether id is shaped like the ids claude gives a conversation.
func IsID(id string) bool {
	return idShaped.MatchString(id)
}

// Recent is the newest conversations a person had, from every folder. Those
// started in first lead the list.
func Recent(first string) []Conversation {
	out := []Conversation{}
	home, err := os.UserHomeDir()
	if err != nil {
		return out
	}
	root := filepath.Join(home, ".claude", "projects")
	projects, _ := os.ReadDir(root)
	open := Open()
	seen := map[string]bool{}
	var all []Conversation
	for _, project := range projects {
		entries, err := os.ReadDir(filepath.Join(root, project.Name()))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			id, ok := strings.CutSuffix(entry.Name(), ".jsonl")
			info, err := entry.Info()
			if !ok || err != nil || seen[id] || info.Size() == 0 || !IsID(id) {
				continue
			}
			seen[id] = true
			all = append(all, Conversation{
				ID: id, Used: info.ModTime(), Running: open[id],
				path: filepath.Join(root, project.Name(), entry.Name()),
			})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Used.After(all[j].Used) })
	for _, one := range all {
		if len(out) == shown {
			break
		}
		if one.read() {
			out = append(out, one)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Dir == first && out[j].Dir != first })
	return out
}

// Find is the conversation with that id, wherever it was started.
func Find(id string) (Conversation, bool) {
	home, err := os.UserHomeDir()
	if err != nil || !IsID(id) {
		return Conversation{}, false
	}
	found, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", id+".jsonl"))
	for _, path := range found {
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			continue
		}
		one := Conversation{ID: id, Used: info.ModTime(), Running: Open()[id], path: path}
		if one.read() {
			return one, true
		}
	}
	return Conversation{}, false
}

// Held is whether claude would resume that conversation from dir.
func Held(dir, id string) bool {
	for _, project := range projectsOf(dir) {
		if info, err := os.Stat(filepath.Join(project, id+".jsonl")); err == nil && info.Size() > 0 {
			return true
		}
	}
	return false
}

// Open is the conversations a living claude has open, by claude's own record.
func Open() map[string]bool {
	open := map[string]bool{}
	home, err := os.UserHomeDir()
	if err != nil {
		return open
	}
	records, _ := filepath.Glob(filepath.Join(home, ".claude", "sessions", "*.json"))
	for _, path := range records {
		raw, err := os.ReadFile(path) // #nosec G304 -- claude's own record of its running sessions
		if err != nil {
			continue
		}
		var one struct {
			PID     int    `json:"pid"`
			Session string `json:"sessionId"`
		}
		if json.Unmarshal(raw, &one) == nil && one.PID > 0 && session.Running(one.PID) {
			open[one.Session] = true
		}
	}
	return open
}

// read fills in the folder and the title. False is one nobody would go back to.
func (c *Conversation) read() bool {
	head := slice(c.path, titleHead, false)
	// A run with no terminal is a program's errand, not a conversation.
	if headless(head) {
		return false
	}
	if c.Dir = startedIn(head, filepath.Base(filepath.Dir(c.path))); c.Dir == "" {
		return false
	}
	if c.Title = namedTitle(slice(c.path, titleTail, true)); c.Title == "" {
		c.Title = firstAsk(head)
	}
	return true
}

// projectsOf is where claude files what it did in dir.
func projectsOf(dir string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	paths := []string{dir}
	if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
		paths = append(paths, real)
	}
	for i, path := range paths {
		paths[i] = filepath.Join(home, ".claude", "projects", projectName(path))
	}
	return paths
}

// projectName is claude's folder for a path: every character it cannot keep, a dash.
func projectName(dir string) string {
	return notInProjectName.ReplaceAllString(dir, "-")
}

// startedIn is the folder a transcript says it ran in, which its own folder must agree with.
func startedIn(head [][]byte, project string) string {
	for _, line := range head {
		if !bytes.Contains(line, []byte(`"cwd"`)) {
			continue
		}
		var one struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(line, &one) == nil && one.Cwd != "" && projectName(one.Cwd) == project {
			return one.Cwd
		}
	}
	return ""
}

// headless is a transcript whose claude was run by a program: `claude -p`, or the SDK.
func headless(head [][]byte) bool {
	for _, line := range head {
		if found := entrypoint.FindSubmatch(line); found != nil {
			return bytes.HasPrefix(found[1], []byte("sdk"))
		}
	}
	return false
}

// slice is the whole lines in one end of a file.
func slice(path string, size int64, fromEnd bool) [][]byte {
	file, err := os.Open(path) // #nosec G304 -- a transcript under claude's own projects folder
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil
	}
	whole := info.Size() <= size
	at := int64(0)
	if fromEnd && !whole {
		at = info.Size() - size
	}
	buf := make([]byte, min(size, info.Size()))
	n, _ := file.ReadAt(buf, at)
	lines := bytes.Split(buf[:n], []byte("\n"))
	switch {
	case whole:
	case fromEnd:
		lines = lines[1:]
	default:
		lines = lines[:len(lines)-1]
	}
	return lines
}

// namedTitle is the last title in the lines: one typed by hand beats claude's own.
func namedTitle(lines [][]byte) string {
	typed, given := "", ""
	for _, line := range lines {
		if !bytes.Contains(line, []byte(`-title"`)) {
			continue
		}
		var one struct {
			Type   string `json:"type"`
			Custom string `json:"customTitle"`
			AI     string `json:"aiTitle"`
		}
		if json.Unmarshal(line, &one) != nil {
			continue
		}
		switch one.Type {
		case "custom-title":
			typed = one.Custom
		case "ai-title":
			given = one.AI
		}
	}
	if typed != "" {
		return clip(typed)
	}
	return clip(given)
}

// firstAsk is the first thing the user typed, past what the CLI wrote for them.
func firstAsk(lines [][]byte) string {
	for _, line := range lines {
		var one struct {
			Type    string `json:"type"`
			Meta    bool   `json:"isMeta"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &one) != nil || one.Type != "user" || one.Meta {
			continue
		}
		if text := clip(said(one.Message.Content)); text != "" && !strings.HasPrefix(text, "<") {
			return text
		}
	}
	return ""
}

// said is a message's text, whether it came as a string or as parts.
func said(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &parts) != nil {
		return ""
	}
	for _, part := range parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			return part.Text
		}
	}
	return ""
}

// clip is the first line of text, short enough for a list.
func clip(text string) string {
	text, _, _ = strings.Cut(strings.TrimSpace(text), "\n")
	if runes := []rune(text); len(runes) > titleMax {
		return string(runes[:titleMax-1]) + "…"
	}
	return text
}
