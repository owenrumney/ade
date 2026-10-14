// Package logs keeps the last of what ade logged, so a failure can be read
// back in the browser rather than hunted for in a terminal that has scrolled.
package logs

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Keep is how many records the ring holds: bounded memory, and enough to cover
// the run that went wrong at the debug chatter a poll loop produces.
const Keep = 8000

// Record is one line, as the page reads it.
type Record struct {
	At    time.Time `json:"at"`
	Level string    `json:"level"`
	Msg   string    `json:"msg"`

	// Attrs is the key/value pairs, rendered, in the order they were logged.
	Attrs []Attr `json:"attrs,omitempty"`

	// Seq orders the ring and tells a page which records it has not seen.
	Seq uint64 `json:"seq"`
}

type Attr struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Ring is a slog.Handler that remembers and then passes on.
type Ring struct {
	next slog.Handler

	mu   sync.RWMutex
	recs []Record
	at   int
	seq  uint64
	full bool

	// changed is told when a record lands, off the ring's lock.
	changed func(Record)
}

// New wraps a handler. next still receives everything, so the terminal reads
// as it always did.
func New(next slog.Handler) *Ring {
	return &Ring{next: next, recs: make([]Record, Keep)}
}

// OnChange sets who to tell. Under the lock: every goroutine in the process
// logs through Handle, and some of them are running before this is called.
func (r *Ring) OnChange(fn func(Record)) {
	r.mu.Lock()
	r.changed = fn
	r.mu.Unlock()
}

// Enabled is true whatever next would drop.
func (r *Ring) Enabled(context.Context, slog.Level) bool { return true }

func (r *Ring) Handle(ctx context.Context, rec slog.Record) error {
	kept := Record{At: rec.Time, Level: rec.Level.String(), Msg: rec.Message}
	rec.Attrs(func(a slog.Attr) bool {
		kept.Attrs = append(kept.Attrs, Attr{Key: a.Key, Value: a.Value.String()})
		return true
	})

	r.mu.Lock()
	r.seq++
	kept.Seq = r.seq
	r.recs[r.at] = kept
	r.at = (r.at + 1) % len(r.recs)
	r.full = r.full || r.at == 0
	changed := r.changed
	r.mu.Unlock()

	if changed != nil {
		changed(kept)
	}
	// Only what next wants: the ring is the wide net, the terminal is not.
	if r.next != nil && r.next.Enabled(ctx, rec.Level) {
		return r.next.Handle(ctx, rec)
	}
	return nil
}

func (r *Ring) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &wrapped{ring: r, attrs: attrs}
}

func (r *Ring) WithGroup(string) slog.Handler { return r }

// Since is every record newer than seq, oldest first; zero is everything.
func (r *Ring) Since(seq uint64) []Record {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Record, 0, len(r.recs))
	for i := 0; i < len(r.recs); i++ {
		rec := r.recs[(r.at+i)%len(r.recs)]
		if rec.Seq > seq {
			out = append(out, rec)
		}
	}
	return out
}

// Latest is the sequence number of the newest record.
func (r *Ring) Latest() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.seq
}

// wrapped carries a logger's own attrs into the ring.
type wrapped struct {
	ring  *Ring
	attrs []slog.Attr
}

func (w *wrapped) Enabled(ctx context.Context, l slog.Level) bool { return w.ring.Enabled(ctx, l) }

func (w *wrapped) Handle(ctx context.Context, rec slog.Record) error {
	with := rec.Clone()
	with.AddAttrs(w.attrs...)
	return w.ring.Handle(ctx, with)
}

func (w *wrapped) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &wrapped{ring: w.ring, attrs: append(append([]slog.Attr{}, w.attrs...), attrs...)}
}

func (w *wrapped) WithGroup(string) slog.Handler { return w }

// Installed is the ring this process logs into, set once at startup.
var Installed *Ring //nolint:gochecknoglobals // the process's log, like slog's own default
