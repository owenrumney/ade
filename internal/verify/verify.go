// Package verify runs a repository's own checks after a turn, so "done" is
// known rather than claimed. Design: .git/ade/docs/verified-done.md.
package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/owenrumney/ade/internal/spawncap"
	"github.com/owenrumney/ade/internal/statedir"
)

// Check is one command, named so a failure can be reported without quoting a
// command line at you.
type Check struct {
	Name string `json:"name"`
	Run  string `json:"run"`
	// Dir is where the command runs, relative to the checkout. Empty is the root.
	Dir string `json:"dir,omitempty"`
	// Narrow is Run scoped to what changed: {{.Packages}} and {{.Files}} expand
	// to the changed set. Empty means the check only runs in the full pass.
	Narrow string `json:"narrow,omitempty"`
}

// Config is the list a checkout runs. Asked records that the offer to detect
// one was made, so declining it is remembered and never asked again.
type Config struct {
	Checks []Check `json:"checks"`
	Asked  bool    `json:"asked,omitempty"`
}

// Result is one command's outcome. Output is the tail, not the whole log: a
// failing build can print megabytes and the last lines are the reason.
type Result struct {
	Name     string `json:"name"`
	Run      string `json:"run"`
	Passed   bool   `json:"passed"`
	TimedOut bool   `json:"timedOut,omitempty"`
	Output   string `json:"output,omitempty"`
	Millis   int64  `json:"millis"`
}

// Outcome is a whole list's run. Ran is what got as far as starting, so a run
// stopped at the second of three says so.
type Outcome struct {
	Ran     []Result  `json:"ran"`
	Failed  *Result   `json:"failed,omitempty"`
	Total   int       `json:"total"`
	At      time.Time `json:"at"`
	Stopped bool      `json:"stopped,omitempty"`
	// Narrow marks the narrow pass, which is feedback and never the verdict.
	Narrow bool `json:"narrow,omitempty"`
}

// Passed is a run where everything that was asked for ran and succeeded. A
// narrow pass is never one: the full list has not run.
func (o Outcome) Passed() bool { return !o.Narrow && o.clean() }

// NarrowPassed is a narrow pass that found nothing, so the full list may start.
func (o Outcome) NarrowPassed() bool { return o.Narrow && o.clean() }

func (o Outcome) clean() bool {
	return o.Failed == nil && !o.Stopped && len(o.Ran) == o.Total && o.Total > 0
}

// eachCap bounds one command and listCap the whole list. A check slower than
// this is one to run yourself.
const (
	eachCap = 5 * time.Minute
	listCap = 15 * time.Minute
)

// tailLines is how much of a failure's output is kept, which is as much as
// the agent needs to see the first error and the summary under it.
const tailLines = 40

// Path is where a repository's list lives. The common git dir, so every
// worktree of a repository shares one list.
func Path(commonGitDir string) string {
	if commonGitDir == "" {
		return ""
	}
	return filepath.Join(statedir.Git(commonGitDir), "checks.json")
}

// Load reads the list. A missing file is an empty config and not an error.
func Load(path string) (Config, error) {
	if path == "" {
		return Config{}, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- ade state under .git
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read the check list: %w", err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("parse the check list: %w", err)
	}
	return config, config.valid()
}

// Save writes the list, making the state directory if this is the first one.
func Save(path string, config Config) error {
	if path == "" {
		return errors.New("nowhere to save the check list")
	}
	if err := config.valid(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// Run works through the list in order and stops at the first failure: a build
// failure makes every later failure noise.
func Run(ctx context.Context, root string, checks []Check) Outcome {
	out := Outcome{Total: len(checks), At: time.Now()}
	if len(checks) == 0 {
		return out
	}
	list, cancel := context.WithTimeout(ctx, listCap)
	defer cancel()

	for _, check := range checks {
		result := one(list, root, check)
		out.Ran = append(out.Ran, result)
		if !result.Passed {
			failed := result
			out.Failed = &failed
			break
		}
	}
	// Cancelled from outside rather than by a check failing: the tree moved
	// under it, or Stop was pressed, and the answer is not "it failed".
	if ctx.Err() != nil {
		out.Stopped = true
		out.Failed = nil
	}
	return out
}

// one runs a single check in a process group of its own, so a command that
// starts a build leaves nothing behind when it is cancelled.
func one(ctx context.Context, root string, check Check) Result {
	started := time.Now()
	each, cancel := context.WithTimeout(ctx, eachCap)
	defer cancel()

	cmd := exec.CommandContext(each, "sh", "-c", check.Run) // #nosec G204 -- the command is ade's own state, never the checkout's
	cmd.Dir = filepath.Join(root, check.Dir)
	cmd.Env = spawncap.Apply(os.Environ())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf

	err := cmd.Run()
	result := Result{
		Name: check.Name, Run: check.Run, Passed: err == nil,
		Millis: time.Since(started).Milliseconds(),
	}
	if errors.Is(each.Err(), context.DeadlineExceeded) {
		result.Passed, result.TimedOut = false, true
	}
	if !result.Passed {
		result.Output = Tail(buf.String(), tailLines)
	}
	return result
}

// Tail is the last n lines of output, with a marker when anything was cut.
func Tail(out string, n int) string {
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return ""
	}
	lines := strings.Split(out, "\n")
	if len(lines) <= n {
		return out
	}
	cut := len(lines) - n
	return fmt.Sprintf("… %d earlier line(s)\n%s", cut, strings.Join(lines[cut:], "\n"))
}

func (c Config) valid() error {
	for i, check := range c.Checks {
		if strings.TrimSpace(check.Name) == "" {
			return fmt.Errorf("check %d has no name", i+1)
		}
		if strings.TrimSpace(check.Run) == "" {
			return fmt.Errorf("check %q has nothing to run", check.Name)
		}
		if check.Dir != "" && !filepath.IsLocal(check.Dir) {
			return fmt.Errorf("check %q runs in %q, which is outside the checkout", check.Name, check.Dir)
		}
		if err := validNarrow(check.Narrow); err != nil {
			return fmt.Errorf("check %q has a narrow form that does not read: %w", check.Name, err)
		}
	}
	return nil
}
