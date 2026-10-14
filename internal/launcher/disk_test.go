package launcher

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/diskuse"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
)

func TestTheDiskReadingNamesTheBiggestFew(t *testing.T) {
	s := newTestLauncher(t)
	dirs := make([]diskuse.Store, 0, 7)
	for i, n := range []int{70, 10, 60, 30, 50, 20, 40} {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "chat.db"), make([]byte, n*1000), 0o600))
		dirs = append(dirs, diskuse.Store{Label: string(rune('a' + i)), Path: dir})
	}
	s.disk = &diskCache{}

	var got diskResponse
	rec := do(t, s, "GET", "/api/disk", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	// The real places, whatever this machine has; the shaped assertions are
	// below against a stand-in set.
	assert.GreaterOrEqual(t, got.Total, int64(0))

	fresh := &diskCache{}
	out := fresh.read(func() []diskuse.Store { return dirs })
	require.Len(t, out.Stores, diskShown, "only the biggest few are shown")
	assert.Equal(t, int64(70_000), out.Stores[0].Bytes, "biggest first")
	assert.Equal(t, int64(30_000), out.Stores[4].Bytes)
	// The total is every place, not just the shown ones, or it would not be
	// what ade is actually costing.
	assert.Equal(t, int64(280_000), out.Total)
}

func TestTheDiskWalkIsNotRedoneOnEveryRequest(t *testing.T) {
	// It is a walk of every checkout, and it moves on the timescale of days.
	walks := 0
	places := func() []diskuse.Store {
		walks++
		return []diskuse.Store{{Label: "one", Path: t.TempDir()}}
	}
	c := &diskCache{}
	for range 4 {
		c.read(places)
	}
	assert.Equal(t, 1, walks)

	c.at = time.Now().Add(-diskTTL - time.Second)
	c.read(places)
	assert.Equal(t, 2, walks, "a cold reading is taken again")
}

func TestOnlyAPlaceThePanelOfferedCanBeOpened(t *testing.T) {
	// The detail view walks a directory a request names, so the name has to
	// come off the list rather than off the request.
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	for _, path := range []string{"/etc", "", "/Users/owen/.ssh", "../.."} {
		rec := do(t, s, "GET", "/api/disk/detail?path="+url.QueryEscape(path), "")
		assert.Equalf(t, http.StatusBadRequest, rec.Code, "%s was walked", path)
	}
}

func TestAdesOwnDirectoryIsOnTheList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".ade"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".ade", "a.json"), make([]byte, 10), 0o600))
	s := newTestLauncher(t)

	rec := do(t, s, "GET", "/api/disk/detail?path="+url.QueryEscape(filepath.Join(home, ".ade")), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got diskuse.Store
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	// Every child, not the three biggest: the modal has room for the list.
	names := map[string]int64{}
	for _, part := range got.Parts {
		names[part.Name] = part.Bytes
	}
	assert.Equal(t, int64(10), names["a.json"])
	assert.GreaterOrEqual(t, got.Bytes, int64(10))
}

func TestPurgingWaitsForASandboxedRun(t *testing.T) {
	// The cache is mounted into the container, so emptying it mid-run would
	// pull the ground from under whatever is building.
	busy := func(running, boxed bool) []sessions.GraphRepo {
		return []sessions.GraphRepo{{Checkouts: []sessions.GraphCheckout{{
			Name:   "ade",
			Server: &sessions.GraphServer{Agent: &session.AgentHealth{Running: running, Sandbox: boxed}},
		}}}}
	}

	got, where := sandboxBusy(busy(true, true))
	assert.True(t, got)
	assert.Equal(t, "ade", where, "the complaint names the checkout to wait for")

	// A run that is not containerised does not touch the cache, and a
	// container that is not running is not reading it.
	for _, graph := range []([]sessions.GraphRepo){busy(true, false), busy(false, true), nil} {
		got, _ := sandboxBusy(graph)
		assert.False(t, got)
	}

	// A checkout with no server at all is not a reason to refuse.
	quiet, _ := sandboxBusy([]sessions.GraphRepo{{Checkouts: []sessions.GraphCheckout{{Name: "ade"}}}})
	assert.False(t, quiet)
}

