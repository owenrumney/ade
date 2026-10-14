package limits

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TTL is how long a reading is good for; usage moves by the turn.
const TTL = 2 * time.Minute

type State struct {
	Claude *Limits `json:"claude,omitempty"`
	Codex  *Limits `json:"codex,omitempty"`

	// Error is why the last Claude reading failed. Without it an empty panel
	// blames the login for what may be a network or an HTTP status.
	Error string `json:"error,omitempty"`
}

// Cache refreshes Claude's reading behind the caller, who never waits on it.
type Cache struct {
	mu         sync.Mutex
	claude     *Limits
	codex      *Limits
	refreshing bool
	failedAt   time.Time
	failed     string
	loaded     bool

	// scannedAt is when the Codex transcripts were last read, found or not.
	// Without it a machine with no rate-limit line rescans the tree every call.
	scannedAt time.Time

	// Ask fetches Claude's windows; a field so a test can stand one in.
	Ask func(ctx context.Context) (Limits, error)
}

func NewCache() *Cache {
	return &Cache{Ask: AskClaude}
}

func AskClaude(ctx context.Context) (Limits, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Limits{}, err
	}
	token, err := ClaudeToken(home)
	if err != nil {
		return Limits{}, err
	}
	return Claude(ctx, nil, "", token)
}

// Snapshot is what is known now. A failed read waits a TTL before retrying.
func (c *Cache) Snapshot(ctx context.Context) State {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Shared through a file: Codex because only the process that ran learns
	// its windows, Claude because the account rate-limits every ade that asks.
	c.loaded = true
	for _, share := range []struct {
		provider string
		into     **Limits
	}{{"codex", &c.codex}, {"claude", &c.claude}} {
		held := *share.into
		if held != nil && time.Since(held.CheckedAt) <= TTL {
			continue
		}
		if got, ok := readShared(share.provider); ok && (held == nil || got.CheckedAt.After(held.CheckedAt)) {
			stored := got
			*share.into = &stored
		}
	}
	if (c.codex == nil || time.Since(c.codex.CheckedAt) > TTL) && (c.scannedAt.IsZero() || time.Since(c.scannedAt) > TTL) {
		c.scannedAt = time.Now()
		if home, err := os.UserHomeDir(); err == nil {
			if got, ok := codexTranscript(filepath.Join(home, ".codex")); ok && (c.codex == nil || got.CheckedAt.After(c.codex.CheckedAt)) {
				c.codex = &got
			}
		}
	}
	stale := c.claude == nil || time.Since(c.claude.CheckedAt) > TTL
	if stale && !c.refreshing && time.Since(c.failedAt) > TTL {
		c.refreshing = true
		go c.refresh(ctx)
	}
	out := State{Claude: c.claude, Codex: c.codex}
	// Only when there is nothing to show: a stale reading beside an error
	// reads as though the numbers were wrong, and they are just old.
	if c.claude == nil {
		out.Error = c.failed
	}
	return out
}

func (c *Cache) refresh(ctx context.Context) {
	got, err := c.Ask(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshing = false
	if err != nil {
		slog.Debug("read the plan limits", "err", err)
		c.failedAt, c.failed = time.Now(), err.Error()
		return
	}
	c.claude, c.failed = &got, ""
	writeShared(got)
}

// NoteCodex records what the Codex backend last said, for every process.
func (c *Cache) NoteCodex(l Limits) {
	c.mu.Lock()
	c.codex = &l
	c.mu.Unlock()
	writeShared(l)
}
