package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// a checkout with one file in it, which is all gopls needs to index.
func checkout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	write("go.mod", "module example.com/hover\n\ngo 1.24\n")
	write("main.go", `package main

// Greet says hello to somebody.
func Greet(name string) string {
	return "hello " + name
}

func main() {
	_ = Greet("owen")
}
`)
	return dir
}

func TestHoverSaysWhatASymbolIs(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	dir := checkout(t)
	pool := New(dir)
	t.Cleanup(pool.Close)

	// Indexing a module from cold is slow the first time.
	ctx, stop := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(stop)

	// Line 9, the call to Greet inside main: `	_ = Greet("owen")`.
	said, err := pool.Hover(ctx, "main.go", 9, 7)
	require.NoError(t, err)
	assert.Contains(t, said, "Greet", "the hover names the symbol under the cursor")
	assert.Contains(t, said, "says hello", "and carries the doc comment with it")
}

func TestDefinitionFindsTheSymbolInTheCheckout(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	dir := checkout(t)
	pool := New(dir)
	t.Cleanup(pool.Close)
	ctx, stop := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(stop)

	// From the call on line 9 to the declaration on line 4.
	at, err := pool.Definition(ctx, "main.go", 9, 7)
	require.NoError(t, err)
	require.NotNil(t, at)
	assert.Equal(t, "main.go", at.Path, "a path in the checkout, not a file:// url")
	assert.Equal(t, 4, at.Line, "counted from one, as the window counts")
}

func TestReferencesFindTheCallAndTheDeclaration(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	dir := checkout(t)
	pool := New(dir)
	t.Cleanup(pool.Close)
	ctx, stop := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(stop)

	found, err := pool.References(ctx, "main.go", 4, 6)
	require.NoError(t, err)
	lines := []int{}
	for _, at := range found {
		lines = append(lines, at.Line)
	}
	assert.Contains(t, lines, 4, "the declaration")
	assert.Contains(t, lines, 9, "and the call")
}

func TestAFileWithNoServerSaysSoRatherThanHanging(t *testing.T) {
	pool := New(t.TempDir())
	t.Cleanup(pool.Close)

	_, err := pool.Hover(context.Background(), "README.md", 1, 1)
	assert.ErrorIs(t, err, ErrNoServer)
}

func TestTheLanguageIsReadOffTheName(t *testing.T) {
	assert.Equal(t, "go", Language("internal/web/web.go"))
	assert.Equal(t, "", Language("README.md"))
	assert.Equal(t, "", Language("Makefile"))
}

func TestOneServerIsDialledHoweverManyAsk(t *testing.T) {
	pool := New(t.TempDir())
	t.Cleanup(pool.Close)
	// A server that is not installed fails once and is remembered as
	// failed: a missing binary must not be looked for on every hover.
	pool.find = func(string) []string { return []string{"ade-no-such-language-server"} }

	for range 3 {
		_, err := pool.Hover(context.Background(), "main.go", 1, 1)
		assert.ErrorIs(t, err, ErrNoServer)
	}
}
