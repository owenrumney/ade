package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWritesJobStartsASessionOnItsOwnBranch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	j := job(t, "---\nschedule: adhoc\nallow: writes\n---\n\nbump the editor\n")
	var asked struct{ dir, name, prompt string }
	out := Start(context.Background(), j, "/repo/ade", func(_ context.Context, dir, name, prompt string) (Started, error) {
		asked.dir, asked.name, asked.prompt = dir, name, prompt
		return Started{URL: "http://127.0.0.1:1/?t=x", Path: "/repo/ade-" + name}, nil
	})

	assert.Equal(t, "/repo/ade", asked.dir)
	assert.Equal(t, WorktreeName(j, time.Now()), asked.name)
	assert.Contains(t, asked.prompt, "bump the editor")
	assert.Contains(t, asked.prompt, "Do not push", "a writes run is told what it may not do")
	assert.NotContains(t, asked.prompt, "You cannot edit it", "the read-only standing text is the wrong one here")
	assert.Equal(t, "/repo/ade-"+asked.name, out.Dir)
	assert.Equal(t, "http://127.0.0.1:1/?t=x", out.URL)
	assert.Contains(t, out.Report, asked.name)

	state := ReadState(j)
	require.Len(t, state.Outcomes, 1)
	assert.Equal(t, out.URL, state.Outcomes[0].URL)
	assert.True(t, state.NextDue.IsZero(), "an ad hoc job has no next due")
}

func TestAWritesJobThatCannotStartIsAnOutcome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	j := job(t, "---\nschedule: adhoc\nallow: writes\n---\n\nx\n")
	out := Start(context.Background(), j, "/repo/ade", func(context.Context, string, string, string) (Started, error) {
		return Started{}, errors.New("ade in /repo/ade did not report a url")
	})
	assert.Contains(t, out.Error, "did not report")
	assert.Empty(t, out.URL)
	require.Len(t, ReadState(j).Outcomes, 1)
}

func TestAWritesJobIsFreshBecauseItsBranchIsItsMemory(t *testing.T) {
	_, err := Parse("/tmp/x.md", ScopeUser, []byte("---\nschedule: adhoc\nallow: writes\ncontinuity: summary\n---\n\nx\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "continuity: fresh")
}
