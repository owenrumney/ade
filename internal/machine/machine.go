// Package machine writes what every ade on this box is doing to one file, for
// a reader that cannot reach a port. No URL and no agent output: what the file
// carries is what ade recorded about itself, so it carries no token.
package machine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/statedir"
)

// Machine is the snapshot: every repository and checkout, and the live server
// on each, as one writer saw them at WrittenAt.
type Machine struct {
	WrittenAt time.Time            `json:"writtenAt"`
	Writer    Writer               `json:"writer"`
	Host      string               `json:"host,omitempty"`
	Repos     []sessions.GraphRepo `json:"repos"`
}

// Writer is the process that produced this, so one can tell its own file
// from another's.
type Writer struct {
	PID  int    `json:"pid"`
	Kind string `json:"kind"`           // "launcher" or "server"
	Root string `json:"root,omitempty"` // the checkout, when Kind is "server"
}

// staleAfter is how long a snapshot holds the lease: shorter than the idle
// cadence, so the writer is whoever is awake.
const staleAfter = 2 * time.Minute

// fileName is the snapshot, and tmpPattern what is renamed onto it: one
// temporary per writer, so two that claim at once do not share a file.
const (
	fileName   = "machine.json"
	tmpPattern = "machine.*.json.tmp"
)

// Dir is where the snapshot lives for the current user.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find your home directory: %w", err)
	}
	return statedir.MachineDir(home), nil
}

// Read loads the snapshot. Absent or unreadable is nothing to go on.
func Read(dir string) (Machine, bool) {
	data, err := os.ReadFile(filepath.Join(dir, fileName)) // #nosec G304 -- a path ade chose, not one from a request
	if err != nil {
		return Machine{}, false
	}
	var m Machine
	if err := json.Unmarshal(data, &m); err != nil || m.WrittenAt.IsZero() {
		return Machine{}, false
	}
	return m, true
}

// Claim reports whether this writer should take the tick. Read before any
// probing, so in the steady state only one process probes.
func Claim(dir string, w Writer, now time.Time) bool {
	held, ok := Read(dir)
	if !ok {
		return true
	}
	return held.Writer.PID == w.PID || now.Sub(held.WrittenAt) >= staleAfter
}

// Write replaces the snapshot through a rename, so no reader sees half a
// file. An older view than the one on disk is refused.
func Write(dir string, m Machine) error {
	if held, ok := Read(dir); ok && m.WrittenAt.Before(held.WrittenAt) {
		return errors.New("a newer snapshot is already there")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create machine dir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode machine: %w", err)
	}
	tmp, err := os.CreateTemp(dir, tmpPattern)
	if err != nil {
		return fmt.Errorf("write machine: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("write machine: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("write machine: %w", err)
	}
	if err := os.Rename(name, filepath.Join(dir, fileName)); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("replace machine: %w", err)
	}
	return nil
}
