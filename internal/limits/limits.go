// Package limits reads how much of a subscription has been used: the plan
// windows Claude and Codex meter, so the chat can say how far from the cap a
// session is before the cap says so itself.
package limits

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Window is one metered period: how much of it is used, and when it resets.
type Window struct {
	Name     string    `json:"name"`
	Percent  float64   `json:"percent"`
	ResetsAt time.Time `json:"resetsAt,omitempty"`
}

// Limits is a provider's windows, as of CheckedAt.
type Limits struct {
	Provider  string    `json:"provider"`
	Windows   []Window  `json:"windows"`
	CheckedAt time.Time `json:"checkedAt"`
}

// Worst is the fullest window, for the one number a badge shows.
func (l Limits) Worst() (Window, bool) {
	var out Window
	found := false
	for _, w := range l.Windows {
		if !found || w.Percent > out.Percent {
			out, found = w, true
		}
	}
	return out, found
}

// usageURL is where Claude Code asks for /usage. Unofficial, like the
// login it needs; shapes are read defensively.
const usageURL = "https://api.anthropic.com/api/oauth/usage"

// ClaudeToken is the OAuth access token Claude Code logged in with: the
// keychain on a Mac, ~/.claude/.credentials.json elsewhere. Never an API key
// — that would be a different bill.
func ClaudeToken(home string) (string, error) {
	var data []byte
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output() // #nosec G204 -- fixed arguments
		if err == nil {
			data = bytes.TrimSpace(out)
		}
	}
	if len(data) == 0 {
		path := filepath.Join(home, ".claude", ".credentials.json")
		read, err := os.ReadFile(path) // #nosec G304 -- the user's own login file
		if err != nil {
			return "", errors.New("no Claude Code login found; run `claude` and log in")
		}
		data = read
	}
	var creds struct {
		OAuth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &creds); err != nil || creds.OAuth.AccessToken == "" {
		return "", errors.New("the Claude Code login has no token in it")
	}
	return creds.OAuth.AccessToken, nil
}

// Claude asks the subscription how full its windows are. url overrides the
// endpoint for a test; empty is the real one.
func Claude(ctx context.Context, client *http.Client, url, token string) (Limits, error) {
	if url == "" {
		url = usageURL
	}
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Limits{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return Limits{}, fmt.Errorf("ask about usage: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Limits{}, fmt.Errorf("usage answered HTTP %d", res.StatusCode)
	}
	var body struct {
		FiveHour *struct {
			Utilization float64   `json:"utilization"`
			ResetsAt    time.Time `json:"resets_at"`
		} `json:"five_hour"`
		SevenDay *struct {
			Utilization float64   `json:"utilization"`
			ResetsAt    time.Time `json:"resets_at"`
		} `json:"seven_day"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return Limits{}, fmt.Errorf("read usage: %w", err)
	}
	out := Limits{Provider: "claude", CheckedAt: time.Now()}
	if body.FiveHour != nil {
		out.Windows = append(out.Windows, Window{Name: "5h", Percent: body.FiveHour.Utilization, ResetsAt: body.FiveHour.ResetsAt})
	}
	if body.SevenDay != nil {
		out.Windows = append(out.Windows, Window{Name: "7d", Percent: body.SevenDay.Utilization, ResetsAt: body.SevenDay.ResetsAt})
	}
	if len(out.Windows) == 0 {
		return Limits{}, errors.New("usage answered with no windows in it")
	}
	return out, nil
}

// FromCodexHeaders reads the windows the Codex backend puts on every
// response: x-codex-primary-* is the short window, x-codex-secondary-* the
// long one. Nothing there is nothing, not zero.
func FromCodexHeaders(h http.Header) (Limits, bool) {
	out := Limits{Provider: "codex", CheckedAt: time.Now()}
	for _, w := range []struct{ prefix, name string }{{"x-codex-primary", "5h"}, {"x-codex-secondary", "7d"}} {
		used := strings.TrimSpace(h.Get(w.prefix + "-used-percent"))
		if used == "" {
			continue
		}
		pct, err := strconv.ParseFloat(used, 64)
		if err != nil {
			continue
		}
		win := Window{Name: w.name, Percent: pct}
		if at, err := strconv.ParseInt(strings.TrimSpace(h.Get(w.prefix+"-reset-at")), 10, 64); err == nil && at > 0 {
			win.ResetsAt = time.Unix(at, 0)
		}
		if mins, err := strconv.Atoi(strings.TrimSpace(h.Get(w.prefix + "-window-minutes"))); err == nil && mins > 0 {
			switch {
			case mins <= 300:
				win.Name = "5h"
			case mins >= 7*24*60:
				win.Name = "7d"
			default:
				win.Name = fmt.Sprintf("%dh", mins/60)
			}
		}
		out.Windows = append(out.Windows, win)
	}
	return out, len(out.Windows) > 0
}
