package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/owenrumney/ade/internal/commitrec"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/lineage"
)

// newWhyCommand answers "why is this line here?" from the local lineage.
func newWhyCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "why <file>:<line>",
		Short:         "Show the turns that wrote a line, and the commit that carried it",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			file, line, err := splitTarget(args[0])
			if err != nil {
				return err
			}
			return why(cmd.Context(), cmd.OutOrStdout(), file, line)
		},
	}
}

func why(ctx context.Context, out io.Writer, file string, line int) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := gitdiff.RepoRoot(ctx, cwd)
	if err != nil {
		return err
	}
	rel, err := repoRel(root, cwd, file)
	if err != nil {
		return err
	}

	blame, err := gitdiff.BlameLine(ctx, root, rel, line)
	if err != nil {
		return err
	}

	gitDir, err := gitdiff.GitDir(ctx, root)
	if err != nil {
		return err
	}
	store, err := lineage.Open(gitDir, gitdiff.Branch(ctx, root))
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "%s:%d\n\n", rel, line)
	printCommit(out, blame)

	var turns []lineage.Turn
	if blame.Committed() {
		turns = store.ForCommit(blame.SHA, rel)
	} else {
		// No sha to look up, so it is found the way the gutter finds it: by
		// re-anchoring against the branch diff.
		diff, diffErr := gitdiff.Load(ctx, gitdiff.Options{Repo: root, Scope: gitdiff.ScopeBranch})
		if diffErr != nil {
			return diffErr
		}
		turns = store.At(diff, rel, line)
	}

	if len(turns) == 0 {
		// The shared record still answers for a line this laptop never wrote:
		// lineage is branch-local, and the wiki is not.
		if printRecords(ctx, out, root, blame.SHA) {
			return nil
		}
		fmt.Fprintln(out, "  written by hand: no turn on this branch's lineage wrote it")
		return nil
	}
	fmt.Fprintln(out, "  Asked:")
	for _, turn := range turns {
		fmt.Fprintf(out, "    %s  %s\n", turn.At.Format("2006-01-02 15:04"), turn.Summary)
	}
	printRecords(ctx, out, root, blame.SHA)
	return nil
}

// printRecords writes what the wiki holds for a commit and says whether it held
// anything. A sha with no record of its own is looked up in the merge index.
func printRecords(ctx context.Context, out io.Writer, root, sha string) bool {
	if sha == "" {
		return false
	}
	// Origin first: a checkout with no origin has no record wherever it looks,
	// and opening the wiki store reaches into ade's state directory.
	owner, name := gitdiff.Origin(ctx, root)
	if owner == "" || name == "" {
		return false
	}
	store, _, err := wikiStore()
	if err != nil || store == nil || !store.Enabled() {
		return false
	}
	repo := owner + "/" + name

	// A one-commit squash keys the same as its branch commit, so dedupe.
	type lookup struct {
		key      string
		viaMerge bool
	}
	var keys []lookup
	seen := map[string]bool{}
	if key, err := gitdiff.PatchID(ctx, root, sha); err == nil && key != "" {
		keys = append(keys, lookup{key: key})
		seen[key] = true
	}
	for _, key := range commitrec.LoadIndex(store, repo).KeysFor(sha) {
		if seen[key] {
			continue
		}
		keys = append(keys, lookup{key: key, viaMerge: true})
		seen[key] = true
	}

	found := false
	for _, at := range keys {
		record, ok := commitrec.Load(store, repo, at.key)
		if !ok {
			continue
		}
		found = true
		printRecord(out, record, at.viaMerge)
	}
	return found
}

func printRecord(out io.Writer, r commitrec.Record, viaMerge bool) {
	if viaMerge {
		fmt.Fprintf(out, "\n  from the squashed commit %q:\n", plain(r.Subject))
	}
	if len(r.Asked) > 0 {
		fmt.Fprintln(out, "  Asked:")
		for _, ask := range r.Asked {
			fmt.Fprintf(out, "    %s  %s\n", ask.At.Format("2006-01-02 15:04"), firstLine(ask.Text))
		}
	}
	if len(r.Review) > 0 {
		fmt.Fprintln(out, "  Review:")
		for _, claim := range r.Review {
			fmt.Fprintf(out, "    %-9s %s\n", plain(claim.Fate), firstLine(claim.Text))
		}
	}
	for _, page := range r.Pages {
		fmt.Fprintf(out, "  Page: %s\n", plain(page))
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return plain(line)
}

// plain drops control characters: a record comes from the shared wiki, which
// any laptop can push to, and this goes to a terminal.
func plain(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

func printCommit(out io.Writer, blame gitdiff.Blame) {
	if !blame.Committed() {
		fmt.Fprintln(out, "  not committed yet")
		fmt.Fprintln(out)
		return
	}
	fmt.Fprintf(out, "  commit %s  %s\n", blame.Short, blame.Summary)
	fmt.Fprintf(out, "  by %s, %s\n\n", blame.Author, blame.At.Format("2006-01-02"))
}

// splitTarget parses file:line, rejecting anything that is not one.
func splitTarget(arg string) (string, int, error) {
	file, num, ok := strings.Cut(arg, ":")
	if !ok {
		return "", 0, fmt.Errorf("%q is not a file:line", arg)
	}
	line, err := strconv.Atoi(num)
	if err != nil || line < 1 {
		return "", 0, fmt.Errorf("%q has no line number", arg)
	}
	return file, line, nil
}

// repoRel makes the argument relative to the repository, whether it was given
// absolute or relative to wherever the command was run.
func repoRel(root, cwd, file string) (string, error) {
	if !filepath.IsAbs(file) {
		file = filepath.Join(cwd, file)
	}
	rel, err := filepath.Rel(root, filepath.Clean(file))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%s is outside %s", file, root)
	}
	return filepath.ToSlash(rel), nil
}
