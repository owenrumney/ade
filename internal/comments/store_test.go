package comments

import (
	"os"
	"path/filepath"
	"testing"

	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"time"

	"github.com/owenrumney/ade/internal/gitdiff"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	gitDir := t.TempDir()
	s, err := Open(gitDir, "feature/thing")
	require.NoError(t, err)
	return s, gitDir
}

func addThread(t *testing.T, s *Store, line int, body string) Thread {
	t.Helper()
	th, err := s.Add(Thread{
		File:     "a.go",
		Side:     SideRight,
		Line:     line,
		Comments: []Comment{{Body: body}},
	})
	require.NoError(t, err)
	return th
}

func TestOpenMissingStoreIsEmpty(t *testing.T) {
	s, gitDir := newStore(t)
	assert.Empty(t, s.List())
	assert.Equal(t, filepath.Join(gitDir, "ade", "feature-thing.json"), s.Path())
	assert.NoFileExists(t, s.Path(), "nothing is written until first use")
}

func TestAddAndPersist(t *testing.T) {
	s, gitDir := newStore(t)

	th := addThread(t, s, 42, "wrong var")
	assert.NotEmpty(t, th.ID)
	assert.Equal(t, AuthorYou, th.Comments[0].Author)
	assert.False(t, th.CreatedAt.IsZero())
	assert.FileExists(t, s.Path())

	reopened, err := Open(gitDir, "feature/thing")
	require.NoError(t, err)
	threads := reopened.List()
	require.Len(t, threads, 1)
	assert.Equal(t, th.ID, threads[0].ID)
	assert.Equal(t, "wrong var", threads[0].Comments[0].Body)
	assert.Equal(t, 42, threads[0].Line)
}

func TestAddValidation(t *testing.T) {
	s, _ := newStore(t)

	tests := []struct {
		name   string
		thread Thread
		errMsg string
	}{
		{
			name:   "no file",
			thread: Thread{Side: SideRight, Line: 1, Comments: []Comment{{Body: "x"}}},
			errMsg: "needs a file and a line",
		},
		{
			name:   "zero line",
			thread: Thread{File: "a.go", Side: SideRight, Comments: []Comment{{Body: "x"}}},
			errMsg: "needs a file and a line",
		},
		{
			name:   "bad side",
			thread: Thread{File: "a.go", Side: "middle", Line: 1, Comments: []Comment{{Body: "x"}}},
			errMsg: "invalid side",
		},
		{
			name:   "no comments",
			thread: Thread{File: "a.go", Side: SideRight, Line: 1},
			errMsg: "needs a first comment",
		},
		{
			name:   "blank first comment",
			thread: Thread{File: "a.go", Side: SideRight, Line: 1, Comments: []Comment{{Body: "  "}}},
			errMsg: "needs a first comment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Add(tt.thread)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
	assert.Empty(t, s.List(), "rejected threads are not stored")
}

func TestReplyReopensThread(t *testing.T) {
	s, _ := newStore(t)
	th := addThread(t, s, 1, "look at this")

	_, err := s.SetResolved(th.ID, true)
	require.NoError(t, err)

	updated, err := s.Reply(th.ID, AuthorYou, "actually no, the problem is X")
	require.NoError(t, err)

	require.Len(t, updated.Comments, 2)
	assert.Equal(t, "actually no, the problem is X", updated.Comments[1].Body)
	assert.False(t, updated.Resolved, "replying reopens the thread")
}

func TestReplyErrors(t *testing.T) {
	s, _ := newStore(t)
	th := addThread(t, s, 1, "hi")

	t.Run("unknown thread", func(t *testing.T) {
		_, err := s.Reply("nope", AuthorYou, "x")
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("empty body", func(t *testing.T) {
		_, err := s.Reply(th.ID, AuthorYou, " ")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty comment")
	})

	t.Run("agent author is preserved", func(t *testing.T) {
		updated, err := s.Reply(th.ID, AuthorAgent, "fixed it")
		require.NoError(t, err)
		assert.Equal(t, AuthorAgent, updated.Comments[1].Author)
	})
}

func TestUnresolvedFilters(t *testing.T) {
	s, _ := newStore(t)
	a := addThread(t, s, 1, "one")
	addThread(t, s, 2, "two")

	_, err := s.SetResolved(a.ID, true)
	require.NoError(t, err)

	open := s.Unresolved()
	require.Len(t, open, 1)
	assert.Equal(t, "two", open[0].Comments[0].Body)
}

func TestGet(t *testing.T) {
	s, _ := newStore(t)
	th := addThread(t, s, 7, "hi")

	got, err := s.Get(th.ID)
	require.NoError(t, err)
	assert.Equal(t, 7, got.Line)

	_, err = s.Get("missing")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDelete(t *testing.T) {
	s, _ := newStore(t)
	a := addThread(t, s, 1, "one")
	b := addThread(t, s, 2, "two")

	require.NoError(t, s.Delete(a.ID))
	threads := s.List()
	require.Len(t, threads, 1)
	assert.Equal(t, b.ID, threads[0].ID)

	assert.ErrorIs(t, s.Delete("nope"), ErrNotFound)

	require.NoError(t, s.Delete(b.ID))
	assert.NoFileExists(t, s.Path(), "the store file is removed once empty")
}

func TestListReturnsCopies(t *testing.T) {
	s, _ := newStore(t)
	addThread(t, s, 1, "original")

	got := s.List()
	got[0].Comments[0].Body = "tampered"

	assert.Equal(t, "original", s.List()[0].Comments[0].Body)
}

func TestReanchorPersists(t *testing.T) {
	s, gitDir := newStore(t)
	anchor, ok := Locate(parseDiff(t, original), "a.go", SideRight, 3)
	require.True(t, ok)

	_, err := s.Add(Thread{
		File: "a.go", Side: SideRight, Line: 3, Anchor: anchor,
		Comments: []Comment{{Body: "check this"}},
	})
	require.NoError(t, err)

	s.Reanchor(parseDiff(t, shifted))

	reopened, err := Open(gitDir, "feature/thing")
	require.NoError(t, err)
	threads := reopened.List()
	require.Len(t, threads, 1)
	assert.Equal(t, 5, threads[0].Line)
	assert.False(t, threads[0].Orphaned)
}

func TestOpenCorruptStore(t *testing.T) {
	gitDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(gitDir, "ade"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "ade", "main.json"), []byte("{not json"), 0o600))

	_, err := Open(gitDir, "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse thread store")
}

func TestSlug(t *testing.T) {
	tests := map[string]string{
		"main":                     "main",
		"feature/thing":            "feature-thing",
		"fix-520-pi/agent SIGTERM": "fix-520-pi-agent-SIGTERM",
		"../../etc/passwd":         "etc-passwd",
		"":                         "detached",
		"///":                      "detached",
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, want, Slug(in))
		})
	}
}

