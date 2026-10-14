package agentbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	suffix, err := magicDNSSuffix(ctx)
	if err != nil {
		return "", fmt.Errorf("find the tailnet name (set %s to skip this): %w", EnvURL, err)
	}
	return "ws://" + host + "." + suffix + "/v1/ws", nil
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
	out, err := exec.CommandContext(ctx, bin, "status", "--json").Output()
	if err != nil {
		return "", fmt.Errorf("tailscale status: %w", err)
	}
	var status struct {
		MagicDNSSuffix string
		CurrentTailnet *struct{ MagicDNSSuffix string }
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return "", err
	}
	if status.CurrentTailnet != nil && status.CurrentTailnet.MagicDNSSuffix != "" {
		return strings.TrimSuffix(status.CurrentTailnet.MagicDNSSuffix, "."), nil
	}
	if status.MagicDNSSuffix == "" {
		return "", errors.New("tailscale status has no MagicDNS suffix")
	}
	return strings.TrimSuffix(status.MagicDNSSuffix, "."), nil
}
