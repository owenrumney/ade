package web

import (
	"fmt"
	"log/slog"
	"strconv"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/ghpr"
)

// prSnapWindow is wider than the review one: an upstream comment may have been
// written against an older push, so its line can have drifted further.
const prSnapWindow = 25

// PRInfo is the pull request context shown in the header.
type PRInfo struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Author  string `json:"author"`
	URL     string `json:"url"`
	State   string `json:"state"`
	HeadRef string `json:"headRef"`

	// ResolveBlocked explains why Resolve is unavailable, and is empty when it
	// can safely run.
	ResolveBlocked string `json:"resolveBlocked,omitempty"`
}

// importComments files upstream review comments as threads. A thread already
// known by ExternalID is merged rather than skipped, so a reply posted after
// the first import still shows up.
func (s *Server) importComments(threads []ghpr.Thread) int {
	w := s.space()
	// The home checkout's pull request, on a timer with no request to refuse.
	if w.viewRoot != "" {
		return 0
	}
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()

	added := 0
	for _, thread := range threads {
		external := strconv.FormatInt(thread.Root.ID, 10)
		incoming := externalComments(thread)

		if existing, ok := w.store.ByExternal(external); ok {
			n, err := w.store.MergeExternalReplies(existing.ID, incoming)
			if err != nil {
				slog.Debug("merge review comment replies", "path", thread.Root.Path, "err", err)
				continue
			}
			added += n
			continue
		}

		anchor, line, ok := comments.LocateNearest(diff, thread.Root.Path, thread.Root.Side, thread.Root.Line, prSnapWindow)

		record := comments.Thread{
			File:       thread.Root.Path,
			Side:       thread.Root.Side,
			Line:       line,
			Anchor:     anchor,
			Orphaned:   !ok,
			ExternalID: external,
			Comments:   incoming,
		}

		if _, err := w.store.Add(record); err != nil {
			slog.Debug("import review comment", "path", thread.Root.Path, "err", err)
			continue
		}
		added++
	}
	return added
}

// externalComments turns a GitHub thread's root and replies into store
// comments, each carrying the GitHub ID it mirrors so a later poll can tell
// what it has already recorded from what just landed.
func externalComments(thread ghpr.Thread) []comments.Comment {
	body := thread.Root.Body
	if thread.Root.Outdated {
		body = "(on an earlier push) " + body
	}
	out := []comments.Comment{
		{Author: thread.Root.Author, Body: body, ExternalID: strconv.FormatInt(thread.Root.ID, 10)},
	}
	for _, reply := range thread.Replies {
		out = append(out, comments.Comment{
			Author:     reply.Author,
			Body:       reply.Body,
			ExternalID: strconv.FormatInt(reply.ID, 10),
		})
	}
	return out
}

// ResolveBlockedReason guards against an agent editing a different branch's code
// than the one under review. A dirty tree on the right branch is fine.
func ResolveBlockedReason(pr *ghpr.PR, headSHA, branch string) string {
	if pr == nil || headSHA == pr.HeadSHA {
		return ""
	}
	// The branch is the question. Unpushed commits and an unfetched remote both
	// move the SHA off the PR head while the code under the edit is still right.
	if branch != "" && branch == pr.HeadRef {
		return ""
	}
	return fmt.Sprintf("the PR head %s is not checked out, so edits would land on the wrong code; run gh pr checkout %d",
		short(pr.HeadSHA), pr.Number)
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
