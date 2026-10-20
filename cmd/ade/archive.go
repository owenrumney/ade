package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/owenrumney/ade/internal/archive"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/jobs"
	adesettings "github.com/owenrumney/ade/internal/settings"
)

func newDocArchiveCommand(docsDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "archive <document>",
		Short: "Publish a finished design to the wiki and delete the local file",
		Long: `archive distils a finished design document into a page on the shared wiki,
under designs/ in this repository's scope, and deletes the local file once that
page is published.

The page is what someone who was not there needs next time: what was built, the
decisions and why, the gotchas. Not the argument that got there. The original
was never in a commit, and losing it is the point: there is one place to look
for a finished design.

A failed publish leaves the document exactly where it was. Use ade doc rm for
the one you started by mistake and never want published.`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			root := repoRootOrCwd(cmd)
			commonGitDir, err := gitdiff.CommonGitDir(ctx, root)
			if err != nil {
				return err
			}
			owner, name := gitdiff.Origin(ctx, root)
			if owner == "" || name == "" {
				return errors.New("this checkout has no origin owner/repo, so there is no wiki scope to archive into")
			}
			doc, err := archive.Find(root, commonGitDir, *docsDir, args[0])
			if err != nil {
				return err
			}
			store, _, err := wikiStore()
			if err != nil {
				return err
			}
			if !store.Enabled() {
				return errors.New("wiki is disabled; run ade wiki init --remote <url>")
			}
			dir, err := adesettings.Dir()
			if err != nil {
				return err
			}
			pr := archive.PRFor(ctx, root, doc.File)
			out, err := archive.One(ctx, store, owner+"/"+name, doc, pr, jobs.ArchiveModel("", dir))
			if err != nil {
				return err
			}
			fmt.Printf("archived %s to %s", out.File, out.Page)
			if out.Commit != "" {
				fmt.Printf(" (%s)", out.Commit[:min(7, len(out.Commit))])
			}
			fmt.Println()
			return nil
		},
	}
}
