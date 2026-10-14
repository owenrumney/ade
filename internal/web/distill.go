package web

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// sayQuiet tells the launcher this checkout has stopped being worked in, so it
// can distil what was said here once it stays that way.
//
// The wait is held there and not here on purpose: a session exiting is the
// case that most needs distilling and the one least able to do it, and the
// prose it has just written is too fresh to read for another ten minutes.
// Design: .git/ade/docs/fix remote.md.
func (s *Server) sayQuiet(ctx context.Context) {
	if s.noLauncher || s.startRoot == "" {
		return
	}
	base := launcherURL()
	if base == "" {
		return
	}
	at, err := url.Parse(base)
	if err != nil {
		return
	}
	body, err := json.Marshal(map[string]string{"dir": s.startRoot})
	if err != nil {
		return
	}
	ask, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ask, http.MethodPost,
		at.Scheme+"://"+at.Host+"/api/quiet", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ade-Token", at.Query().Get("t"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Debug("tell the launcher this checkout went quiet", "err", err)
		return
	}
	_ = res.Body.Close()
}

// quietly says it without holding up whatever is on its way out.
func (s *Server) quietly() {
	s.spawn(func() { s.sayQuiet(context.WithoutCancel(s.ctx)) })
}
