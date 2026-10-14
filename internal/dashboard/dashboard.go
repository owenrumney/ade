// Package dashboard pushes this machine's usage to a kiosk screen. Nothing
// waits on it. Design: dashboard/.git/ade/docs/ade-client-prompt.md.
package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Tile is one thing on the screen. Source is the path, not part of the body.
type Tile struct {
	Source string `json:"-"`

	Type   string         `json:"type"`
	Title  string         `json:"title"`
	Group  string         `json:"group,omitempty"`
	Order  int            `json:"order,omitempty"`
	Reduce string         `json:"reduce"`
	Host   string         `json:"host"`
	At     time.Time      `json:"observed_at"`
	Data   map[string]any `json:"data"`
	// Labels is provider and account only; every other breakdown is an item.
	Labels map[string]string `json:"labels,omitempty"`
}

const (
	TypeSubscription = "subscription"
	TypeMoney        = "money"
	TypeCounter      = "counter"
	TypeList         = "list"
	TypeSparkline    = "sparkline"

	Latest = "latest"
	Sum    = "sum"
	Merge  = "merge"
)

// send bounds one request: a pi that has gone is a connection that hangs.
const send = 200 * time.Millisecond

// queued is how many ticks wait before the oldest is dropped.
const queued = 8

// batch is one tick's tiles; done is closed once they have been attempted.
type batch struct {
	tiles []Tile
	done  chan struct{}
}

// grumble is how often a failing push is worth a line.
const grumble = time.Minute

// Reporter is the queue and the one goroutine that drains it. Every method
// takes a nil receiver, which is what an unconfigured machine gets.
type Reporter struct {
	base  string
	token string
	host  string

	out    chan batch
	client *http.Client

	mu       sync.Mutex
	declared map[string]bool
	moaned   time.Time
}

// New starts the reporter, or answers nil when the config is off.
func New(ctx context.Context, base, token, host string) *Reporter {
	if base == "" || token == "" {
		return nil
	}
	r := &Reporter{
		base: base, token: token, host: host,
		out:      make(chan batch, queued),
		client:   &http.Client{Timeout: send},
		declared: map[string]bool{},
	}
	go r.drain(ctx)
	return r
}

// Host is the name this machine reports under.
func (r *Reporter) Host() string {
	if r == nil {
		return ""
	}
	return r.host
}

// Send queues a tick's tiles. It never blocks: a full queue drops its oldest.
func (r *Reporter) Send(tiles ...Tile) {
	if r == nil || len(tiles) == 0 {
		return
	}
	r.queue(batch{tiles: tiles, done: make(chan struct{})})
}

// Drain waits for what is queued to have been attempted, for a command that
// would otherwise exit with its last tile still in the channel.
func (r *Reporter) Drain(ctx context.Context) error {
	if r == nil {
		return nil
	}
	marker := batch{done: make(chan struct{})}
	r.queue(marker)
	select {
	case <-marker.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// queue drops the oldest when the channel is full, marking it done: nothing
// may wait forever on a batch that was dropped.
func (r *Reporter) queue(one batch) {
	for {
		select {
		case r.out <- one:
			return
		default:
		}
		select {
		case old := <-r.out:
			close(old.done)
		default:
			// Drained between the two selects; room now, so go round again.
		}
	}
}

// drain is the only goroutine. One batch at a time, in the order sent.
func (r *Reporter) drain(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case one := <-r.out:
			for _, tile := range one.tiles {
				r.one(ctx, tile)
			}
			close(one.done)
		}
	}
}

func (r *Reporter) one(ctx context.Context, tile Tile) {
	if tile.Source == "" {
		return
	}
	r.declare(ctx, tile.Source)
	body, err := json.Marshal(tile)
	if err != nil {
		r.note("build the "+tile.Source+" tile", err)
		return
	}
	if err := r.do(ctx, "/ingest/"+esc(tile.Source), body); err != nil {
		r.note("report "+tile.Source, err)
	}
}

// declare tells the dashboard this machine contributes to a source, which is
// what populates its machines list. Once per source: it does not change.
func (r *Reporter) declare(ctx context.Context, source string) {
	r.mu.Lock()
	done := r.declared[source]
	r.declared[source] = true
	r.mu.Unlock()
	if done {
		return
	}
	if err := r.do(ctx, "/ingest/"+esc(source)+"/host/"+esc(r.host), nil); err != nil {
		r.mu.Lock()
		delete(r.declared, source)
		r.mu.Unlock()
		r.note("declare this machine for "+source, err)
	}
}

func (r *Reporter) do(ctx context.Context, path string, body []byte) error {
	ctx, cancel := context.WithTimeout(ctx, send)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	// 208 is the server keeping a reading it already had, which is the right
	// answer to a tick that read nothing newer.
	if res.StatusCode >= 300 && res.StatusCode != http.StatusAlreadyReported {
		return fmt.Errorf("the dashboard answered %s", res.Status)
	}
	return nil
}

// note logs a failure at debug, at most once a minute. A pi that is off is
// not news every two minutes for a week.
func (r *Reporter) note(what string, err error) {
	r.mu.Lock()
	quiet := time.Since(r.moaned) < grumble
	if !quiet {
		r.moaned = time.Now()
	}
	r.mu.Unlock()
	if !quiet {
		slog.Debug("could not "+what, "err", err)
	}
}

// esc keeps a label with a space or a slash in it from reshaping the path.
func esc(part string) string { return url.PathEscape(part) }
