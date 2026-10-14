package web

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/commitrec"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/lineage"
	"github.com/owenrumney/ade/internal/settings"
)

// verbatimKey publishes the ask in full rather than its first line. This repo's
// own config only, so one repo's answer is never every repo's.
const verbatimKey = "ade.lineage.verbatim"

// recordTimeout bounds one publish: it clones and pushes.
const recordTimeout = 5 * time.Minute

// verbatim is whether full asks may leave this laptop. Both halves: the
// checkout's config an agent can write, ade's settings it cannot.
func verbatim(ctx context.Context, root string) bool {
	if gitdiff.LocalConfigValue(ctx, root, verbatimKey) != "true" {
		return false
	}
	config, err := settings.Load()
	return err == nil && config.WikiVerbatim
}

// recordCommit publishes one commit's record, after the stamp that says which
// turns it carried. An amend republishes and keeps the old key.
func (s *Server) recordCommit(ctx context.Context, sha, replacing string) {
	if s.wiki == nil || !s.wiki.Enabled() || sha == "" {
		return
	}
	// One publisher: a second Submit over the same draft loses a record.
	s.recording.Lock()
	defer s.recording.Unlock()
	ctx, cancel := context.WithTimeout(ctx, recordTimeout)
	defer cancel()

	w := s.space()
	store := s.lineageStore()
	if w == nil || store == nil || w.snapshot {
		return
	}
	root := w.diffRoot()

	owner, name := gitdiff.Origin(ctx, root)
	if owner == "" || name == "" {
		return
	}
	repo := owner + "/" + name

	var threads []comments.Thread
	if w.store != nil {
		threads = w.store.List()
	}

	turns := store.ForCommit(sha, "")
	if len(turns) == 0 {
		// Nothing the conversation wrote. "Written by hand" is an answer, but
		// not one worth a file in a shared wiki.
		return
	}

	record, err := commitrec.Build(ctx, commitrec.Source{
		Repo:     repo,
		Root:     root,
		SHA:      sha,
		Branch:   w.branch,
		Turns:    turns,
		Threads:  threads,
		Verbatim: verbatim(ctx, root),
	})
	if err != nil {
		slog.Debug("could not build a commit record", "sha", sha, "err", err)
		return
	}

	// An amend rewrote the sha and with it the patch-id. Carry the old keys
	// forward rather than leaving a record nothing can look up.
	if replacing != "" {
		if old, ok := commitrec.Load(s.wiki, repo, s.recordKey(ctx, root, replacing)); ok {
			record.Absorb(old)
		}
	}

	if err := commitrec.Save(ctx, s.wiki, root, repo, []commitrec.Record{record}, nil); err != nil {
		slog.Info("could not publish a commit record", "sha", sha, "err", err)
	}
}

// recordKey is a commit's patch-id, empty when the commit is gone — which an
// amend's replaced sha always is, so this stays quiet about it.
func (s *Server) recordKey(ctx context.Context, root, sha string) string {
	key, err := gitdiff.PatchID(ctx, root, sha)
	if err != nil {
		return ""
	}
	return key
}

// recordMerge maps a merge commit to the records its branch carried. The next
// refresh that sees state MERGED is the trigger, and the only one.
func (s *Server) recordMerge(ctx context.Context, pr *ghpr.PR) {
	if s.wiki == nil || !s.wiki.Enabled() || pr == nil {
		return
	}
	if !strings.EqualFold(pr.State, "merged") || pr.MergeCommit == "" {
		return
	}
	// Every refresh of a merged pull request comes here, and each publish clones.
	if !s.recording.TryLock() {
		return
	}
	defer s.recording.Unlock()
	ctx, cancel := context.WithTimeout(ctx, recordTimeout)
	defer cancel()

	w := s.space()
	store := s.lineageStore()
	if w == nil || store == nil || w.snapshot {
		return
	}
	root := w.diffRoot()

	owner, name := gitdiff.Origin(ctx, root)
	if owner == "" || name == "" {
		return
	}
	repo := owner + "/" + name

	keys := s.branchKeys(ctx, root, store)
	if len(keys) == 0 {
		return
	}

	// Tried, whether or not it landed: the index read below lags a push, and
	// retrying on that is a clone per tick for as long as the workspace is open.
	tried := pr.MergeCommit + " " + strings.Join(keys, " ")
	if s.recorded[pr.Number] == tried {
		return
	}

	idx := commitrec.LoadIndex(s.wiki, repo)
	if slices.Equal(idx.KeysFor(pr.MergeCommit), keys) {
		return
	}
	idx = idx.Put(commitrec.Entry{Merge: pr.MergeCommit, PR: pr.Number, Keys: keys})

	records := make([]commitrec.Record, 0, len(keys))
	for _, key := range keys {
		record, ok := commitrec.Load(s.wiki, repo, key)
		if !ok {
			continue
		}
		record.PR, record.Merge = pr.Number, pr.MergeCommit
		records = append(records, record)
	}

	if s.recorded == nil {
		s.recorded = map[int]string{}
	}
	s.recorded[pr.Number] = tried

	if err := commitrec.Save(ctx, s.wiki, root, repo, records, idx); err != nil {
		slog.Info("could not re-key commit records on merge", "pr", pr.Number, "err", err)
	}
}

// branchKeys is the patch-id of every commit this branch's lineage was stamped
// with, sorted so an unchanged set compares equal.
func (s *Server) branchKeys(ctx context.Context, root string, store *lineage.Store) []string {
	seen := map[string]bool{}
	var keys []string
	for _, entry := range store.List() {
		for _, r := range entry.Ranges {
			if r.Commit == "" || seen[r.Commit] {
				continue
			}
			seen[r.Commit] = true
			if key := s.recordKey(ctx, root, r.Commit); key != "" {
				keys = append(keys, key)
			}
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}
