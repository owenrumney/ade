package web

import (
	"context"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/audit"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/ship"
	"github.com/owenrumney/ade/internal/verify"
)

// shipTTL is the branch poll's own thirty seconds: every read behind it is a
// round trip through gh.
const shipTTL = 30 * time.Second

// shipCache is the pull request of a repository that is not this session's, by
// root and branch. The branch's own is already polled into s.branchPR.
type shipCache struct {
	mu   sync.Mutex
	seen map[string]shipSeen
}

type shipSeen struct {
	at time.Time
	pr *ship.PR
}

// ShipState is one page for the whole session: a column per repository, and
// the gate rail beside them.
type ShipState struct {
	Projects []ship.Project `json:"projects"`
	Gates    []ship.Gate    `json:"gates"`

	// Home and View are this session's checkout and the one the Diff tab is on.
	// Commit and push act on View, so the page says which that is.
	Home string `json:"home"`
	View string `json:"view,omitempty"`

	// Draft is the commit message the agent has written, which describes the
	// index of View and nowhere else.
	Draft *CommitDraft `json:"draft,omitempty"`

	// Running is whether the agent has the session, so a card that would start
	// a run can say why it cannot.
	Running bool `json:"running"`
}

// handleShip answers every repository this session touches in one call.
func (s *Server) handleShip(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.shipState(r.Context()))
}

func (s *Server) shipState(ctx context.Context) ShipState {
	ws := s.space()
	repos := s.shipRepos(ws)
	projects := make([]ship.Project, 0, len(repos))
	for _, repo := range repos {
		projects = append(projects, ship.Read(ctx, repo, s.shipLook))
	}
	return ShipState{
		Projects: projects,
		Gates:    ship.Standard().Run(s.shipSession(projects)),
		Home:     ws.homeRoot,
		View:     ws.viewRoot,
		Draft:    s.currentDraft(),
		Running:  s.agent.State().Running,
	}
}

// shipRepos is the repository list, which is the diff's own: a session with
// one checkout has no list at all, and is its own single column.
func (s *Server) shipRepos(ws *workspace) []ship.Repo {
	roots := s.currentRoots()
	if len(roots) == 0 {
		if ws.homeRoot == "" {
			return nil
		}
		return []ship.Repo{{
			Root: ws.homeRoot, Name: filepath.Base(ws.homeRoot),
			Branch: ws.branch, Home: true,
		}}
	}
	out := make([]ship.Repo, 0, len(roots))
	for _, root := range roots {
		out = append(out, ship.Repo{
			Root: root.Root, Name: root.Name, Branch: root.Branch, Home: root.Home,
		})
	}
	return out
}

// shipLook is the pull request for one repository. This session's own comes
// from the poll that already reads it; anything else is read here and kept.
func (s *Server) shipLook(ctx context.Context, repo ship.Repo) *ship.PR {
	if repo.Home {
		return shipPR(s.currentBranchPR(), repo.Branch)
	}
	if repo.Branch == "" || repo.Branch == "detached" {
		return nil
	}

	key := repo.Root + "\x00" + repo.Branch
	s.ships.mu.Lock()
	if seen, ok := s.ships.seen[key]; ok && time.Since(seen.at) < shipTTL {
		s.ships.mu.Unlock()
		return seen.pr
	}
	s.ships.mu.Unlock()

	found, err := ghpr.ForBranch(ctx, repo.Root, repo.Branch)
	if err != nil {
		slog.Debug("read another repository's pull request", "root", repo.Root, "err", err)
		return nil
	}
	var checks []ghpr.Check
	if found != nil {
		if checks, err = ghpr.Checks(ctx, repo.Root, found.Number); err != nil {
			slog.Debug("read another repository's checks", "root", repo.Root, "err", err)
		}
	}

	pr := ship.FromPR(found, checks)
	s.ships.mu.Lock()
	if s.ships.seen == nil {
		s.ships.seen = map[string]shipSeen{}
	}
	s.ships.seen[key] = shipSeen{at: time.Now(), pr: pr}
	s.ships.mu.Unlock()
	return pr
}

// shipPR is the branch's own pull request in the page's terms. Nil when the
// poll is describing somebody else's, which is not this branch's state.
func shipPR(pr *BranchPR, branch string) *ship.PR {
	if pr == nil || (branch != "" && pr.HeadRef != "" && pr.HeadRef != branch) {
		return nil
	}
	failing, line := ship.Failing(pr.Checks)
	return &ship.PR{
		Number: pr.Number, Title: pr.Title, URL: pr.URL, State: pr.State,
		Draft: pr.Draft, BaseRef: pr.BaseRef,
		ReviewDecision: pr.ReviewDecision, Reviews: pr.Reviews,
		Checks: pr.Checks, Failing: failing, FailLine: line,
		MergeBlocked: pr.MergeBlocked,
	}
}

// shipSession is what a gate may read, every field of it something ade had
// already read for its own reasons.
func (s *Server) shipSession(projects []ship.Project) ship.Session {
	session := ship.Session{Projects: projects}
	if store := s.space().store; store != nil {
		session.Threads = len(store.Unresolved())
	}

	// The rail is about the whole change, not an index someone is mid-staging.
	report := s.ledgerReport()
	session.Ledger.Unread, session.Ledger.Agent = report.Unread, report.Agent

	if config, err := verify.Load(s.verifyPath()); err == nil {
		session.Tests.Configured = len(config.Checks) > 0
	}
	s.mu.RLock()
	last, running := s.verifyLast, s.verifyRunning
	s.mu.RUnlock()
	session.Tests.Running = running
	if last != nil && !last.Narrow {
		session.Tests.Ran, session.Tests.Passed = true, last.Passed()
		if last.Failed != nil {
			session.Tests.Failed = last.Failed.Name
		}
	}

	if file := s.currentDoc(); file != "" {
		session.Design.Open, session.Design.File = true, file
		notes, err := s.notesNow()
		if err != nil {
			slog.Debug("read the design's notes for the gate rail", "err", err)
		}
		for _, note := range notes {
			if !note.Resolved {
				session.Design.Unresolved++
			}
		}
	}
	return session
}

// shipVeto is why a step is refused, or "" to let it through. anyway is the
// override, which is recorded rather than silently honoured.
func (s *Server) shipVeto(ctx context.Context, step ship.Step, anyway bool) string {
	ws := s.space()
	home := ws.viewRoot == ""
	// ws.branch is the home checkout's. Another repository's is read by
	// ship.Read, which looks it up when the branch is empty.
	branch := ""
	if home {
		branch = ws.branch
	}
	project := ship.Read(ctx, ship.Repo{
		Root: ws.diffRoot(), Name: filepath.Base(ws.diffRoot()),
		Branch: branch, Home: home,
	}, nil)
	why := ship.Standing().Reason(step, s.shipSession(nil), project)
	if why == "" {
		return ""
	}
	if !anyway {
		return why
	}
	audit.Note(audit.Entry{
		Kind: "override", What: string(step), Where: project.Name, Why: why, From: "ade",
	})
	return ""
}
