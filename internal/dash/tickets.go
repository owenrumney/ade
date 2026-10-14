package dash

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/linear"
	"github.com/owenrumney/ade/internal/settings"
)

// Tickets is the Linear issues assigned to you, as last read.
type Tickets struct {
	Issues    []linear.Issue `json:"issues"`
	Error     string         `json:"error,omitempty"`
	CheckedAt time.Time      `json:"checkedAt"`
}

// linearCache is the last reading, refreshed in the background.
type linearCache struct {
	mu    sync.Mutex
	state *Tickets
	// ask fetches the issues for a key; a field so a test can stand one in.
	ask func(ctx context.Context, key string) ([]linear.Issue, error)
}

func newLinearCache() *linearCache {
	return &linearCache{ask: func(ctx context.Context, key string) ([]linear.Issue, error) {
		return linear.Client{Key: key}.MyIssues(ctx)
	}}
}

// AskLinear stands a fetcher in, for a test that must not call Linear.
func (b *Board) AskLinear(ask func(ctx context.Context, key string) ([]linear.Issue, error)) {
	b.linear.mu.Lock()
	b.linear.ask = ask
	b.linear.mu.Unlock()
}

// Tickets is the last reading, or nil when no Linear key is set.
func (b *Board) Tickets() *Tickets {
	b.linear.mu.Lock()
	defer b.linear.mu.Unlock()
	return b.linear.state
}

// RefreshTickets reads your issues if a key is set, and tells the page when
// they changed. No key means no Linear: nothing shown, nothing asked.
func (b *Board) RefreshTickets(ctx context.Context) {
	cfg, err := settings.Load()
	if err != nil {
		// Settings that do not parse used to leave the card reading "nothing on
		// you", which is the same thing it says when Linear is working.
		c := b.linear
		c.mu.Lock()
		was := ""
		if c.state != nil {
			was = c.state.Error
		}
		c.state = &Tickets{Issues: []linear.Issue{}, Error: err.Error(), CheckedAt: time.Now()}
		c.mu.Unlock()
		// New failures only: this runs on a timer, and a file that stays broken
		// would otherwise be the whole log ring.
		level := slog.LevelDebug
		if err.Error() != was {
			level = slog.LevelWarn
		}
		slog.Log(ctx, level, "could not read the settings for Linear", "err", err)
		b.changed()
		return
	}
	key := strings.TrimSpace(cfg.LinearAPIKey)
	c := b.linear
	if key == "" {
		c.mu.Lock()
		had := c.state != nil
		c.state = nil
		c.mu.Unlock()
		if had {
			b.changed()
		}
		return
	}
	c.mu.Lock()
	ask := c.ask
	was := ""
	if c.state != nil {
		was = c.state.Error
	}
	c.mu.Unlock()

	started := time.Now()
	issues, err := ask(ctx, key)
	next := &Tickets{Issues: issues, CheckedAt: time.Now()}
	if err != nil {
		next.Error = err.Error()
		// Warn on a new failure only: this runs on a timer, and Linear being down
		// would otherwise be the only thing left in the log ring.
		level := slog.LevelDebug
		if next.Error != was {
			level = slog.LevelWarn
		}
		slog.Log(ctx, level, "could not list Linear issues",
			"err", err, "took", time.Since(started).Round(time.Millisecond))
	}
	if next.Issues == nil {
		next.Issues = []linear.Issue{}
	}
	c.mu.Lock()
	c.state = next
	c.mu.Unlock()
	b.changed()
}
