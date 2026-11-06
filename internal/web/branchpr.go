package web

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/render"
)

// branchPRInterval is how often the pull request and its checks are re-read. It
// is far slower than the diff poll because each read is a network round trip
// through gh, and CI does not move second to second.
const branchPRInterval = 30 * time.Second

// commentInterval is slower still: reading review comments is its own paginated
// round trip, and people do not comment every thirty seconds.
const commentInterval = 2 * time.Minute

// BranchPR is the pull request for the current branch, with its checks.
type BranchPR struct {
	Number   int          `json:"number"`
	HeadSHA  string       `json:"headSha,omitempty"`
	Title    string       `json:"title"`
	Author   string       `json:"author"`
	Mine     bool         `json:"mine"`
	URL      string       `json:"url"`
	State    string       `json:"state"`
	Draft    bool         `json:"draft"`
	BaseRef  string       `json:"baseRef"`
	HeadRef  string       `json:"headRef"`
	Body     string       `json:"body,omitempty"`
	BodyHTML string       `json:"bodyHtml,omitempty"`
	Checks   []ghpr.Check `json:"checks,omitempty"`

	// MergeBlocked is why this cannot be merged, or empty when it can be. The
	// reason is worth more than a disabled button with nothing to explain it.
	MergeBlocked string `json:"mergeBlocked,omitempty"`

	// ApproveBlocked and RequestChangesBlocked are the same for the two review
	// verdicts, and Reviews is who has said what so far.
	ApproveBlocked        string        `json:"approveBlocked,omitempty"`
	RequestChangesBlocked string        `json:"requestChangesBlocked,omitempty"`
	ReviewDecision        string        `json:"reviewDecision,omitempty"`
	Reviews               []ghpr.Review `json:"reviews,omitempty"`

	// Failing is how many checks need attention, so the tab can badge it without
	// the client counting.
	Failing int `json:"failing"`

	// CheckedAt is when this was last read, since it is polled slowly enough to
	// be visibly stale.
	CheckedAt time.Time `json:"checkedAt"`
}

// pollBranchPR keeps the Review tab current. It runs on its own timer rather
// than the diff poll, since every pass shells out to gh over the network.
// A fetched snapshot polls too: the pull request is on GitHub whether or not its
// head is checked out, and skipping it left `ade --pr` with no Review tab.
func (s *Server) pollBranchPR(ctx context.Context) {
	s.refreshBranchPR(ctx)
	// A stale origin/main puts commits main already has into the diff.
	if !s.space().snapshot {
		if _, err := s.pullBase(ctx); err != nil {
			slog.Debug("pull the base branch", "err", err)
		}
	}
	s.board.RefreshReviews(ctx)
	s.board.RefreshNotifications(ctx)
	s.importReviewComments(ctx)
	s.board.RefreshTickets(ctx)

	checks := time.NewTicker(branchPRInterval)
	defer checks.Stop()
	remarks := time.NewTicker(commentInterval)
	defer remarks.Stop()
	issues := time.NewTicker(linearInterval)
	defer issues.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-checks.C:
			s.refreshBranchPR(ctx)
			s.board.RefreshReviews(ctx)
			s.board.RefreshNotifications(ctx)
		case <-remarks.C:
			s.importReviewComments(ctx)
		case <-issues.C:
			s.board.RefreshTickets(ctx)
		}
	}
}

// importReviewComments pulls what reviewers said into the diff, the same way
// --pr does. They arrive as ordinary threads, marked with the id they came from
// so a second pass does not duplicate them.
func (s *Server) importReviewComments(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("import review comments panicked", "panic", r)
		}
	}()

	pr := s.currentBranchPR()
	if pr == nil {
		return
	}
	ws := s.space()
	found, err := ghpr.Comments(ctx, ws.homeRoot, pr.Number)
	if err != nil {
		slog.Debug("read review comments", "err", err)
		return
	}
	if n := s.importComments(ghpr.GroupThreads(found)); n > 0 {
		slog.Info("imported review comments", "count", n)
		s.hub.Broadcast("threads", nil)
	}
}

// reviewing is the pull request the current view is of: the one opened from the
// picker if there is one, otherwise this branch's.
func (s *Server) reviewing(ctx context.Context, ws *workspace) (*ghpr.PR, error) {
	if ws.pr != nil {
		return ghpr.Fetch(ctx, ws.homeRoot, ws.pr.Number)
	}
	if ws.snapshot {
		// The diff came from GitHub, so the checked-out branch is not what is on
		// screen; its pull request would describe a diff you are not looking at.
		return nil, nil
	}
	return ghpr.ForBranch(ctx, ws.homeRoot, ws.branch)
}

