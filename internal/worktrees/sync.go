package worktrees

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/gitdiff"
)

// What a sync did to one checkout.
const (
	OutcomeFastForwarded = "fast-forwarded"
	OutcomeRebased       = "rebased"
	OutcomeUpToDate      = "up to date"
	OutcomeSkipped       = "skipped"
	OutcomeConflicts     = "conflicts"
	OutcomeWouldRebase   = "would rebase"

	// A dry run says what it would do: naming the outcome "fast-forwarded"
	// would read, and colour, as work already done.
	OutcomeWouldFastForward = "would fast-forward"
)

// Result is what happened to one checkout, or would have on a dry run.
type Result struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Branch  string `json:"branch,omitempty"`
	Main    bool   `json:"main,omitempty"`
	Outcome string `json:"outcome"`
	Detail  string `json:"detail,omitempty"`
}

// Report is one sync: the ref everything was measured against, how much it
// moved, and a line per checkout.
type Report struct {
	// Base is the ref the sync rebased onto — origin's copy when there is one.
	Base string `json:"base"`
	// Fetched is how many commits the fetch brought in, and Note says why
	// there was no fetch when there was not.
	Fetched int      `json:"fetched"`
	Note    string   `json:"note,omitempty"`
	Results []Result `json:"results"`
}

// SyncBudget is how long a whole sync gets once it has started. It replaces
// the caller's deadline rather than extending it.
const SyncBudget = 10 * time.Minute

// AbortBudget is what undoing a failed rebase gets, outside the sync's budget:
// the whole point is that it still runs when that one has expired.
const AbortBudget = time.Minute

// Sync fetches the base once, then brings every checkout up to it: the main
// one fast-forwarded, the rest rebased.
func Sync(ctx context.Context, root string, dryRun bool) (Report, error) {
	// Uncancellable: a closed tab between a rebase and its abort would leave
	// a worktree mid-rebase with no ade in it to finish one.
	ctx, stop := context.WithTimeout(context.WithoutCancel(ctx), SyncBudget)
	defer stop()

	common, err := gitdiff.CommonGitDir(ctx, root)
	if err != nil {
		return Report{}, err
	}
	main := filepath.Dir(common)

	base := gitdiff.BaseBranch(ctx, main)
	if base == "" {
		return Report{}, fmt.Errorf("%s has no main or master branch to sync onto", main)
	}

	report := Report{Results: []Result{}}
	before := gitdiff.RemoteBase(ctx, main, base)
	was := gitdiff.RefSHA(ctx, main, before)
	if fetched, err := gitdiff.FetchBase(ctx, main, base); err != nil {
		// The local branch, not origin's copy of it: a fetch that failed is
		// no reason to rebase onto a remote ref nobody has refreshed.
		report.Note = fmt.Sprintf("could not fetch: %v", err)
		report.Base = base
		if local := strings.TrimPrefix(base, "origin/"); gitdiff.RefSHA(ctx, main, local) != "" {
			report.Base = local
		}
	} else {
		report.Base = gitdiff.RemoteBase(ctx, main, fetched)
	}
	// Both readings have to be of one ref: a failed fetch leaves report.Base the
	// local branch, and origin's stale copy against it counts nothing real.
	if now := gitdiff.RefSHA(ctx, main, report.Base); report.Base == before && was != "" && now != was {
		report.Fetched = countRange(ctx, main, was, now)
	}

	report.Results = append(report.Results, syncMain(ctx, main, report.Base, dryRun))

	found, err := Scan(ctx, root, false)
	if err != nil {
		return report, err
	}
	for _, wt := range found {
		report.Results = append(report.Results, syncOne(ctx, wt, report.Base, dryRun))
	}
	return report, nil
}