func TestPurgingEmptiesOnlyTheBuildCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cache := filepath.Join(home, ".ade", "cache")
	require.NoError(t, os.MkdirAll(cache, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(cache, "go-build"), make([]byte, 2048), 0o600))
	// A conversation beside it, which is the thing that must survive.
	require.NoError(t, os.WriteFile(filepath.Join(home, ".ade", "settings.json"), []byte("{}"), 0o600))

	s := newTestLauncher(t)
	rec := do(t, s, "POST", "/api/disk/purge", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"freed":2048`)
	assert.NoDirExists(t, cache)
	assert.FileExists(t, filepath.Join(home, ".ade", "settings.json"))
}

func TestPurgingEmptiesAReadOnlyModuleCache(t *testing.T) {
	// Go writes its module cache with read-only directories, and an entry is
	// unlinked from its parent — so plain RemoveAll fails partway through with
	// "permission denied" on a file it can see but not remove.
	home := t.TempDir()
	t.Setenv("HOME", home)
	mod := filepath.Join(home, ".ade", "cache", "go-mod", "gopkg.in", "yaml.v3@v3.0.1")
	require.NoError(t, os.MkdirAll(mod, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(mod, "decode_test.go"), make([]byte, 64), 0o444))
	require.NoError(t, os.Chmod(mod, 0o555))
	require.NoError(t, os.Chmod(filepath.Dir(mod), 0o555))
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(mod), 0o750) })

	s := newTestLauncher(t)
	rec := do(t, s, "POST", "/api/disk/purge", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NoDirExists(t, filepath.Join(home, ".ade", "cache"))
}

func TestAReadOnlyDirectoryIsMadeWritableFirst(t *testing.T) {
	// The integration test above runs as root in CI and in the dev container,
	// where permissions are not enforced — so it would pass without the fix.
	// This checks the thing the fix actually does.
	root := t.TempDir()
	deep := filepath.Join(root, "go-mod", "gopkg.in", "yaml.v3@v3.0.1")
	require.NoError(t, os.MkdirAll(deep, 0o750))
	require.NoError(t, os.Chmod(deep, 0o555))

	makeWritable(root)

	info, err := os.Stat(deep)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&0o200, "the directory is still read-only, so its entries cannot be unlinked")
}

func TestAttachmentsAreOfferedAsAnUnrecoverablePurge(t *testing.T) {
	// A build cache comes back on its own; an attachment is the only copy of
	// a file someone dropped, so the two cannot be offered the same way.
	home := t.TempDir()
	t.Setenv("HOME", home)
	cache := filepath.Join(home, ".ade", "cache")
	require.NoError(t, os.MkdirAll(cache, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(cache, "go-build"), make([]byte, 512), 0o600))

	ade := filepath.Join(t.TempDir(), "ade")
	attachments := filepath.Join(ade, "attachments")
	require.NoError(t, os.MkdirAll(attachments, 0o750))
	for _, name := range []string{"0001-spec.pdf", "0002-run.log"} {
		require.NoError(t, os.WriteFile(filepath.Join(attachments, name), make([]byte, 256), 0o600))
	}

	s := newTestLauncher(t)

	byLabel := map[string]purgeable{}
	for _, tree := range s.purgeable(diskuse.Store{Label: "a repo", Path: ade}) {
		byLabel[tree.Label] = tree
	}
	got, ok := byLabel["message attachments"]
	require.True(t, ok, "attachments are not offered")
	assert.False(t, got.Recoverable, "losing an attachment is not recoverable")
	assert.Equal(t, 2, got.Files, "the purge has to say how many files go")
	assert.Equal(t, int64(512), got.Bytes)

	// And the cache, where it lives, still is — as recoverable.
	cacheTrees := s.purgeable(diskuse.Store{Label: "ade itself", Path: filepath.Join(home, ".ade")})
	require.Len(t, cacheTrees, 1)
	assert.True(t, cacheTrees[0].Recoverable)
}

func TestAnEmptyAttachmentsDirectoryIsNotOffered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ade := filepath.Join(t.TempDir(), "ade")
	require.NoError(t, os.MkdirAll(filepath.Join(ade, "attachments"), 0o750))

	s := newTestLauncher(t)
	assert.Empty(t, s.purgeable(diskuse.Store{Label: "a repo", Path: ade}),
		"nothing to empty is nothing to offer")
}

func TestOnlyATreeTheDetailOfferedCanBeEmptied(t *testing.T) {
	// A purge takes a path, so it must not take any path.
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	victim := filepath.Join(t.TempDir(), "keep")
	require.NoError(t, os.MkdirAll(victim, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(victim, "a.txt"), []byte("mine"), 0o600))

	for _, path := range []string{victim, "/etc", "/", "../.."} {
		rec := do(t, s, "POST", "/api/disk/purge?path="+url.QueryEscape(path), "")
		assert.Equalf(t, http.StatusBadRequest, rec.Code, "%s was emptied", path)
	}
	assert.FileExists(t, filepath.Join(victim, "a.txt"))
}
