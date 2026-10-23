package dash

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
)

// reviewWorktree writes a pull request's worktree and the threads in it, the
// way the review that filed them would have.
func reviewWorktree(t *testing.T, main string, number int, threads []comments.Thread) {
	t.Helper()
	path := gitdiff.WorktreePath(main, "pr-"+strconv.Itoa(number))
	dir := filepath.Join(path, ".git", "ade")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	raw, err := json.Marshal(threads)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pr-"+strconv.Itoa(number)+".json"), raw, 0o600))
}

func sentAt() *time.Time {
	at := time.Now()
	return &at
}

func TestAPullRequestWithNoWorktreeIsStillJustRequested(t *testing.T) {
	got := reviewStateIn(t.TempDir(), 12, false)
	assert.Equal(t, StateRequested, got.State)
	assert.Zero(t, got.Threads)
}

func TestTheStateOfAReview(t *testing.T) {
	tests := []struct {
		name    string
		running bool
		threads []comments.Thread
		want    string
		unsent  int
	}{
		{
			name:    "a worktree with a review going and nothing filed yet",
			running: true,
			want:    StateRunning,
		},
		{
			name:    "findings filed, none sent",
			threads: []comments.Thread{{ID: "a"}, {ID: "b"}},
			want:    StateDrafted,
			unsent:  2,
		},
		{
			name:    "every one of them on GitHub",
			threads: []comments.Thread{{ID: "a", SentAt: sentAt()}, {ID: "b", SentAt: sentAt()}},
			want:    StateSent,
		},
		{
			name:    "one sent, one still drafted",
			threads: []comments.Thread{{ID: "a", SentAt: sentAt()}, {ID: "b"}},
			want:    StateDrafted,
			unsent:  1,
		},
		{
			name: "a reply of yours that has not gone",
			threads: []comments.Thread{{
				ID:     "a",
				SentAt: sentAt(),
				Comments: []comments.Comment{
					{ID: "c1", ExternalID: "gh1", SentAt: sentAt()},
					{ID: "c2", Body: "answered"},
				},
			}},
			want:   StateDrafted,
			unsent: 1,
		},
		{
			name:    "a withdrawn finding never goes, and must not hold the row",
			threads: []comments.Thread{{ID: "a", SentAt: sentAt()}, {ID: "b", Withdrawn: true}},
			want:    StateSent,
		},
		{
			name:    "one resolved without sending is read, not drafted",
			threads: []comments.Thread{{ID: "a", Resolved: true}},
			want:    StateReady,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			main := t.TempDir()
			reviewWorktree(t, main, 12, test.threads)

			got := reviewStateIn(main, 12, test.running)

			assert.Equal(t, test.want, got.State)
			assert.Equal(t, test.unsent, got.Unsent)
		})
	}
}

func TestAnImportedCommentIsTheirsAndNotCounted(t *testing.T) {
	main := t.TempDir()
	reviewWorktree(t, main, 12, []comments.Thread{{ID: "a", ExternalID: "gh9"}})

	got := reviewStateIn(main, 12, false)

	assert.Equal(t, StateRequested, got.State)
	assert.Zero(t, got.Threads, "a thread imported from GitHub is not yours to send")
}
