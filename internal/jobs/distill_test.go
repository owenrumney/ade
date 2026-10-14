package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/wiki"
)

// stubClaude is a claude that prints what it is told to and exits with code.
func stubClaude(t *testing.T, out string, code int) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "claude")
	script := fmt.Sprintf("#!/bin/sh\ncat >/dev/null\n%s\nexit %d\n", out, code)
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o700))
	return bin
}

func TestALoginThatExpiredIsNotBlamedOnTheModel(t *testing.T) {
	bin := stubClaude(t, `echo "Failed to authenticate: OAuth token expired" >&2`, 1)
	_, _, err := DistillModel(bin, t.TempDir())(context.Background(), "distil this")
	require.Error(t, err)
	assert.True(t, errors.Is(err, wiki.ErrNotTheModel), "a night of expired tokens must not hold the job for days")
}

func TestATurnThatRanAndMentionedOAuthStillCounts(t *testing.T) {
	// The brake is what stops a failing repository being run every half hour
	// forever, so only a run that never reached the model is exempt from it.
	bin := stubClaude(t, `echo '{"type":"result","is_error":true,"result":"the transcript mentions oauth","total_cost_usd":0.4,"session_id":"s1"}'`, 0)
	_, _, err := DistillModel(bin, t.TempDir())(context.Background(), "distil this")
	require.Error(t, err)
	assert.False(t, errors.Is(err, wiki.ErrNotTheModel))
}

// A session distils itself when it goes idle, so nothing schedules it any
// more. Turned fully on is the case that used to add the job.
func TestNothingSchedulesDistillationAnyMore(t *testing.T) {
	on := settings.Settings{WikiRemote: "git@example.com:w.git", WikiDistill: true}
	_, found := Find(WithBuiltins(nil, on), KindWikiDistill)
	assert.False(t, found, "the session owns its own distillation now")

	mine := Job{Name: KindWikiDistill, Scope: ScopeUser}
	list := WithBuiltins([]Job{mine}, on)
	assert.Equal(t, []Job{mine}, list, "a job written by hand is the only way back to a schedule")
}

func TestAnExpiredLoginEndsTheFireAndSaysWhatToDo(t *testing.T) {
	// Every repository shares the one login, so the second to try it only
	// spends another claude start on the same answer.
	bin := stubClaude(t, `echo "Failed to authenticate: OAuth session expired and could not be refreshed" >&2`, 1)
	_, _, err := DistillModel(bin, t.TempDir())(context.Background(), "distil this")
	require.Error(t, err)
	assert.True(t, endsTheFire(fmt.Errorf("run the model: %w", err)), "the wrapped error is what a fire sees")

	note := loginGone(3)
	assert.Contains(t, note, "log in again")
	assert.Contains(t, note, "claude")
	assert.Contains(t, note, "3 more repositories")
	assert.NotContains(t, loginGone(0), "repositories", "the last repository leaves nothing behind it")
}

func TestWhatDoesNotEndTheFire(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"nothing failed", nil},
		{"a repository of its own", errors.New("sync the wiki: exit status 1")},
		{"a turn that ran and said oauth", fmt.Errorf("the transcript mentions oauth")},
		{"a failure before the model that was not the login", fmt.Errorf("seed a draft: %w: disk full", wiki.ErrNotTheModel)},
	} {
		t.Run(tc.name, func(t *testing.T) { assert.False(t, endsTheFire(tc.err)) })
	}
}
