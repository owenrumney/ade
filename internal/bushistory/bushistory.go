// Package bushistory keeps what was said with each person on agentbus. The bus
// server keeps nothing, so without this an exchange is gone once dismissed.
package bushistory

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Kinds of entry.
const (
	KindAsk    = "ask"
	KindReview = "review"
	KindAnswer = "answer"
)

// Entry is one thing said, by you or to you.
type Entry struct {
	At   time.Time `json:"at"`
	ID   string    `json:"id"`
	Kind string    `json:"kind"`
	// Mine is said by you; Agent is said by an agent rather than a person,
	// yours when Mine and theirs when not.
	Mine  bool `json:"mine,omitempty"`
	Agent bool `json:"agent,omitempty"`

	Question string   `json:"question,omitempty"`
	Options  []string `json:"options,omitempty"`
	PR       string   `json:"pr,omitempty"`
	Text     string   `json:"text,omitempty"`
	Option   string   `json:"option,omitempty"`
	Verdict  string   `json:"verdict,omitempty"`
}

// Log is a file per person under one directory.
type Log struct {
	dir string
	mu  sync.Mutex
}

// Open is the log under ade's home. Nothing is created until something is said.
func Open(home string) *Log {
	return &Log{dir: filepath.Join(home, "bus")}
}

// ErrNobody is a name that cannot be a file.
var ErrNobody = errors.New("no one to file that under")

// Add appends one entry to who's file. The same id said the same way twice
// is once: a reconnect replays what the bus still holds.
func (l *Log) Add(who string, e Entry) error {
	path, err := l.path(who)
	if err != nil {
		return err
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	had, err := read(path)
	if err != nil {
		return err
	}
	for _, one := range had {
		if one.ID == e.ID && one.Kind == e.Kind && one.Mine == e.Mine {
			return nil
		}
	}
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", l.dir, err)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- a slug under ade's home
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// With is everything said with who, oldest first.
func (l *Log) With(who string) ([]Entry, error) {
	path, err := l.path(who)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return read(path)
}

func (l *Log) path(who string) (string, error) {
	slug := Slug(who)
	if slug == "" {
		return "", ErrNobody
	}
	return filepath.Join(l.dir, slug+".jsonl"), nil
}

// Slug is who as a file name: an address, a team, or anyone. Nothing in it can
// leave the directory, and "." or ".." on their own are nobody.
func Slug(who string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(who)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '@', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" || strings.Trim(out, "_") == "" {
		return ""
	}
	return out
}

func read(path string) ([]Entry, error) {
	file, err := os.Open(path) // #nosec G304 -- a slug under ade's home
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	var out []Entry
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scan.Scan() {
		var e Entry
		// A line that does not parse is a write cut short, not a reason to
		// lose the rest of the conversation.
		if json.Unmarshal(scan.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	return out, scan.Err()
}
