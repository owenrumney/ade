package netcheck

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/owenrumney/ade/internal/fdlimit"
	"github.com/owenrumney/ade/internal/settings"
)

// checked is what ade talks to in-process, asked all at once: that is what
// separates one dead service from no outbound network.
var checked = []Target{ //nolint:gochecknoglobals // a table, not state
	{Name: "linear", URL: "https://api.linear.app/graphql"},
	{Name: "anthropic", URL: "https://api.anthropic.com/api/oauth/usage"},
	{Name: "github", URL: "https://api.github.com/"},
	{Name: "github-avatars", URL: "https://github.com/owenrumney.png"},

	// The same host per family, to say whether one path is the broken one.
	{Name: "linear-ipv4", URL: "https://api.linear.app/graphql", Network: "tcp4"},
	{Name: "linear-ipv6", URL: "https://api.linear.app/graphql", Network: "tcp6"},
}

// Handler asks from this process, the only place the answer counts: a curl is
// a different binary under a different network policy.
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// First, and once: a process out of descriptors looks like a dead network,
		// and the probes below move the count.
		fds := fdlimit.Now()
		if fds.Near() {
			slog.Warn("close to the open-file limit", "open", fds.Open, "limit", fds.Limit)
		} else {
			slog.Info("open files", "open", fds.Open, "limit", fds.Limit)
		}
		found := ProbeAll(r.Context(), nil, withCredentials(checked))
		for _, got := range found {
			if got.Reachable {
				slog.Info("net check", "target", got.Name, "status", got.Status, "took", got.Took)
				continue
			}
			slog.Warn("net check failed", "target", got.Name, "took", got.Took,
				"reached", got.Reached, "addresses", got.Addresses, "err", got.Err)
		}
		hosts := DialHosts(r.Context(), lanHosts())
		for _, got := range hosts {
			if got.Reachable {
				slog.Info("host check", "line", got.Line())
				continue
			}
			slog.Warn("host check failed", "line", got.Line())
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": found,
			"hosts":   hosts,
			"files":   map[string]any{"open": fds.Open, "limit": fds.Limit},
		})
	}
}

// lanHosts is the bookmarked machines, without this one.
func lanHosts() []settings.Host {
	cfg, err := settings.Load()
	if err != nil {
		return nil
	}
	var out []settings.Host
	for _, host := range cfg.Hosts {
		bare := host.Name[strings.LastIndex(host.Name, "@")+1:]
		if bare == "" || bare == "localhost" || strings.HasPrefix(bare, "127.") {
			continue
		}
		out = append(out, host)
	}
	return out
}

// withCredentials attaches the keys ade holds, so a probe says whether a
// service would take them. Only the verdict is logged, never the key.
func withCredentials(targets []Target) []Target {
	cfg, err := settings.Load()
	if err != nil {
		return targets
	}
	out := append([]Target(nil), targets...)
	for i := range out {
		if out[i].Name == "linear" {
			out[i].Auth = strings.TrimSpace(cfg.LinearAPIKey)
		}
	}
	return out
}
