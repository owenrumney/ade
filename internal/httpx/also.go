package httpx

import (
	"fmt"
	"net"
	"strconv"
)

// AlsoOn adds one address on the port already held, never a wildcard.
func AlsoOn(host, port string) (net.Listener, error) {
	if host == "" || host == loopback {
		return nil, nil
	}
	if _, err := strconv.Atoi(port); err != nil {
		return nil, fmt.Errorf("not a port: %q", port)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, fmt.Errorf("also listen on %s: %w", net.JoinHostPort(host, port), err)
	}
	Allow(host)
	return ln, nil
}