func TestRepoScopedThreads(t *testing.T) {
	s, gitDir := newStore(t)

	th, err := s.Add(Thread{
		Scope:    ScopeRepo,
		Comments: []Comment{{Body: "error handling is inconsistent across the package"}},
	})
	require.NoError(t, err, "a repo comment needs no file or line")

	assert.Equal(t, ScopeRepo, th.Scope)
	assert.Empty(t, th.File)
	assert.Zero(t, th.Line)
	assert.Empty(t, th.Side)

	t.Run("a stray anchor is discarded", func(t *testing.T) {
		got, err := s.Add(Thread{
			Scope:    ScopeRepo,
			File:     "a.go",
			Side:     SideRight,
			Line:     42,
			Comments: []Comment{{Body: "x"}},
		})
		require.NoError(t, err)
		assert.Empty(t, got.File)
		assert.Zero(t, got.Line)
	})

	t.Run("reanchoring never orphans it", func(t *testing.T) {
		s.Reanchor(parseDiff(t, rewritten))
		for _, got := range s.List() {
			if got.Scope == ScopeRepo {
				assert.False(t, got.Orphaned, "a repo comment has nothing to lose its anchor to")
			}
		}
	})

	t.Run("it persists", func(t *testing.T) {
		reopened, err := Open(gitDir, "feature/thing")
		require.NoError(t, err)
		var found bool
		for _, got := range reopened.List() {
			if got.Scope == ScopeRepo {
				found = true
			}
		}
		assert.True(t, found)
	})

	t.Run("line scope still requires an anchor", func(t *testing.T) {
		_, err := s.Add(Thread{Scope: ScopeLine, Comments: []Comment{{Body: "x"}}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "needs a file and a line")
	})

	t.Run("an unknown scope is rejected", func(t *testing.T) {
		_, err := s.Add(Thread{Scope: "galaxy", Comments: []Comment{{Body: "x"}}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid scope")
	})
}

func TestAThreadThatHasNotBeenSentSaysNothingAboutBeingSent(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir, "main")
	require.NoError(t, err)

	thread, err := store.Add(Thread{
		File: "a.go", Side: "right", Line: 3,
		Comments: []Comment{{Author: "agent", Body: "finding"}},
	})
	require.NoError(t, err)

	// omitempty does nothing for a time.Time: a zero one is a struct, not an
	// empty value, so it marshalled as year 1 and the browser read every thread
	// as already sent — "sent to GitHub 739848d ago", about comments that had
	// gone nowhere.
	encoded, err := json.Marshal(thread)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "sentAt")
	assert.NotContains(t, string(encoded), "0001-01-01")

	// And it is still offered, which the same bug had suppressed.
	require.Len(t, store.Sendable(), 1)

	when := time.Now()
	require.NoError(t, store.MarkSent([]string{thread.ID}, when))

	sent, err := store.Get(thread.ID)
	require.NoError(t, err)
	require.NotNil(t, sent.SentAt)
	assert.WithinDuration(t, when, *sent.SentAt, time.Second)

	encoded, err = json.Marshal(sent)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), "sentAt")
	assert.Empty(t, store.Sendable())
}

