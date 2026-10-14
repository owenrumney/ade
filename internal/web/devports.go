package web

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

// devPortRe matches a dev server announcing itself, with a scheme or without —
// the same shape links.ts anchors, so what is clickable is what is forwarded.
var devPortRe = regexp.MustCompile(`(?:https?://|^|[^\w.\-/])(?:localhost|127\.0\.0\.1|\[::1\]):(\d{1,5})`)

// exposeWait bounds telling the launcher. It is a loopback POST.
const exposeWait = 3 * time.Second

// exposeDevPorts forwards a port a tool's output announced. A session on a
// worker serves on that machine's loopback, and the browser is on this one.
func (s *Server) exposeDevPorts(ctx context.Context, output string) {
	if s.noLauncher || output == "" {
		return
	}
	for _, port := range devPortsIn(output) {
		s.exposePort(ctx, port)
	}
}

// devPortsIn is every distinct loopback port the text announced.
func devPortsIn(output string) []int {
	seen := map[int]bool{}
	var out []int
	for _, match := range devPortRe.FindAllStringSubmatch(output, -1) {
		port, err := strconv.Atoi(match[1])
		if err != nil || port < 1 || port > 65535 || seen[port] {
			continue
		}
		seen[port] = true
		out = append(out, port)
	}
	return out
}

func (s *Server) exposePort(ctx context.Context, port int) {
	base := launcherURL()
	if base == "" {
		return
	}
	at, err := url.Parse(base)
	if err != nil {
		return
	}
	body, err := json.Marshal(map[string]int{"port": port})
	if err != nil {
		return
	}
	ask, cancel := context.WithTimeout(ctx, exposeWait)
	defer cancel()
	req, err := http.NewRequestWithContext(ask, http.MethodPost,
		at.Scheme+"://"+at.Host+"/api/ports/expose", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ade-Token", at.Query().Get("t"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Debug("ask the launcher to forward a dev server", "port", port, "err", err)
		return
	}
	_ = res.Body.Close()
}
