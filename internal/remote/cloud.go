package remote

import (
	"context"
	"fmt"
	"time"
)

// Cloud is what the hub's tools log in with, as ~/.aws and ~/.kube hold it.
type Cloud struct {
	AWSConfig      []byte
	AWSCredentials []byte
	// SSOCache is ~/.aws/sso/cache by base name; the bearer token is in there.
	SSOCache   map[string][]byte
	Kubeconfig []byte
	// Profile is AWS_PROFILE here, so the hub picks the same one.
	Profile string
	// At is when this machine's copies were written, for the newest-wins rule.
	At time.Time
}

func (c Cloud) Any() bool {
	return len(c.AWSConfig) > 0 || len(c.AWSCredentials) > 0 ||
		len(c.SSOCache) > 0 || len(c.Kubeconfig) > 0
}

// CloudHome is under Run, which goes with a reboot, as an SSO token does.
func CloudHome(roots Roots) string { return roots.Run + "/hub" }

// CloudEnv points the hub's tools at the files: it has no HOME to find them in.
func CloudEnv(roots Roots, c Cloud) []string {
	home := CloudHome(roots)
	out := []string{
		"AWS_CONFIG_FILE=" + home + "/aws/config",
		"AWS_SHARED_CREDENTIALS_FILE=" + home + "/aws/credentials",
		"KUBECONFIG=" + home + "/kube/config",
	}
	if c.Profile != "" {
		out = append(out, "AWS_PROFILE="+c.Profile)
	}
	return out
}

// PrepareCloud sends this machine's cloud configuration for the hub to mount.
func (h *Host) PrepareCloud(ctx context.Context, roots Roots, c Cloud) error {
	if !c.Any() {
		return nil
	}
	home := CloudHome(roots)
	mk := `set -eu
umask 077
mkdir -p "$1/aws/sso/cache" "$1/kube"`
	if _, err := h.Run(ctx, "sh", "-c", mk, "sh", home); err != nil {
		return fmt.Errorf("prepare cloud credentials on %s: %w", h.Name, err)
	}
	files := []struct {
		path string
		data []byte
	}{
		{home + "/aws/config", c.AWSConfig},
		{home + "/aws/credentials", c.AWSCredentials},
		{home + "/kube/config", c.Kubeconfig},
	}
	for name, data := range c.SSOCache {
		files = append(files, struct {
			path string
			data []byte
		}{home + "/aws/sso/cache/" + name, data})
	}
	for _, file := range files {
		if len(file.data) == 0 || !h.olderThan(ctx, file.path, c.At) {
			continue
		}
		command := "set -eu\numask 077\n" + putFile + "\nput \"$1\""
		if _, err := h.RunInput(ctx, file.data, "sh", "-c", command, "sh", file.path); err != nil {
			return fmt.Errorf("send cloud credentials to %s: %w", h.Name, err)
		}
	}
	return nil
}
