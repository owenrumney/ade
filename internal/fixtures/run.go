package fixtures

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/verify"
)

// DefaultModel is what the suite runs on unless told otherwise: the cheapest,
// since a suite is fixtures times runs. The report names it.
const DefaultModel = "haiku"

// Request is one prompt to run in a checkout that is the fixture's to ruin.
type Request struct {
	Dir    string
	Prompt string
	Model  string
}

// Response is what the run said and what it cost.
type Response struct {
	Text    string
	CostUSD float64
}

// Runner runs a prompt. Tests inject one; the CLI uses ColdRunner.
type Runner func(ctx context.Context, req Request) (Response, error)

// Options shape a suite run. Runs below one is one.
type Options struct {
	Model string
	Runs  int
}

// Result is one run of one fixture. Checked names the assertions it makes, so
// a report can carry the denominator; Failed is the subset that did not hold, in words.
type Result struct {
	Name    string   `json:"name"`
	Run     int      `json:"run"`
	Model   string   `json:"model"`
	Passed  bool     `json:"passed"`
	Checked []string `json:"checked"`
	Failed  []string `json:"failed"`
	Stale   bool     `json:"stale,omitempty"`
	Error   string   `json:"error,omitempty"`
	CostUSD float64  `json:"costUSD,omitempty"`
}

// Assertions is the order the report lists them in.
var Assertions = []string{"touches", "untouched", "verify", "comments", "asks"} //nolint:gochecknoglobals // a list, not state

// rules are the files the agent reads before it starts, copied from the checkout
// over the fixture's tree: the suite measures them as they are now, not at the base.
var rules = []string{"CLAUDE.md", "AGENTS.md", ".claude"} //nolint:gochecknoglobals // a list, not state

// Run replays each named fixture, or all of them, Runs times each. Serial:
// two agents in two worktrees of one repository is two agents on one laptop.
func Run(ctx context.Context, root, dir string, run Runner, names []string, opts Options) ([]Result, error) {
	all, err := List(dir)
	if err != nil {
		return nil, err
	}
	chosen, err := pick(all, names)
	if err != nil {
		return nil, err
	}
	if opts.Model == "" {
		opts.Model = DefaultModel
	}
	checks, err := verify.Load(verify.Path(commonGitDir(ctx, root)))
	if err != nil {
		return nil, err
	}

	var results []Result
	for _, fixture := range chosen {
		for n := 1; n <= max(1, opts.Runs); n++ {
			if ctx.Err() != nil {
				return results, ctx.Err()
			}
			result := one(ctx, root, fixture, run, opts.Model, checks.Checks)
			result.Run = n
			results = append(results, result)
		}
	}
	return results, nil
}

func pick(all []Fixture, names []string) ([]Fixture, error) {
	if len(names) == 0 {
		return all, nil
	}
	var chosen []Fixture
	for _, name := range names {
		at := slices.IndexFunc(all, func(f Fixture) bool { return f.Name == name })
		if at < 0 {
			return nil, fmt.Errorf("no fixture called %q; `ade fixtures ls` lists them", name)
		}
		chosen = append(chosen, all[at])
	}
	return chosen, nil
}

// one replays a fixture in a throwaway worktree, so the checkout it was
// captured from is never touched and "restore" is deleting the worktree.
func one(ctx context.Context, root string, fixture Fixture, run Runner, model string, checks []verify.Check) Result {
	result := Result{Name: fixture.Name, Model: model, Stale: fixture.Stale(time.Now()), Checked: fixture.Expect.checked()}
	if fixture.Expect.Empty() {
		result.Failed = []string{"expect.yaml asserts nothing"}
		return result
	}
	wt, done, err := worktree(ctx, root, fixture)
	if err != nil {
		result.Error = err.Error()
		result.Failed = []string{"setup: " + err.Error()}
		return result
	}
	defer done()

	res, err := run(ctx, Request{Dir: wt, Prompt: fixture.Prompt, Model: model})
	result.CostUSD = res.CostUSD
	if err != nil {
		result.Error = err.Error()
		result.Failed = []string{"run: " + err.Error()}
		return result
	}
	result.Failed = measure(ctx, wt, fixture.Expect, res.Text, checks)
	result.Passed = len(result.Failed) == 0
	return result
}

