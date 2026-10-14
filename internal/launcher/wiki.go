package launcher

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/render"
)

const wikiRefreshAfter = time.Minute

// refreshWiki asks for one background fetch. A failed fetch leaves the status
// stale, so without the flag every later search would queue another worker.
func (s *Server) refreshWiki() {
	if !s.wikiSyncing.CompareAndSwap(false, true) {
		return
	}
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		defer s.wikiSyncing.Store(false)
		_ = s.wiki.Sync(ctx)
	}()
}

func (s *Server) handleWikiSearch(w http.ResponseWriter, r *http.Request) {
	if s.wiki == nil || !s.wiki.Enabled() {
		writeJSON(w, http.StatusOK, map[string]any{"hits": []any{}, "status": "disabled"})
		return
	}
	hits := s.wiki.Search(r.URL.Query().Get("q"))
	st := s.wiki.CachedStatus()
	if st.LastFetch.IsZero() || time.Since(st.LastFetch) > wikiRefreshAfter {
		s.refreshWiki()
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits, "stale": st.LastError, "revision": st.Revision})
}

func (s *Server) handleWikiPage(w http.ResponseWriter, r *http.Request) {
	if s.wiki == nil {
		writeError(w, http.StatusNotFound, "wiki is disabled")
		return
	}
	path := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/wiki/page/"))
	if path == "" {
		writeError(w, http.StatusNotFound, "wiki page not found")
		return
	}
	data, err := s.wiki.Page(path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	// Unsafe stays off: a wiki page is someone else's Markdown, and raw HTML
	// in it must render as text.
	html, err := render.Markdown(data, render.Options{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The markdown as well as the render: an editor opens the source, where a
	// browser can only show the page.
	writeJSON(w, http.StatusOK, map[string]string{
		"path": path, "html": string(html), "markdown": string(data),
	})
}
