package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/archive"
	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/wiki"
)

// KindDesignArchive is the built-in job that sweeps finished design documents
// onto the shared wiki. See the purge-design-docs design doc.
const KindDesignArchive = "design-archive"

// ArchivePerRepo bounds one repository's sweep: a `gh pr list`, then a
// one-shot and a push for each document that merged.
const ArchivePerRepo = 10 * time.Minute

// DesignArchiveJob is the built-in definition. It has no prompt: its kind goes
// to a Go runner, since only the distil step needs a model.
func DesignArchiveJob() Job {
	every, _ := ParseSchedule("@every 168h")
	return Job{
		Name:        KindDesignArchive,
		Kind:        KindDesignArchive,
		Description: "Archive the design documents whose branch merged onto the shared wiki",
		Schedule:    every.Spec(),
		Every:       every,
		Continuity:  Fresh,
		Allow:       Reads,
		Scope:       ScopeBuiltin,
	}
}

// ArchiveTarget is one repository and the checkout its designs live in.
type ArchiveTarget struct {
	Repo         string
	Root         string
	CommonGitDir string
}

// ArchiveTargets is one entry per origin, however many checkouts it has: the
// designs of every worktree are searched from the main one.
func ArchiveTargets(ctx context.Context, repos []sessions.Repo) []ArchiveTarget {
	seen := map[string]bool{}
	var out []ArchiveTarget
	for _, repo := range repos {
		owner, name := gitdiff.Origin(ctx, repo.Path)
		if owner == "" || name == "" || seen[owner+"/"+name] {
			continue
		}
		common, err := gitdiff.CommonGitDir(ctx, repo.Path)
		if err != nil {
			continue
		}
		seen[owner+"/"+name] = true
		out = append(out, ArchiveTarget{Repo: owner + "/" + name, Root: repo.Path, CommonGitDir: common})
	}
	return out
}

// ArchiveModel is the toolless one-shot an archive asks. It runs in dir,
// outside every checkout, and leaves no transcript behind it.
func ArchiveModel(bin, dir string) archive.Runner {
	return func(ctx context.Context, prompt string) (string, error) {
		turn, err := bridge.OneShot(ctx, bridge.OneShotOpts{
			Bin:                  bin,
			Dir:                  dir,
			Model:                archive.Model,
			Prompt:               prompt,
			Tools:                []string{},
			NoMCP:                true,
			NoSessionPersistence: true,
		})
		if err != nil {
			return "", err
		}
		if turn == nil {
			return "", nil
		}
		return turn.Text, nil
	}
}

// FireArchive sweeps every repository: what merged is archived, what vanished
// unmerged is reported. With no wiki, nothing goes and only the report does.
func FireArchive(ctx context.Context, store *wiki.Store, targets []ArchiveTarget, model archive.Runner) Outcome {
	started := time.Now()
	out := Outcome{FiredAt: started}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	publishes := store != nil && store.Enabled()
	var report, failed []string
	for i, target := range targets {
		if ctx.Err() != nil {
			report = append(report, fmt.Sprintf("%d repositories were not reached this fire", len(targets)-i))
			break
		}
		runCtx, done := context.WithTimeout(ctx, ArchivePerRepo)
		state, err := archive.Look(runCtx, target.Root, target.CommonGitDir, "")
		if err != nil {
			done()
			// A remote with no repository behind it fails every fire, so it is
			// reported rather than failing the sweep of the others.
			if ghpr.NoRepository(err) {
				report = append(report, fmt.Sprintf("%s: skipped, its origin has no repository on GitHub", target.Repo))
				continue
			}
			failed = append(failed, fmt.Sprintf("%s: %s", target.Repo, err))
			continue
		}
		merged, abandoned := archive.Sweep(state)
		for _, candidate := range merged {
			if !publishes {
				report = append(report, fmt.Sprintf("%s: %s is ready to archive, but the wiki is disabled", target.Repo, candidate.File))
				continue
			}
			got, err := archive.One(runCtx, store, target.Repo, candidate.Doc, candidate.PR, model)
			if err != nil {
				slog.Warn("archive a design", "repo", target.Repo, "doc", candidate.File, "err", err)
				failed = append(failed, fmt.Sprintf("%s: %s: %s", target.Repo, candidate.File, err))
				continue
			}
			report = append(report, fmt.Sprintf("%s: archived %s to %s", target.Repo, candidate.File, got.Page))
		}
		for _, candidate := range abandoned {
			report = append(report, fmt.Sprintf("%s: %s was written for %s, which never merged; archive it or `ade rm` it",
				target.Repo, candidate.File, candidate.Branch))
		}
		done()
	}
	out.Millis = time.Since(started).Milliseconds()
	out.Report = strings.Join(report, "\n")
	out.Error = strings.Join(failed, "\n")
	return out
}
