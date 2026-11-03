// Package ship reads where one repository's change stands: the working tree,
// the branch, and the pull request. It keeps no state and serves no HTTP.
package ship

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
)

// Repo is one repository to read, as the session already knows it.
type Repo struct {
	Root   string
	Name   string
	Branch string
	Home   bool
}

// Project is where that repository's change stands, in the order the page
// draws it: work, branch, pull request.
type Project struct {
	Root   string `json:"root"`
	Name   string `json:"name"`
	Branch string `json:"branch,omitempty"`
	Home   bool   `json:"home,omitempty"`
	Base   string `json:"base,omitempty"`

	// Files is the paths with uncommitted work, Staged how many of them are in
	// the index.
	Files  int `json:"files"`
	Staged int `json:"staged"`

	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`

	// Unpushed is base..HEAD, newest first: the commits a pull request would
	// be made of.
	Unpushed []gitdiff.Rev `json:"unpushed,omitempty"`

	PR *PR `json:"pr,omitempty"`

	// Note is why a field is missing, for a repository git would not answer for.
	Note string `json:"note,omitempty"`
}

// PR is the pull request as the page draws it: GitHub's own verdicts, never
// one worked out here.
type PR struct {
	Number         int           `json:"number"`
	Title          string        `json:"title"`
	URL            string        `json:"url"`
	State          string        `json:"state"`
	Draft          bool          `json:"draft,omitempty"`
	BaseRef        string        `json:"baseRef,omitempty"`
	ReviewDecision string        `json:"reviewDecision,omitempty"`
	Reviews        []ghpr.Review `json:"reviews,omitempty"`
	Checks         []ghpr.Check  `json:"checks,omitempty"`
	Failing        int           `json:"failing"`

	// FailLine is the first failing check and what it said about itself, so the
	// card says why rather than how many.
	FailLine string `json:"failLine,omitempty"`

	// MergeBlocked is why this cannot be merged, or empty when it can be.
	MergeBlocked string `json:"mergeBlocked,omitempty"`
}

// Look answers a repository's pull request and checks from whatever cache the
// caller keeps. Nil is the ordinary answer: most branches have none.
type Look func(ctx context.Context, repo Repo) *PR

// Read is one project, as git and the caller's cache have it. A repository git
// will not answer for comes back named, with the reason on Note.
func Read(ctx context.Context, repo Repo, look Look) Project {
	p := Project{Root: repo.Root, Name: repo.Name, Branch: repo.Branch, Home: repo.Home}
	if p.Name == "" {
		p.Name = filepath.Base(repo.Root)
	}
	if p.Branch == "" {
		p.Branch = gitdiff.Branch(ctx, repo.Root)
	}

	status, err := gitdiff.WorktreeStatus(ctx, repo.Root)
	if err != nil {
		p.Note = err.Error()
		return p
	}
	p.Staged = len(status.Staged)
	p.Files = touched(status)
	p.Upstream, p.Ahead = status.Upstream, status.Ahead

	p.Base = gitdiff.BaseBranch(ctx, repo.Root)
	if p.Base != "" {
		p.Behind, _ = gitdiff.CommitsBehind(ctx, repo.Root, p.Base)
		if revs, err := gitdiff.Subjects(ctx, repo.Root, p.Base, "HEAD"); err == nil {
			p.Unpushed = revs
		}
	}

	if look != nil {
		repo.Branch = p.Branch
		p.PR = look(ctx, repo)
	}
	return p
}

// touched counts paths, not entries: a file both staged and edited since is
// one piece of work and was counted twice.
func touched(status gitdiff.Status) int {
	seen := map[string]bool{}
	for _, path := range status.Staged {
		seen[path] = true
	}
	for _, change := range status.Unstaged {
		seen[change.Path] = true
	}
	return len(seen)
}

// FromPR is the pull request in the page's terms. One place, so the branch's
// own read and another repository's cannot describe it differently.
func FromPR(pr *ghpr.PR, checks []ghpr.Check) *PR {
	if pr == nil {
		return nil
	}
	out := &PR{
		Number: pr.Number, Title: pr.Title, URL: pr.URL, State: pr.State,
		Draft: pr.Draft, BaseRef: pr.BaseRef,
		ReviewDecision: pr.ReviewDecision, Reviews: pr.Reviews,
		Checks: checks, MergeBlocked: pr.MergeBlocked(),
	}
	out.Failing, out.FailLine = Failing(checks)
	return out
}

// Failing counts the checks worth acting on and describes the first of them.
func Failing(checks []ghpr.Check) (int, string) {
	count, line := 0, ""
	for _, check := range checks {
		if !check.Failed() {
			continue
		}
		count++
		if line == "" {
			line = describe(check)
		}
	}
	return count, line
}

func describe(check ghpr.Check) string {
	name := check.Name
	if check.Workflow != "" {
		name = fmt.Sprintf("%s (%s)", check.Name, check.Workflow)
	}
	if desc := strings.TrimSpace(check.Description); desc != "" {
		return name + ": " + desc
	}
	return name
}
