package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/preview"
)

func TestIgnoredTakesPathsAndBareNames(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	cfg := Settings{IgnoredDirs: []string{"node_modules", "~/code/junk", "/opt/vendor"}}

	tests := []struct {
		dir  string
		want bool
	}{
		// A bare name drops every directory called that, wherever it is.
		{filepath.Join(home, "code", "node_modules"), true},
		{"/anywhere/else/node_modules", true},
		// A path drops that directory and everything under it.
		{filepath.Join(home, "code", "junk"), true},
		{filepath.Join(home, "code", "junk", "repo"), true},
		{"/opt/vendor/thing", true},
		// Near misses must not be caught: a prefix is not a parent.
		{filepath.Join(home, "code", "junkyard"), false},
		{"/opt/vendored", false},
		{filepath.Join(home, "code", "ade"), false},
		{"/anywhere/node_modules_old", false},
	}
	for _, tc := range tests {
		assert.Equalf(t, tc.want, cfg.Ignored(tc.dir), tc.dir)
	}
}

func TestIgnoredSkipsEmptyEntries(t *testing.T) {
	cfg := Settings{IgnoredDirs: []string{"", "   "}}
	assert.False(t, cfg.Ignored("/Users/someone/code/repo"))
}

func TestExpand(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(home, "code"), Expand("~/code"))
	assert.Equal(t, home, Expand("~"))
	assert.Equal(t, "/opt/thing", Expand("/opt/thing"))
	assert.Empty(t, Expand("  "))
	// A tilde that is not the home marker is an ordinary directory name.
	assert.NotEqual(t, filepath.Join(home, "user"), Expand("~user"))
}

func TestRootsExpand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	// The scratch directory comes last and is not configured: a scratch is
	// found again like any other checkout.
	scratch, err := ScratchDir()
	require.NoError(t, err)

	cfg := Settings{ScanDirs: []string{"~/code", "", "/srv/repos"}}
	assert.Equal(t, []string{filepath.Join(home, "code"), "/srv/repos", scratch}, cfg.Roots())
}

func TestWorkerScanRootOverridesTheHomeDefault(t *testing.T) {
	t.Setenv("ADE_SCAN_DIR", "/srv/ade")
	assert.Equal(t, []string{"/srv/ade"}, Defaults().ScanDirs)
}

func TestDefaultsScanYourCode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	// The file is written on first run, so these are what a machine that has
	// never been configured behaves as.
	scratch, err := ScratchDir()
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(home, "code"), scratch}, Defaults().Roots())
	assert.Empty(t, Defaults().IgnoredDirs)
}

func TestParseKeepsIgnoredAndFillsMissingRoots(t *testing.T) {
	// Leaving scan_dirs out is how you say "the usual place, but skip these".
	loaded, err := parse([]byte(`{"ignored_dirs":["scratch"]}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"scratch"}, loaded.IgnoredDirs)
	assert.Equal(t, Defaults().ScanDirs, loaded.ScanDirs)

	loaded, err = parse([]byte(`{"scan_dirs":["/srv/repos"],"ignored_dirs":[]}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"/srv/repos"}, loaded.ScanDirs)

	// A broken file falls back rather than leaving the launcher with nothing.
	loaded, err = parse([]byte(`{`))
	require.Error(t, err)
	assert.Equal(t, Defaults().ScanDirs, loaded.ScanDirs)
}

func TestAScratchIsRecognisedThroughASymlinkedHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root, err := ScratchDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "thing"), 0o700))

	// git reports a checkout by its resolved path, and on macOS a temp home
	// reaches it through /var. Both names are the same scratch.
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, "thing"))
	require.NoError(t, err)
	assert.True(t, IsScratch(filepath.Join(root, "thing")))
	assert.True(t, IsScratch(resolved))

	assert.False(t, IsScratch(root), "the directory holding them is not one")
	assert.False(t, IsScratch(filepath.Join(home, "code", "ade")))
	assert.False(t, IsScratch(""))
}

func TestOnlyATemporaryHomeReachesTheStateDirectory(t *testing.T) {
	// A prefix is not a parent: /tmpfoo is nobody's temporary directory, and
	// the guard is all that stands between a suite and the user's sessions.
	assert.True(t, underTemp(t.TempDir()))
	assert.False(t, underTemp(os.TempDir()+"foo"))
	assert.False(t, underTemp(filepath.Join("/home", "someone")))
}

func TestTheURLToCaptureFallsBackFromRepositoryToGlobalToDefault(t *testing.T) {
	tests := []struct {
		name   string
		global Preview
		repo   string
		want   string
	}{
		{"nothing set anywhere", Preview{}, "", preview.DefaultURL},
		{"a global default", Preview{URL: "http://localhost:3000"}, "", "http://localhost:3000"},
		{"the repository overrides it", Preview{URL: "http://localhost:3000"}, "http://localhost:8080", "http://localhost:8080"},
		{"off beats both", Preview{URL: "http://localhost:3000", Off: true}, "http://localhost:8080", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.global.PreviewURL(tc.repo))
		})
	}
}

// A worker is told its scan root by the environment. It writes a settings file
// on first run, so a default-only override means the second run silently scans
// wherever the first one happened to save.
func TestTheScanRootTheEnvironmentNamesBeatsASavedOne(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, Save(Settings{ScanDirs: []string{"/srv/ade"}, IgnoredDirs: []string{}}))

	t.Setenv("ADE_SCAN_DIR", "/Users/owen/code")
	got, err := Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"/Users/owen/code"}, got.ScanDirs)
}

func TestASavedScanRootStandsWithNoOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ADE_SCAN_DIR", "")
	require.NoError(t, Save(Settings{ScanDirs: []string{"/one", "/two"}, IgnoredDirs: []string{}}))

	got, err := Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"/one", "/two"}, got.ScanDirs)
}

func TestAWorkerScansBothTheWorkRootAndItsOwnCheckouts(t *testing.T) {
	t.Setenv("ADE_WORK_DIR", "/srv/ade")
	t.Setenv("ADE_SCAN_DIR", "/home/owen/code")
	// The clones are in one and the user's repositories in the other, so a
	// worker that scanned only one would lose half of what it serves.
	assert.Equal(t, []string{"/srv/ade", "/home/owen/code"}, Defaults().ScanDirs)

	t.Setenv("ADE_SCAN_DIR", "/srv/ade")
	assert.Equal(t, []string{"/srv/ade"}, Defaults().ScanDirs, "one root, named twice, is one root")
}
