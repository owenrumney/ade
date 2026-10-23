package launcher

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/httpx"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
)

// proxyPath fronts every session on one origin: a browser allows six
// connections per origin, so a stream apiece would spend the lot.
const proxyPath = "/s/"

var htmlRoot = regexp.MustCompile(`(src|href)="/([^/"])`)

// mountProxy takes the methods one at a time: a bare "/s/{id}/" outranks the
// index's "GET /" and the mux refuses the pair.
func (s *Server) mountProxy(mux *http.ServeMux) {
	for _, method := range []string{
		http.MethodGet, http.MethodHead, http.MethodPost,
		http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions,
	} {
		// Not behind guard: the page has public parts, and a stylesheet the
		// browser fetches carries no token.
		mux.HandleFunc(method+" "+proxyPath+"{id}/", s.handleProxy)
		mux.HandleFunc(method+" "+proxyPath+"{id}", s.handleProxyRoot)
	}
}

// handleProxy forwards to the ade serving a session. The id is resolved
// against the scan, never dialled off the request.
func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	prefix := proxyPath + id
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}

	// The far ade's token is attached only to a request that carried one of
	// ours, so this does not hand its API to anything else on the machine.
	authorized := s.hasToken(r)
	if !authorized && !publicPath(rest) {
		writeError(w, http.StatusForbidden, "bad or missing token")
		return
	}
	s.touch()

	dir, port, ok := s.sessionServer(r.Context(), id)
	if !ok {
		// Not here: it may be a conversation on a machine this one is
		// connected to, which has a launcher and a proxy of its own.
		if away, path, token, found := s.sessionAway(r.Context(), id); found {
			s.relay(w, r, away, path+id+rest, prefix, token)
			return
		}
		writeError(w, http.StatusNotFound, "no ade is serving that session")
		return
	}
	token := ""
	if authorized {
		token = s.upstreamToken(dir)
	}
	target := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}
	s.relay(w, r, target, rest, prefix, token)
}

// relay is the proxy itself, pointed at one address: the ade serving
// this session, or the launcher of the machine it is on.
func (s *Server) relay(
	w http.ResponseWriter,
	r *http.Request,
	target *url.URL,
	path, prefix, token string,
) {
	(&httputil.ReverseProxy{
		// Without this an event stream is held until the buffer fills.
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = target.Host
			pr.Out.URL.Path, pr.Out.URL.RawPath = path, ""
			pr.Out.Header.Set("X-Ade-Token", token)
			if query := pr.Out.URL.Query(); query.Has("t") {
				query.Set("t", token)
				pr.Out.URL.RawQuery = query.Encode()
			}
		},
		ModifyResponse: func(res *http.Response) error { return reprefix(res, prefix) },
	}).ServeHTTP(w, r)
}

// sessionAway finds the machine holding a conversation: its launcher
// has the same proxy at the same path. Read off the sweep, not dialled.
func (s *Server) sessionAway(
	ctx context.Context,
	id string,
) (target *url.URL, path, token string, ok bool) {
	for _, machine := range s.hostRepos(ctx) {
		for _, repo := range machine.Repos {
			for _, checkout := range repo.Checkouts {
				if _, found := sessions.Find(checkout.Sessions, checkout.Path, id); !found {
					continue
				}
				live := s.connection(machine.Machine)
				if live == nil {
					return nil, "", "", false
				}
				at, err := url.Parse(live.url)
				if err != nil {
					return nil, "", "", false
				}
				return &url.URL{Scheme: at.Scheme, Host: at.Host},
					proxyPath, at.Query().Get("t"), true
			}
		}
	}
	return nil, "", "", false
}

func (s *Server) handleProxyRoot(w http.ResponseWriter, r *http.Request) {
	s.touch()
	http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
}

// proxyURL is a session's address here, carrying the caller's own token.
func (s *Server) proxyURL(r *http.Request, id string) (string, bool) {
	if id == "" || s.ownPort() == 0 {
		return "", false
	}
	// No second probe for the server just started: the scan can lag it by a
	// sweep. The proxy resolves the id per request anyway.
	token := r.Header.Get("X-Ade-Token")
	if token == "" {
		token = r.URL.Query().Get("t")
	}
	return "//" + r.Host + proxyPath + url.PathEscape(id) + "/?t=" + url.QueryEscape(token), true
}

// publicPath is what the checkout's own server serves without a token: the
// page, and what the browser loads with it carrying no header.
func publicPath(rest string) bool {
	if rest == "/" || rest == "/healthz" || rest == "/manifest.webmanifest" || rest == "/icon.svg" {
		return true
	}
	return strings.HasPrefix(rest, "/assets/") || strings.HasPrefix(rest, "/design-assets/")
}

