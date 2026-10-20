package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/archive"
	"github.com/owenrumney/ade/internal/jobs"
	"github.com/owenrumney/ade/internal/settings"
)

// prunableTimeout bounds one repository's read-only sweep. It is a `gh pr
// list` and a directory listing: a repo that hangs hangs its own row.
const prunableTimeout = 2 * time.Minute

// pruneArchiveTimeout covers one document's distillation and push, as the
// checkout server's own archive does.
const pruneArchiveTimeout = 5 * time.Minute

// errArchiveBusy is what a caller is told when the scheduled design-archive
// job held the gate for longer than the caller was willing to wait.
var errArchiveBusy = errors.New("the design-archive job is archiving right now; try again in a moment")

// designRepo is one repository the prune panel draws a group for.
type designRepo struct {
	Repo string `json:"repo"`
	Root string `json:"root"`
}

// wikiState is whether there is anywhere to archive to, said once up front
// rather than as a row failure per document.
type wikiState struct {
	Enabled bool   `json:"enabled"`
	Why     string `json:"why,omitempty"`
}

// prunable is one repository's answer: what merged, what vanished unmerged,
// and the documents no branch ever matched. Error is set instead of the three.
type prunable struct {
	Repo      string              `json:"repo"`
	Merged    []archive.Candidate `json:"merged"`
	Abandoned []archive.Candidate `json:"abandoned"`
	Unmatched []archive.Stale     `json:"unmatched"`
	Error     string              `json:"error,omitempty"`
	Wiki      wikiState           `json:"wiki"`
}

// handleDesignRepos names every repository the launcher holds designs for,
// with the wiki state. It answers from the scan, so it calls nothing.
func (s *Server) handleDesignRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []designRepo{}
	for _, target := range jobs.ArchiveTargets(r.Context(), repos) {
		out = append(out, designRepo{Repo: target.Repo, Root: target.Root})
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": out, "wiki": s.wikiState()})
}

// handleDesignsPrunable is one repository's sweep: no model, no writes, safe
// to call as the panel opens. A repo it cannot sweep answers with its reason.
func (s *Server) handleDesignsPrunable(w http.ResponseWriter, r *http.Request) {
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	if repo == "" {
		writeError(w, http.StatusBadRequest, "repo is required")
		return
	}
	target, ok := s.archiveTarget(r.Context(), repo)
	if !ok {
		writeError(w, http.StatusNotFound, "ade holds no checkout of "+repo)
		return
	}
	out := prunable{
		Repo:      repo,
		Merged:    []archive.Candidate{},
		Abandoned: []archive.Candidate{},
		Unmatched: []archive.Stale{},
		Wiki:      s.wikiState(),
	}
	ctx, cancel := context.WithTimeout(r.Context(), prunableTimeout)
	defer cancel()
	state, err := s.lookAt(ctx, target)
	if err != nil {
		out.Error = err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	merged, abandoned := archive.Sweep(state)
	out.Merged = append(out.Merged, merged...)
	out.Abandoned = append(out.Abandoned, abandoned...)
	out.Unmatched = archive.Unmatched(state)
	writeJSON(w, http.StatusOK, out)
}

// handleDesignArchive publishes one document and deletes the local file. The
// session server's own works only on the checkout it was started in.
func (s *Server) handleDesignArchive(w http.ResponseWriter, r *http.Request) {
	var asked struct {
		Repo string `json:"repo"`
		File string `json:"file"`
	}
	if err := json.NewDecoder(r.Body).Decode(&asked); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	repo, file := strings.TrimSpace(asked.Repo), strings.TrimSpace(asked.File)
	if repo == "" || file == "" {
		writeError(w, http.StatusBadRequest, "name a repository and a document")
		return
	}
	target, ok := s.archiveTarget(r.Context(), repo)
	if !ok {
		writeError(w, http.StatusNotFound, "ade holds no checkout of "+repo)
		return
	}
	store := s.distillStore()
	if store == nil || !store.Enabled() {
		writeError(w, http.StatusBadRequest, "wiki is disabled; run ade wiki init --remote <url>")
		return
	}
	model, err := s.archiveRunner()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Not the request's context: a closed panel must not cancel a publish
	// half-way through, where the page is out and the document is still here.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), pruneArchiveTimeout)
	defer cancel()
	// The job runs on a schedule nobody here controls: without the gate the
	// two can take the same document, and the loser gets a push conflict.
	release, err := s.holdArchive(ctx)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	defer release()

	doc, err := archive.Find(target.Root, target.CommonGitDir, "", file)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	out, err := archive.One(ctx, store, target.Repo, doc, archive.PRFor(ctx, target.Root, doc.File), model)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.refreshWiki()
	writeJSON(w, http.StatusOK, out)
}

// wikiState is whether a prune has anywhere to publish to.
func (s *Server) wikiState() wikiState {
	if store := s.distillStore(); store != nil && store.Enabled() {
		return wikiState{Enabled: true}
	}
	return wikiState{Why: "wiki is disabled; run ade wiki init --remote <url>"}
}

// archiveTarget is the checkout one owner/name is held in, from the same list
// the design-archive job sweeps.
func (s *Server) archiveTarget(ctx context.Context, repo string) (jobs.ArchiveTarget, bool) {
	all, err := s.repos()
	if err != nil {
		return jobs.ArchiveTarget{}, false
	}
	for _, target := range jobs.ArchiveTargets(ctx, all) {
		if strings.EqualFold(target.Repo, repo) {
			return target, true
		}
	}
	return jobs.ArchiveTarget{}, false
}

// lookAt reads one checkout's state. nil look is the real sweep; a test
// stands one in so the mapping is testable without a gh.
func (s *Server) lookAt(ctx context.Context, target jobs.ArchiveTarget) (archive.State, error) {
	if s.look != nil {
		return s.look(ctx, target)
	}
	return archive.Look(ctx, target.Root, target.CommonGitDir, "")
}

// archiveRunner is the one-shot an archive distils with: the job's if a test
// stood one in, else the real cheap model outside every checkout.
func (s *Server) archiveRunner() (archive.Runner, error) {
	if s.jobs.archive != nil {
		return s.jobs.archive, nil
	}
	dir, err := settings.Dir()
	if err != nil {
		return nil, err
	}
	return jobs.ArchiveModel("", dir), nil
}

// holdArchive takes the archive gate. The job holds it for a whole fire, so
// a caller can wait minutes; one that gives up first is told who has it.
func (s *Server) holdArchive(ctx context.Context) (func(), error) {
	select {
	case s.archiving <- struct{}{}:
		return func() { <-s.archiving }, nil
	case <-ctx.Done():
		return nil, errArchiveBusy
	}
}
