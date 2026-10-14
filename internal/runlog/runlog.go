// Package runlog remembers, for one checkout, which Claude session the agent
// is in.
//
// The design document, the review comments and the conversation all survive a
// restart. The agent did not: it came back with no memory of the work and no
// way to be told about it, so the first thing you did after restarting was
// explain yourself again.
package runlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/owenrumney/ade/internal/statedir"
	"os"
	"path/filepath"
	"sync"
)

// Log is a checkout's agent memory, kept in memory and written through to a
// file.
//
// A nil Log, and one opened on an empty path, both remember nothing and write
// nothing. That is what a server with no checkout behind it has, and it saves
// every caller a nil check.
type Log struct {
	mu       sync.Mutex
	path     string
	session  string
	provider string
	draft    string
}

// stored is the file's shape.
type stored struct {
	Session  string `json:"session,omitempty"`
	Provider string `json:"provider,omitempty"`
	Draft    string `json:"wiki_draft,omitempty"`
}

// Open reads the memory at path, or starts an empty one. A missing or
// unreadable file is an empty memory rather than a failure: refusing to start
// because an old file will not parse would be worse than forgetting it.
//
// An empty path is a memory that is never written, for a server with no
// checkout behind it.
func Open(path string) *Log {
	l := &Log{path: path}
	if path == "" {
		return l
	}

	data, err := os.ReadFile(path) // #nosec G304 -- derived from the repo's own git dir
	if err != nil {
		return l
	}
	var held stored
	if err := json.Unmarshal(data, &held); err != nil {
		return l
	}
	l.session = held.Session
	l.provider = held.Provider
	l.draft = held.Draft
	return l
}

// WikiDraft is the opaque draft owned by this checkout's active agent.
func (l *Log) WikiDraft() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.draft
}

// SetWikiDraft records the draft independently of the provider session.
func (l *Log) SetWikiDraft(id string) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	if l.draft == id {
		l.mu.Unlock()
		return nil
	}
	l.draft = id
	data, err := l.encode()
	l.mu.Unlock()
	if err != nil {
		return err
	}
	return l.write(data)
}

// Session is the conversation this checkout was last in, if any.
func (l *Log) Session() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.session
}

// Provider is the agent that owns the remembered session.
func (l *Log) Provider() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.provider
}

// SetSession records a Claude session for compatibility with existing callers.
func (l *Log) SetSession(id string) error { return l.SetAgentSession(id, "claude") }

// SetAgentSession records the conversation and its provider.
func (l *Log) SetAgentSession(id, provider string) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	if l.session == id && l.provider == provider {
		l.mu.Unlock()
		return nil
	}
	l.session, l.provider = id, provider
	data, err := l.encode()
	l.mu.Unlock()

	if err != nil {
		return err
	}
	return l.write(data)
}

// Path is where this memory is written.
func (l *Log) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Clear forgets the session and removes the file.
func (l *Log) Clear() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	l.session, l.provider, l.draft = "", "", ""
	path := l.path
	l.mu.Unlock()

	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove agent memory: %w", err)
	}
	return nil
}

// encode renders the memory. The caller holds the lock.
func (l *Log) encode() ([]byte, error) {
	data, err := json.MarshalIndent(stored{Session: l.session, Provider: l.provider, Draft: l.draft}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode agent memory: %w", err)
	}
	return data, nil
}

// write replaces the file atomically, so a crash mid-write cannot leave half a
// memory behind.
func (l *Log) write(data []byte) error { return writeFile(l.path, data) }

func writeFile(path string, data []byte) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create agent memory dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".ade-agent-*")
	if err != nil {
		return fmt.Errorf("create temp agent memory: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write agent memory: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("chmod agent memory: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace agent memory: %w", err)
	}
	return nil
}

// Path is where a checkout's agent memory lives. It is keyed by the git dir
// alone, not by branch: Claude Code keys transcripts by working directory, so a
// checkout is in one conversation whatever branch it is on. A worktree has its
// own git dir, so its memory is its own.
func Path(gitDir string) string {
	if gitDir == "" {
		return ""
	}
	return filepath.Join(statedir.Git(gitDir), "agent.json")
}
