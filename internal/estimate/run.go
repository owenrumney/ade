// Package estimate says how long a build will take and how much of the plan
// window it will use, from how builds like it went before.
package estimate

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/transcripts"
)

// KindBuild is a design handed to a fresh agent to implement.
const KindBuild = "build"

// file is the machine-wide record, under settings.Dir(): a build in another
// repository is still evidence.
const file = "estimates.jsonl"

// Run is one finished run and what it was like.
type Run struct {
	Kind           string            `json:"kind"`
	Model          string            `json:"model,omitempty"`
	Repo           string            `json:"repo,omitempty"`
	DocWords       int               `json:"docWords,omitempty"`
	ChecklistItems int               `json:"checklistItems,omitempty"`
	Started        time.Time         `json:"started"`
	Finished       time.Time         `json:"finished"`
	Usage          transcripts.Usage `json:"usage"`
	// Failed is a run that errored or was stopped: kept so Merge can drop its
	// backfilled copy, never counted as how long a run takes.
	Failed bool `json:"failed,omitempty"`
}

// Duration is the run's wall-clock time.
func (r Run) Duration() time.Duration { return r.Finished.Sub(r.Started) }

// Units is the run's cost-weighted usage, or zero when it could not be priced.
func (r Run) Units() float64 {
	if !r.Usage.CostKnown {
		return 0
	}
	return r.Usage.CostUSD
}

// Record appends a finished run to the machine's history in dir.
func Record(dir string, run Run) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	line, err := json.Marshal(run)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, file), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- ade's own state directory
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Recorded is every run recorded in dir. A line that does not parse is skipped.
func Recorded(dir string) ([]Run, error) {
	f, err := os.Open(filepath.Join(dir, file)) // #nosec G304 -- ade's own state directory
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []Run
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 64*1024), 1024*1024)
	for scan.Scan() {
		var run Run
		if json.Unmarshal(scan.Bytes(), &run) == nil && run.Duration() > 0 {
			if run.Repo != "" {
				run.Repo = resolved(run.Repo)
			}
			out = append(out, run)
		}
	}
	return out, scan.Err()
}

// Size is how big a design is: its words, and the requirements drift reads.
func Size(doc string) (words, items int) {
	return len(strings.Fields(doc)), len(design.Requirements(doc))
}

// Repo is the main checkout dir belongs to, so a build in one of its
// worktrees counts as the same repository.
func Repo(dir string) string {
	if dir == "" {
		return ""
	}
	dir = resolved(filepath.Clean(dir))
	for at := dir; ; {
		git := filepath.Join(at, ".git")
		if info, err := os.Stat(git); err == nil {
			if info.IsDir() {
				return at
			}
			return resolved(worktreeMain(git, at))
		}
		parent := filepath.Dir(at)
		if parent == at {
			return dir
		}
		at = parent
	}
}

// resolved is path with its symlinks followed, so two routes to one checkout
// are one repository; a path that does not resolve is kept as given.
func resolved(path string) string {
	if out, err := filepath.EvalSymlinks(path); err == nil {
		return out
	}
	return path
}

// worktreeMain follows a worktree's .git file back to its main checkout.
func worktreeMain(gitFile, at string) string {
	data, err := os.ReadFile(gitFile) // #nosec G304 -- a checkout's own .git file
	if err != nil {
		return at
	}
	pointer := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
	if !filepath.IsAbs(pointer) {
		pointer = filepath.Join(at, pointer)
	}
	sep := string(filepath.Separator)
	if main, _, ok := strings.Cut(filepath.Clean(pointer), sep+".git"+sep+"worktrees"+sep); ok {
		return main
	}
	return at
}
