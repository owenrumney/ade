package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"
)

type Mapping struct {
	Local  int
	Remote int
}

// Same is what a checkout gets: the port is the same both ends.
func Same(port int) Mapping { return Mapping{Local: port, Remote: port} }

// Forwarder drives the live forwards towards a wanted set.
type Forwarder struct {
	host *Host

	// free reports a port this machine already holds. A field so a test does
	// not have to find genuinely unused ports to be deterministic.
	free func(int) error

	// live is by remote port, which is what the far side reports.
	mu   sync.Mutex
	live map[int]Mapping
}

func NewForwarder(host *Host) *Forwarder {
	return &Forwarder{host: host, free: freeLocally, live: map[int]Mapping{}}
}

// Sync adds what is missing and cancels what is no longer wanted. A port that
// fails to forward is left out of the live set, so the next Sync tries again.
func (f *Forwarder) Sync(ctx context.Context, want []Mapping) error {
	// A port on this machine is already where the browser will look for it.
	if f.host.Local() {
		return nil
	}
	wanted := map[int]Mapping{}
	for _, m := range want {
		wanted[m.Remote] = m
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	var errs []error
	for _, m := range sorted(wanted) {
		if f.live[m.Remote] == m {
			continue
		}
		// Cancel first, or ssh holds both and the stale one keeps the port.
		if old, ok := f.live[m.Remote]; ok {
			_ = f.cancel(ctx, old)
			delete(f.live, m.Remote)
		}
		if err := f.add(ctx, m); err != nil {
			errs = append(errs, err)
			continue
		}
		f.live[m.Remote] = m
	}
	for _, m := range sorted(f.live) {
		if _, ok := wanted[m.Remote]; ok {
			continue
		}
		if err := f.cancel(ctx, m); err != nil {
			errs = append(errs, err)
		}
		// Dropped either way: a cancel that failed is not a forward worth
		// believing in, and holding it would stop it ever being retried.
		delete(f.live, m.Remote)
	}
	return errors.Join(errs...)
}

func (f *Forwarder) Live() []Mapping {
	f.mu.Lock()
	defer f.mu.Unlock()
	return sorted(f.live)
}

// Close drops every forward. The master going away would do it too; this is
// for when the connection outlives the forwards.
func (f *Forwarder) Close(ctx context.Context) error {
	return f.Sync(ctx, nil)
}

func (f *Forwarder) add(ctx context.Context, m Mapping) error {
	// Checked here because ssh's own failure does not name the port.
	if err := f.free(m.Local); err != nil {
		return err
	}
	if _, errOut, err := f.host.ssh(ctx, []string{"-S", f.host.sock, "-O", "forward", "-L", spec(m), f.host.Name}, nil); err != nil {
		return fmt.Errorf("forward port %d from %s: %s", m.Remote, f.host.Name, oneLine(errOut, err))
	}
	return nil
}

func (f *Forwarder) cancel(ctx context.Context, m Mapping) error {
	if _, errOut, err := f.host.ssh(ctx, []string{"-S", f.host.sock, "-O", "cancel", "-L", spec(m), f.host.Name}, nil); err != nil {
		return fmt.Errorf("stop forwarding port %d from %s: %s", m.Remote, f.host.Name, oneLine(errOut, err))
	}
	return nil
}

func spec(m Mapping) string {
	return fmt.Sprintf("%d:127.0.0.1:%d", m.Local, m.Remote)
}

// freeLocally reports a port something here already holds. It is a diagnostic
// rather than a reservation — ssh binds it a moment later — but it turns an
// obscure ssh failure into a sentence naming the port.
func freeLocally(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("port %d is already in use on this machine, so it cannot be mapped to the same port over there", port)
	}
	return ln.Close()
}

func sorted(set map[int]Mapping) []Mapping {
	out := make([]Mapping, 0, len(set))
	for _, m := range set {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Remote < out[j].Remote })
	return out
}
