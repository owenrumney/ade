package machine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
)

func TestAMissingSnapshotIsNotAnError(t *testing.T) {
	_, ok := Read(t.TempDir())
	assert.False(t, ok)
}

func TestTheSnapshotSurvivesARoundTrip(t *testing.T) {
	dir := t.TempDir()
	at := time.Now().Truncate(time.Millisecond)
	want := Machine{
		WrittenAt: at,
		Writer:    Writer{PID: 41, Kind: "server", Root: "/repo"},
		Host:      "box",
		Repos:     []sessions.GraphRepo{{Path: "/repo", Name: "repo"}},
	}
	require.NoError(t, Write(dir, want))

	got, ok := Read(dir)
	require.True(t, ok)
	assert.True(t, want.WrittenAt.Equal(got.WrittenAt))
	assert.Equal(t, want.Writer, got.Writer)
	assert.Equal(t, want.Repos, got.Repos)
}

func TestTheWriteLeavesNoTemporaryFileBehind(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Write(dir, Machine{WrittenAt: time.Now()}))

	found, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, fileName, found[0].Name())
}

func TestTheSnapshotIsNotWorldReadable(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Write(dir, Machine{WrittenAt: time.Now()}))

	info, err := os.Stat(filepath.Join(dir, fileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestASecondWriterCannotMoveTheSnapshotBackwards(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	first := Writer{PID: 1, Kind: "launcher"}
	second := Writer{PID: 2, Kind: "server", Root: "/repo"}

	require.NoError(t, Write(dir, Machine{WrittenAt: now, Writer: first}))

	// The lease: a fresh file belongs to whoever wrote it, so the second
	// writer never probes at all.
	assert.False(t, Claim(dir, second, now.Add(time.Second)))

	// And if it writes anyway, with the older view it had in hand, the file
	// does not go backwards.
	require.Error(t, Write(dir, Machine{WrittenAt: now.Add(-time.Minute), Writer: second}))
	held, ok := Read(dir)
	require.True(t, ok)
	assert.Equal(t, first, held.Writer)
	assert.True(t, now.Equal(held.WrittenAt))
}

func TestWhoTakesTheTick(t *testing.T) {
	now := time.Now()
	mine := Writer{PID: 1, Kind: "launcher"}
	theirs := Writer{PID: 2, Kind: "server"}

	for _, tc := range []struct {
		name    string
		written time.Time
		by      Writer
		want    bool
	}{
		{"a fresh file belonging to someone else", now, theirs, false},
		{"a fresh file of our own", now, mine, true},
		{"a file just past staleAfter", now.Add(-staleAfter), theirs, true},
		{"a long dead file", now.Add(-time.Hour), theirs, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, Write(dir, Machine{WrittenAt: tc.written, Writer: tc.by}))
			assert.Equal(t, tc.want, Claim(dir, mine, now))
		})
	}
}

func TestAnAbsentFileIsAlwaysClaimable(t *testing.T) {
	assert.True(t, Claim(t.TempDir(), Writer{PID: 1}, time.Now()))
}

func TestATickTheLeaseRefusesProbesNothing(t *testing.T) {
	dir := t.TempDir()
	held := Machine{WrittenAt: time.Now(), Writer: Writer{PID: 999, Kind: "launcher"}}
	require.NoError(t, Write(dir, held))

	asked := false
	src := Source{
		Dir:    dir,
		Writer: Writer{PID: 1, Kind: "server"},
		Repos:  func() ([]sessions.Repo, error) { asked = true; return nil, nil },
	}
	assert.False(t, src.Tick(t.Context()))
	assert.False(t, asked, "the lease is read before anything is probed")

	after, ok := Read(dir)
	require.True(t, ok)
	assert.Equal(t, held.Writer, after.Writer)
}

