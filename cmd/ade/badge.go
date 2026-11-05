package main

import (
	"github.com/spf13/cobra"

	"github.com/owenrumney/ade/internal/gate"
)

// newBadgeCommand is what an agent CLI's own hooks run to say what it is
// doing. It prints nothing: claude reads a hook's output as context.
func newBadgeCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "badge",
		Short:         "Tell ade what the agent in this terminal is doing",
		Hidden:        true,
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = gate.Badge(cmd.Context(), cmd.InOrStdin(), args)
			return nil
		},
	}
}
