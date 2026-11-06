package sessions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

// waitingFrom is the rule over a fixed set of checkouts, every one asking
// unless badges says otherwise.
func waitingFrom(t *testing.T, repos []Repo, badges map[string]string) []Row {
	t.Helper()
	all := map[string]string{}
	for _, dir := range repos[0].Checkouts {
		for _, session := range dir.Sessions {
			all[session.ID] = "needs"
		}
	}
	for id, badge := range badges {
		all[id] = badge
	}
	return WaitingIn(repos, all, settings.Seen())
}

func waitingRepo(dir string, made ...Session) []Repo {
	return []Repo{{
		Path: dir, Name: "ade",
		Checkouts: []Dir{{Path: dir, Sessions: made}},
	}}
}

func TestASessionThatAskedSinceYouLookedIsWaiting(t *testing.T) {
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
	t.Setenv("HOME", t.TempDir())
	got := waitingFrom(t, waitingRepo("/work/ade",
		Session{ID: "fresh", Name: "a", LastUsed: time.Now()},
	), nil)
	require.Len(t, got, 1)
	assert.Equal(t, "fresh", got[0].ID)
}

func TestOnlyAnAgentThatStoppedForYouIsWaiting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now()
	got := waitingFrom(t, waitingRepo("/work/ade",
		Session{ID: "busy", Name: "a", LastUsed: now},
		Session{ID: "asking", Name: "b", LastUsed: now.Add(-time.Minute)},
		Session{ID: "stuck", Name: "c", LastUsed: now},
		Session{ID: "done", Name: "d", LastUsed: now},
		Session{ID: "gone", Name: "e", LastUsed: now},
		Session{ID: "closed", Name: "f", LastUsed: now},
	), map[string]string{"busy": "working", "stuck": "blocked", "done": "idle", "gone": "ended", "closed": ""})

	require.Len(t, got, 2)
	assert.Equal(t, "asking", got[0].ID)
	assert.Equal(t, "stuck", got[1].ID)
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
	t.Setenv("HOME", t.TempDir())
	got := waitingFrom(t, waitingRepo("/work/ade",
		Session{ID: "away", Name: "a", LastUsed: time.Now(), Archived: true},
	), nil)
	assert.Empty(t, got)
}
