package repostats

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// counted is one checkout's language count at one commit.
type counted struct {
	Head      string     `json:"head"`
	Languages []Language `json:"languages"`
	Lines     int        `json:"lines"`
	Files     int        `json:"files"`
}

// countedLanguages fills the language split from cache when HEAD has not
// moved, and counts and stores it when it has. An empty cache dir never caches.
func (s *Stats) countedLanguages(ctx context.Context, dir, cache string) {
	head, err := gitLine(ctx, dir, "rev-parse", "HEAD")
	if cache == "" || err != nil || head == "" {
		s.languages(ctx, dir)
		return
	}
	sum := sha256.Sum256([]byte(dir))
	path := filepath.Join(cache, hex.EncodeToString(sum[:8])+".json")

	var held counted
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &held) == nil && held.Head == head { // #nosec G304 -- ade's own cache directory
		s.Languages, s.Lines, s.Files = held.Languages, held.Lines, held.Files
		return
	}
	s.languages(ctx, dir)
	data, err := json.Marshal(counted{Head: head, Languages: s.Languages, Lines: s.Lines, Files: s.Files})
	if err != nil || os.MkdirAll(cache, 0o750) != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil && os.Rename(tmp, path) != nil {
		_ = os.Remove(tmp)
	}
}
