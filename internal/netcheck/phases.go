// Package netcheck reports how far an HTTP request got, so a call that never
// finished can say which phase stalled rather than only that it timed out.
package netcheck

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"
)

// Phases records how far a request got.
type Phases struct {
	mu    sync.Mutex
	start time.Time
	marks []string
	addrs string
}

// New starts recording. Attach it with Trace and read it with Reached.
func New() *Phases { return &Phases{start: time.Now()} }

func (p *Phases) mark(what string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.marks = append(p.marks, fmt.Sprintf("%s@%s", what, time.Since(p.start).Round(time.Millisecond)))
}

// Trace is the context to make the request with.
func (p *Phases) Trace(ctx context.Context) context.Context {
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { p.mark("dns") },
		DNSDone: func(info httptrace.DNSDoneInfo) {
			var found []string
			for _, a := range info.Addrs {
				found = append(found, a.IP.String())
			}
			p.mu.Lock()
			p.addrs = strings.Join(found, ",")
			p.mu.Unlock()
			p.mark(outcome("resolved", "dns-failed", info.Err))
		},
		ConnectStart: func(string, string) { p.mark("connect") },
		// These fire on failure too, so the error decides the word. Reporting a
		// refused connect as "connected" sends the reader after the wrong thing.
		ConnectDone: func(_, addr string, err error) {
			p.mark(outcome("connected "+addr, "connect-failed "+addr+": "+reason(err), err))
		},
		TLSHandshakeStart: func() { p.mark("tls") },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			p.mark(outcome("handshook", "tls-failed: "+reason(err), err))
		},
		GotConn:              func(httptrace.GotConnInfo) { p.mark("gotconn") },
		WroteRequest:         func(httptrace.WroteRequestInfo) { p.mark("sent") },
		GotFirstResponseByte: func() { p.mark("firstbyte") },
	})
}

// Reached is what the request managed. "nothing" means it never got as far as
// a lookup, which points at the client — an undialable proxy, most often.
func (p *Phases) Reached() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.marks) == 0 {
		return "nothing"
	}
	return strings.Join(p.marks, " ")
}

// Addresses is what DNS returned, empty if it never answered.
func (p *Phases) Addresses() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.addrs
}

// outcome is the word for a phase that reports its own failure.
func outcome(ok, bad string, err error) string {
	if err != nil {
		return bad
	}
	return ok
}

// reason is an error in one short line, for a mark.
func reason(err error) string {
	if err == nil {
		return ""
	}
	return strings.SplitN(err.Error(), "\n", 2)[0]
}
