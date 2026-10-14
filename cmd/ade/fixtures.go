package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/owenrumney/ade/internal/fixtures"
	"github.com/owenrumney/ade/internal/gitdiff"
)

func newFixturesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fixtures",
		Short: "Replay saved prompts and measure whether a CLAUDE.md rule works",
		Long: `A fixture is a turn saved from the chat: its prompt, the tree it ran on, and
an expect.yaml saying what should be true of the result. Run the suite before
and after editing a rule and compare the two to see whether the rule did
anything. Fixtures live under .git/ade/fixtures/<name>/.`,
	}
	cmd.AddCommand(newFixturesListCommand(), newFixturesRunCommand(), newFixturesCompareCommand())
	return cmd
}

func newFixturesListCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "ls",
		Short:         "The fixtures this repository has",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, dir, err := fixturesHere(cmd.Context())
			if err != nil {
				return err
			}
			all, err := fixtures.List(dir)
			if err != nil {
				return err
			}
			for _, f := range all {
				age := "undated"
				if !f.Captured.IsZero() {
					age = fmt.Sprintf("%d days old", int(time.Since(f.Captured).Hours()/24))
				}
				if f.Stale(time.Now()) {
					age += ", stale"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-24s %s  %s\n", f.Name, age, firstLine(f.Prompt))
			}
			return nil
		},
	}
}

func newFixturesRunCommand() *cobra.Command {
	var opts fixtures.Options
	var out string
	cmd := &cobra.Command{
		Use:   "run [name...]",
		Short: "Replay fixtures and print how many assertions held",
		Long: `Each fixture is replayed in a throwaway worktree with the checkout's current
CLAUDE.md, AGENTS.md and .claude laid over it, one at a time. Runs on haiku
unless --model says otherwise. The results are written as JSON for
"ade fixtures compare".`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, names []string) error {
			root, dir, err := fixturesHere(cmd.Context())
			if err != nil {
				return err
			}
			if out == "" {
				out = filepath.Join(dir, "runs", time.Now().Format("20060102-150405")+".json")
			}
			results, err := fixtures.Run(cmd.Context(), root, dir, fixtures.ColdRunner(""), names, opts)
			if err != nil {
				return err
			}
			if err := writeResults(out, results); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), fixtures.Compare(nil, results))
			fmt.Fprintf(cmd.OutOrStdout(), "\nsaved to %s\n", out)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Model, "model", fixtures.DefaultModel, "the model to run the prompts on")
	cmd.Flags().IntVar(&opts.Runs, "runs", 1, "how many times to run each fixture; one run of a nondeterministic agent says little")
	cmd.Flags().StringVar(&out, "out", "", "where to write the results; under .git/ade/fixtures/runs by default")
	return cmd
}

func newFixturesCompareCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "compare <before.json> <after.json>",
		Short:         "Two runs side by side: did the rule change anything?",
		Args:          cobra.ExactArgs(2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			before, err := readResults(args[0])
			if err != nil {
				return err
			}
			after, err := readResults(args[1])
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), fixtures.Compare(before, after))
			return nil
		},
	}
}

func fixturesHere(ctx context.Context) (root, dir string, err error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	root, err = gitdiff.RepoRoot(ctx, cwd)
	if err != nil {
		return "", "", err
	}
	common, err := gitdiff.CommonGitDir(ctx, root)
	if err != nil {
		return "", "", err
	}
	return root, fixtures.Dir(common), nil
}

func writeResults(path string, results []fixtures.Result) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func readResults(path string) ([]fixtures.Result, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- a path the user named
	if err != nil {
		return nil, err
	}
	var results []fixtures.Result
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, fmt.Errorf("%s is not a fixtures run: %w", path, err)
	}
	if len(results) == 0 {
		return nil, errors.New(path + " holds no results")
	}
	return results, nil
}
