package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tree writes files under a fresh root, each with a one-line body.
func tree(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, file := range files {
		full := filepath.Join(root, filepath.FromSlash(file))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
		body := "x\n"
		if filepath.Base(file) == "go.mod" {
			body = "module example.com/app\n\ngo 1.24\n"
		}
		require.NoError(t, os.WriteFile(full, []byte(body), 0o600))
	}
	return root
}

func TestGoNarrowsToThePackagesThatChanged(t *testing.T) {
	root := tree(t, "go.mod", "main.go", "internal/words/words.go", "internal/words/more.go", "README.md")
	checks := []Check{{Name: "go test", Run: "go test ./...", Narrow: "go test {{.Packages}}"}}

	got := Narrowed(root, checks, []string{"internal/words/words.go", "internal/words/more.go", "main.go", "README.md"})

	assert.Equal(t, []Check{{Name: "go test", Run: "go test example.com/app example.com/app/internal/words"}}, got)
}

func TestFilesAreTheOnesUnderTheChecksDirectory(t *testing.T) {
	root := tree(t, "go.mod", "main.go", "app/pages/design.ts", "app/pages/my file.ts", "app/pages/it's.ts")
	checks := []Check{{Name: "vitest", Run: "npx vitest run", Dir: "app", Narrow: "npx vitest related --run {{.Files}}"}}

	got := Narrowed(root, checks, []string{"main.go", "app/pages/design.ts", "app/pages/my file.ts", "app/pages/it's.ts"})

	require.Len(t, got, 1)
	assert.Equal(t, "app", got[0].Dir)
	assert.Equal(t, `npx vitest related --run pages/design.ts 'pages/it'\''s.ts' 'pages/my file.ts'`, got[0].Run)
}

func TestAQuotedPathReachesTheCommandWhole(t *testing.T) {
	root := tree(t, "a b.txt", "$(touch owned).txt")
	checks := []Check{{Name: "ls", Run: "true", Narrow: "ls {{.Files}} > seen"}}

	narrow := Narrowed(root, checks, []string{"a b.txt", "$(touch owned).txt"})
	out := Run(context.Background(), root, narrow)

	require.True(t, out.clean(), "%+v", out.Failed)
	seen, err := os.ReadFile(filepath.Join(root, "seen"))
	require.NoError(t, err)
	assert.Equal(t, "$(touch owned).txt\na b.txt\n", string(seen))
	assert.NoFileExists(t, filepath.Join(root, "owned"))
}

func TestAnEmptyExpansionSkipsTheCheck(t *testing.T) {
	root := tree(t, "go.mod", "README.md", "testdata/x.go", "app/pages/a.ts")
	checks := []Check{
		{Name: "go test", Run: "go test ./...", Narrow: "go test {{.Packages}}"},
		{Name: "vitest", Run: "npx vitest run", Dir: "app", Narrow: "npx vitest run {{.Files}}"},
		{Name: "build", Run: "go build ./..."},
	}

	assert.Empty(t, Narrowed(root, checks, []string{"README.md", "testdata/x.go", "gone.go", "../out.go"}))
	assert.Empty(t, Narrowed(root, checks, nil))
}

func TestAGoFileOutsideAModuleNamesNoPackage(t *testing.T) {
	root := tree(t, "tools/gen.go", "svc/go.mod", "svc/api/api.go")
	checks := []Check{{Name: "go test", Run: "go test ./...", Narrow: "go test {{.Packages}}"}}

	got := Narrowed(root, checks, []string{"tools/gen.go", "svc/api/api.go"})

	require.Len(t, got, 1)
	assert.Equal(t, "go test example.com/app/api", got[0].Run)
}

func TestANarrowOutcomeIsNeverAPass(t *testing.T) {
	out := Run(context.Background(), t.TempDir(), []Check{{Name: "ok", Run: "true"}})
	require.True(t, out.Passed())

	out.Narrow = true
	assert.False(t, out.Passed(), "a narrow pass proves less than the list")
	assert.True(t, out.NarrowPassed())
}

func TestACheckRunsInItsOwnDirectory(t *testing.T) {
	root := tree(t, "app/package.json")
	out := Run(context.Background(), root, []Check{{Name: "here", Run: "test -f package.json", Dir: "app"}})
	assert.True(t, out.Passed())
}

func TestABadNarrowFormOrDirectoryIsRefusedOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checks.json")
	save := func(check Check) error { return Save(path, Config{Checks: []Check{check}}) }

	assert.ErrorContains(t, save(Check{Name: "t", Run: "go test ./...", Narrow: "go test {{.Package}}"}), "narrow form")
	assert.ErrorContains(t, save(Check{Name: "t", Run: "go test ./...", Narrow: "go test {{.Packages"}), "narrow form")
	assert.ErrorContains(t, save(Check{Name: "t", Run: "true", Dir: "../elsewhere"}), "outside the checkout")
	assert.NoError(t, save(Check{Name: "t", Run: "go test ./...", Dir: "svc", Narrow: "go test {{.Packages}}"}))
}
