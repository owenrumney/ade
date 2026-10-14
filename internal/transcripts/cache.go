package transcripts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/statedir"
)

// cacheVersion invalidates every stored entry at once. Bump it when what is
// stored changes shape; the price table invalidates itself, below.
const cacheVersion = 4

// priceVersion is the price table, hashed. A stored cost is only good for the
// prices it was worked out from, and nothing else has to remember that.
var priceVersion = pricesHash()

// cacheKeep drops an entry for a transcript nothing has asked about in this
// long, so a machine's worth of deleted sessions does not accumulate.
const cacheKeep = 30 * 24 * time.Hour

// stamp identifies a transcript's contents cheaply: one is appended to and
// never rewritten, so size and modification time are as good as reading it.
type stamp struct {
	Size int64 `json:"size"`
	Mod  int64 `json:"mod"`
}

// entry is one cached answer, good while the file still carries its stamp.
type entry[T any] struct {
	Stamp stamp `json:"stamp"`
	Seen  int64 `json:"seen"`
	Value T     `json:"value"`
}

// file is the shape on disk: the versions guard against reading entries this
// build no longer understands, or costs worked out from other prices.
type file[T any] struct {
	Version int                 `json:"version"`
	Prices  string              `json:"prices,omitempty"`
	Entries map[string]entry[T] `json:"entries"`
}

// cache is a stamped memo of per-transcript work, persisted so a restart does
// not re-read the machine. A lost write costs a re-read and nothing else.
type cache[T any] struct {
	name string

	mu sync.Mutex
	// from is the file the held entries came from. A different one means the
	// state directory moved under us, which is what a test does.
	from    string
	dirty   bool
	entries map[string]entry[T]
}

var (
	indexCache   = &cache[*indexRow]{name: "index.json"}
	summaryCache = &cache[*summaryRow]{name: "summaries.json"}
)

// get is what was stored for a path, while the file still carries that stamp.
// A nil value is an answer: an empty transcript is worth remembering too.
func (c *cache[T]) get(path string, at stamp) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	held, ok := c.entries[path]
	if !ok || held.Stamp != at {
		var zero T
		return zero, false
	}
	if now := time.Now().Unix(); now-held.Seen > int64(time.Hour/time.Second) {
		held.Seen = now
		c.entries[path] = held
		c.dirty = true
	}
	return held.Value, true
}

func (c *cache[T]) put(path string, at stamp, value T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	c.entries[path] = entry[T]{Stamp: at, Seen: time.Now().Unix(), Value: value}
	c.dirty = true
}

// flush writes the cache back, dropping what has gone unasked-for. Failure is
// silent: a cache that cannot be written only costs time.
func (c *cache[T]) flush() {
	c.mu.Lock()
	if !c.dirty {
		c.mu.Unlock()
		return
	}
	cutoff := time.Now().Add(-cacheKeep).Unix()
	kept := make(map[string]entry[T], len(c.entries))
	for path, held := range c.entries {
		if held.Seen >= cutoff {
			kept[path] = held
		}
	}
	c.entries, c.dirty = kept, false
	path := c.from
	data, err := json.Marshal(file[T]{Version: cacheVersion, Prices: priceVersion, Entries: kept})
	c.mu.Unlock()
	if err != nil || path == "" {
		return
	}
	writeCache(path, data)
}

// load reads the file the first time anything asks. Held under c.mu.
func (c *cache[T]) load() {
	path, err := cachePath(c.name)
	if err != nil {
		path = ""
	}
	if c.entries != nil && c.from == path {
		return
	}
	c.from, c.entries, c.dirty = path, map[string]entry[T]{}, false
	if path == "" {
		return
	}
	data, err := os.ReadFile(path) // #nosec G304 -- ade's own cache directory
	if err != nil {
		return
	}
	var held file[T]
	if json.Unmarshal(data, &held) != nil || held.Version != cacheVersion || held.Prices != priceVersion {
		return
	}
	if held.Entries != nil {
		c.entries = held.Entries
	}
}

func cachePath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(statedir.Home(home), "cache", "transcripts", name), nil
}

// writeCache replaces the file atomically, so a reader never sees half of one.
func writeCache(path string, data []byte) {
	if os.MkdirAll(filepath.Dir(path), 0o750) != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ade-transcripts-*")
	if err != nil {
		return
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return
	}
	if os.Rename(tmp.Name(), path) != nil {
		_ = os.Remove(tmp.Name())
	}
}

func stampOf(path string) (stamp, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return stamp{}, false
	}
	return stamp{Size: info.Size(), Mod: info.ModTime().Unix()}, true
}
