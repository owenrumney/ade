package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/jobs"
	"github.com/owenrumney/ade/internal/limits"
	"github.com/owenrumney/ade/internal/sessions"
	adesettings "github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/transcripts"
	"github.com/owenrumney/ade/internal/wiki"
)

func newWikiDistillCommand() *cobra.Command {
	var dryRun bool
	var repo string
	cmd := &cobra.Command{
		Use:           "distill [--dry-run] [--repo owner/repo]",
		Short:         "Distil one repository's recent sessions into the shared wiki",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, config, err := wikiStore()
			if err != nil {
				return err
			}
			if !store.Enabled() {
				return errors.New("wiki is disabled; run ade wiki init --remote <url>")
			}
			if !dryRun && !config.WikiDistill {
				return errors.New("wiki distillation is off; run ade wiki init --distill to turn it on")
			}
			// Without the marks every transcript reads from byte 0, so the first run
			// only writes them: what was said before this is never published.
			if !store.DistillStarted() {
				if err := store.StartDistilling(transcriptPaths()); err != nil {
					return err
				}
				fmt.Println("started: sessions are read from here on")
				return nil
			}
			if ok, why := jobs.WithinSpend(limits.NewCache().Snapshot(cmd.Context())); !ok {
				return errors.New("held: " + why)
			}
			here := repoRootOrCwd(cmd)
			if repo == "" {
				owner, name := gitdiff.Origin(cmd.Context(), here)
				if owner == "" || name == "" {
					return errors.New("this checkout has no origin owner/repo; name one with --repo")
				}
				repo = owner + "/" + name
			}
			repos := append(sessions.Repos(config.Roots(), config.Ignored), sessions.Repo{Path: here})
			var target *jobs.DistillTarget
			for _, t := range jobs.DistillTargets(cmd.Context(), repos, transcripts.Scan()) {
				if t.Repo == repo {
					target = &t
					break
				}
			}
			if target == nil {
				return fmt.Errorf("no checkout of %s on this machine", repo)
			}
			dir, err := adesettings.Dir()
			if err != nil {
				return err
			}
			out, err := store.Distill(cmd.Context(), wiki.DistillRun{
				Repo: repo, Sessions: target.Sessions, Manual: true, DryRun: dryRun,
				Model: jobs.DistillModel("", dir),
			})
			printDistill(out, dryRun)
			if err == nil && out.Error != "" {
				err = errors.New(out.Error)
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Ask the model and show the pages, but publish nothing")
	cmd.Flags().StringVar(&repo, "repo", "", "The owner/repo to distil, when not this checkout's")
	return cmd
}

func printDistill(out wiki.DistillOutcome, dryRun bool) {
	fmt.Printf("%s: %s, %d sessions", out.Repo, out.State, out.Sessions)
	if out.CostUSD > 0 {
		fmt.Printf(", $%.2f", out.CostUSD)
	}
	fmt.Println()
	if dryRun && out.Prompt != "" {
		fmt.Printf("prompt: %d bytes\n", len(out.Prompt))
	}
	if out.Reply != nil {
		for _, page := range out.Reply.Pages {
			fmt.Printf("page: %s (%d bytes) — %s\n", page.Path, len(page.Body), page.Why)
		}
		if out.Reply.Log != "" {
			fmt.Printf("log: %s\n", out.Reply.Log)
		}
	} else {
		for _, page := range out.Pages {
			fmt.Printf("page: %s\n", page)
		}
	}
	if out.Commit != "" {
		fmt.Printf("commit: %s\n", out.Commit)
	}
}

func transcriptPaths() []string {
	var paths []string
	for _, session := range transcripts.Scan() {
		paths = append(paths, session.Path)
	}
	return paths
}