func TestATickWithTheLeaseWritesWhatItProbed(t *testing.T) {
	dir := t.TempDir()
	src := Source{
		Dir:    dir,
		Writer: Writer{PID: 1, Kind: "launcher"},
		Repos: func() ([]sessions.Repo, error) {
			return []sessions.Repo{{Path: "/repo", Name: "repo"}}, nil
		},
	}
	assert.False(t, src.Tick(t.Context()), "nothing is running")

	snap, ok := Read(dir)
	require.True(t, ok)
	assert.Equal(t, src.Writer, snap.Writer)
	require.Len(t, snap.Repos, 1)
	assert.Equal(t, "repo", snap.Repos[0].Name)
	assert.False(t, snap.WrittenAt.IsZero())
}

func TestAFailedScanKeepsTheCadenceOfTheLastKnownState(t *testing.T) {
	dir := t.TempDir()
	mine := Writer{PID: 1, Kind: "launcher"}
	running := []sessions.GraphRepo{{Checkouts: []sessions.GraphCheckout{
		{Server: &sessions.GraphServer{Agent: &session.AgentHealth{Running: true}}},
	}}}
	require.NoError(t, Write(dir, Machine{WrittenAt: time.Now(), Writer: mine, Repos: running}))

	src := Source{
		Dir:    dir,
		Writer: mine,
		Repos:  func() ([]sessions.Repo, error) { return nil, errors.New("the settings would not load") },
	}
	assert.True(t, src.Tick(t.Context()), "a hiccup is not an idle machine")
}

func TestTheSnapshotDoesNotCarryWhatTheAgentPrinted(t *testing.T) {
	repos := []sessions.GraphRepo{{Checkouts: []sessions.GraphCheckout{
		{Server: &sessions.GraphServer{Agent: &session.AgentHealth{Error: "claude exited: ADE_DELEGATE_TOKEN=hunter2"}}},
		{Server: &sessions.GraphServer{Agent: &session.AgentHealth{}}},
		{},
	}}}

	redact(repos)
	failed := repos[0].Checkouts[0].Server.Agent
	assert.NotContains(t, failed.Error, "hunter2", "the stderr tail is where a token would surface")
	assert.Equal(t, Failed, State(failed), "and a failure is still a failure")
	assert.Empty(t, repos[0].Checkouts[1].Server.Agent.Error)
}

func TestTheCadenceFollowsWhetherAnythingIsMoving(t *testing.T) {
	assert.Equal(t, busyEvery, every(true))
	assert.Equal(t, idleEvery, every(false))
	assert.Less(t, busyEvery, idleEvery, "a working machine is sampled more often than an idle one")
}

func TestTheLoopWritesBeforeItWaitsAndStopsWithItsContext(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	src := Source{
		Dir:    dir,
		Writer: Writer{PID: 1, Kind: "launcher"},
		Repos:  func() ([]sessions.Repo, error) { return []sessions.Repo{{Path: "/repo", Name: "repo"}}, nil },
	}

	done := make(chan struct{})
	go func() { defer close(done); src.Loop(ctx) }()
	require.Eventually(t, func() bool { _, ok := Read(dir); return ok },
		2*time.Second, 10*time.Millisecond, "the first tick does not wait for a cadence")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the loop outlived its context")
	}
}

func TestALoopWithNothingToProbeWritesNothing(t *testing.T) {
	dir := t.TempDir()
	Source{Dir: dir}.Loop(t.Context())

	_, ok := Read(dir)
	assert.False(t, ok)
}

func TestARunningAgentSetsTheFasterCadence(t *testing.T) {
	running := []sessions.GraphRepo{{Checkouts: []sessions.GraphCheckout{
		{Server: &sessions.GraphServer{Agent: &session.AgentHealth{Running: true}}},
	}}}
	quiet := []sessions.GraphRepo{{Checkouts: []sessions.GraphCheckout{
		{Server: &sessions.GraphServer{Agent: &session.AgentHealth{}}},
		{},
	}}}

	assert.True(t, busy(running))
	assert.False(t, busy(quiet))
	assert.False(t, busy(nil))
}