// syncMain fast-forwards the main checkout, never rebases it. Dirty or ahead
// it is skipped, which makes the broken rule visible rather than silent.
func syncMain(ctx context.Context, main, base string, dryRun bool) Result {
	res := Result{Path: main, Name: filepath.Base(main), Branch: gitdiff.Branch(ctx, main), Main: true}

	status, err := gitdiff.WorktreeStatus(ctx, main)
	if err != nil {
		res.Outcome, res.Detail = OutcomeSkipped, err.Error()
		return res
	}
	if n := len(status.Staged) + len(status.Unstaged); n > 0 {
		res.Outcome, res.Detail = OutcomeSkipped, fmt.Sprintf("%d uncommitted file(s)", n)
		return res
	}
	// Only the base branch is fast-forwarded: a main checkout parked on a
	// feature branch would have that branch's tip moved onto the base.
	if res.Branch != strings.TrimPrefix(base, "origin/") {
		on := res.Branch
		if on == "" {
			on = "a detached HEAD"
		}
		res.Outcome, res.Detail = OutcomeSkipped, fmt.Sprintf("on %s, not %s", on, base)
		return res
	}
	if ahead, err := gitdiff.CommitsAhead(ctx, main, base); err == nil && ahead > 0 {
		res.Outcome = OutcomeSkipped
		res.Detail = fmt.Sprintf("%d local commit(s) not on %s", ahead, base)
		return res
	}
	behind, err := gitdiff.CommitsBehind(ctx, main, base)
	if err != nil {
		res.Outcome, res.Detail = OutcomeSkipped, err.Error()
		return res
	}
	if behind == 0 {
		res.Outcome = OutcomeUpToDate
		return res
	}
	res.Detail = fmt.Sprintf("%d commit(s)", behind)
	if dryRun {
		res.Outcome = OutcomeWouldFastForward
		return res
	}
	if err := gitdiff.FastForward(ctx, main, base); err != nil {
		res.Outcome, res.Detail = OutcomeSkipped, err.Error()
		return res
	}
	res.Outcome = OutcomeFastForwarded
	return res
}

func syncOne(ctx context.Context, wt Worktree, base string, dryRun bool) Result {
	res := Result{Path: wt.Path, Name: wt.Name, Branch: wt.Branch}

	if wt.Kind == KindReview {
		res.Outcome, res.Detail = OutcomeSkipped, "a review checkout has no base to be behind"
		return res
	}
	// Asked of the checkout, not read off Keep: inspect stops at KeepServing
	// without counting a served worktree's edits, which a rebase would lose.
	status, err := gitdiff.WorktreeStatus(ctx, wt.Path)
	if err != nil {
		res.Outcome, res.Detail = OutcomeSkipped, err.Error()
		return res
	}
	if n := len(status.Staged) + len(status.Unstaged); n > 0 {
		res.Outcome, res.Detail = OutcomeSkipped, fmt.Sprintf("%d uncommitted file(s)", n)
		return res
	}
	// Measured against the ref about to be rebased onto, not read off wt.Behind:
	// Scan counts against origin's copy, which a failed fetch is not using.
	behind, err := gitdiff.CommitsBehind(ctx, wt.Path, base)
	if err != nil {
		res.Outcome, res.Detail = OutcomeSkipped, err.Error()
		return res
	}
	if behind == 0 {
		res.Outcome = OutcomeUpToDate
		return res
	}

	if dryRun {
		res.Outcome = OutcomeWouldRebase
		res.Detail = fmt.Sprintf("%d commit(s) behind", behind)
		return res
	}

	replayed, err := gitdiff.CommitsAhead(ctx, wt.Path, base)
	if err != nil {
		res.Outcome, res.Detail = OutcomeSkipped, err.Error()
		return res
	}
	res.Detail = fmt.Sprintf("%d commit(s) replayed", replayed)
	if err := gitdiff.Rebase(ctx, wt.Path, base); err != nil {
		res.Outcome = OutcomeConflicts
		res.Detail = "rebase aborted, fix by hand"
		// Its own budget: a rebase the sync's deadline killed would otherwise be
		// left in progress by an abort that deadline has already failed.
		abortCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), AbortBudget)
		defer stop()
		// The abort also fails when the rebase never started, so what is said
		// is that it could not be undone, not that one is halfway through.
		if abort := gitdiff.RebaseAbort(abortCtx, wt.Path); abort != nil {
			res.Detail = fmt.Sprintf("rebase failed and could not be undone: %v", abort)
		}
		return res
	}
	res.Outcome = OutcomeRebased
	return res
}

// countRange is how many commits new has that old does not. A count that
// cannot be read is zero: the number is decoration on a fetch that worked.
func countRange(ctx context.Context, dir, old, new string) int {
	n, err := gitdiff.CountRange(ctx, dir, old, new)
	if err != nil {
		return 0
	}
	return n
}
