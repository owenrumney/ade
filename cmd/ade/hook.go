package main

import (
	"github.com/spf13/cobra"

	"github.com/owenrumney/ade/internal/gate"
)

// newHookCommand is what Claude Code runs before a gated tool call. Not for a
// person to type: the agent's own settings point at it.
func newHookCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "hook",
		Short:         "Decide one tool call against ade's permission rules",
		Hidden:        true,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The decision is on stdout either way; an error here is only for
			// the log, so a refused call still reads as refused.
			_ = gate.Run(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
			return nil
		},
	}
}