// worktree checks the base out, lays the patch and the current rules over it,
// and commits, so what the run changed is one diff against HEAD.
func worktree(ctx context.Context, root string, fixture Fixture) (string, func(), error) {
	if _, err := git(ctx, root, "cat-file", "-e", fixture.Base+"^{commit}"); err != nil {
		return "", nil, fmt.Errorf("base commit %s is gone", short(fixture.Base))
	}
	wt, err := os.MkdirTemp("", "ade-fixture-"+fixture.Name+"-")
	if err != nil {
		return "", nil, err
	}
	done := func() {
		_, _ = git(ctx, root, "worktree", "remove", "--force", wt)
		_ = os.RemoveAll(wt)
	}
	if _, err := git(ctx, root, "worktree", "add", "--detach", "-q", wt, fixture.Base); err != nil {
		done()
		return "", nil, err
	}
	if len(strings.TrimSpace(string(fixture.Patch))) > 0 {
		patch := filepath.Join(wt, ".ade-tree.patch")
		if err := os.WriteFile(patch, fixture.Patch, 0o600); err != nil {
			done()
			return "", nil, err
		}
		_, err := git(ctx, wt, "apply", "--whitespace=nowarn", patch)
		_ = os.Remove(patch)
		if err != nil {
			done()
			return "", nil, fmt.Errorf("the tree patch no longer applies: %w", err)
		}
	}
	if err := overlayRules(root, wt); err != nil {
		done()
		return "", nil, err
	}
	if _, err := git(ctx, wt, "add", "-A"); err != nil {
		done()
		return "", nil, err
	}
	if _, err := gitAs(ctx, wt, "commit", "-q", "--allow-empty", "-m", "ade: fixture start"); err != nil {
		done()
		return "", nil, err
	}
	return wt, done, nil
}

func overlayRules(root, wt string) error {
	for _, name := range rules {
		src := filepath.Join(root, name)
		if err := os.RemoveAll(filepath.Join(wt, name)); err != nil {
			return err
		}
		info, err := os.Stat(src)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := os.CopyFS(filepath.Join(wt, name), os.DirFS(src)); err != nil {
				return err
			}
			continue
		}
		body, err := os.ReadFile(src) // #nosec G304 -- the checkout's own rule file
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(wt, name), body, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// measure checks every assertion the fixture makes against the worktree, and
// answers the ones that failed in words.
func measure(ctx context.Context, wt string, expect Expect, text string, checks []verify.Check) []string {
	var failed []string
	if _, err := git(ctx, wt, "add", "-A"); err != nil {
		return []string{"stage the result: " + err.Error()}
	}
	named, _ := git(ctx, wt, "diff", "--cached", "--name-only", "HEAD")
	touched := map[string]bool{}
	for _, path := range strings.Split(named, "\n") {
		if path != "" {
			touched[path] = true
		}
	}
	for _, path := range expect.Touches {
		if !touched[path] {
			failed = append(failed, "touches: "+path+" was not changed")
		}
	}
	for _, path := range expect.Untouched {
		if touched[path] {
			failed = append(failed, "untouched: "+path+" was changed")
		}
	}
	if expect.Verify == "pass" {
		switch out := verify.Run(ctx, wt, checks); {
		case out.Total == 0:
			failed = append(failed, "verify: no checks are configured for this repository")
		case !out.Passed():
			failed = append(failed, "verify: "+out.Failed.Name+" failed")
		}
	}
	if expect.Comments.MaxLines > 0 {
		diff, _ := git(ctx, wt, "diff", "--cached", "-U0", "HEAD", "--", "*.go", "*.ts", "*.tsx")
		for _, over := range bridge.LongComments(strings.Split(diff, "\n"), expect.Comments.MaxLines) {
			failed = append(failed, "comments: "+over)
		}
	}
	if expect.Asks != nil && *expect.Asks != Asks(text) {
		if *expect.Asks {
			failed = append(failed, "asks: the turn did not end in a question")
		} else {
			failed = append(failed, "asks: the turn ended in a question")
		}
	}
	return failed
}

// Asks is a reply whose last line is a question: the turn stopped to ask
// rather than doing the work.
func Asks(text string) bool {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	return strings.HasSuffix(last, "?")
}

// checked is which assertions an expect makes, in report order.
func (e Expect) checked() []string {
	var out []string
	if len(e.Touches) > 0 {
		out = append(out, "touches")
	}
	if len(e.Untouched) > 0 {
		out = append(out, "untouched")
	}
	if e.Verify != "" {
		out = append(out, "verify")
	}
	if e.Comments.MaxLines > 0 {
		out = append(out, "comments")
	}
	if e.Asks != nil {
		out = append(out, "asks")
	}
	return out
}

func commonGitDir(ctx context.Context, root string) string {
	out, err := git(ctx, root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return ""
	}
	return out
}

var identity = []string{ //nolint:gochecknoglobals // a constant list
	"GIT_AUTHOR_NAME=ade", "GIT_AUTHOR_EMAIL=ade@localhost",
	"GIT_COMMITTER_NAME=ade", "GIT_COMMITTER_EMAIL=ade@localhost",
}

func gitAs(ctx context.Context, dir string, args ...string) (string, error) {
	return gitEnv(ctx, dir, identity, args...)
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
