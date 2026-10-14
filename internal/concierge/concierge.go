// Package concierge is the front desk's home: one checkout under ade's own
// state, outside every scan root, holding a conversation that spans sessions.
package concierge

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/owenrumney/ade/internal/runlog"
	"github.com/owenrumney/ade/internal/settings"
)

// Model is what the desk runs on. It reads ade's own API and never a
// repository, so the cheap one is the right one.
const Model = "haiku"

// Agent is the provider: only Claude forks, resumes and takes an MCP config
// the way the desk needs.
const Agent = "claude"

// name is the directory under ade's state. A sibling of scratch, not a child,
// which is what keeps it out of every scan root.
const name = "concierge"

//go:embed role.md
var role string

// Role is the instruction the desk runs under, written into its checkout.
func Role() string { return strings.TrimSpace(role) + "\n" }

// Home is where the desk lives, whether or not it has been made.
func Home() (string, error) {
	dir, err := settings.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// Is reports whether a checkout is the desk's, so a surface that lists work
// can leave it out.
func Is(path string) bool {
	home, err := Home()
	if err != nil || strings.TrimSpace(path) == "" {
		return false
	}
	here, err := filepath.EvalSymlinks(path)
	if err != nil {
		here = path
	}
	there, err := filepath.EvalSymlinks(home)
	if err != nil {
		there = home
	}
	return filepath.Clean(here) == filepath.Clean(there)
}

// Ensure makes the desk's checkout if it is not there: a real repository,
// because every ade surface assumes one. The role is rewritten each time.
func Ensure(ctx context.Context) (string, error) {
	dir, err := Home()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("make the concierge's home: %w", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		if err := start(ctx, dir); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(Role()), 0o600); err != nil {
		return "", fmt.Errorf("write the concierge's role: %w", err)
	}
	// The desk is never boxed. A container gets no MCP config, which would
	// leave it with no tools at all. See the design doc.
	if err := runlog.Open(runlog.Path(filepath.Join(dir, ".git"))).SetSandbox(false); err != nil {
		return "", fmt.Errorf("record that the desk runs unboxed: %w", err)
	}
	return dir, nil
}

func start(ctx context.Context, dir string) error {
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"commit", "--allow-empty", "-m", "the front desk"},
	} {
		if err := git(ctx, dir, args...); err != nil {
			return err
		}
	}
	return nil
}

// git folds what git said into the error, and commits as ade: the desk's
// checkout is ade's own and not anybody's work.
func git(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- ade's own arguments
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=ade", "GIT_AUTHOR_EMAIL=ade@localhost",
		"GIT_COMMITTER_NAME=ade", "GIT_COMMITTER_EMAIL=ade@localhost")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s in %s: %w: %s", args[0], dir, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// MCPPath is where the launcher serves the desk's tools. Named here so the
// session's side can reach it without importing the launcher.
const MCPPath = "/mcp/concierge"

// ServerName is what the desk sees its tools as. The role names it.
const ServerName = "ade_concierge"