func TestAYearOneTimestampIsNotASend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ade", "main.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))

	// What an earlier version wrote for every thread, sent or not.
	require.NoError(t, os.WriteFile(path, []byte(`[{
		"id":"t1","file":"a.go","side":"right","line":3,
		"comments":[{"id":"c1","author":"agent","body":"finding"}],
		"outgoing":"Could we reset the counter?",
		"sentAt":"0001-01-01T00:00:00Z"
	}]`), 0o600))

	store, err := Open(dir, "main")
	require.NoError(t, err)

	thread, err := store.Get("t1")
	require.NoError(t, err)
	assert.False(t, thread.Sent(), "a year-1 timestamp claimed the comment had been posted")
	assert.Nil(t, thread.SentAt, "the bad value should not survive being read")

	// And the comment is offered again, rather than being hidden behind a send
	// that never happened.
	require.Len(t, store.Sendable(), 1)

	// The field goes entirely, rather than going back out as year 1. Other
	// timestamps in this fixture are unset and are not what this is about.
	encoded, err := json.Marshal(thread)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "sentAt")
}

func TestSent(t *testing.T) {
	when := time.Now()
	zero := time.Time{}

	assert.False(t, Thread{}.Sent())
	assert.False(t, Thread{SentAt: &zero}.Sent())
	assert.True(t, Thread{SentAt: &when}.Sent())
}

func TestAddOrReplyFoldsASecondReviewerOntoTheSameLine(t *testing.T) {
	s, _ := newStore(t)

	first := Thread{
		File: "a.go", Side: SideRight, Line: 4, Severity: "low", Category: "style",
		Comments: []Comment{{Author: "security", Body: "unvalidated input"}},
	}
	_, merged, err := s.AddOrReply(first)
	require.NoError(t, err)
	assert.False(t, merged, "the first finding on a line opens the thread")

	second := Thread{
		File: "a.go", Side: SideRight, Line: 4, Severity: "critical",
		Comments: []Comment{{Author: "tester", Body: "same hole, no test"}},
	}
	got, merged, err := s.AddOrReply(second)
	require.NoError(t, err)
	assert.True(t, merged)

	require.Len(t, s.List(), 1, "three reviewers on one line is one thread")
	require.Len(t, got.Comments, 2)
	assert.Equal(t, "tester", got.Comments[1].Author)
	assert.Equal(t, SeverityCritical, got.Severity, "the thread is as bad as its worst finding")
	assert.Equal(t, "style", got.Category, "the first finding's category stands")
}

func TestAddOrReplyLeavesYourOwnThreadsAlone(t *testing.T) {
	s, _ := newStore(t)

	mine, err := s.Add(Thread{
		File: "a.go", Side: SideRight, Line: 4,
		Comments: []Comment{{Author: AuthorYou, Body: "why this way?"}},
	})
	require.NoError(t, err)

	_, merged, err := s.AddOrReply(Thread{
		File: "a.go", Side: SideRight, Line: 4,
		Comments: []Comment{{Author: "security", Body: "a finding"}},
	})
	require.NoError(t, err)
	assert.False(t, merged, "an agent must not append to a question you asked")
	require.Len(t, s.List(), 2)

	again, err := s.Get(mine.ID)
	require.NoError(t, err)
	assert.Len(t, again.Comments, 1)
}

func TestAddOrReplyStartsAgainOnAResolvedThread(t *testing.T) {
	s, _ := newStore(t)

	first, err := s.Add(Thread{
		File: "a.go", Side: SideRight, Line: 4,
		Comments: []Comment{{Author: "security", Body: "found once"}},
	})
	require.NoError(t, err)
	_, err = s.SetResolved(first.ID, true)
	require.NoError(t, err)

	_, merged, err := s.AddOrReply(Thread{
		File: "a.go", Side: SideRight, Line: 4,
		Comments: []Comment{{Author: "security", Body: "found again"}},
	})
	require.NoError(t, err)
	assert.False(t, merged, "a settled thread is not reopened by a new review")
	assert.Len(t, s.List(), 2)
}

