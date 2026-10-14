package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
)

// portsInterval is how often the set is rescanned. The interesting change —
// a checkout being opened — is published the moment it happens, so this is
// only for a server that started or died some other way.
const portsInterval = 5 * time.Second

// portsKeepalive stops a stream that has had nothing to say from looking dead.
const portsKeepalive = 20 * time.Second

// handlePorts streams the loopback ports this machine serves ade on, for a ade
// on another machine to forward. The whole set goes out every time, so the
// reader reconciles rather than applies a delta.
func (s *Server) handlePorts(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	wake, unsubscribe := s.subscribePorts()
	defer unsubscribe()

	last := ""
	sent := time.Now()
	send := func() {
		payload, err := json.Marshal(map[string][]int{"ports": s.ports(r.Context())})
		if err != nil || string(payload) == last {
			return
		}
		last = string(payload)
		sent = time.Now()
		_, _ = fmt.Fprintf(w, "event: ports\ndata: %s\n\n", payload)
		flusher.Flush()
	}
	send()

	ticker := time.NewTicker(portsInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case <-wake:
			send()
		case <-ticker.C:
			send()
			if time.Since(sent) >= portsKeepalive {
				sent = time.Now()
				_, _ = fmt.Fprint(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}
}

// ports is the launcher's own, plus every checkout whose server answers.
func (s *Server) ports(ctx context.Context) []int {
	found := map[int]bool{}
	if own := s.ownPort(); own > 0 {
		found[own] = true
	}
	for _, port := range s.servingPorts(ctx) {
		found[port] = true
	}
	s.dropDeadExposed(ctx)
	s.mu.Lock()
	for port := range s.exposed {
		found[port] = true
	}
	s.mu.Unlock()
	return sortedPorts(found)
}

func (s *Server) handleExposePort(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Port int `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Port < 1 || req.Port > 65535 {
		writeError(w, http.StatusBadRequest, "name a TCP port from 1 to 65535")
		return
	}
	s.mu.Lock()
	s.exposed[req.Port] = true
	s.mu.Unlock()
	s.notifyPorts()
	writeJSON(w, http.StatusCreated, map[string]string{"url": fmt.Sprintf("http://127.0.0.1:%d", req.Port)})
}

// dropDeadExposed forgets a port nothing serves any more. The set is
// reconciled every frame, so a stale one is a forward to nothing.
func (s *Server) dropDeadExposed(ctx context.Context) {
	s.mu.Lock()
	ports := make([]int, 0, len(s.exposed))
	for port := range s.exposed {
		ports = append(ports, port)
	}
	s.mu.Unlock()

	for _, port := range ports {
		if listening(ctx, port) {
			continue
		}
		s.mu.Lock()
		delete(s.exposed, port)
		s.mu.Unlock()
	}
}

// listening reports something answering on this machine's port.
func listening(ctx context.Context, port int) bool {
	var dialer net.Dialer
	probe, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	conn, err := dialer.DialContext(probe, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// servingPorts is the port each checkout's ade answers on, by checkout path.
func (s *Server) servingPorts(ctx context.Context) map[string]int {
	found := map[string]int{}
	repos, err := s.repos()
	if err != nil {
		return found
	}

	probe := s.probe
	if probe == nil {
		probe = session.Probe
	}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)
	for _, repo := range repos {
		for _, dir := range repo.Checkouts {
			rec, ok := session.Read(session.Path(sessions.GitDir(dir.Path)))
			if !ok {
				continue
			}
			wg.Add(1)
			go func(path string, rec *session.Record) {
				defer wg.Done()
				if _, live := probe(ctx, rec); !live {
					return
				}
				port := portOf(rec.URL)
				if port <= 0 {
					return
				}
				mu.Lock()
				found[path] = port
				mu.Unlock()
			}(dir.Path, rec)
		}
	}
	wg.Wait()
	return found
}

// notifyPorts wakes every open stream. Called where a server has just been
// started, so the far side has the forward up before the page that asked for
// it has finished reading the reply.
func (s *Server) notifyPorts() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for watcher := range s.watchers {
		select {
		case watcher <- struct{}{}:
		default:
		}
	}
}

func (s *Server) subscribePorts() (<-chan struct{}, func()) {
	watcher := make(chan struct{}, 1)
	s.mu.Lock()
	s.watchers[watcher] = struct{}{}
	s.mu.Unlock()
	return watcher, func() {
		s.mu.Lock()
		delete(s.watchers, watcher)
		s.mu.Unlock()
	}
}

func (s *Server) ownPort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

func (s *Server) setPort(port int) {
	s.mu.Lock()
	s.port = port
	s.mu.Unlock()
}

// portOf reads the port out of a recorded URL.
func portOf(recURL string) int {
	parsed, err := url.Parse(recURL)
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		return 0
	}
	return port
}

func sortedPorts(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for port := range set {
		out = append(out, port)
	}
	sort.Ints(out)
	return out
}
