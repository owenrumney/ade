package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	adesettings "github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/wiki"
)

func newWikiCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wiki",
		Short: "Manage ADE's shared wiki clone",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newWikiInitCommand(), newWikiStatusCommand(), newWikiSyncCommand(), newWikiDiscardCommand(), newWikiPruneCommand(), newWikiDistillCommand())
	return cmd
}

func wikiStore() (*wiki.Store, adesettings.Settings, error) {
	config, err := adesettings.Load()
	if err != nil {
		return nil, adesettings.Settings{}, err
	}
	dir, err := adesettings.Dir()
	if err != nil {
		return nil, adesettings.Settings{}, err
	}
	return wiki.New(dir, config.WikiRemote), config, nil
}

func newWikiInitCommand() *cobra.Command {
	var remote string
	var distill bool
	cmd := &cobra.Command{
		Use:           "init --remote <url> [--distill]",
		Short:         "Enable the shared wiki",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, config, err := wikiStore()
			if err != nil {
				return err
			}
			// --distill alone turns distillation on for a wiki already set up.
			if remote != "" || !distill || !store.Enabled() {
				if err := store.Init(cmd.Context(), remote); err != nil {
					return err
				}
				config.WikiRemote = strings.TrimSpace(remote)
			}
			if distill && !config.WikiDistill {
				if err := store.StartDistilling(transcriptPaths()); err != nil {
					return err
				}
				config.WikiDistill = true
			}
			if err := adesettings.Save(config); err != nil {
				return err
			}
			fmt.Println(store.Root())
			if distill {
				fmt.Println("distillation is on: sessions from now on are published to the wiki")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&remote, "remote", "", "Git remote for the shared wiki")
	cmd.Flags().BoolVar(&distill, "distill", false, "Publish pages distilled from this machine's sessions, from now on")
	return cmd
}

func newWikiStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "status",
		Short:         "Show shared wiki status",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, _, err := wikiStore()
			if err != nil {
				return err
			}
			st := store.Status(cmd.Context())
			fmt.Printf("root: %s\nremote: %s\nrevision: %s\nlast fetch: %s\ndirty: %t\n", st.Root, st.Remote, st.Revision, st.LastFetch.Format("2006-01-02T15:04:05Z07:00"), st.Dirty)
			if st.Repair != "" {
				fmt.Printf("repair: %s\n", st.Repair)
			}
			if st.LastError != "" {
				fmt.Printf("last sync error: %s\n", st.LastError)
			}
			held, err := store.DistillStatus()
			if err != nil {
				return err
			}
			for _, one := range held {
				if one.Failures > 0 {
					fmt.Printf("distill %s: %d failures in a row", one.Repo, one.Failures)
					if one.LastError != "" {
						fmt.Printf(" — %s", one.LastError)
					}
					fmt.Println()
				}
				if one.Skipped != "" {
					fmt.Printf("distill %s: gave up on prose — %s\n", one.Repo, one.Skipped)
				}
			}
			drafts, err := store.Drafts()
			if err != nil {
				return err
			}
			for _, draft := range drafts {
				fmt.Printf("retained draft: %s (%s)", draft.Session, draft.Repo)
				if draft.State != "" {
					fmt.Printf(" %s", draft.State)
				}
				if draft.Error != "" {
					fmt.Printf(" — %s", draft.Error)
				}
				fmt.Println()
			}
			return nil
		},
	}
}

func newWikiDiscardCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "discard <session-id>",
		Short:         "Remove one retained wiki draft",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, err := wikiStore()
			if err != nil {
				return err
			}
			return store.Discard(args[0])
		},
	}
}

func newWikiPruneCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "prune",
		Short:         "Remove clean abandoned drafts and expired publish clones",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, _, err := wikiStore()
			if err != nil {
				return err
			}
			report, err := store.Prune(cmd.Context())
			for _, draft := range report.Retained {
				fmt.Printf("kept: %s (%s) — ade wiki discard %s to remove it\n", draft.Session, draft.Repo, draft.Session)
			}
			for _, session := range report.Removed {
				fmt.Printf("removed: %s\n", session)
			}
			for _, clone := range report.Clones {
				fmt.Printf("expired publish clone: %s\n", clone)
			}
			return err
		},
	}
}

func newWikiSyncCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "sync",
		Short:         "Refresh the shared wiki now",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, _, err := wikiStore()
			if err != nil {
				return err
			}
			if err := store.Sync(cmd.Context()); err != nil {
				return err
			}
			fmt.Println("wiki refreshed")
			return nil
		},
	}
}
