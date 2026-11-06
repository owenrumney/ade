package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/oneshot"
)

// showLimit is the largest picture worth putting in the window.
const showLimit = 8 << 20

// showTypes are what the Shots tab draws.
var showTypes = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true}

// newShowCommand puts pictures in the window's Shots tab, so an agent can show
// the change it just made where the claim about it is being made.
func newShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "show <image>...",
		Short:         "Show an image in ade's Shots tab",
		Args:          cobra.MinimumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			box, err := outbox(cmd.Context())
			if err != nil {
				return err
			}
			for _, at := range args {
				to, err := showImage(box, at)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), to)
			}
			return nil
		},
	}
	// Kept so the old call still works: the tab draws at its own size.
	cmd.Flags().String("width", "", "unused; the Shots tab sizes the picture")
	_ = cmd.Flags().MarkHidden("width")
	return cmd
}

// outbox is the directory the window watches for this checkout.
func outbox(ctx context.Context) (string, error) {
	if at := os.Getenv(oneshot.OutboxEnv); at != "" {
		return at, nil
	}
	here, err := os.Getwd()
	if err != nil {
		return "", err
	}
	gitDir, err := gitdiff.GitDir(ctx, here)
	if err != nil {
		return "", fmt.Errorf("no checkout here to show a picture in: %w", err)
	}
	return filepath.Join(gitDir, "ade", "outbox"), nil
}

// showImage copies one picture into the outbox under its own name, and answers
// where it landed. The window is watching the directory, not this process.
func showImage(box, at string) (string, error) {
	kind := strings.ToLower(filepath.Ext(at))
	if !showTypes[kind] {
		return "", fmt.Errorf("%s: only png, jpeg and gif can be shown", at)
	}
	info, err := os.Stat(at)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", at, err)
	}
	if info.Size() > showLimit {
		return "", fmt.Errorf("%s is %dMB; the limit is %dMB", at, info.Size()>>20, showLimit>>20)
	}
	if err := os.MkdirAll(box, 0o750); err != nil {
		return "", fmt.Errorf("make the outbox: %w", err)
	}
	to := filepath.Join(box, filepath.Base(at))
	if err := copyFile(at, to); err != nil {
		return "", err
	}
	return to, nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from) // #nosec G304 -- the file the user named
	if err != nil {
		return fmt.Errorf("read %s: %w", from, err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- the outbox, under a stripped basename
	if err != nil {
		return fmt.Errorf("write %s: %w", to, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("write %s: %w", to, err)
	}
	return out.Close()
}