// refreshBranchPR reads the pull request under review and its checks. A branch with no
// pull request is the ordinary case, and clears what was there before.
func (s *Server) refreshBranchPR(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("branch pr refresh panicked", "panic", r)
		}
	}()

	ws := s.space()

	// Describe whatever is being reviewed, not whatever the branch happens to
	// have. Open somebody else's pull request from the picker and the Review tab
	// has to be about that one, or it describes a diff you are not looking at.
	pr, err := s.reviewing(ctx, ws)
	if err != nil {
		slog.Debug("read pull request", "err", err)
		return
	}
	if pr == nil {
		s.setBranchPR(nil)
		if listed, err := ghpr.List(ctx, ws.homeRoot, 0); err == nil {
			s.setOpenPRs(listed)
		}
		return
	}

	s.adoptPRBase(ctx, ws, pr.BaseRef)

	found, err := ghpr.Checks(ctx, ws.homeRoot, pr.Number)
	if err != nil {
		slog.Debug("read pr checks", "err", err)
	}

	// The open list comes on the same pass: the Review tab is where you pick
	// somebody else's pull request, and it cannot offer that if it has to be
	// visible before anything asks for the list.
	if listed, err := ghpr.List(ctx, ws.homeRoot, 0); err == nil {
		s.setOpenPRs(listed)
	} else {
		slog.Debug("list pull requests", "err", err)
	}

	viewer := s.who(ctx, ws.homeRoot)
	current := &BranchPR{
		Number: pr.Number, HeadSHA: pr.HeadSHA, Title: pr.Title, Author: pr.Author, URL: pr.URL,
		Mine:  viewer != "" && strings.EqualFold(pr.Author, viewer),
		State: pr.State, Draft: pr.Draft, BaseRef: pr.BaseRef, HeadRef: pr.HeadRef,
		Body: pr.Body, BodyHTML: markdownHTML(pr.Body),
		Checks: found, Failing: countFailing(found), CheckedAt: time.Now(),
		MergeBlocked:          pr.MergeBlocked(),
		ApproveBlocked:        pr.ApproveBlocked(viewer),
		RequestChangesBlocked: pr.RequestChangesBlocked(viewer),
		ReviewDecision:        pr.ReviewDecision,
		Reviews:               pr.Reviews,
	}
	s.setBranchPR(current)
}

// adoptPRBase diffs against the base the pull request is against, so a branch
// stacked on another branch is not diffed against main.
func (s *Server) adoptPRBase(ctx context.Context, ws *workspace, baseRef string) {
	if ws.pr != nil || ws.snapshot || s.startBase != "" || baseRef == "" {
		return
	}
	// The base is the home branch's, and the diff is not showing it.
	if ws.viewRoot != "" {
		return
	}
	want := gitdiff.RemoteBase(ctx, ws.homeRoot, baseRef)
	if want == "" || gitdiff.SameBase(ctx, ws.homeRoot, want, ws.opts.Base) {
		return
	}

	opts := ws.opts
	opts.Base = want
	next := &workspace{
		homeRoot: ws.homeRoot, branch: ws.branch, gitDir: ws.gitDir,
		opts: opts, store: ws.store, source: gitSource(opts, false),
	}
	if err := s.swap(ctx, next); err != nil {
		slog.Debug("adopt the pull request base", "err", err)
		return
	}
	slog.Info("diffing against the pull request base", "base", want)
	s.hub.Broadcast("state", nil)
	s.hub.Broadcast("threads", nil)
}

func countFailing(found []ghpr.Check) int {
	n := 0
	for _, check := range found {
		if check.Failed() {
			n++
		}
	}
	return n
}

func (s *Server) setBranchPR(pr *BranchPR) {
	s.mu.Lock()
	changed := branchPRChanged(s.branchPR, pr)
	s.branchPR = pr
	s.mu.Unlock()
	if changed {
		s.hub.Broadcast("state", nil)
	}
}

// branchPRChanged ignores CheckedAt, so a poll that finds nothing new does not
// make the browser refetch every thirty seconds.
func branchPRChanged(before, after *BranchPR) bool {
	if before == nil || after == nil {
		return before != after
	}
	if before.Number != after.Number || before.State != after.State ||
		before.Draft != after.Draft || before.Title != after.Title ||
		before.Body != after.Body || before.MergeBlocked != after.MergeBlocked ||
		before.ApproveBlocked != after.ApproveBlocked ||
		before.RequestChangesBlocked != after.RequestChangesBlocked ||
		before.ReviewDecision != after.ReviewDecision ||
		len(before.Reviews) != len(after.Reviews) ||
		len(before.Checks) != len(after.Checks) {
		return true
	}
	for i, check := range before.Checks {
		if check != after.Checks[i] {
			return true
		}
	}
	return false
}

func (s *Server) currentBranchPR() *BranchPR {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.branchPR
}

// handleRefreshChecks re-reads the pull request now, for when you have just
// pushed and do not want to wait out the poll.
func (s *Server) handleRefreshChecks(w http.ResponseWriter, r *http.Request) {
	s.refreshBranchPR(r.Context())
	s.importReviewComments(r.Context())
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{"pr": s.currentBranchPR()})
}

func (s *Server) setOpenPRs(listed []ghpr.Listed) {
	s.mu.Lock()
	changed := len(listed) != len(s.openPRs)
	for i := range listed {
		if !changed && listed[i] != s.openPRs[i] {
			changed = true
		}
	}
	s.openPRs = listed
	s.mu.Unlock()
	if changed {
		s.hub.Broadcast("state", nil)
	}
}

func (s *Server) currentOpenPRs() []ghpr.Listed {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ghpr.Listed(nil), s.openPRs...)
}

// markdownHTML renders a pull request's body for the page.
func markdownHTML(body string) string {
	html, err := render.Markdown([]byte(body), render.Options{Theme: render.ThemeDark})
	if err != nil {
		return ""
	}
	return string(html)
}
