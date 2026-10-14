package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/coder/websocket"

	"github.com/owenrumney/ade/internal/httpx"
	"github.com/owenrumney/ade/internal/term"
)

// termControl is what the browser says that is not a keystroke. Keystrokes are
// binary frames and carry no envelope at all, so a fast typist costs no
// encoding; only the rare message needs a name.
type termControl struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// termReadLimit caps one inbound frame. A keystroke is a byte and a paste is
// rarely more than a few pages, so this is generous.
const termReadLimit = 1 << 20

// termPaneLimit is how many shells one checkout may have open at once.
const termPaneLimit = 16

// termPaneName is what a pane id may look like: the browser makes them, so the
// map key they become is checked before it is used.
var termPaneName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// handleTerminal attaches a browser to the shell running in the checkout.
//
// The shell is the machine's, not the sandbox's: this is the terminal you would
// have opened next to the editor, and putting it inside the agent's container
// would give you a different filesystem to the one the Diff tab is showing.
// Reaching it needs the session token, as every other endpoint does, and
// websockets are exempt from the same-origin rules that protect the rest — so
// the origin is checked here as well as the host.
func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if !httpx.LoopbackOrigin(origin) {
		slog.Warn("terminal refused an origin", "origin", origin, "host", r.Host)
		http.Error(w, "that origin may not open a terminal", http.StatusForbidden)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		// A refusal here is invisible to the pane, which only sees no socket.
		slog.Warn("terminal refused a connection", "origin", origin, "host", r.Host, "err", err)
		return
	}
	defer conn.CloseNow() //nolint:errcheck // closing a dead socket is not news
	conn.SetReadLimit(termReadLimit)

	key, err := paneKey(r.URL.Query().Get("pane"), r.URL.Query().Get("kind"))
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	var session *term.Session
	if key == "editor" {
		session, err = s.editor()
	} else {
		session, err = s.pane(key)
	}
	if err != nil {
		_ = conn.Close(websocket.StatusTryAgainLater, err.Error())
		return
	}

	id, replay, out, err := session.Attach()
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, err.Error())
		return
	}
	defer session.Detach(id)

	ctx, stop := context.WithCancel(r.Context())
	defer stop()

	// What is already on the screen, before anything new: a reload comes up to
	// the session it left rather than to a blank rectangle.
	if len(replay) > 0 {
		if err := conn.Write(ctx, websocket.MessageBinary, replay); err != nil {
			return
		}
	}

	go s.termWrite(ctx, stop, conn, out)
	s.termRead(ctx, conn, session)
}

// termRead feeds the shell until the browser goes away.
func (s *Server) termRead(ctx context.Context, conn *websocket.Conn, session *term.Session) {
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		if kind == websocket.MessageBinary {
			if err := session.Write(data); err != nil {
				return
			}
			continue
		}

		var msg termControl
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Type == "resize" {
			if err := session.Resize(msg.Cols, msg.Rows); err != nil && !errors.Is(err, term.ErrGone) {
				slog.Debug("terminal resize", "err", err)
			}
		}
	}
}

// termWrite pushes the shell's output at the browser. The channel closing means
// the shell exited, or that this browser fell too far behind to be kept: either
// way the socket closes and the page reconnects, which replays the screen.
func (s *Server) termWrite(ctx context.Context, stop context.CancelFunc, conn *websocket.Conn, out <-chan []byte) {
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-out:
			if !ok {
				_ = conn.Close(websocket.StatusNormalClosure, "shell exited")
				return
			}
			if err := conn.Write(ctx, websocket.MessageBinary, chunk); err != nil {
				return
			}
		}
	}
}

// pane returns the shell behind one pane, made on first use and keyed by the
// browser's pane id, so a split, a second tab and a reload each find their own.
func (s *Server) pane(key string, argv ...string) (*term.Session, error) {
	root := s.space().homeRoot

	s.termMu.Lock()
	defer s.termMu.Unlock()

	if s.terms == nil {
		s.terms = map[string]*term.Session{}
	}
	// The checkout moved: a prompt in a directory the page is no longer showing
	// is worse than a new one.
	if s.termRoot != root {
		for k, sess := range s.terms {
			sess.Close()
			delete(s.terms, k)
		}
		s.termRoot = root
	}
	if sess := s.terms[key]; sess != nil {
		return sess, nil
	}
	// A bound on how many shells one page can fork, since the id comes from the
	// browser and nothing else limits how many it asks for.
	if len(s.terms) >= termPaneLimit {
		return nil, fmt.Errorf("too many terminals open (limit %d)", termPaneLimit)
	}

	sess := term.New(root)
	if len(argv) > 0 {
		sess = term.NewCommand(root, argv...)
	}
	s.terms[key] = sess
	return sess, nil
}

// terminal is the default pane's shell, which is what a page with no layout
// saved yet asks for.
func (s *Server) terminal() *term.Session {
	key, _ := paneKey("", "") //nolint:errcheck // an empty id is always allowed
	sess, _ := s.pane(key)    //nolint:errcheck // the first pane is always under the limit
	return sess
}

// editor refuses rather than falling back to a shell: a machine with no editor
// would otherwise retry an exec of a binary that is not there, once a second.
func (s *Server) editor() (*term.Session, error) {
	argv := term.Editor()
	if len(argv) == 0 {
		return nil, errors.New("no editor on the machine this session runs on: install vim, or set $EDITOR")
	}
	return s.pane("editor", argv...)
}

// handleTerminalClose kills one pane's shell. Closing the socket only detaches,
// because a reload does that too; this is the pane being closed for good.
func (s *Server) handleTerminalClose(w http.ResponseWriter, r *http.Request) {
	key, err := paneKey(r.URL.Query().Get("pane"), "")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.termMu.Lock()
	sess := s.terms[key]
	delete(s.terms, key)
	s.termMu.Unlock()

	if sess != nil {
		sess.Close()
	}
	w.WriteHeader(http.StatusNoContent)
}

// paneKey namespaces the browser's pane id so that it cannot collide with the
// editor's, and refuses anything that is not one of ours.
func paneKey(id, kind string) (string, error) {
	if kind == "editor" {
		return "editor", nil
	}
	if id != "" && !termPaneName.MatchString(id) {
		return "", errors.New("bad terminal pane id")
	}
	return "pane:" + id, nil
}

// closeTerminal kills every shell and anything they started. Without it a
// server left running in the terminal outlives the ade that opened it.
func (s *Server) closeTerminal() {
	s.termMu.Lock()
	defer s.termMu.Unlock()
	for key, sess := range s.terms {
		sess.Close()
		delete(s.terms, key)
	}
}
