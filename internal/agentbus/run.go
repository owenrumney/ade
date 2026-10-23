package agentbus

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"
)

// Vars, not consts, so a test does not have to wait out a real backoff.
var (
	backoffFirst = 2 * time.Second
	backoffMax   = 2 * time.Minute
)

// resolveURL is ResolveURL, as a var so a test does not need a tailnet.
var resolveURL = ResolveURL

// Resolve keeps asking for the bus's address until the tailnet answers, so a
// launcher that started before Tailscale did still joins once it is up.
func Resolve(ctx context.Context, trouble func(error)) string {
	wait := backoffFirst
	for ctx.Err() == nil {
		url, err := resolveURL(ctx)
		if err == nil {
			return url
		}
		slog.Debug("no agentbus to join", "err", err)
		if trouble != nil {
			trouble(err)
		}
		if !sleep(ctx, jitter(wait)) {
			return ""
		}
		wait = min(wait*2, backoffMax)
	}
	return ""
}

// Run keeps one connection up until ctx ends, redialling with backoff. onUp is
// called with each live client, and must not block for long.
func Run(ctx context.Context, opts Options, onUp func(*Client)) {
	wait := backoffFirst
	for ctx.Err() == nil {
		c, err := Dial(ctx, opts)
		if err != nil {
			slog.Debug("agentbus is not reachable", "url", opts.URL, "err", err)
			if opts.OnTrouble != nil {
				opts.OnTrouble(err)
			}
			if !sleep(ctx, jitter(wait)) {
				return
			}
			wait = min(wait*2, backoffMax)
			continue
		}
		slog.Info("joined agentbus", "as", c.You().Instance.ID)
		wait = backoffFirst
		if onUp != nil {
			onUp(c)
		}
		select {
		case <-ctx.Done():
			_ = c.Close()
			return
		case <-c.Done():
		}
		slog.Debug("agentbus connection dropped", "err", c.Err())
		if onUp != nil {
			onUp(nil)
		}
		if !sleep(ctx, jitter(backoffFirst)) {
			return
		}
	}
}

// jitter spreads reconnects so every ade on a machine does not redial at once.
func jitter(d time.Duration) time.Duration {
	return d/2 + time.Duration(rand.Int64N(int64(d)))
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