// Serving is announced rather than discovered: the scan below only knows
// sessions that have left a transcript.
type Serving struct {
	ID   string `json:"id"`
	Dir  string `json:"dir"`
	Port int    `json:"port"`
}

// handleServing checks the claim against the checkout's record, so it names a
// live ade and not a port.
func (s *Server) handleServing(w http.ResponseWriter, r *http.Request) {
	var req Serving
	if !decodeJSON(w, r, &req) {
		return
	}
	req.ID, req.Dir = strings.TrimSpace(req.ID), strings.TrimSpace(req.Dir)
	if req.ID == "" || !filepath.IsAbs(req.Dir) || req.Port <= 0 {
		writeError(w, http.StatusBadRequest, "name a session, its checkout and its port")
		return
	}
	if !serves(req.Dir, req.Port) {
		writeError(w, http.StatusBadRequest, "no ade is recorded on that port for that checkout")
		return
	}
	s.mu.Lock()
	s.onlyServer(req.Dir, req.ID)
	s.served[req.ID] = req
	// A new ade is the one thing the rail must not wait a second for.
	s.active, s.activeAt = nil, time.Time{}
	s.mu.Unlock()
	s.remember()
	w.WriteHeader(http.StatusNoContent)
}

// onlyServer drops any other row for this checkout: a restart announces a new id.
func (s *Server) onlyServer(dir, id string) {
	for was, row := range s.served {
		if was != id && row.Dir == dir {
			delete(s.served, was)
		}
	}
}

func serves(dir string, port int) bool {
	rec, ok := session.Read(session.Path(sessions.GitDir(dir)))
	return ok && session.Running(rec.PID) && portOf(rec.URL) == port
}

// announcedServer re-checks on the way out, so a dead one falls back.
func (s *Server) announcedServer(id string) (Serving, bool) {
	s.mu.Lock()
	row, ok := s.served[id]
	s.mu.Unlock()
	if !ok || !serves(row.Dir, row.Port) {
		return Serving{}, false
	}
	return row, true
}

// sessionServer is a session's checkout and port. The proxy starts no
// process, so a session nothing serves is a 404.
func (s *Server) sessionServer(ctx context.Context, id string) (dir string, port int, ok bool) {
	if s.locate != nil {
		return s.locate(ctx, id)
	}
	if id = strings.TrimSpace(id); id == "" {
		return "", 0, false
	}
	if row, found := s.announcedServer(id); found {
		return row.Dir, row.Port, true
	}
	repos, err := s.repos()
	if err != nil {
		return "", 0, false
	}
	ports := s.servingPorts(ctx)
	for _, repo := range repos {
		for _, checkout := range repo.Checkouts {
			if _, found := sessions.Find(checkout.Sessions, checkout.Path, id); !found {
				continue
			}
			if at := ports[checkout.Path]; at > 0 {
				return checkout.Path, at, true
			}
			return "", 0, false
		}
	}
	return "", 0, false
}

// announcedWait bounds waiting for a ade to say what it calls itself. It has
// already printed its URL, so the announcement is in flight, not pending work.
var announcedWait = 3 * time.Second

// announcedFor is the id the ade serving a checkout announced, pending or not.
func (s *Server) announcedFor(dir string) string {
	wait := announcedWait
	if httpx.Wide() {
		wait *= 10
	}
	deadline := time.Now().Add(wait)
	for {
		s.mu.Lock()
		for id, row := range s.served {
			if row.Dir == dir {
				s.mu.Unlock()
				return id
			}
		}
		s.mu.Unlock()
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// upstreamToken is the far ade's own, off the record in its git dir.
func (s *Server) upstreamToken(dir string) string {
	rec, ok := session.Read(session.Path(sessions.GitDir(dir)))
	if !ok {
		return ""
	}
	parsed, err := url.Parse(rec.URL)
	if err != nil {
		return ""
	}
	return parsed.Query().Get("t")
}

// reprefix prefixes a page's root-anchored links. HTML only: reading an event
// stream to the end never returns.
func reprefix(res *http.Response, prefix string) error {
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") || res.Header.Get("Content-Encoding") != "" {
		return nil
	}
	body, err := readAll(res)
	if err != nil {
		return err
	}
	body = htmlRoot.ReplaceAll(body, []byte(`$1="`+prefix+`/$2`))
	res.Body = io.NopCloser(bytes.NewReader(body))
	res.ContentLength = int64(len(body))
	res.Header.Set("Content-Length", strconv.Itoa(len(body)))
	return nil
}

func readAll(res *http.Response) ([]byte, error) {
	defer func() { _ = res.Body.Close() }()
	return io.ReadAll(res.Body)
}
