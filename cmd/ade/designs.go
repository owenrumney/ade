package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/owenrumney/ade/internal/designs"
	"github.com/owenrumney/ade/internal/docs"
	"github.com/owenrumney/ade/internal/gitdiff"
	adesettings "github.com/owenrumney/ade/internal/settings"
)

// newDesignsCommand is the shared store: one directory per design, the same
// on every machine. See the design doc.
func newDesignsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "designs",
		Short: "The shared design store",
		Long: `designs is where design documents live: one directory each, under
~/.ade/designs, shared between your machines over git when designs_remote is
set in ~/.ade/settings.json.

A design's front matter names the repositories it touches, and starting work
on it makes a worktree in each of them on one branch.`,
		SilenceUsage: true,
	}
	cmd.AddCommand(newDesignsListCommand(), newDesignsImportCommand(), newDesignsSyncCommand())
	return cmd
}

// store is the one on this machine, made if it is not there yet.
func store() (*designs.Store, error) {
	home, err := adesettings.Dir()
	if err != nil {
		return nil, err
	}
	remote := ""
	if cfg, err := adesettings.Load(); err == nil {
		remote = cfg.DesignsRemote
	}
	return designs.New(filepath.Join(home, "designs"), remote), nil
}

func newDesignsListCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "ls",
		Short:         "List the designs in the store",
		Aliases:       []string{"list"},
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			at, err := store()
			if err != nil {
				return err
			}
			list, err := at.List()
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Printf("no designs in %s yet\n", at.Root())
				return nil
			}
			for _, design := range list {
				where := strings.Join(design.Meta.Repos, ", ")
				if where == "" {
					where = "no repositories named"
				}
				fmt.Printf("%-40s %s\n", design.Name, where)
			}
			return nil
		},
	}
}

func newDesignsSyncCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "sync",
		Short:         "Fetch and push the store",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			at, err := store()
			if err != nil {
				return err
			}
			if err := at.Sync(cmd.Context()); err != nil {
				return err
			}
			if !at.Shared() {
				fmt.Println("committed; no designs_remote set, so nothing was pushed")
				return nil
			}
			fmt.Printf("synced %s with %s\n", at.Root(), at.Remote())
			return nil
		},
	}
}

func newDesignsImportCommand() *cobra.Command {
	var repo string
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Copy this checkout's design documents into the store",
		Long: `import copies every .md this checkout keeps as a design into the
store, adding the repository to each document's front matter.

The originals are left where they are, and a name already in the store is
left alone rather than overwritten. Run it once per repository.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			root, err := gitdiff.RepoRoot(ctx, cwd)
			if err != nil {
				return err
			}
			common, err := gitdiff.CommonGitDir(ctx, root)
			if err != nil {
				return err
			}
			at, err := store()
			if err != nil {
				return err
			}
			if err := at.Ensure(ctx); err != nil {
				return err
			}
			if repo == "" {
				if owner, name := gitdiff.Origin(ctx, root); name != "" {
					repo = strings.TrimPrefix(owner+"/"+name, "/")
				}
			}
			var all []designs.Imported
			for _, dir := range docs.SearchDirs(root, common, "") {
				took, err := designs.Import(at.Root(), dir, repo)
				if err != nil {
					return err
				}
				all = append(all, took...)
			}
			for _, each := range all {
				if each.Took() {
					fmt.Printf("imported %s\n", each.Name)
					continue
				}
				fmt.Printf("left     %s — %s\n", each.Name, each.Why)
			}
			fmt.Println(designs.Summary(all))
			return nil
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "owner/name to record (default: this checkout's origin)")
	return cmd
}
