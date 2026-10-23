// Package editor opens a checkout in the user's editor, which is the only
// front end ade has.
package editor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/owenrumney/ade/internal/concierge"
)

// ErrNone means there is no editor to open: none on PATH, or ADE_EDITOR=none.
var ErrNone = errors.New("no editor cli on PATH")

// candidates are tried in order when ADE_EDITOR does not name one.
var candidates = []string{"ade-editor", "code", "cursor", "codium"} //nolint:gochecknoglobals // a table, not state

// appCLI is ADE.app's own cli, for when make editor-install could not link it onto PATH.
const appCLI = "/Applications/ADE.app/Contents/Resources/app/bin/ade-editor"

// OpenDesk shows dir in the one window: the desk's workspace, with dir among
// its folders. The extension attaches to the folder as it arrives.
func OpenDesk(dir string) error {
	file, err := concierge.Desk(context.Background(), dir)
	if err != nil {
		return err
	}
	return Open(file)
}

// Open shows dir in the editor: a window already on it comes forward, and
// otherwise a new one opens. It does not wait for the window.
func Open(dir string) error {
	bin, err := find(os.Getenv("ADE_EDITOR"), lookPath)
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, dir) // #nosec G204 -- the user's own editor, and a directory ade is serving
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open %s in %s: %w", dir, bin, err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func lookPath(name string) (string, error) {
	bin, err := exec.LookPath(name)
	if err != nil && name == "ade-editor" {
		return exec.LookPath(appCLI)
	}
	return bin, err
}

// find is which cli to run. A named one that is missing is an error rather
// than a quiet fall through to another editor.
func find(named string, look func(string) (string, error)) (string, error) {
	named = strings.TrimSpace(named)
	if named == "none" {
		return "", ErrNone
	}
	if named != "" {
		bin, err := look(named)
		if err != nil {
			return "", fmt.Errorf("ADE_EDITOR names %s: %w", named, err)
		}
		return bin, nil
	}
	for _, name := range candidates {
		if bin, err := look(name); err == nil {
			return bin, nil
		}
	}
	return "", ErrNone
}
