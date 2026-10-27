package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/memory"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/transcripts"
	"github.com/owenrumney/ade/internal/wiki"
)

// KindWikiDistill is the built-in job that keeps the shared wiki up to date
// from this machine's transcripts. See the wiki-distill design doc.
const KindWikiDistill = "wiki-distill"

// ScopeBuiltin is a job ade defines itself rather than reads from a file.
const ScopeBuiltin = "builtin"

// DistillPerRepo bounds one repository's run: a sync, a toolless one-shot and
// a push.
const DistillPerRepo = 5 * time.Minute

// DistillTarget is one repository and the transcripts that belong to it.
type DistillTarget struct {
	Repo     string
	Sessions []*transcripts.Session
}

// WikiDistillJob is the built-in definition. It has no prompt: its kind goes
// to a Go runner, not to a model with tools.
func WikiDistillJob() Job {
	every, _ := ParseSchedule("@every 30m")
	return Job{
		Name:        KindWikiDistill,
		Kind:        KindWikiDistill,
		Description: "Distil what was said in this machine's sessions into the shared wiki",
		Schedule:    every.Spec(),
		Every:       every,
		Continuity:  Fresh,
		Allow:       Reads,
		Scope:       ScopeBuiltin,
	}
}

// WithBuiltins adds the built-in jobs; one written by hand wins.
func WithBuiltins(list []Job, _ settings.Settings) []Job {
	for _, builtin := range []Job{DesignArchiveJob(), MyDayJob()} {
		if _, found := Find(list, builtin.Name); !found {
			list = append(list, builtin)
		}
	}
	return list
}

// Yours orders a tick's work so a job you wrote claims a slot before ade's own
// housekeeping. Nothing is dropped: the cap is what makes the order matter.
func Yours(list []Job) []Job {
	out := append([]Job(nil), list...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Scope != ScopeBuiltin && out[j].Scope == ScopeBuiltin
	})
	return out
}

// DistillTargets groups the machine's transcripts by the wiki repository they
// belong to, one entry per origin however many checkouts it has.
func DistillTargets(ctx context.Context, repos []sessions.Repo, all []*transcripts.Session) []DistillTarget {
	seen := map[string]bool{}
	var out []DistillTarget
	for _, repo := range repos {
		owner, name := gitdiff.Origin(ctx, repo.Path)
		if owner == "" || name == "" || seen[owner+"/"+name] {
			continue
		}
		id, err := memory.Resolve(ctx, repo.Path)
		if err != nil {
			continue
		}
		seen[owner+"/"+name] = true
		out = append(out, DistillTarget{Repo: owner + "/" + name, Sessions: memory.Corpus(ctx, id, all)})
	}
	return out
}

// DistillModel is the toolless one-shot a distillation asks. It runs in dir,
// outside every checkout, and leaves no transcript to be distilled in turn.
func DistillModel(bin, dir string) wiki.DistillModel {
	return func(ctx context.Context, prompt string) (string, float64, error) {
		turn, err := bridge.OneShot(ctx, bridge.OneShotOpts{
			Bin:                  bin,
			Dir:                  dir,
			Prompt:               prompt,
			Tools:                []string{},
			NoMCP:                true,
			NoSessionPersistence: true,
		})
		// Only a run that cost nothing: a turn that reached the model and said
		// something about oauth must still count against the failure brake.
		if bridge.IsAuthFailure(err) && (turn == nil || turn.CostUSD == 0) {
			err = fmt.Errorf("%w: %w", wiki.ErrNotTheModel, err)
		}
		if turn == nil {
			return "", 0, err
		}
		return turn.Text, turn.CostUSD, err
	}
}

// FireDistill runs every repository in turn and folds what came of them into
// one outcome. Manual is the button: it skips the quiet period and the brake.
func FireDistill(ctx context.Context, store *wiki.Store, targets []DistillTarget, model wiki.DistillModel, manual bool) Outcome {
	started := time.Now()
	out := Outcome{FiredAt: started}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	var report, failed []string
	// Set once the login has gone, so the verdict can lead with what to do.
	loginLeft := -1
	for i, target := range targets {
		// The fire's own timeout is the whole of it. A repository gets its own, so
		// a slow one ahead does not cut short the ones behind it.
		if ctx.Err() != nil {
			report = append(report, fmt.Sprintf("%d repositories were not reached this fire", len(targets)-i))
			break
		}
		runCtx, done := context.WithTimeout(ctx, DistillPerRepo)
		got, err := store.Distill(runCtx, wiki.DistillRun{Repo: target.Repo, Sessions: target.Sessions, Manual: manual, Model: model})
		done()
		out.CostUSD += got.CostUSD
		switch {
		// Another process has it. Not news, and not this fire's business.
		case got.State == wiki.DistillHeld:
			slog.Info("wiki distill already running", "repo", target.Repo)
		case err != nil || got.Error != "":
			why := got.Error
			if why == "" {
				why = err.Error()
			}
			slog.Warn("wiki distill failed", "repo", target.Repo, "state", got.State, "err", why)
			failed = append(failed, fmt.Sprintf("%s: %s: %s", target.Repo, got.State, why))
			if endsTheFire(err) {
				loginLeft = len(targets) - i - 1
			}
		case got.State == "published":
			report = append(report, fmt.Sprintf("%s: published %s (%s)", target.Repo, strings.Join(got.Pages, ", "), short(got.Commit)))
		}
		if loginLeft >= 0 {
			break
		}
	}
	if loginLeft >= 0 {
		failed = append([]string{loginGone(loginLeft)}, failed...)
	}
	out.Millis = time.Since(started).Milliseconds()
	out.Report = strings.Join(report, "\n")
	out.Error = strings.Join(failed, "\n")
	return out
}

// endsTheFire is whether a failure was the machine's rather than this
// repository's. The login is shared, so every repository behind it repeats it.
func endsTheFire(err error) bool {
	return errors.Is(err, wiki.ErrNotTheModel) && bridge.IsAuthFailure(err)
}

// loginGone names the login and where to mend it, which the raw error did not.
func loginGone(left int) string {
	note := "log in again: ade runs this job's model on this machine, and that login has expired — run `claude` here"
	if left > 0 {
		note += fmt.Sprintf(" (%d more repositories were not reached)", left)
	}
	return note
}

func short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}
