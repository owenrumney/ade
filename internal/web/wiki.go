package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/wiki"
)

// wikiStore opens the shared wiki for this run, or nil when no remote is set.
func wikiStore() *wiki.Store {
	config, err := settings.Load()
	if err != nil || config.WikiRemote == "" {
		return nil
	}
	dir, err := settings.Dir()
	if err != nil {
		return nil
	}
	return wiki.New(dir, config.WikiRemote)
}

// handleWikiMCP is ADE's own MCP endpoint. The draft id in the Authorization
// header is the capability; no session token gets near the sandbox.
func (s *Server) handleWikiMCP(w http.ResponseWriter, r *http.Request) {
	if s.wiki == nil {
		http.Error(w, "wiki is disabled", http.StatusNotFound)
		return
	}
	wiki.MCP{Store: s.wiki, Draft: s.agent.draftID(), Agent: s.agent.providerName}.ServeHTTP(w, r)
}

func (a *agentRunner) draftID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.wikiDraft
}

func (a *agentRunner) providerName() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.provider
}

// wikiDraftFor seeds this session's draft. An unreachable or disabled wiki is
// not a reason to fail a run, so a failure is only logged.
func wikiDraftFor(ctx context.Context, store *wiki.Store, id, dir string) (wiki.Draft, bool) {
	if store == nil || id == "" || !store.Enabled() {
		return wiki.Draft{}, false
	}
	draft, err := store.Draft(ctx, id, dir)
	if err != nil {
		slog.Warn("no wiki draft for this session; its edits will not publish", "err", err)
		return wiki.Draft{}, false
	}
	return draft, true
}

// wikiMounts is the draft's two scopes, and nothing else beneath it: the
// agent never sees the canonical clone or another repository's notes.
func wikiMounts(draft wiki.Draft) ([]sandbox.Mount, map[string]string) {
	mounts := []sandbox.Mount{}
	env := map[string]string{}
	for key, dir := range map[string]string{
		"ADE_WIKI_GENERAL": "general",
		"ADE_WIKI_REPO":    filepath.Join("repos", filepath.FromSlash(draft.Repo)),
	} {
		path := filepath.Join(draft.Root, dir)
		if err := os.MkdirAll(path, 0o700); err != nil {
			continue
		}
		mounts = append(mounts, sandbox.Mount{Host: path, Guest: path, RW: true})
		env[key] = path
	}
	return mounts, env
}

// wikiServers is ADE's MCP entry for a sandboxed run. base is this server's
// own address, which the box rewrites to whatever the container calls it.
func wikiServers(base string, draft wiki.Draft) map[string]json.RawMessage {
	if base == "" {
		return nil
	}
	return wiki.ServerConfig(base, draft.Session)
}

// workerBase is base from a worker's box, whose loopback is the hub's.
func workerBase(rt sandbox.Runtime, base string) string {
	return rehost(base, workerHost(rt))
}
