package myday

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/linear"
	"github.com/owenrumney/ade/internal/settings"
)

// CacheFile is where the estimates are kept, beside ade's other state.
const CacheFile = "my-day.json"

// writeMu serialises the read-modify-write of the cache file.
var writeMu sync.Mutex

// Entry is one estimate and the ticket version it was made from. Linear
// touching the ticket is what invalidates it, not the day turning over.
type Entry struct {
	UpdatedAt time.Time `json:"updatedAt"`
	MadeAt    time.Time `json:"madeAt"`
	Low       float64   `json:"lowHours"`
	High      float64   `json:"highHours"`
	Why       string    `json:"why,omitempty"`
}

// Cache is every estimate held, by ticket identifier.
type Cache map[string]Entry

// Get is a ticket's estimate, and whether it is still good for it. One made
// from an older ticket is kept for the next fire to replace, never printed.
func (c Cache) Get(issue linear.Issue) (Entry, bool) {
	entry, ok := c[strings.ToUpper(issue.Identifier)]
	if !ok {
		return Entry{}, false
	}
	return entry, !issue.UpdatedAt.After(entry.UpdatedAt)
}

// CachePath is the file the estimates are kept in.
func CachePath() (string, error) {
	dir, err := settings.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, CacheFile), nil
}

// Load reads the estimates. A missing or unreadable file is an empty cache:
// the worst it costs is one morning's estimates made again.
func Load() Cache {
	out := Cache{}
	path, err := CachePath()
	if err != nil {
		return out
	}
	data, err := os.ReadFile(path) // #nosec G304 -- ade's own state directory
	if err != nil {
		return out
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return Cache{}
	}
	return out
}

// Save replaces the file, atomically.
func Save(cache Cache) error {
	writeMu.Lock()
	defer writeMu.Unlock()

	path, err := CachePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the estimates: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
