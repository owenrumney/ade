package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestARunStopsAtTheFirstFailure(t *testing.T) {
	out := Run(context.Background(), t.TempDir(), []Check{
		{Name: "build", Run: "true"},
		{Name: "vet", Run: "echo 'a.go:4: undefined: x' >&2; false"},
		{Name: "format", Run: "touch should-not-run"},
	})

	require.Len(t, out.Ran, 2, "the third never runs")
	assert.Equal(t, 3, out.Total)
	assert.False(t, out.Passed())
	require.NotNil(t, out.Failed)
	assert.Equal(t, "vet", out.Failed.Name)
	assert.Contains(t, out.Failed.Output, "undefined: x")
	assert.Empty(t, out.Ran[0].Output, "a passing check keeps no output")
}

func TestEverythingPassingIsAPass(t *testing.T) {
	out := Run(context.Background(), t.TempDir(), []Check{
		{Name: "build", Run: "true"},
		{Name: "vet", Run: "true"},
	})
	assert.True(t, out.Passed())
	assert.Nil(t, out.Failed)
	assert.Len(t, out.Ran, 2)
}

func TestAnEmptyListIsNotAPass(t *testing.T) {
	out := Run(context.Background(), t.TempDir(), nil)
	assert.False(t, out.Passed(), "nothing ran, so nothing is verified")
	assert.Empty(t, out.Ran)
}

func TestAChecksCommandRunsInTheCheckout(t *testing.T) {
	root := t.TempDir()
	out := Run(context.Background(), root, []Check{{Name: "write", Run: "echo hi > made.txt"}})
	require.True(t, out.Passed())
	assert.FileExists(t, filepath.Join(root, "made.txt"))
}

func TestCancellingIsStoppedAndNotFailed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	out := Run(ctx, t.TempDir(), []Check{{Name: "slow", Run: "sleep 30"}})

	assert.True(t, out.Stopped)
	assert.Nil(t, out.Failed, "you stopping it is not the build failing")
	assert.False(t, out.Passed())
}

// A cancelled check takes its own children with it: `go build` is a tree.
func TestCancellingTakesTheWholeTree(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "alive")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Outcome, 1)
	go func() {
		done <- Run(ctx, t.TempDir(), []Check{{Name: "storm",
			Run: "sh -c 'while :; do touch " + marker + "; sleep 0.1; done' & sleep 30"}})
	}()
	require.Eventually(t, func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}, 3*time.Second, 20*time.Millisecond, "the grandchild never started")

	cancel()
	<-done
	require.NoError(t, os.Remove(marker))
	time.Sleep(400 * time.Millisecond)
	assert.NoFileExists(t, marker, "the grandchild outlived the check")
}

func TestTheTailKeepsTheEndAndSaysWhatItCut(t *testing.T) {
	assert.Empty(t, Tail("", 3))
	assert.Equal(t, "one\ntwo", Tail("one\ntwo\n", 3))

	var lines []string
	for i := range 10 {
		lines = append(lines, string(rune('a'+i)))
	}
	got := Tail(strings.Join(lines, "\n"), 3)
	assert.Equal(t, "… 7 earlier line(s)\nh\ni\nj", got)
}

func TestTheListIsReadAndWrittenUnderTheCommonGitDir(t *testing.T) {
	gitDir := filepath.Join(t.TempDir(), ".git")
	path := Path(gitDir)
	assert.Equal(t, filepath.Join(gitDir, "ade", "checks.json"), path)
	assert.Empty(t, Path(""))

	// A repository with no list has an empty one, not an error.
	config, err := Load(path)
	require.NoError(t, err)
	assert.Empty(t, config.Checks)
	assert.False(t, config.Asked)

	want := Config{Checks: []Check{{Name: "build", Run: "go build ./..."}}, Asked: true}
	require.NoError(t, Save(path, want))
	got, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestAListWithAHalfWrittenCheckIsRefused(t *testing.T) {
	path := Path(filepath.Join(t.TempDir(), ".git"))
	assert.ErrorContains(t, Save(path, Config{Checks: []Check{{Run: "go build ./..."}}}), "check 1 has no name")
	assert.ErrorContains(t, Save(path, Config{Checks: []Check{{Name: "build"}}}), `check "build" has nothing to run`)
	assert.NoFileExists(t, path)

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(`{"checks":[{"name":"build"}]}`), 0o600))
	_, err := Load(path)
	assert.ErrorContains(t, err, "nothing to run")

	require.NoError(t, os.WriteFile(path, []byte(`not json`), 0o600))
	_, err = Load(path)
	assert.ErrorContains(t, err, "parse the check list")
}

func TestDetectNeverOffersATestCommand(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o600))
	}
	// Every way this repository says "test": a Makefile target, a go.mod and
	// a package.json script. None of them may become a check.
	write("Makefile", "test:\n\tgo test ./...\n")
	write("go.mod", "module x\n")
	write("package.json", `{"scripts":{"build":"tsc","test":"vitest run"}}`)

	got := Detect(root)
	assert.Equal(t, []Check{
		{Name: "build", Run: "go build ./..."},
		{Name: "vet", Run: "go vet ./..."},
		{Name: "build", Run: "npm run build"},
	}, got)
	for _, check := range got {
		assert.NotContains(t, check.Run, "test")
	}
}

func TestDetectReadsWhatIsInTheRoot(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  []Check
	}{
		{"nothing it knows", map[string]string{"README.md": "hi"}, nil},
		{"rust", map[string]string{"Cargo.toml": "[package]"}, []Check{{Name: "check", Run: "cargo check"}}},
		{
			"pnpm",
			map[string]string{"package.json": `{"scripts":{"build":"vite build"}}`, "pnpm-lock.yaml": ""},
			[]Check{{Name: "build", Run: "pnpm run build"}},
		},
		{
			"yarn",
			map[string]string{"package.json": `{"scripts":{"build":"vite build"}}`, "yarn.lock": ""},
			[]Check{{Name: "build", Run: "yarn build"}},
		},
		{"a node project with no build script", map[string]string{"package.json": `{"scripts":{"test":"vitest"}}`}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			for name, body := range tt.files {
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o600))
			}
			assert.Equal(t, tt.want, Detect(root))
		})
	}
	assert.Nil(t, Detect(""))
}
