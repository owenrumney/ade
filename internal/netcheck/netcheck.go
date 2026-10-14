package netcheck

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// probeWait bounds one target: long enough to tell slow from dead.
const probeWait = 10 * time.Second

// Target is one thing ade talks to. Auth rides as the Authorization header so
// a refused credential is told apart from one that never got the chance.
type Target struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Auth string `json:"-"`

	// Network pins the family — "tcp4", "tcp6" — where empty lets Go choose.
	Network string `json:"network,omitempty"`
}

// Result is how a probe went. Any answer is reachable, 401 included: the
// question is whether packets get there and back.
type Result struct {
	Target
	Status    string `json:"status,omitempty"`
	Reachable bool   `json:"reachable"`

	// Refused is a credential the far end would not take — reachable, and not
	// working, which is a different problem from not answering.
	Refused   bool   `json:"refused,omitempty"`
	Took      string `json:"took"`
	Reached   string `json:"reached"`
	Addresses string `json:"addresses,omitempty"`
	Err       string `json:"err,omitempty"`
}

// Probe asks one target and reports how far it got.
func Probe(ctx context.Context, client *http.Client, t Target) Result {
	ctx, cancel := context.WithTimeout(ctx, probeWait)
	defer cancel()

	seen := New()
	out := Result{Target: t}
	req, err := http.NewRequestWithContext(seen.Trace(ctx), http.MethodGet, t.URL, nil)
	if err != nil {
		out.Err = err.Error()
		return out
	}
	if t.Auth != "" {
		req.Header.Set("Authorization", t.Auth)
	}
	if client == nil {
		client = &http.Client{}
	}
	if t.Network != "" {
		client = pinned(client, t.Network, seen)
		// Nothing reuses a transport built for one probe, and an idle connection
		// it keeps is a descriptor this check exists to account for.
		if tr, ok := client.Transport.(*http.Transport); ok {
			defer tr.CloseIdleConnections()
		}
	}

	start := time.Now()
	res, err := client.Do(req)
	out.Took = time.Since(start).Round(time.Millisecond).String()
	out.Reached, out.Addresses = seen.Reached(), seen.Addresses()
	if err != nil {
		out.Err = err.Error()
		return out
	}
	defer res.Body.Close()
	out.Status, out.Reachable = res.Status, true
	out.Refused = res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden
	return out
}

// ProbeAll asks every target at once: which one hangs is the answer.
func ProbeAll(ctx context.Context, client *http.Client, targets []Target) []Result {
	out := make([]Result, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = Probe(ctx, client, t)
		}()
	}
	wg.Wait()
	return out
}

// Line is one result as a log line.
func (r Result) Line() string {
	if r.Err != "" {
		return fmt.Sprintf("%s UNREACHABLE after %s (got as far as: %s): %s", r.Name, r.Took, r.Reached, r.Err)
	}
	if r.Refused {
		return fmt.Sprintf("%s reachable but refused the credential (%s) in %s", r.Name, r.Status, r.Took)
	}
	return fmt.Sprintf("%s ok %s in %s", r.Name, r.Status, r.Took)
}

// pinned copies a client with its dialer fixed to one family. The dial marks
// itself: net/http's hooks do not fire through a DialContext of our own.
func pinned(from *http.Client, network string, seen *Phases) *http.Client {
	dial := &net.Dialer{Timeout: probeWait}
	out := *from
	out.Transport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			seen.mark("connect " + network)
			conn, err := dial.DialContext(ctx, network, addr)
			seen.mark(outcome("connected "+addr, "connect-failed "+addr+": "+reason(err), err))
			return conn, err
		},
	}
	return &out
}
