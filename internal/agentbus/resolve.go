package agentbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Environment variables, all optional.
const (
	EnvURL      = "AGENTBUS_URL"
	EnvHost     = "AGENTBUS_HOST"
	EnvInstance = "AGENTBUS_INSTANCE"
	EnvModel    = "AGENTBUS_MODEL"
)

// macTailscale is where the Mac app keeps its CLI, which is not on PATH.
const macTailscale = "/Applications/Tailscale.app/Contents/MacOS/Tailscale"

// resolveWait bounds the lookup of the bare name.
const resolveWait = 2 * time.Second

var (
	cgnat       = netip.MustParsePrefix("100.64.0.0/10")
	tailscaleV6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

// ResolveURL is $AGENTBUS_URL, or ws://agentbus.<tailnet>/v1/ws with the
// tailnet read from tailscale.
func ResolveURL(ctx context.Context) (string, error) {
	if u := os.Getenv(EnvURL); u != "" {
		return u, nil
	}
	host := os.Getenv(EnvHost)
	if host == "" {
		host = "agentbus"
	}
	if strings.Contains(host, ".") {
		return "ws://" + host + "/v1/ws", nil
	}
	// MagicDNS puts the tailnet in the search domains, so the bare name
	// usually resolves and the CLI is never asked.
	if onTailnet(ctx, host) {
		return "ws://" + host + "/v1/ws", nil
	}
	suffix, err := magicDNSSuffix(ctx)
	if err != nil {
		return "", fmt.Errorf("find the tailnet name (set %s to skip this): %w", EnvURL, err)
	}
	return "ws://" + host + "." + suffix + "/v1/ws", nil
}

// onTailnet reports whether a bare name resolves, and only to tailnet
// addresses. Anywhere else `agentbus` is somebody else's host.
func onTailnet(ctx context.Context, host string) bool {
	ctx, cancel := context.WithTimeout(ctx, resolveWait)
	defer cancel()
	found, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(found) == 0 {
		return false
	}
	for _, ip := range found {
		if !tailnetIP(ip) {
			return false
		}
	}
	return true
}

// tailnetIP is the range MagicDNS answers in: CGNAT, and the ULA prefix
// Tailscale carves its IPv6 out of.
func tailnetIP(ip netip.Addr) bool {
	return cgnat.Contains(ip.Unmap()) || tailscaleV6.Contains(ip)
}

// said is the first line of what a command printed, for an error that
// would otherwise be a parse complaint about somebody else's prose.
func said(out []byte) string {
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if len(line) > 120 {
		line = line[:120] + "…"
	}
	if line == "" {
		return "nothing"
	}
	return line
}

func magicDNSSuffix(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	bin := "tailscale"
	if _, err := exec.LookPath(bin); err != nil {
		if _, err := os.Stat(macTailscale); err != nil {
			return "", errors.New("tailscale CLI not found")
		}
		bin = macTailscale
	}
	cmd := exec.CommandContext(ctx, bin, "status", "--json")
	// Without SHLVL the Mac app's CLI decides it was not run from a shell,
	// tries to start the GUI, says so on stdout and exits 0.
	cmd.Env = append(os.Environ(), "SHLVL=1")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tailscale status: %w", err)
	}
	var status struct {
		MagicDNSSuffix string
		CurrentTailnet *struct{ MagicDNSSuffix string }
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return "", fmt.Errorf("tailscale status said %q", said(out))
	}
	if status.CurrentTailnet != nil && status.CurrentTailnet.MagicDNSSuffix != "" {
		return strings.TrimSuffix(status.CurrentTailnet.MagicDNSSuffix, "."), nil
	}
	if status.MagicDNSSuffix == "" {
		return "", errors.New("tailscale status has no MagicDNS suffix")
	}
	return strings.TrimSuffix(status.MagicDNSSuffix, "."), nil
}
