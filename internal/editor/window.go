package editor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/owenrumney/ade/internal/settings"
)

// ErrNoWindow means ade's own window is not installed on this machine.
var ErrNoWindow = errors.New("ade's own window is not installed: make app-install")

// windowApp is where `make app-install` puts it.
var windowApp = "/Applications/ADE.app"

// Wanted is which front end `ade .` should open. The setting decides,
// and ADE_FRONT overrides it for one invocation.
func Wanted() string {
	if named := os.Getenv("ADE_FRONT"); named != "" {
		return named
	}
	if cfg, err := settings.Load(); err == nil {
		return cfg.Front
	}
	return ""
}

// OpenWindow shows dir in ade's own window. Unlike the editor it takes
// the checkout itself: the window has no workspace file to put it in.
func OpenWindow(dir string) error {
	if runtime.GOOS != "darwin" {
		return ErrNoWindow
	}
	if _, err := os.Stat(windowApp); err != nil {
		return ErrNoWindow
	}
	// -n so a second checkout is a second window, as the editor does.
	cmd := exec.Command("open", "-na", windowApp, "--args", dir) // #nosec G204 -- a directory ade is serving
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open %s in the window: %w", dir, err)
	}
	return cmd.Process.Release()
}
