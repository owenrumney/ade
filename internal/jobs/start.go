package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/owenrumney/ade/internal/sessions"
)

// Started is where a writes job went: its worktree, and the ade serving it.
type Started struct {
	URL  string
	Path string
}

// Starter opens a worktree and a session for a writes job and hands it the
// prompt. The real one runs `ade create`; a test stands one in.
type Starter func(ctx context.Context, dir, name, prompt string) (Started, error)

// FirstTurnWait bounds handing the session its task. The reply is not waited
// for: the branch is what is delivered, and the session outlives this call.
const FirstTurnWait = 15 * time.Second

// Start fires a writes job. It is a session of its own on a branch of its
// own, not a report, so the outcome says where it is and comes back at once.
func Start(ctx context.Context, job Job, dir string, start Starter) Outcome {
	state := ReadState(job)
	began := time.Now()
	out := Outcome{FiredAt: began, Dir: dir}
	name := WorktreeName(job, began)

	slog.Info("job started", "job", job.Name, "dir", dir, "worktree", name)
	got, err := start(ctx, dir, name, Prompt(job, state))
	out.Millis = time.Since(began).Milliseconds()
	if err != nil {
		slog.Warn("job could not start", "job", job.Name, "err", err)
		out.Error = err.Error()
	} else {
		out.Dir, out.URL = got.Path, got.URL
		out.Report = fmt.Sprintf("Working on %s in %s", name, got.Path)
	}
	if _, err := Record(job, out, state.Handover, time.Now()); err != nil {
		slog.Warn("record a job start", "job", job.Name, "err", err)
	}
	return out
}

// WorktreeName is the branch a fire works on: the job and the month, so a
// second run in the month lands in the same place rather than beside it.
func WorktreeName(job Job, at time.Time) string {
	return job.Name + "-" + at.Format("2006-01")
}

// CreateStarter is the real starter: `ade create` beside the checkout, from
// its HEAD rather than main, then the prompt as the session's first turn.
func CreateStarter() Starter {
	return func(ctx context.Context, dir, name, prompt string) (Started, error) {
		url, path, err := sessions.CreateNamedFrom(ctx, dir, name, "", "", "HEAD")
		if err != nil {
			return Started{}, err
		}
		body, err := json.Marshal(map[string]string{"message": prompt})
		if err != nil {
			return Started{}, err
		}
		if _, err := sessions.AskFor(ctx, url, http.MethodPost, "/api/chat", string(body), FirstTurnWait); err != nil {
			return Started{URL: url, Path: path}, fmt.Errorf("hand the session its task: %w", err)
		}
		return Started{URL: url, Path: path}, nil
	}
}
