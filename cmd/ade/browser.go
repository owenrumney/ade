package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/owenrumney/ade/internal/browserhost"
)

func newBrowserCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "browser",
		Short: "The Firefox add-on that opens a pull request in ade",
		Long: `The add-on injects an "Open in Ade" button into a GitHub pull request page.
A browser cannot run a command, so it talks to ade through Firefox's native
messaging: install writes the manifest that allows it, host is what Firefox
then runs.`,
	}
	cmd.AddCommand(newBrowserInstallCommand())
	cmd.AddCommand(newBrowserUninstallCommand())
	cmd.AddCommand(newBrowserHostCommand())
	return cmd
}

func newBrowserInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "install",
		Short:         "Allow the Firefox add-on to talk to this ade",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			done, err := browserhost.Install()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "manifest  %s\n", done.Manifest)
			fmt.Fprintf(out, "host      %s\n", done.Shim)
			fmt.Fprintf(out, "\nThe add-on itself is installed separately: make extension-sign for one\n")
			fmt.Fprintf(out, "that survives a restart, or about:debugging → Load Temporary Add-on →\n")
			fmt.Fprintf(out, "extension/manifest.json for one that does not.\n")
			return nil
		},
	}
}

func newBrowserUninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "uninstall",
		Short:         "Stop the add-on from talking to ade",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := browserhost.Uninstall()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", path)
			return nil
		},
	}
}

// newBrowserHostCommand is what Firefox runs. Its stdout is the protocol, so
// it must print nothing of its own.
func newBrowserHostCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "host",
		Short:         "Speak Firefox's native messaging protocol on stdin and stdout",
		Hidden:        true,
		Args:          cobra.ArbitraryArgs, // Firefox passes the manifest path and the add-on id
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return browserhost.Serve(cmd.Context(), os.Stdin, os.Stdout, version)
		},
	}
}
