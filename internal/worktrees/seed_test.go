package worktrees

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
)

// seeded writes the repository's seed file and returns the main checkout.
func seeded(t *testing.T, root string, seed Seed) {
	t.Helper()
	common, err := gitdiff.CommonGitDir(context.Background(), root)
	require.NoError(t, err)
	file := SeedPath(common)
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o750))
	data, err := json.Marshal(seed)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, data, 0o600))
}

func TestACarriedFileLandsInANewWorktree(t *testing.T) {
	root := repo(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=1\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "config"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "config", "local.yaml"), []byte("a: 1\n"), 0o600))
	seeded(t, root, Seed{Carry: []string{".env", "config/local.yaml", "never-written"}})

	path := add(t, root, "fix")
	placed, err := Sow(context.Background(), root, path)
	require.NoError(t, err)
	assert.Equal(t, []string{".env", "config/local.yaml"}, placed, "a missing source is skipped silently")

	body, err := os.ReadFile(filepath.Join(path, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "TOKEN=1\n", string(body))
	assert.FileExists(t, filepath.Join(path, "config", "local.yaml"))
}

// A carried directory can hold a secret, and a symlink inside it used to stop
// the copy part way and leave the truncated tree behind.
func TestACarriedDirectoryKeepsItsModesAndItsSymlinks(t *testing.T) {
	root := repo(t)
	dir := filepath.Join(root, "config", "local")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "local.yaml"), []byte("password: 1\n"), 0o600))
	require.NoError(t, os.Symlink("local.yaml", filepath.Join(dir, "current.yaml")))
	seeded(t, root, Seed{Carry: []string{"config/local"}})

	path := add(t, root, "fix")
	placed, err := Sow(context.Background(), root, path)
	require.NoError(t, err)
	assert.Equal(t, []string{"config/local"}, placed)

	info, err := os.Stat(filepath.Join(path, "config", "local", "local.yaml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "a 0600 secret stays 0600")

	link, err := os.Lstat(filepath.Join(path, "config", "local", "current.yaml"))
	require.NoError(t, err)
	assert.NotZero(t, link.Mode()&os.ModeSymlink, "the symlink is reproduced, not followed")
}

func TestALinkedPathIsASymlink(t *testing.T) {
	root := repo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "node_modules", "left-pad"), 0o750))
	seeded(t, root, Seed{Link: []string{"node_modules"}})

	path := add(t, root, "fix")
	_, err := Sow(context.Background(), root, path)
	require.NoError(t, err)

	info, err := os.Lstat(filepath.Join(path, "node_modules"))
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "a gigabyte is linked, not copied")
	assert.DirExists(t, filepath.Join(path, "node_modules", "left-pad"))
}

func TestASeedPathOutsideTheCheckoutIsRefused(t *testing.T) {
	root := repo(t)
	seeded(t, root, Seed{Carry: []string{"../secrets", "/etc/passwd"}})

	path := add(t, root, "fix")
	placed, err := Sow(context.Background(), root, path)
	require.Error(t, err)
	assert.Empty(t, placed)
	assert.NoFileExists(t, filepath.Join(filepath.Dir(path), "secrets"))
}

// The seed entry is checked, so the escape goes one level down: a symlink out
// of the checkout inside a carried directory would be planted in every worktree.
func TestASymlinkOutOfTheCheckoutIsNotCarriedWithItsDirectory(t *testing.T) {
	root := repo(t)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "id_ed25519"), []byte("secret\n"), 0o600))

	dir := filepath.Join(root, "config", "local")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "local.yaml"), []byte("a: 1\n"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(outside, "id_ed25519"), filepath.Join(dir, "key")))
	seeded(t, root, Seed{Carry: []string{"config/local"}})

	path := add(t, root, "fix")
	placed, err := Sow(context.Background(), root, path)
	require.NoError(t, err)
	assert.Equal(t, []string{"config/local"}, placed)

	assert.FileExists(t, filepath.Join(path, "config", "local", "local.yaml"), "the rest of the tree still comes")
	_, err = os.Lstat(filepath.Join(path, "config", "local", "key"))
	assert.True(t, os.IsNotExist(err), "the link out of the checkout was left behind")
}

func TestAnExistingDestinationSurvives(t *testing.T) {
	root := repo(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=main\n"), 0o600))
	seeded(t, root, Seed{Carry: []string{".env"}})

	path := add(t, root, "fix")
	require.NoError(t, os.WriteFile(filepath.Join(path, ".env"), []byte("TOKEN=mine\n"), 0o600))
	placed, err := Sow(context.Background(), root, path)
	require.NoError(t, err)
	assert.Empty(t, placed)

	body, err := os.ReadFile(filepath.Join(path, ".env"))
	require.NoError(t, err)
	assert.Equal(t, "TOKEN=mine\n", string(body))
}

func TestARepositoryWithNoSeedCarriesNothing(t *testing.T) {
	root := repo(t)
	path := add(t, root, "fix")
	placed, err := Sow(context.Background(), root, path)
	require.NoError(t, err)
	assert.Empty(t, placed)
}

// Lexically the path is inside the checkout; a symlink on the way out is not
// something a cleaned path can see.
func TestASeedPathThatResolvesOutsideTheCheckoutIsRefused(t *testing.T) {
	root := repo(t)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "key"), []byte("secret\n"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "away")))
	seeded(t, root, Seed{Carry: []string{"away/key"}})

	path := add(t, root, "fix")
	placed, err := Sow(context.Background(), root, path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the checkout")
	assert.Empty(t, placed)
	assert.NoFileExists(t, filepath.Join(path, "away", "key"))
}
