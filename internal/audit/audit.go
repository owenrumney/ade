// Package audit appends to the trail the window keeps of what the person
// did. The window writes its own presses; this is for work the launcher does
// on their behalf, where the reason a thing failed exists nowhere else.
package audit

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/statedir"
)

// Entry is one line, in the shape app/src/audit.ts writes and the Audit panel
// reads. From names the writer, so a line ade took on your behalf is not
// mistaken for a press.
type Entry struct {
	At    string `json:"at"`
	Trace string `json:"trace,omitempty"`
	Kind  string `json:"kind"`
	What  string `json:"what"`
	Where string `json:"where,omitempty"`
	From  string `json:"from,omitempty"`
	OK    *bool  `json:"ok,omitempty"`
	Why   string `json:"why,omitempty"`
	MS    int64  `json:"ms,omitempty"`
}

// Keep is how many days of trail are kept, matching the window's own sweep.
const Keep = 30

var mu sync.Mutex

// Trace ties the lines of one piece of work together, so the panel folds
// them under it. Short and random: it is only ever compared to itself.
func Trace() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return time.Now().UTC().Format("150405.000000000")
	}
	return hex.EncodeToString(raw[:])
}

// Note appends one line. It never returns an error: the trail must not be the
// reason an action fails, and a connect that worked must not read as failed
// because a disk did.
func Note(entry Entry) {
	if entry.At == "" {
		entry.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	line, err := json.Marshal(entry)
	if err != nil {
		slog.Debug("audit marshal", "err", err)
		return
	}
	if err := appendLine(append(line, '\n')); err != nil {
		slog.Debug("audit write", "err", err)
	}
}

// Step records one stage of a longer piece of work, under the trace its
// parent line carries. ok is whether that stage passed.
func Step(trace, where, what string, began time.Time, err error) {
	ok := err == nil
	why := ""
	if err != nil {
		why = err.Error()
	}
	Note(Entry{
		Trace: trace,
		Kind:  "step",
		What:  what,
		Where: where,
		From:  "launcher",
		OK:    &ok,
		Why:   why,
		MS:    time.Since(began).Milliseconds(),
	})
}

// Dir is where the trail is written, the same directory the window uses.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(statedir.Home(home), "audit"), nil
}

func appendLine(line []byte) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// One writer at a time within this process; O_APPEND keeps whole lines
	// between processes, which is how a window and a launcher share a file.
	mu.Lock()
	defer mu.Unlock()
	file, err := os.OpenFile(day(dir), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	_, err = file.Write(line)
	return err
}

func day(dir string) string {
	return filepath.Join(dir, time.Now().UTC().Format("2006-01-02")+".jsonl")
}
