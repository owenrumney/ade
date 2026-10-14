package launcher

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/owenrumney/ade/internal/session"
)

// watchAsk bounds the question. Opening a tab is the recoverable mistake, so
// anything slow or broken answers no.
const watchAsk = 2 * time.Second

// Watched reports whether anything is looking at the launcher: a page open on
// it, or the shell, which holds its event stream for as long as it runs.
func Watched(ctx context.Context) bool {
	record, err := RecordPath()
	if err != nil {
		return false
	}
	rec, ok := session.Read(record)
	if !ok || !session.Running(rec.PID) {
		return false
	}
	return WatchedAt(ctx, rec.URL)
}

// WatchedAt is the same question of a launcher whose address is already known.
func WatchedAt(ctx context.Context, launcherURL string) bool {
	at, err := url.Parse(launcherURL)
	if err != nil {
		return false
	}
	ask, cancel := context.WithTimeout(ctx, watchAsk)
	defer cancel()
	req, err := http.NewRequestWithContext(ask, http.MethodGet,
		at.Scheme+"://"+at.Host+"/api/watching", nil)
	if err != nil {
		return false
	}
	req.Header.Set("X-Ade-Token", at.Query().Get("t"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = res.Body.Close() }()
	var body struct {
		Watching bool `json:"watching"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&body) != nil {
		return false
	}
	return body.Watching
}

// Reveal is a session a terminal asked for while something holds the launcher.
// URL is for one with no id yet: the rail has no tile to find it by.
type Reveal struct {
	Dir string `json:"dir"`
	ID  string `json:"id,omitempty"`
	URL string `json:"url,omitempty"`
}

// Show asks whatever is watching the launcher to bring a session to the front.
// False is nobody watching: the caller still owes the user a window.
func Show(ctx context.Context, want Reveal) bool {
	record, err := RecordPath()
	if err != nil {
		return false
	}
	rec, ok := session.Read(record)
	if !ok || !session.Running(rec.PID) {
		return false
	}
	return ShowAt(ctx, rec.URL, want)
}

// ShowAt is the same ask of a launcher whose address is already known.
func ShowAt(ctx context.Context, launcherURL string, want Reveal) bool {
	at, err := url.Parse(launcherURL)
	if err != nil || want.Dir == "" {
		return false
	}
	body, err := json.Marshal(want)
	if err != nil {
		return false
	}
	ask, cancel := context.WithTimeout(ctx, watchAsk)
	defer cancel()
	req, err := http.NewRequestWithContext(ask, http.MethodPost,
		at.Scheme+"://"+at.Host+"/api/reveal", bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ade-Token", at.Query().Get("t"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = res.Body.Close() }()
	var answer struct {
		Revealed bool `json:"revealed"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&answer) != nil {
		return false
	}
	return answer.Revealed
}
