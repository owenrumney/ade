package todo

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The bug this pins: listing before anything was ever added pushed a branch
// with no commit on it, and git rejected the refspec.
func TestListingAnEmptySharedListDoesNotPush(t *testing.T) {
	s := NewShared(filepath.Join(t.TempDir(), "todos"), bareRemote(t))
	items, err := s.List(context.Background(), "")
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestASharedTodoReachesTheNextCheckout(t *testing.T) {
	remote := bareRemote(t)
	ctx := context.Background()
	first := NewShared(filepath.Join(t.TempDir(), "todos"), remote)
	_, err := first.Add(ctx, "", Item{Text: "fix the thing"})
	require.NoError(t, err)

	second := NewShared(filepath.Join(t.TempDir(), "todos"), remote)
	items, err := second.List(ctx, "")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "fix the thing", items[0].Text)
}

func bareRemote(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "remote.git")
	out, err := exec.Command("git", "init", "--bare", dir).CombinedOutput()
	require.NoError(t, err, string(out))
	return dir
}