func TestSeverityNormalises(t *testing.T) {
	assert.Equal(t, SeverityCritical, Severity(" Critical "))
	assert.Equal(t, SeverityMedium, Severity(""), "unranked reads as medium")
	assert.Equal(t, SeverityMedium, Severity("catastrophic"))
	assert.True(t, Worse("high", "medium"))
	assert.False(t, Worse("low", "medium"))
	assert.False(t, Worse("medium", ""), "unset is medium, not worse than it")
}

func TestResetClearsWhatThisStoreOwns(t *testing.T) {
	s, _ := newStore(t)
	mine := addThread(t, s, 1, "mine")
	addThread(t, s, 2, "the reviewer's")

	imported, err := s.Add(Thread{
		File: "a.go", Side: SideRight, Line: 3, ExternalID: "gh-1",
		Comments: []Comment{{Body: "from the pull request"}},
	})
	require.NoError(t, err)

	posted := addThread(t, s, 4, "already gone to GitHub")
	require.NoError(t, s.MarkSent([]string{posted.ID}, time.Now()))

	resettable := s.Resettable()
	require.Len(t, resettable, 2)
	assert.Equal(t, mine.ID, resettable[0].ID)

	gone, err := s.Reset()
	require.NoError(t, err)
	assert.Equal(t, 2, gone)

	left := s.List()
	require.Len(t, left, 2, "an imported or sent thread is not this store's to throw away")
	ids := []string{left[0].ID, left[1].ID}
	assert.ElementsMatch(t, []string{imported.ID, posted.ID}, ids)
}

func TestResetOnNothingWritesNothing(t *testing.T) {
	s, _ := newStore(t)
	gone, err := s.Reset()
	require.NoError(t, err)
	assert.Zero(t, gone)
	assert.NoFileExists(t, s.Path())
}

func TestSetVerdictMarksAThreadWithoutRemovingIt(t *testing.T) {
	s, _ := newStore(t)
	first := addThread(t, s, 3, "the nil check is the wrong way round")
	second := addThread(t, s, 4, "same point again")

	got, err := s.SetVerdict(second.ID, Verdict{Disputed: true, Withdrawn: true, DuplicateOf: first.ID})
	require.NoError(t, err)
	assert.True(t, got.Disputed)
	assert.True(t, got.Withdrawn)
	assert.Equal(t, first.ID, got.DuplicateOf)
	assert.Len(t, s.List(), 2, "a verdict marks a thread; it never removes one")

	// It survives a reopen, beside Resolved.
	again, err := Open(filepath.Dir(filepath.Dir(s.Path())), "feature/thing")
	require.NoError(t, err)
	reloaded, err := again.Get(second.ID)
	require.NoError(t, err)
	assert.True(t, reloaded.Withdrawn)
	assert.Equal(t, first.ID, reloaded.DuplicateOf)

	_, err = s.SetVerdict("nope", Verdict{Disputed: true})
	assert.ErrorIs(t, err, ErrNotFound)
}

// Reviewers of one bug pick different lines for it — the deref, the check
// above it, the signature. An exact-line rule left three comments saying one
// thing in the diff.
func TestAddOrReplyFoldsANearbyFindingOfTheSameKind(t *testing.T) {
	s, _ := newStore(t)

	_, _, err := s.AddOrReply(Thread{
		File: "a.go", Side: SideRight, Line: 40, Category: "correctness",
		Comments: []Comment{{Author: "security", Body: "nil deref"}},
	})
	require.NoError(t, err)

	got, merged, err := s.AddOrReply(Thread{
		File: "a.go", Side: SideRight, Line: 44, Category: "correctness",
		Comments: []Comment{{Author: "tester", Body: "the same nil deref"}},
	})
	require.NoError(t, err)
	assert.True(t, merged, "four lines apart, same kind of problem")
	require.Len(t, s.List(), 1)
	assert.Equal(t, 40, got.Line, "the thread stays where it was filed")

	// A different kind of remark is its own point once it is off the line.
	_, merged, err = s.AddOrReply(Thread{
		File: "a.go", Side: SideRight, Line: 44, Category: "style",
		Comments: []Comment{{Author: "simplifier", Body: "this reads badly"}},
	})
	require.NoError(t, err)
	assert.False(t, merged, "a style note is not a reply to a correctness bug")

	// Too far to be the same point.
	_, merged, err = s.AddOrReply(Thread{
		File: "a.go", Side: SideRight, Line: 80, Category: "correctness",
		Comments: []Comment{{Author: "staff-reviewer", Body: "different bug"}},
	})
	require.NoError(t, err)
	assert.False(t, merged, "forty lines away is another part of the file")
	assert.Len(t, s.List(), 3)
}

