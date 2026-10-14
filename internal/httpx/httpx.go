// Package httpx holds the local-server plumbing shared by browser-backed tools.
package httpx

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net"
	"net/http"
	"sync"
	"time"
)

// Hub fans server-side events out to connected browsers.
type Hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
	done    chan struct{}
}

// client is one browser's stream. frames is what it has yet to be sent;
// lagged is raised when a frame had to be dropped because frames was full,
// so the stream can tell the page to fetch what it missed.
type client struct {
	frames chan string
	lagged chan struct{}
}

// Listen binds a listener and returns its loopback URL. key picks the port
// tried first, so a browser's per-origin decisions hold across restarts.
func Listen(key string) (net.Listener, string, error) {
	if ln, url, err := listenOn(StablePort(key)); err == nil {
		return ln, url, nil
	}
	ln, url, err := listenOn(0)
	if err != nil {
		return nil, "", fmt.Errorf("listen: %w", err)
	}
	return ln, url, nil
}

// ListenFixed binds the port key hashes to and fails rather than moving: a
// browser grants notification permission per origin, and the port is in one.
func ListenFixed(key string) (net.Listener, string, error) {
	port := StablePort(key)
	ln, url, err := listenOn(port)
	if err != nil {
		return nil, "", fmt.Errorf("port %d is taken by something else; the launcher has to have that one, so free it and start again: %w", port, err)
	}
	return ln, url, nil
}

// ListenOn binds one named port and fails rather than moving: a caller that
// named a port wants that one, or to be told why not.
func ListenOn(port int) (net.Listener, string, error) {
	ln, url, err := listenOn(port)
	if err != nil {
		return nil, "", fmt.Errorf("port %d is taken by something else: %w", port, err)
	}
	return ln, url, nil
}

func listenOn(port int) (net.Listener, string, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", BindHost(), port))
	if err != nil {
		return nil, "", err
	}
	// The url is always loopback: a wide bind is a container's wildcard, and
	// the reader is the laptop that published the port to its own loopback.
	_, got, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		_ = ln.Close()
		return nil, "", err
	}
	return ln, fmt.Sprintf("http://%s:%s", loopback, got), nil
}

// StablePort is the port a key is offered first: a hash of it into
// 20000–39999, clear of the well-known ports and of what dev servers tend to
// take.
func StablePort(key string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return 20000 + int(h.Sum32()%20000)
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*client]struct{}), done: make(chan struct{})}
}

// Bye tells every page why the server is going, and then closes. A stream
// that just stops leaves a tab retrying a port nothing is listening on, with
// no way to tell a restart from a shutdown; this is the difference.
//
// Nothing is broadcast after a farewell, so this is the last thing a hub does.
func (h *Hub) Bye(reason string) {
	h.Broadcast("bye", map[string]string{"reason": reason})
	h.Close()
}

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-h.done:
	default:
		close(h.done)
	}
}

// Broadcast sends a named event to every client and reports how many got it.
// A client whose buffer is full misses the frame, and is told so with a
// resync event once its stream drains: a busy run can amend one message
// faster than a throttled tab reads, and a message that never arrives is
// worse than one fetched again.
func (h *Hub) Broadcast(event string, data any) int {
	payload, err := json.Marshal(data)
	if err != nil {
		return 0
	}
	frame := fmt.Sprintf("event: %s\ndata: %s\n\n", event, payload)

	h.mu.Lock()
	defer h.mu.Unlock()
	sent := 0
	for c := range h.clients {
		select {
		case c.frames <- frame:
			sent++
		default:
			select {
			case c.lagged <- struct{}{}:
			default:
			}
		}
	}
	return sent
}

// KeepAlive is how often a silent stream says something. A named event and
// not a comment, because EventSource hides comments from the page.
var KeepAlive = 20 * time.Second

func (h *Hub) HandleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	c := h.subscribe()
	defer h.unsubscribe(c)

	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(KeepAlive)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.done:
			// The farewell is queued and the hub is already closed, so
			// whatever is left has to go out ahead of the stream.
			for {
				select {
				case frame := <-c.frames:
					_, _ = fmt.Fprint(w, frame)
				default:
					flusher.Flush()
					return
				}
			}
		case frame := <-c.frames:
			_, _ = fmt.Fprint(w, frame)
			flusher.Flush()
		case <-c.lagged:
			_, _ = fmt.Fprint(w, "event: resync\ndata: {}\n\n")
			flusher.Flush()
		case <-keepalive.C:
			_, _ = fmt.Fprint(w, "event: ping\ndata: {}\n\n")
			flusher.Flush()
		}
	}
}

// Clients reports how many browsers are attached. A caller deciding whether it
// is idle needs this without having to broadcast something to find out.
func (h *Hub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *Hub) subscribe() *client {
	c := &client{frames: make(chan string, 64), lagged: make(chan struct{}, 1)}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c
}

func (h *Hub) unsubscribe(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	close(c.frames)
}
