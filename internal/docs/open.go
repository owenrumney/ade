package docs

import (
	"encoding/json"
	"fmt"
	"github.com/owenrumney/ade/internal/statedir"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var openMu sync.Mutex

// Open is what a branch had open, and the conversation that opened it. The
// session is what stops a new one inheriting the last one's brief.
type Open struct {
	File    string `json:"file"`
	Session string `json:"session,omitempty"`
}

// Opened is what session left open on branch, and whether that is recorded.
// An empty name is a close; no record, or another session's, matches by name.
func Opened(commonGitDir, branch, session string) (string, bool) {
	if commonGitDir == "" || branch == "" {
		return "", false
	}
	openMu.Lock()
	defer openMu.Unlock()
	entry, ok := readOpen(openPath(commonGitDir))[branch]
	if !ok || !entry.ours(session) {
		return "", false
	}
	return entry.File, true
}

// ours reports a record this session may adopt. One written before sessions
// were recorded belongs to whoever asks, so an upgrade closes nothing — but a
// caller with no session yet is a fresh conversation, not every session.
func (o Open) ours(session string) bool {
	return o.Session == "" || o.Session == session
}

// SetOpened records what session has open on branch, an empty file being the
// close.
func SetOpened(commonGitDir, branch, session, file string) error {
	if commonGitDir == "" || branch == "" {
		return nil
	}
	openMu.Lock()
	defer openMu.Unlock()

	path := openPath(commonGitDir)
	open := readOpen(path)
	if was, ok := open[branch]; ok && was.File == file && was.Session == session {
		return nil
	}
	open[branch] = Open{File: file, Session: session}

	data, err := json.MarshalIndent(open, "", "  ")
	if err != nil {
		return fmt.Errorf("encode open documents: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
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

func openPath(commonGitDir string) string {
	return filepath.Join(statedir.Git(commonGitDir), "open-docs.json")
}

// readOpen is branch to record; an unreadable file reads as nothing. A bare
// string is the shape written before the session was recorded.
func readOpen(path string) map[string]Open {
	open := map[string]Open{}
	data, err := os.ReadFile(path) // #nosec G304 -- ade's own state directory
	if err != nil {
		return open
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return map[string]Open{}
	}
	for branch, value := range raw {
		var entry Open
		if err := json.Unmarshal(value, &entry); err == nil {
			open[branch] = entry
			continue
		}
		var file string
		if err := json.Unmarshal(value, &file); err == nil {
			open[branch] = Open{File: file}
		}
	}
	return open
}

// Newest is the most recently written document of those searched, if any was
// written after since.
func Newest(dirs []string, since time.Time) (string, string, bool) {
	var (
		bestDir, bestFile string
		bestAt            time.Time
	)
	for _, dir := range dirs {
		files, err := markdownFiles(dir)
		if err != nil {
			continue
		}
		for _, file := range files {
			full, err := Resolve(dir, file)
			if err != nil {
				continue
			}
			info, err := os.Stat(full)
			if err != nil || !info.ModTime().After(since) || !info.ModTime().After(bestAt) {
				continue
			}
			bestDir, bestFile, bestAt = dir, file, info.ModTime()
		}
	}
	return bestDir, bestFile, bestFile != ""
}