// Of two threads in range the nearer one takes the finding, so a comment on
// the line itself is not folded into one further off that was filed first.
func TestAddOrReplyFoldsIntoTheNearestThread(t *testing.T) {
	s, _ := newStore(t)

	far, _, err := s.AddOrReply(Thread{
		File: "a.go", Side: SideRight, Line: 30, Category: "correctness",
		Comments: []Comment{{Author: "security", Body: "far"}},
	})
	require.NoError(t, err)
	near, _, err := s.AddOrReply(Thread{
		File: "a.go", Side: SideRight, Line: 38, Category: "style",
		Comments: []Comment{{Author: "simplifier", Body: "near"}},
	})
	require.NoError(t, err)

	got, merged, err := s.AddOrReply(Thread{
		File: "a.go", Side: SideRight, Line: 38, Category: "correctness",
		Comments: []Comment{{Author: "tester", Body: "on the line"}},
	})
	require.NoError(t, err)
	require.True(t, merged)
	assert.Equal(t, near.ID, got.ID, "the line itself beats a matching category eight lines off")
	assert.NotEqual(t, far.ID, got.ID)
}

func TestSetExternalRecordsWhatASentThreadBecame(t *testing.T) {
	s, _ := newStore(t)
	th := addThread(t, s, 3, "this reads oddly")

	updated, err := s.SetExternal(th.ID, "9001")
	require.NoError(t, err)
	assert.Equal(t, "9001", updated.ExternalID)

	found, ok := s.ByExternal("9001")
	require.True(t, ok, "the thread is now findable by the ID GitHub gave it")
	assert.Equal(t, th.ID, found.ID)

	_, err = s.SetExternal("nope", "9002")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestByExternalIgnoresLocalThreads(t *testing.T) {
	s, _ := newStore(t)
	addThread(t, s, 3, "mine alone")

	_, ok := s.ByExternal("")
	assert.False(t, ok, "an empty external ID must not match every local thread")
}

func TestMergeExternalReplies(t *testing.T) {
	setup := func(t *testing.T) (*Store, Thread) {
		t.Helper()
		s, _ := newStore(t)
		th, err := s.Add(Thread{
			File: "a.go", Side: SideRight, Line: 3, ExternalID: "100",
			Comments: []Comment{{Author: "Copilot", Body: "why 2?", ExternalID: "100"}},
		})
		require.NoError(t, err)
		return s, th
	}

	t.Run("a new reply is appended", func(t *testing.T) {
		s, th := setup(t)
		n, err := s.MergeExternalReplies(th.ID, []Comment{
			{Author: "Copilot", Body: "why 2?", ExternalID: "100"},
			{Author: "owenrumney", Body: "because", ExternalID: "101"},
		})
		require.NoError(t, err)
		assert.Equal(t, 1, n, "only the reply it had not seen")

		got, err := s.Get(th.ID)
		require.NoError(t, err)
		require.Len(t, got.Comments, 2)
		assert.Equal(t, "because", got.Comments[1].Body)
		assert.NotEmpty(t, got.Comments[1].ID, "the appended reply gets a local ID")
	})

	t.Run("a reply already recorded is skipped", func(t *testing.T) {
		s, th := setup(t)
		incoming := []Comment{
			{Author: "Copilot", Body: "why 2?", ExternalID: "100"},
			{Author: "owenrumney", Body: "because", ExternalID: "101"},
		}
		_, err := s.MergeExternalReplies(th.ID, incoming)
		require.NoError(t, err)

		n, err := s.MergeExternalReplies(th.ID, incoming)
		require.NoError(t, err)
		assert.Equal(t, 0, n, "a second poll of the same thread adds nothing")

		got, err := s.Get(th.ID)
		require.NoError(t, err)
		assert.Len(t, got.Comments, 2)
	})

	t.Run("a new reply reopens a resolved thread", func(t *testing.T) {
		s, th := setup(t)
		_, err := s.SetResolved(th.ID, true)
		require.NoError(t, err)

		n, err := s.MergeExternalReplies(th.ID, []Comment{
			{Author: "owenrumney", Body: "still wrong", ExternalID: "101"},
		})
		require.NoError(t, err)
		require.Equal(t, 1, n)

		got, err := s.Get(th.ID)
		require.NoError(t, err)
		assert.False(t, got.Resolved, "a reply means the point was not settled")
	})

	t.Run("nothing incoming leaves a resolved thread resolved", func(t *testing.T) {
		s, th := setup(t)
		_, err := s.SetResolved(th.ID, true)
		require.NoError(t, err)

		n, err := s.MergeExternalReplies(th.ID, nil)
		require.NoError(t, err)
		assert.Equal(t, 0, n)

		got, err := s.Get(th.ID)
		require.NoError(t, err)
		assert.True(t, got.Resolved, "an empty poll must not reopen it")
	})

	t.Run("a locally written comment is never matched", func(t *testing.T) {
		s, th := setup(t)
		n, err := s.MergeExternalReplies(th.ID, []Comment{{Author: "you", Body: "no ID"}})
		require.NoError(t, err)
		assert.Equal(t, 0, n, "a comment with no GitHub ID cannot be deduplicated, so it is not imported")
	})

	t.Run("an unknown thread is an error", func(t *testing.T) {
		s, _ := setup(t)
		_, err := s.MergeExternalReplies("nope", []Comment{{ExternalID: "1"}})
		assert.ErrorIs(t, err, ErrNotFound)
	})
}

func TestPendingReplyIsWhatHasNotGoneBackYet(t *testing.T) {
	imported := func(id, body string) Comment {
		return Comment{Author: "Copilot", Body: body, ExternalID: id}
	}
	when := time.Now()

	cases := []struct {
		name     string
		comments []Comment
		want     []string
	}{
		{
			name:     "your answer to an imported comment",
			comments: []Comment{imported("100", "why 2?"), {Author: AuthorYou, Body: "because"}},
			want:     []string{"because"},
		},
		{
			name:     "a drafted answer counts too",
			comments: []Comment{imported("100", "why 2?"), {Author: AuthorDraft, Body: "drafted"}},
			want:     []string{"drafted"},
		},
		{
			name:     "nothing to say back",
			comments: []Comment{imported("100", "why 2?")},
			want:     nil,
		},
		{
			name: "an answer already sent is not offered again",
			comments: []Comment{
				imported("100", "why 2?"),
				{Author: AuthorYou, Body: "because", SentAt: &when},
			},
			want: nil,
		},
		{
			name: "only what follows the last imported comment",
			comments: []Comment{
				imported("100", "why 2?"),
				{Author: AuthorYou, Body: "answered once", SentAt: &when},
				imported("101", "still no"),
				{Author: AuthorYou, Body: "answered twice"},
			},
			want: []string{"answered twice"},
		},
		{
			name: "an earlier unsent answer is left behind by a new import",
			comments: []Comment{
				imported("100", "why 2?"),
				{Author: AuthorYou, Body: "never sent"},
				imported("101", "any news?"),
			},
			want: nil,
		},
		{
			name: "a reviewing agent's words are not yours to send",
			comments: []Comment{
				imported("100", "why 2?"),
				{Author: "staff-reviewer", Body: "a finding"},
			},
			want: nil,
		},
		{
			name:     "several answers in a row all go",
			comments: []Comment{imported("100", "why 2?"), {Author: AuthorYou, Body: "one"}, {Author: AuthorYou, Body: "two"}},
			want:     []string{"one", "two"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Thread{Comments: tc.comments}.PendingReply()
			bodies := make([]string, 0, len(got))
			for _, c := range got {
				bodies = append(bodies, c.Body)
			}
			if tc.want == nil {
				assert.Empty(t, bodies)
				return
			}
			assert.Equal(t, tc.want, bodies)
		})
	}
}

func TestMarkReplySent(t *testing.T) {
	s, _ := newStore(t)
	th, err := s.Add(Thread{
		File: "a.go", Side: SideRight, Line: 3, ExternalID: "100",
		Comments: []Comment{
			{Author: "Copilot", Body: "why 2?", ExternalID: "100"},
			{Author: AuthorYou, Body: "because"},
		},
	})
	require.NoError(t, err)
	pending := th.PendingReply()
	require.Len(t, pending, 1)

	require.NoError(t, s.MarkReplySent(th.ID, []string{pending[0].ID}, time.Now()))

	got, err := s.Get(th.ID)
	require.NoError(t, err)
	assert.Empty(t, got.PendingReply(), "it is on GitHub, so it must not be offered again")
	assert.True(t, got.Comments[1].Sent())
	assert.False(t, got.Comments[0].Sent(), "the imported comment is untouched")

	assert.ErrorIs(t, s.MarkReplySent("nope", []string{"x"}, time.Now()), ErrNotFound)
}

// swept is tested through Reanchor rather than directly: what matters is that
// the pass that repositions threads is the pass that drops them.
func sweepDiff(t *testing.T, body string) *gitdiff.Diff {
	t.Helper()
	diff := parseDiff(t, body)
	diff.Mode = gitdiff.ModeWorktree
	return diff
}

// aGo has "var x = 2" on the new side, which newThread's anchor matches.
const aGo = `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1,3 +1,3 @@
 package main
 
-var x = 1
+var x = 2`

// aGoRewritten still touches a.go, but the anchored line has gone.
const aGoRewritten = `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1,3 +1,3 @@
 package main
 
-var x = 1
+var y = 9`

// anchoredThread is pinned by content, as a real one is: a thread with no
// anchor never matches anything and would orphan on the first pass.
func anchoredThread(t *testing.T, s *Store) Thread {
	t.Helper()
	anchor, ok := Locate(sweepDiff(t, aGo), "a.go", SideRight, 3)
	require.True(t, ok, "the fixture diff has no line 3 to pin to")
	th, err := s.Add(Thread{
		File: "a.go", Side: SideRight, Line: 3, Anchor: anchor,
		Comments: []Comment{{Body: "about the line"}},
	})
	require.NoError(t, err)
	return th
}

func orphanedThread(t *testing.T, s *Store, ago time.Duration) Thread {
	t.Helper()
	th := anchoredThread(t, s)
	s.Reanchor(sweepDiff(t, aGoRewritten))

	// Wind the clock back on the record itself: the sweep reads what is stored.
	s.mu.Lock()
	when := time.Now().Add(-ago)
	s.find(th.ID).OrphanedAt = &when
	s.mu.Unlock()
	return th
}

func TestAnOrphanGoesOnceItHasBeenGoneLongEnough(t *testing.T) {
	s, _ := newStore(t)
	th := orphanedThread(t, s, orphanGrace+time.Minute)

	gone := s.Reanchor(sweepDiff(t, aGoRewritten))

	_, err := s.Get(th.ID)
	assert.ErrorIs(t, err, ErrNotFound, "the code went and the comment stayed")
	require.Len(t, gone, 1, "nothing said what was deleted")
	assert.Equal(t, th.ID, gone[0].ID)
}

// A narrower scope shows fewer lines of the same file. The line is still in the
// branch, so an orphan it caused is the view talking, not a deletion.
func TestANarrowerScopeNeverCostsAComment(t *testing.T) {
	for _, mode := range []string{gitdiff.ModeUncommitted, gitdiff.ModeCommitted, gitdiff.ModePR} {
		t.Run(mode, func(t *testing.T) {
			s, _ := newStore(t)
			th := orphanedThread(t, s, orphanGrace*24)

			narrow := sweepDiff(t, aGoRewritten)
			narrow.Mode = mode
			assert.Empty(t, s.Reanchor(narrow))

			got, err := s.Get(th.ID)
			require.NoError(t, err)
			assert.True(t, got.Orphaned, "still orphaned, and still there")
		})
	}
}

// The grace is meant to be time the user had to notice. A closed session gives
// none of it, so the clock starts again when the store is opened.
func TestTheOrphanClockStartsAgainOnOpen(t *testing.T) {
	s, gitDir := newStore(t)
	th := orphanedThread(t, s, orphanGrace*24)

	// The wound-back stamp has to reach the file: that is what Open reads.
	s.mu.Lock()
	require.NoError(t, s.save())
	s.mu.Unlock()

	again, err := Open(gitDir, "feature/thing")
	require.NoError(t, err)
	assert.Empty(t, again.Reanchor(sweepDiff(t, aGoRewritten)))

	got, err := again.Get(th.ID)
	require.NoError(t, err)
	assert.True(t, got.Orphaned, "the hour passed with nothing running")
}

func TestAFreshOrphanIsKeptAndTimed(t *testing.T) {
	s, _ := newStore(t)
	th := anchoredThread(t, s)
	s.Reanchor(sweepDiff(t, aGoRewritten))

	got, err := s.Get(th.ID)
	require.NoError(t, err)
	assert.True(t, got.Orphaned)
	require.NotNil(t, got.OrphanedAt, "nothing says when it went")
	assert.WithinDuration(t, time.Now(), *got.OrphanedAt, time.Minute)
}

// The dangerous case: a file that left the diff is a change of scope or base,
// not a deletion, and sweeping on it would clear a branch's review at a click.
func TestAFileThatLeftTheDiffNeverCostsAComment(t *testing.T) {
	s, _ := newStore(t)
	th := orphanedThread(t, s, orphanGrace*24)

	s.Reanchor(sweepDiff(t, `diff --git a/other.go b/other.go
index 1..2 100644
--- a/other.go
+++ b/other.go
@@ -1 +1 @@
-a
+b`))

	got, err := s.Get(th.ID)
	require.NoError(t, err)
	assert.True(t, got.Orphaned, "still orphaned, and still there")
}

func TestTheClockResetsWhenTheLineComesBack(t *testing.T) {
	s, _ := newStore(t)
	th := orphanedThread(t, s, orphanGrace*2)

	s.Reanchor(sweepDiff(t, aGo))

	got, err := s.Get(th.ID)
	require.NoError(t, err)
	assert.False(t, got.Orphaned)
	assert.Nil(t, got.OrphanedAt, "an undone edit must not leave the clock running")
}

// Sent and imported threads are exempt from Reset for the same reasons: one
// lives on GitHub already, the other would simply be imported again.
func TestASentOrImportedOrphanIsNeverSwept(t *testing.T) {
	for _, tc := range []struct {
		name string
		mark func(*Store, string)
	}{
		{"sent", func(s *Store, id string) { require.NoError(t, s.MarkSent([]string{id}, time.Now())) }},
		{"imported", func(s *Store, id string) {
			_, err := s.SetExternal(id, "gh-7")
			require.NoError(t, err)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newStore(t)
			th := orphanedThread(t, s, orphanGrace*24)
			tc.mark(s, th.ID)

			s.Reanchor(sweepDiff(t, aGoRewritten))

			_, err := s.Get(th.ID)
			assert.NoError(t, err)
		})
	}
}

// A pending review is visible only to its author, so a comment waiting in one
// has not been said to anybody yet and must not count as sent.
func TestACommentInADraftIsNotSent(t *testing.T) {
	s, _ := newStore(t)
	one := addThread(t, s, 3, "the finding")
	_, err := s.SetOutgoing(one.ID, "the words")
	require.NoError(t, err)
	require.NoError(t, s.MarkDrafted(one.ID, "REVIEW1", "THREAD1"))

	got, err2 := s.Get(one.ID)
	require.NoError(t, err2)
	assert.True(t, got.Drafted())
	assert.False(t, got.Sent())
	assert.Equal(t, "REVIEW1", got.DraftReview)

	assert.Len(t, s.InDraft("REVIEW1"), 1)
	assert.Empty(t, s.InDraft("REVIEW2"))

	require.NoError(t, s.MarkSent([]string{one.ID}, time.Now()))
	got, err2 = s.Get(one.ID)
	require.NoError(t, err2)
	assert.True(t, got.Sent())
	assert.False(t, got.Drafted(), "submitted, so no longer waiting in a draft")
	assert.Empty(t, s.InDraft(""))
}

// The draft is a real review: it can be submitted or deleted from the GitHub
// UI between one press and the next, and then it is not ours to add to.
func TestADraftThatWentCanBeForgotten(t *testing.T) {
	s, _ := newStore(t)
	one := addThread(t, s, 3, "one")
	two := addThread(t, s, 20, "two")
	require.NoError(t, s.MarkDrafted(one.ID, "GONE", "T1"))
	require.NoError(t, s.MarkDrafted(two.ID, "GONE", "T2"))

	require.NoError(t, s.ForgetDraft(one.ID, ""))
	assert.Len(t, s.InDraft(""), 1, "one taken back out, the other still waiting")

	require.NoError(t, s.ForgetDraft("", "GONE"))
	assert.Empty(t, s.InDraft(""))
}

// Sendable is what may be passed on to the author. A claim the reviewers
// themselves took back is not that, however well it was written up.
func TestWhatTheReviewersTookBackIsNotSendable(t *testing.T) {
	s, _ := newStore(t)
	kept := addThread(t, s, 3, "stands")
	gone := addThread(t, s, 30, "withdrawn")
	same := addThread(t, s, 60, "a repeat")

	_, err := s.SetVerdict(gone.ID, Verdict{Withdrawn: true})
	require.NoError(t, err)
	_, err = s.SetVerdict(same.ID, Verdict{DuplicateOf: kept.ID})
	require.NoError(t, err)

	ids := []string{}
	for _, t := range s.Sendable() {
		ids = append(ids, t.ID)
	}
	assert.Equal(t, []string{kept.ID}, ids)
}
