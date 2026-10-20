package archive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/docs"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
)

// Candidate is one design document and the work it was written for.
type Candidate struct {
	Doc
	Stem   string `json:"stem"`
	Branch string `json:"branch"`
	PR     string `json:"pr,omitempty"`
	Number int    `json:"number,omitempty"`
}

// State is what a sweep reads: the documents, the branches the repository
// still has, and what its pull requests say became of the ones it does not.
type State struct {
	Docs     []Doc
	Branches []string
	Heads    []ghpr.Head
}

// Sweep splits the documents: the ones whose branch merged and went, and the
// ones whose branch vanished unmerged. Matching no branch at all is neither.
func Sweep(st State) (merged, abandoned []Candidate) {
	local := map[string]bool{}
	for _, branch := range st.Branches {
		local[strings.ToLower(docs.LastSegment(branch))] = true
	}
	// Open wins, so work in flight is left alone whatever else shares its name.
	heads := map[string]ghpr.Head{}
	for _, head := range st.Heads {
		key := strings.ToLower(docs.LastSegment(head.Branch))
		if held, ok := heads[key]; ok && rank(held) >= rank(head) {
			continue
		}
		heads[key] = head
	}

	for _, doc := range st.Docs {
		stem := strings.ToLower(docs.Stem(doc.File))
		head, ok := heads[stem]
		if !ok || local[stem] || head.Open() {
			continue
		}
		candidate := Candidate{Doc: doc, Stem: docs.Stem(doc.File), Branch: head.Branch, PR: head.URL, Number: head.Number}
		if head.Merged() {
			merged = append(merged, candidate)
			continue
		}
		abandoned = append(abandoned, candidate)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].File < merged[j].File })
	sort.Slice(abandoned, func(i, j int) bool { return abandoned[i].File < abandoned[j].File })
	return merged, abandoned
}

// rank is which of two pull requests on the same name decides the verdict.
func rank(head ghpr.Head) int {
	switch {
	case head.Open():
		return 3
	case head.Merged():
		return 2
	default:
		return 1
	}
}

// Look reads one checkout's state. The branch set is the local branches and
// every worktree's, so a design still being worked on is never a candidate.
func Look(ctx context.Context, root, commonGitDir, override string) (State, error) {
	var st State
	for _, dir := range docs.SearchDirs(root, commonGitDir, override) {
		files, err := docs.List(dir)
		if err != nil {
			continue
		}
		for _, file := range files {
			st.Docs = append(st.Docs, Doc{Dir: dir, File: file})
		}
	}
	branches, err := gitdiff.Branches(ctx, root)
	if err != nil {
		return st, err
	}
	st.Branches = branches
	if paths, err := gitdiff.Worktrees(ctx, root); err == nil {
		for _, path := range paths {
			if branch := gitdiff.Branch(ctx, path); branch != "" && branch != "detached" {
				st.Branches = append(st.Branches, branch)
			}
		}
	}
	heads, err := ghpr.Heads(ctx, root, 0)
	if err != nil {
		return st, err
	}
	st.Heads = heads
	return st, nil
}

// PRFor is the pull request of the branch a document's name matches, or empty
// when there is not one. Absence is ordinary, never an error.
func PRFor(ctx context.Context, root, file string) string {
	stem := docs.Stem(file)
	for _, branch := range []string{stem, gitdiff.Branch(ctx, root)} {
		if branch == "" || branch == "detached" || !strings.EqualFold(docs.LastSegment(branch), stem) {
			continue
		}
		if pr, err := ghpr.ForBranch(ctx, root, branch); err == nil && pr != nil {
			return pr.URL
		}
	}
	return ""
}

// Find picks one document out of a checkout's designs by name, so `ade
// archive` reaches only a document the same search would list.
func Find(root, commonGitDir, override, name string) (Doc, error) {
	dirs := docs.SearchDirs(root, commonGitDir, override)
	full, err := docs.Locate(dirs, name)
	if err != nil {
		return Doc{}, err
	}
	for _, dir := range dirs {
		rel, err := filepath.Rel(dir, full)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		return Doc{Dir: dir, File: filepath.ToSlash(rel)}, nil
	}
	return Doc{}, fmt.Errorf("no document called %s", name)
}

// Stale is a design document whose name no branch and no pull request ever
// matched, with when the file was last written.
type Stale struct {
	Doc
	Stem string `json:"stem"`
	// Touched is the file's mtime: these documents are never in a commit, so
	// git has no date for them.
	Touched time.Time `json:"touched"`
}

// Unmatched is every document the sweep cannot see: no branch, open or gone,
// and no pull request shares its name. Most of what accumulates is these.
func Unmatched(st State) []Stale {
	known := map[string]bool{}
	for _, branch := range st.Branches {
		known[strings.ToLower(docs.LastSegment(branch))] = true
	}
	for _, head := range st.Heads {
		known[strings.ToLower(docs.LastSegment(head.Branch))] = true
	}
	out := []Stale{}
	for _, doc := range st.Docs {
		stem := docs.Stem(doc.File)
		if known[strings.ToLower(stem)] {
			continue
		}
		stale := Stale{Doc: doc, Stem: stem}
		if full, err := docs.Resolve(doc.Dir, doc.File); err == nil {
			if info, err := os.Stat(full); err == nil {
				stale.Touched = info.ModTime()
			}
		}
		out = append(out, stale)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out
}
