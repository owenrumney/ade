package launcher

import (
	"cmp"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/sessions"
)

var nameWithOwner = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// handlePR is one pull request in full, with its checks: what a window
// shows when one is picked, with no session server behind it.
func (s *Server) handlePR(w http.ResponseWriter, r *http.Request) {
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	number, err := strconv.Atoi(r.URL.Query().Get("number"))
	if repo == "" || err != nil || number <= 0 {
		writeError(w, http.StatusBadRequest, "repo and number are required")
		return
	}
	repos, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A checkout tells gh which forge and credentials to use; a review
	// request from a repository never cloned here is asked for by name.
	fetch := func() (*ghpr.PR, error) { return ghpr.FetchRepo(r.Context(), repo, number) }
	read := func() ([]ghpr.Check, error) { return ghpr.ChecksRepo(r.Context(), repo, number) }
	if dir, ok := checkoutNamed(repos, repo); ok {
		fetch = func() (*ghpr.PR, error) { return ghpr.Fetch(r.Context(), dir, number) }
		read = func() ([]ghpr.Check, error) { return ghpr.Checks(r.Context(), dir, number) }
	} else if !nameWithOwner.MatchString(repo) {
		writeError(w, http.StatusBadRequest, "repo must be owner/name")
		return
	}
	pr, err := fetch()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// Checks are a second read, and a failure there is not a failure to
	// show the pull request: the page says what it has.
	checks, err := read()
	if err != nil {
		checks = nil
	}
	writeJSON(w, http.StatusOK, map[string]any{"pr": pr, "checks": checks})
}

// checkoutNamed resolves owner/name to the main checkout on this machine.
// The owner is matched when it is known and ignored when the scan has none.
func checkoutNamed(repos []sessions.Repo, nameWithOwner string) (string, bool) {
	owner, name, ok := strings.Cut(strings.TrimSpace(nameWithOwner), "/")
	if !ok {
		owner, name = "", strings.TrimSpace(nameWithOwner)
	}
	if name == "" {
		return "", false
	}
	loose := ""
	for _, repo := range repos {
		if !strings.EqualFold(repo.Name, name) {
			continue
		}
		main := mainOf(repo)
		if main == "" {
			continue
		}
		if owner == "" || strings.EqualFold(repo.Owner, owner) {
			return main, true
		}
		if loose == "" {
			loose = main
		}
	}
	return loose, loose != ""
}

// mainOf is the repository's main checkout, which is where a worktree is
// made beside and where its documents used to live.
func mainOf(repo sessions.Repo) string {
	for _, checkout := range repo.Checkouts {
		if !checkout.Worktree {
			return checkout.Path
		}
	}
	if len(repo.Checkouts) > 0 {
		return cmp.Or(repo.Checkouts[0].Main, repo.Checkouts[0].Path)
	}
	return ""
}
