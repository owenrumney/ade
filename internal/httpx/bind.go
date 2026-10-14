package httpx

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// loopback is where every listener goes; nothing reaches ade off this machine.
const loopback = "127.0.0.1"

// envBind is how a container says to bind its wildcard. `-p 127.0.0.1:N:N` is
// what keeps the port off the network there, not the bind.
const envBind = "ADE_BIND"

// BindHost is the address listeners take. An env var rather than a flag: a
// flag is in --help and would get used on a laptop, where it is a hole.
func BindHost() string {
	if host := strings.TrimSpace(os.Getenv(envBind)); host != "" {
		return host
	}
	return loopback
}

// Wide reports a bind that is not loopback, which only a container should have.
func Wide() bool { return BindHost() != loopback }

// OnLoopback reports a request nothing off this machine could have sent. A wide
// bind is no exemption: a sibling container reaches that interface directly.
func OnLoopback(r *http.Request) bool {
	addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok {
		return true
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// HostAllowed refuses a name that merely resolves here. A published container
// port is reached through the forward, so the laptop still types its loopback.
func HostAllowed(name string) bool {
	switch name {
	case loopback, "localhost", "::1", "":
		return true
	}
	return false
}

// LoopbackOrigin reports whether an Origin names this machine. Not a glob: the
// matcher reads `[::1]`'s brackets as a class, so IPv6 is always refused.
func LoopbackOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
