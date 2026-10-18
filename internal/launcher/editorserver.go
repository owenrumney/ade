package launcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/remote"
	"github.com/owenrumney/ade/internal/settings"
)

// editorServerWait bounds the whole thing: a first install is 120 MiB of curl
// and an untar on a machine that may be swapping.
const editorServerWait = 5 * time.Minute

// editorServerFrom is the only place a tarball may come from: the url arrives
// on a request and ends up in a curl on another machine.
const editorServerFrom = "github.com"

// handleEditorServer is a running editor server on a host for the ade-ssh
// resolver, forwarded to a port here. It connects the host if it has to.
func (s *Server) handleEditorServer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host string `json:"host"`
		// Commit is the editor's own, which the server must be built from, and
		// Template its serverDownloadUrlTemplate with ${os} and ${arch} in it.
		Commit      string `json:"commit"`
		Template    string `json:"template"`
		Unvalidated bool   `json:"unvalidated"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	cfg, err := settings.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	host, ok := cfg.Host(strings.TrimSpace(req.Host))
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not one of your hosts; add it under Settings → Hosts")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), editorServerWait)
	defer cancel()

	// One at a time per machine: two windows resolving at once would both
	// download the tarball and both start a server.
	unlock := s.lockEditorServer(host.Name)
	defer unlock()

	server, err := s.editorServer(ctx, host.Name, req.Commit, req.Template, req.Unvalidated)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, server)
}

// editorServerView is what the resolver turns into a ResolvedAuthority.
type editorServerView struct {
	Port   int    `json:"port"`
	Token  string `json:"token"`
	Commit string `json:"commit"`
}

func (s *Server) editorServer(ctx context.Context, name, commit, template string, unvalidated bool) (editorServerView, error) {
	live := s.connection(name)
	if live == nil {
		if _, err := s.connect(ctx, name); err != nil {
			s.noteHost(name, err)
			return editorServerView{}, err
		}
		s.noteHost(name, nil)
		if live = s.connection(name); live == nil {
			return editorServerView{}, fmt.Errorf("%s connected and went away again", name)
		}
	}

	system, arch, err := live.arch(ctx)
	if err != nil {
		return editorServerView{}, err
	}
	url, err := editorServerURL(template, system, arch)
	if err != nil {
		return editorServerView{}, err
	}
	// A tarball built from another commit cannot share this commit's tree.
	tree := commit
	if unvalidated {
		tree = urlTree(url)
	}
	server, err := live.host.EditorServer(ctx, remote.EditorServerWant{
		Commit: tree, URL: url, Unvalidated: unvalidated,
	})
	if err != nil {
		return editorServerView{}, err
	}

	local, err := freeLocalPort()
	if err != nil {
		return editorServerView{}, err
	}
	mapping := remote.Mapping{Local: local, Remote: server.Port}
	live.setEditor(mapping, server.Commit)
	if err := live.fwd.Sync(ctx, live.want()); err != nil {
		return editorServerView{}, err
	}
	// After the install, which is when the current commit is known.
	if err := live.host.PruneEditorServers(ctx, server.Commit); err != nil {
		slog.Warn("the old editor server trees stay on the host", "host", name, "err", err)
	}
	return editorServerView{Port: mapping.Local, Token: server.Token, Commit: server.Commit}, nil
}

// editorServerURL fills in the template product.json carries, in the host's
// own names: only the host knows which tarball it can run.
func editorServerURL(template, system, arch string) (string, error) {
	filled := strings.NewReplacer(
		"${os}", system, "${arch}", arch,
		"${quality}", "stable", "${version}", "",
	).Replace(strings.TrimSpace(template))
	parsed, err := neturl.Parse(filled)
	if err != nil {
		return "", fmt.Errorf("the editor's server download url is not a url: %s", filled)
	}
	if parsed.Scheme != "https" || parsed.Hostname() != editorServerFrom {
		return "", fmt.Errorf("an editor server is downloaded from %s over https, not %s", editorServerFrom, filled)
	}
	if strings.Contains(filled, "${") {
		return "", fmt.Errorf("the editor's server download url still has a placeholder in it: %s", filled)
	}
	return filled, nil
}

// urlTree keys a tarball ade did not build, so two unvalidated resolves of
// the same one reuse the install.
func urlTree(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])[:40]
}

func (s *Server) lockEditorServer(name string) func() {
	s.mu.Lock()
	lock, ok := s.editorLocks[name]
	if !ok {
		lock = &sync.Mutex{}
		s.editorLocks[name] = lock
	}
	s.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func freeLocalPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find a free port on this machine: %w", err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port, nil
}
