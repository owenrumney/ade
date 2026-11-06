package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// showLimit is the largest picture worth putting in a terminal. Past it the
// escape sequence costs more than looking at the file.
const showLimit = 8 << 20

// showTypes are what the terminal's image addon decodes.
var showTypes = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true}

// newShowCommand draws pictures in ade's terminal, so an agent can show the
// change it just made where it makes the claim.
func newShowCommand() *cobra.Command {
	var width string
	cmd := &cobra.Command{
		Use:           "show <image>...",
		Short:         "Draw an image in ade's terminal",
		Args:          cobra.MinimumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, at := range args {
				if err := showImage(cmd.OutOrStdout(), at, width); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&width, "width", "auto",
		"how wide to draw it: auto, a number of columns, or a percentage such as 60%")
	return cmd
}

// showImage writes one picture as an iTerm2 inline image, which ade's terminal
// reads. Anything else on the line is left alone.
func showImage(out io.Writer, at, width string) error {
	kind := strings.ToLower(filepath.Ext(at))
	if !showTypes[kind] {
		return fmt.Errorf("%s: only png, jpeg and gif can be drawn", at)
	}
	info, err := os.Stat(at)
	if err != nil {
		return fmt.Errorf("read %s: %w", at, err)
	}
	if info.Size() > showLimit {
		return fmt.Errorf("%s is %dMB; the limit is %dMB", at, info.Size()>>20, showLimit>>20)
	}
	raw, err := os.ReadFile(at) // #nosec G304 -- the file the user named
	if err != nil {
		return fmt.Errorf("read %s: %w", at, err)
	}
	_, err = fmt.Fprintf(out, "\x1b]1337;File=inline=1;size=%d;width=%s;preserveAspectRatio=1:%s\a\n",
		len(raw), width, base64.StdEncoding.EncodeToString(raw))
	return err
}
