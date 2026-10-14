package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Recent bounds the history. Five is what fits in a dropdown you read at a
// glance; the switcher is there for everything else.
const Recent = 5

// keep is how many are stored, so that dropping in and out of one checkout does
// not push the rest out of the file for good.
const keep = 20

// Workspace is somewhere you have worked, as the history remembers it.
type Workspace struct {
	Path   string    `json:"path"`
	Name   string    `json:"name"`
	Branch string    `json:"branch,omitempty"`
	At     time.Time `json:"at"`
}

// recentMu serialises the read-modify-write. Two ade servers starting at once
// would otherwise each write the history they read before the other's entry.
var recentMu sync.Mutex

// RecentPath is the history file.
func RecentPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "recent.json"), nil
}

// Touch records a checkout as the most recently opened.
func Touch(path, branch string, at time.Time) error {
	recentMu.Lock()
	defer recentMu.Unlock()

	file, err := RecentPath()
	if err != nil {
		return err
	}
	history := readRecent(file)

	// One entry per checkout: opening it again moves it up rather than adding a
	// second line saying the same thing.
	next := []Workspace{{Path: path, Name: filepath.Base(path), Branch: branch, At: at}}
	for _, was := range history {
		if was.Path != path {
			next = append(next, was)
		}
	}
	if len(next) > keep {
		next = next[:keep]
	}
	return writeRecent(file, next)
}

// Recents returns the most recent checkouts, newest first, skipping any that
// have since been removed.
func Recents(limit int) []Workspace {
	if limit <= 0 {
		limit = Recent
	}
	file, err := RecentPath()
	if err != nil {
		return nil
	}

	recentMu.Lock()
	history := readRecent(file)
	recentMu.Unlock()

	out := make([]Workspace, 0, limit)
	for _, was := range history {
		// A checkout that has been deleted or renamed is not somewhere to go
		// back to.
		if info, err := os.Stat(was.Path); err != nil || !info.IsDir() {
			continue
		}
		out = append(out, was)
		if len(out) == limit {
			break
		}
	}
	return out
}

func readRecent(file string) []Workspace {
	data, err := os.ReadFile(file) // #nosec G304 -- ade's own state directory
	if err != nil {
		return nil
	}
	var history []Workspace
	if err := json.Unmarshal(data, &history); err != nil {
		return nil
	}
	sort.SliceStable(history, func(i, j int) bool { return history[i].At.After(history[j].At) })
	return history
}

func writeRecent(file string, history []Workspace) error {
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("encode history: %w", err)
	}
	// The history is a list of records rather than a set or a map, so it keeps
	// its own shape; the atomic write is the one thing it shares with the
	// stores in store.go.
	return replace(file, data, ".ade-recent-*")
}
