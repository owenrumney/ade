package sessions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

// waitingFrom is the rule over a fixed set of checkouts, so the tests do not
// depend on what is on the fixture running them.
func waitingFrom(t *testing.T, repos []Repo, running map[string]bool) []Row {
	t.Helper()
	return WaitingIn(repos, running, settings.Seen(), settings.Archived())
}

func waitingRepo(dir string, made ...Session) []Repo {
	return []Repo{{
		Path: dir, Name: "ade",
		Checkouts: []Dir{{Path: dir, Sessions: made}},
	}}
}

func TestASessionThatSpokeSinceYouLookedIsWaiting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	spoke := time.Now().Add(-time.Minute)
	require.NoError(t, settings.MarkSeen("read", spoke.Add(time.Second)))

	got := waitingFrom(t, waitingRepo("/work/ade",
		Session{ID: "unread", Name: "a", LastUsed: spoke},
		Session{ID: "read", Name: "b", LastUsed: spoke},
	), nil)

	require.Len(t, got, 1, "the one you have read is not waiting")
	assert.Equal(t, "unread", got[0].ID)
}

func TestASessionYouHaveNeverSeenIsWaiting(t *testing.T) {
	// No record is not "seen at the beginning of time": a session you have
	// never opened is exactly one that wants you.
	t.Setenv("HOME", t.TempDir())
	got := waitingFrom(t, waitingRepo("/work/ade",
		Session{ID: "fresh", Name: "a", LastUsed: time.Now()},
	), nil)
	require.Len(t, got, 1)
	assert.Equal(t, "fresh", got[0].ID)
}

func TestARunningSessionIsWorkingRatherThanWaiting(t *testing.T) {
	// Its transcript moves while it runs, so without this every live session
	// would sit in the panel claiming to want an answer.
	t.Setenv("HOME", t.TempDir())
	got := waitingFrom(t, waitingRepo("/work/ade",
		Session{ID: "busy", Name: "a", LastUsed: time.Now()},
		Session{ID: "beside", Name: "b", LastUsed: time.Now()},
	), map[string]bool{"busy": true})
	// Per session and not per checkout: the one beside it is still waiting.
	require.Len(t, got, 1)
	assert.Equal(t, "beside", got[0].ID)
}

func TestTheLongestWaitIsFirst(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now()
	got := waitingFrom(t, waitingRepo("/work/ade",
		Session{ID: "recent", Name: "a", LastUsed: now.Add(-time.Minute)},
		Session{ID: "oldest", Name: "b", LastUsed: now.Add(-time.Hour)},
	), nil)
	require.Len(t, got, 2)
	assert.Equal(t, "oldest", got[0].ID, "the one waiting longest is the one to answer")
}

func TestAnArchivedSessionIsNotWaiting(t *testing.T) {
	// Putting one away is saying you are done with it.
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.SetArchived("away", true))
	got := waitingFrom(t, waitingRepo("/work/ade",
		Session{ID: "away", Name: "a", LastUsed: time.Now()},
	), nil)
	assert.Empty(t, got)
}

func TestASessionThatNeverRanIsNotWaiting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got := waitingFrom(t, waitingRepo("/work/ade",
		Session{ID: "never", Name: "a"},
	), nil)
	assert.Empty(t, got)
}

func TestOnlyAReplyThatAsksSomethingIsWaiting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply string
		want  bool
	}{
		{"a question", "Two ways to do it.\n\nWhich?", true},
		{"a report", "Done. Tests pass.", false},
		{"a trailing blank line", "Shall I go on?\n\n", true},
		{"bold", "**Want me to fix it?**", true},
		{"a list under the question", "Pick one?\n\n- a\n- b", true},
		{"more options than a window allowed", "Which?\n\n- a\n- b\n- c\n- d\n- e\n- f", true},
		{"a question earlier in the prose", "Which?\n\nI went with the first one.", false},
		{"a question inside a code block", "Run it:\n\n```\ngit log --oneline?\n```", false},
		{"a question above a table", "Which?\n\n| a | b |\n| - | - |", true},
		{"a numbered list under the question", "Which?\n\n1. a\n2. b", true},
		{"nothing said", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsQuestion(tc.reply))
		})
	}
}

// The row shows what was asked, so you answer the question rather than
// recalling it. The line comes off the reply the rule already reads.
func TestTheQuestionIsKeptNotJustCounted(t *testing.T) {
	line, ok := Question("I can do either.\n\n**Postgres or SQLite?**\n\n- a\n- b")
	assert.True(t, ok)
	assert.Equal(t, "Postgres or SQLite?", line, "the markdown around it is not the question")

	line, ok = Question("Done. Tests pass.")
	assert.False(t, ok)
	assert.Empty(t, line)
}
