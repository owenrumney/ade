// Package fixtures replays a captured prompt on the tree it was asked against
// and checks the result, so a CLAUDE.md rule can be measured. Design: 08-prompt-fixtures.md.
package fixtures

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/owenrumney/ade/internal/rewind"
	"github.com/owenrumney/ade/internal/statedir"
)

// StaleAfter is how old a fixture's base may be before the report flags it.
const StaleAfter = 90 * 24 * time.Hour

// Expect is what should be true of the tree once the prompt has run. Every
// field is optional; a fixture that asserts nothing is reported as failed.
type Expect struct {
	Touches   []string `yaml:"touches"`
	Untouched []string `yaml:"untouched"`
	Verify    string   `yaml:"verify"`
	Comments  struct {
		MaxLines int `yaml:"maxLines"`
	} `yaml:"comments"`
	Asks *bool `yaml:"asks"`
}

// Fixture is one captured turn: the prompt, where the tree started, and what
// is expected of the result.
type Fixture struct {
	Name     string
	Prompt   string
	Base     string
	Patch    []byte
	Captured time.Time
	Expect   Expect
}

var (
	// ErrNoSnapshot is a turn whose pre-turn tree was never saved, or has
	// since been pruned.
	ErrNoSnapshot = errors.New("that turn has no snapshot of the tree before it ran")
	ErrNoFixtures = errors.New("no fixtures yet; save one from a turn in the chat")
)

const expectTemplate = `# What should be true of the tree after the prompt has run.
# Every key is optional; a fixture that asserts nothing fails.
#
# touches:   [internal/verify/verify.go]   every named path was changed
# untouched: [internal/web/web.go]         none of these were changed
# verify:    pass                          the repository's checks pass
# comments:  { maxLines: 2 }               no added comment longer than this
# asks:      false                         the turn did not end in a question
verify: pass
`

// Dir is where a repository's fixtures live: under the common git dir, so
// every worktree of it shares one suite.
func Dir(commonGitDir string) string {
	return filepath.Join(statedir.Git(commonGitDir), "fixtures")
}

// Capture writes a fixture from a turn's rewind snapshot: the base is the
// commit the snapshot was taken on and the patch is what the tree had on top.
func Capture(ctx context.Context, root, dir, name, prompt, snapshot string) error {
	if strings.TrimSpace(prompt) == "" {
		return errors.New("the turn has no prompt to save")
	}
	if !validName(name) {
		return fmt.Errorf("%q is not a fixture name: letters, digits and dashes", name)
	}
	ref := rewind.Ref(snapshot)
	if snapshot == "" {
		return ErrNoSnapshot
	}
	base, err := git(ctx, root, "rev-parse", "--verify", "-q", ref+"^")
	if err != nil {
		return ErrNoSnapshot
	}
	patch, err := git(ctx, root, "diff", "--binary", base, ref)
	if err != nil {
		return err
	}
	if patch != "" {
		patch += "\n"
	}

	at := filepath.Join(dir, name)
	if _, err := os.Stat(at); err == nil {
		return fmt.Errorf("a fixture called %q already exists", name)
	}
	if err := os.MkdirAll(at, 0o750); err != nil {
		return err
	}
	files := map[string]string{
		"prompt.md":   prompt,
		"base":        base + "\n",
		"tree.patch":  patch,
		"captured":    time.Now().UTC().Format(time.RFC3339) + "\n",
		"expect.yaml": expectTemplate,
	}
	for file, body := range files {
		if err := os.WriteFile(filepath.Join(at, file), []byte(body), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// Name derives a fixture name from a prompt: the first few words of its
// first line, and a numbered suffix if dir already has one of that name.
func Name(dir, prompt string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(prompt), "\n")
	words := strings.FieldsFunc(strings.ToLower(line), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	if len(words) > 4 {
		words = words[:4]
	}
	name := strings.Join(words, "-")
	if name == "" {
		name = "turn"
	}
	chosen := name
	for n := 2; exists(filepath.Join(dir, chosen)); n++ {
		chosen = fmt.Sprintf("%s-%d", name, n)
	}
	return chosen
}

// List loads every fixture under dir, by name.
func List(dir string) ([]Fixture, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoFixtures
	}
	if err != nil {
		return nil, err
	}
	var out []Fixture
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || entry.Name() == "runs" {
			continue
		}
		fixture, err := Load(dir, entry.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, fixture)
	}
	if len(out) == 0 {
		return nil, ErrNoFixtures
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Load reads one fixture. A malformed expect.yaml is an error naming it.
func Load(dir, name string) (Fixture, error) {
	at := filepath.Join(dir, name)
	read := func(file string) (string, error) {
		body, err := os.ReadFile(filepath.Join(at, file)) // #nosec G304 -- ade state under .git
		if err != nil {
			return "", fmt.Errorf("fixture %s: %w", name, err)
		}
		return string(body), nil
	}
	prompt, err := read("prompt.md")
	if err != nil {
		return Fixture{}, err
	}
	base, err := read("base")
	if err != nil {
		return Fixture{}, err
	}
	patch, _ := read("tree.patch")
	captured, _ := read("captured")
	expect, err := read("expect.yaml")
	if err != nil {
		return Fixture{}, err
	}
	f := Fixture{Name: name, Prompt: prompt, Base: strings.TrimSpace(base), Patch: []byte(patch)}
	f.Captured, _ = time.Parse(time.RFC3339, strings.TrimSpace(captured))
	if f.Expect, err = ParseExpect([]byte(expect)); err != nil {
		return Fixture{}, fmt.Errorf("fixture %s: expect.yaml: %w", name, err)
	}
	return f, nil
}

// ParseExpect reads an expect.yaml strictly: a misspelt key asserts nothing,
// which would otherwise pass quietly.
func ParseExpect(body []byte) (Expect, error) {
	var e Expect
	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)
	if err := dec.Decode(&e); err != nil && !errors.Is(err, io.EOF) {
		return Expect{}, err
	}
	switch e.Verify {
	case "", "pass":
	default:
		return Expect{}, fmt.Errorf("verify must be \"pass\" or absent, not %q", e.Verify)
	}
	if e.Comments.MaxLines < 0 {
		return Expect{}, errors.New("comments.maxLines must not be negative")
	}
	return e, nil
}

// Stale is a fixture whose base is old enough that passing it says little.
func (f Fixture) Stale(now time.Time) bool {
	return !f.Captured.IsZero() && now.Sub(f.Captured) > StaleAfter
}

// Empty is an expect that asserts nothing.
func (e Expect) Empty() bool {
	return len(e.Touches) == 0 && len(e.Untouched) == 0 && e.Verify == "" &&
		e.Comments.MaxLines == 0 && e.Asks == nil
}

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func validName(name string) bool { return nameRE.MatchString(name) }

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	return gitEnv(ctx, dir, nil, args...)
}

func gitEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- args are built here
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(string(out), "\n"), nil
}
