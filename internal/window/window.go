// Package window opens a checkout in ade's own window, the only front end.
package window

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

var ErrNone = errors.New("ade's own window is not installed: make app-install")

var windowApp = "/Applications/ADE.app"

func Open(dir string) error {
	if runtime.GOOS != "darwin" {
		return ErrNone
	}
	if _, err := os.Stat(windowApp); err != nil {
		return ErrNone
	}
	// -n so a second checkout is a second window.
	cmd := exec.Command("open", "-na", windowApp, "--args", dir) // #nosec G204 -- a directory ade is serving
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open %s in the window: %w", dir, err)
	}
	return cmd.Process.Release()
}
