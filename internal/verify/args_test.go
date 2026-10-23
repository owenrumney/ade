package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func checkout(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, file := range files {
		full := filepath.Join(root, file)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte("x"), 0o600))
	}
	return root
}

func TestNarrowedArgumentsGoAfterTheCommandAndNeverIntoIt(t *testing.T) {
	root := checkout(t, "internal/foo/a.go")
	check := Check{Name: "test", Run: "go test", Filter: "-run=%s"}

	argv, err := Argv(root, check, Args{Paths: []string{"internal/foo"}, Filter: "TestBar"})

	require.NoError(t, err)
	assert.Equal(t, []string{"sh", "-c", `go test "$@"`, "test", "./internal/foo", "-run=TestBar"}, argv)
}

func TestACheckWithNothingToNarrowRunsAsItStands(t *testing.T) {
	argv, err := Argv(t.TempDir(), Check{Name: "vet", Run: "go vet ./..."}, Args{})
	require.NoError(t, err)
	assert.Equal(t, []string{"sh", "-c", "go vet ./..."}, argv)
}

// The one that matters: nothing an agent sends may become a flag, a second
// command, or a path outside the checkout.
func TestWhatAnAgentMayNotSmuggleIn(t *testing.T) {
	root := checkout(t, "a.go")
	check := Check{Name: "test", Run: "go test", Filter: "-run=%s"}

	tests := []struct {
		name string
		args Args
	}{
		{"a flag dressed as a package", Args{Paths: []string{"-exec=sh -c 'id'"}}},
		{"a path climbing out", Args{Paths: []string{"../../etc/passwd"}}},
		{"an absolute path", Args{Paths: []string{"/etc/passwd"}}},
		{"a path that is not there", Args{Paths: []string{"nope"}}},
		{"an empty path", Args{Paths: []string{"  "}}},
		{"a second command in the pattern", Args{Filter: "x; id"}},
		{"a substitution in the pattern", Args{Filter: "$(id)"}},
		{"a pattern with a space", Args{Filter: "Test Bar"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Argv(root, check, test.args)
			require.Error(t, err)
		})
	}
}

func TestAPatternNeedsACheckThatTakesOne(t *testing.T) {
	_, err := Argv(t.TempDir(), Check{Name: "build", Run: "go build"}, Args{Filter: "TestX"})
	require.ErrorContains(t, err, "takes no test-name pattern")
}

func TestAFilterFormIsOneSubstitution(t *testing.T) {
	assert.NoError(t, Config{Checks: []Check{{Name: "t", Run: "go test", Filter: "-run=%s"}}}.valid())
	assert.Error(t, Config{Checks: []Check{{Name: "t", Run: "go test", Filter: "-run=%s %s"}}}.valid())
	assert.Error(t, Config{Checks: []Check{{Name: "t", Run: "go test", Filter: "-run=%d"}}}.valid())
}

// What the argv actually does when run, rather than what it looks like.
func TestTheSmuggledArgumentArrivesAsOneWord(t *testing.T) {
	root := checkout(t, "a.go")
	check := Check{Name: "echo", Run: "printf '%s\\n'", Filter: "-run=%s"}

	got, err := One(context.Background(), root, check, Args{Paths: []string{"a.go"}})

	require.NoError(t, err)
	assert.True(t, got.Passed, got.Output)
}

func TestAPathIsGivenRelativeToTheChecksOwnDir(t *testing.T) {
	root := checkout(t, "web/src/app.ts")
	check := Check{Name: "lint", Run: "eslint", Dir: "web"}

	argv, err := Argv(root, check, Args{Paths: []string{"src/app.ts"}})

	require.NoError(t, err)
	assert.Equal(t, "./src/app.ts", argv[len(argv)-1])
}
