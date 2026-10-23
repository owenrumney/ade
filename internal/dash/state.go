package dash

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/statedir"
)

// What a pull request row says about your own review of it.
const (
	// StateRequested is asked for, with nothing of yours against it yet.
	StateRequested = "requested"
	// StateRunning is a review agent working on it now.
	StateRunning = "running"
	// StateReady is findings filed and none of them read by you.
	StateReady = "ready"
	// StateDrafted is a comment or reply of yours that has not gone.
	StateDrafted = "drafted"
	// StateSent is everything of yours on GitHub.
	StateSent = "sent"
)

// ReviewState is one pull request's review, from the thread store on disk:
// a session server nothing is attached to shuts itself down.
type ReviewState struct {
	State   string `json:"state"`
	Threads int    `json:"threads,omitempty"`
	Unsent  int    `json:"unsent,omitempty"`
	Sent    int    `json:"sent,omitempty"`
	Path    string `json:"path,omitempty"`
}

// reviewStateIn reads the review worktree for a pull request. Its git dir is
// its own, so the whole directory belongs to this one pull request.
func reviewStateIn(main string, number int, running bool) ReviewState {
	path := gitdiff.WorktreePath(main, fmt.Sprintf("pr-%d", number))
	if _, err := os.Stat(path); err != nil {
		return ReviewState{State: StateRequested}
	}
	out := ReviewState{State: StateRunning, Path: path}
	if !running {
		out.State = StateRequested
	}
	for _, thread := range threadsIn(path) {
		// A withdrawn finding is never sent, so counting it would leave the
		// row drafted for good. Same for one a rebuttal round resolved.
		if thread.Withdrawn || thread.ExternalID != "" {
			continue
		}
		out.Threads++
		switch {
		case unsent(thread):
			out.Unsent++
		case thread.Sent():
			out.Sent++
		}
	}
	return settled(out, running)
}

// settled is the one word for the counts: what is unsent leads, because that
// is the one that gets forgotten.
func settled(out ReviewState, running bool) ReviewState {
	switch {
	case out.Unsent > 0:
		out.State = StateDrafted
	case out.Sent > 0 && out.Sent == out.Threads:
		out.State = StateSent
	case out.Threads > 0:
		out.State = StateReady
	case running:
		out.State = StateRunning
	default:
		out.State = StateRequested
	}
	return out
}

// unsent is a thread of yours not on GitHub, or an unsent reply under one
// that is: a reply carries its own stamp.
func unsent(thread comments.Thread) bool {
	if !thread.Sent() && !thread.Resolved {
		return true
	}
	for _, c := range thread.Comments {
		if c.ExternalID == "" && !c.Sent() {
			return true
		}
	}
	return false
}

// threadsIn reads every store under a checkout's own git dir. One file per
// branch, and a review worktree is only ever on the one.
func threadsIn(path string) []comments.Thread {
	dir := statedir.Git(sessions.GitDir(path))
	found, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []comments.Thread
	for _, each := range found {
		if each.IsDir() || !strings.HasSuffix(each.Name(), ".json") || each.Name() == "server.json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, each.Name())) // #nosec G304 -- a checkout's own git dir
		if err != nil {
			continue
		}
		var threads []comments.Thread
		if json.Unmarshal(data, &threads) == nil {
			out = append(out, threads...)
		}
	}
	return out
}
