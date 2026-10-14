package commitrec

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/lineage"
)

// Source is what a record is assembled from. Verbatim decides what leaves the
// laptop, not what was captured: the full ask is always in lineage.
type Source struct {
	Repo     string
	Root     string
	SHA      string
	Branch   string
	Turns    []lineage.Turn
	Threads  []comments.Thread
	Verbatim bool
}

// Build assembles the record for one commit. It needs git for the commit's key,
// subject and added lines, so it can only run where the commit exists.
func Build(ctx context.Context, src Source) (Record, error) {
	key, err := gitdiff.PatchID(ctx, src.Root, src.SHA)
	if err != nil {
		return Record{}, err
	}
	added, err := gitdiff.AddedLines(ctx, src.Root, src.SHA)
	if err != nil {
		return Record{}, err
	}

	r := Record{
		Repo:    src.Repo,
		Commit:  src.SHA,
		Branch:  src.Branch,
		Subject: gitdiff.CommitSubject(ctx, src.Root, src.SHA),
		At:      time.Now().UTC(),
		Asked:   asks(src.Turns, src.Verbatim),
		Review:  claims(ctx, src, added),
	}
	if key != "" {
		r.Keys = []string{key}
	}
	return r, nil
}

// asks is one entry per turn, oldest first and one per message: a turn that
// wrote two ranges in the commit is still one ask.
func asks(turns []lineage.Turn, verbatim bool) []Ask {
	seen := map[string]bool{}
	var out []Ask
	for _, t := range turns {
		text := t.Summary
		if verbatim && t.Prompt != "" {
			text = t.Prompt
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		id := t.Session + "/" + t.MessageID
		if t.MessageID == "" {
			id = t.Session + "/" + t.At.String()
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, Ask{At: t.At, Session: t.Session, MessageID: t.MessageID, Text: text})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// claims is every finding against a line this commit added. A line it did not
// add belongs to another commit, and a repo-scoped thread to none.
func claims(ctx context.Context, src Source, added map[string][]string) []Claim {
	lines := map[string]map[string]bool{}
	for file, rows := range added {
		set := make(map[string]bool, len(rows))
		for _, row := range rows {
			if strings.TrimSpace(row) != "" {
				set[row] = true
			}
		}
		lines[file] = set
	}

	var out []Claim
	for _, t := range src.Threads {
		if t.Scope == comments.ScopeRepo || t.Side == comments.SideLeft {
			continue
		}
		if !lines[t.File][t.Anchor.Content] {
			continue
		}
		// The same text can be added twice in a file, so content alone would
		// file a finding against whichever commit happened to add a copy.
		if !wroteLine(ctx, src, t.File, t.Line) {
			continue
		}
		body, author := firstComment(t)
		out = append(out, Claim{
			File:     t.File,
			Line:     t.Line,
			Author:   author,
			Category: t.Category,
			Severity: t.Severity,
			Fate:     t.Fate(),
			Text:     body,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// wroteLine reports whether this commit is the one blame names for the line a
// thread points at. Unreadable blame keeps the claim: the content matched.
func wroteLine(ctx context.Context, src Source, file string, line int) bool {
	if line <= 0 {
		return true
	}
	blame, err := gitdiff.BlameLine(ctx, src.Root, file, line)
	if err != nil || !blame.Committed() {
		return true
	}
	return blame.SHA == src.SHA
}

// firstComment is the claim itself: the thread's opening comment, not the
// replies arguing about it.
func firstComment(t comments.Thread) (body, author string) {
	if len(t.Comments) == 0 {
		return "", ""
	}
	return t.Comments[0].Body, t.Comments[0].Author
}
