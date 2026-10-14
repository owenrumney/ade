package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := Path(filepath.Join(t.TempDir(), ".git"))
	store, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

func TestAConversationSurvivesBeingReopened(t *testing.T) {
	store, path := openStore(t)

	_, err := store.Append("sess-1", Message{Kind: KindYou, Body: "add LSP support"})
	require.NoError(t, err)
	_, err = store.Append("sess-1", Message{Kind: KindAgent, Body: "done", HTML: "<p>done</p>"})
	require.NoError(t, err)
	require.NoError(t, store.Close())

	// The design document and the review comments already survived a restart.
	// The talking that produced them is worth as much.
	again, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = again.Close() }()

	got := again.List("sess-1")
	require.Len(t, got, 2)
	assert.Equal(t, "add LSP support", got[0].Body)
	assert.Equal(t, KindAgent, got[1].Kind)
	assert.Equal(t, "<p>done</p>", got[1].HTML)
	assert.False(t, got[0].At.IsZero(), "a message is stamped on the way in")
}

func TestTheFileIsPrivate(t *testing.T) {
	store, path := openStore(t)
	_, err := store.Append("sess-1", Message{Kind: KindYou, Body: "something private"})
	require.NoError(t, err)

	// A conversation about unreleased work is nobody else's on a shared box.
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestEachSessionIsItsOwnConversation(t *testing.T) {
	store, _ := openStore(t)
	_, err := store.Append("sess-1", Message{Kind: KindYou, Body: "one"})
	require.NoError(t, err)
	_, err = store.Append("sess-2", Message{Kind: KindYou, Body: "two"})
	require.NoError(t, err)

	assert.Equal(t, 1, store.Len("sess-1"))
	assert.Equal(t, 1, store.Len("sess-2"))
	assert.Zero(t, store.Len("sess-3"))
	assert.ElementsMatch(t, []string{"sess-1", "sess-2"}, store.Sessions())
}

func TestMessagesComeBackInTheOrderTheyWereSaid(t *testing.T) {
	store, _ := openStore(t)

	// Past the width of a single digit, so a lexical order would misfile them.
	for i := range 25 {
		_, err := store.Append("sess-1", Message{Kind: KindYou, Body: fmt.Sprintf("%d", i)})
		require.NoError(t, err)
	}
	got := store.List("sess-1")
	require.Len(t, got, 25)
	for i, msg := range got {
		assert.Equal(t, fmt.Sprintf("%d", i), msg.Body)
	}
}

func TestAToolCallCanBeUpdatedWithItsResult(t *testing.T) {
	store, _ := openStore(t)
	call, err := store.Append("sess-1", Message{Kind: KindTool, Tool: "Bash", ToolID: "toolu_1", Input: `{"command":"go test"}`})
	require.NoError(t, err)

	call.Output = "ok"
	require.NoError(t, store.Update("sess-1", call))

	got := store.List("sess-1")
	require.Len(t, got, 1)
	assert.Equal(t, "ok", got[0].Output)
	assert.Equal(t, call.ID, got[0].ID, "an update keeps its place")

	// Updating what is not there is a mistake worth hearing about.
	assert.Error(t, store.Update("sess-1", Message{ID: "nope"}))
	assert.Error(t, store.Update("sess-9", call))
}

func TestAToolResultIsBounded(t *testing.T) {
	store, _ := openStore(t)
	huge := strings.Repeat("x", maxOutput*3)
	call, err := store.Append("sess-1", Message{Kind: KindTool, Output: huge})
	require.NoError(t, err)

	// A Bash call can print megabytes. The folded row exists to be opened
	// occasionally, not to carry all of it.
	assert.Less(t, len(call.Output), maxOutput+64)
	assert.Contains(t, call.Output, "truncated")
}

func TestAMessageCanBeTakenBackOut(t *testing.T) {
	store, _ := openStore(t)
	first, err := store.Append("sess-1", Message{Kind: KindYou, Body: "keep this"})
	require.NoError(t, err)
	second, err := store.Append("sess-1", Message{Kind: KindYou, Body: "drop this"})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID, "two turns must not share an id")

	require.NoError(t, store.Remove("sess-1", second.ID))

	got := store.List("sess-1")
	require.Len(t, got, 1)
	assert.Equal(t, "keep this", got[0].Body)

	// Removing what is not there, or nothing at all, is not an error.
	assert.NoError(t, store.Remove("sess-1", second.ID))
	assert.NoError(t, store.Remove("sess-1", ""))
	assert.NoError(t, store.Remove("sess-9", first.ID))
	assert.Equal(t, 1, store.Len("sess-1"))
}

func TestAFreshAgentsConversationFollowsItOnceItIsNamed(t *testing.T) {
	store, _ := openStore(t)

	// What you said to start a fresh session is written before the session has
	// an id, and has to end up under the id once the agent reports one.
	_, err := store.Append(Pending, Message{Kind: KindYou, Body: "start here"})
	require.NoError(t, err)
	_, err = store.Append(Pending, Message{Kind: KindNote, Body: "starting"})
	require.NoError(t, err)

	require.NoError(t, store.Rename(Pending, "sess-new"))
	assert.Zero(t, store.Len(Pending))

	got := store.List("sess-new")
	require.Len(t, got, 2)
	assert.Equal(t, "start here", got[0].Body)
	assert.Equal(t, "starting", got[1].Body)
	assert.NotContains(t, store.Sessions(), Pending)

	// Renaming what is not there is nothing; renaming onto itself is nothing.
	assert.NoError(t, store.Rename("sess-absent", "sess-new"))
	assert.NoError(t, store.Rename("sess-new", "sess-new"))
	assert.Equal(t, 2, store.Len("sess-new"))
}

func TestAForkedSessionKeepsWhatWasSaidBeforeIt(t *testing.T) {
	store, _ := openStore(t)
	_, err := store.Append("sess-old", Message{Kind: KindYou, Body: "before the fork"})
	require.NoError(t, err)
	_, err = store.Append("sess-new", Message{Kind: KindNote, Body: "already here"})
	require.NoError(t, err)

	// Resuming can fork to a new id. The conversation so far goes with it,
	// after anything the new id already holds, and nothing is lost.
	require.NoError(t, store.Rename("sess-old", "sess-new"))
	got := store.List("sess-new")
	require.Len(t, got, 2)
	assert.Equal(t, "already here", got[0].Body)
	assert.Equal(t, "before the fork", got[1].Body)
	assert.Less(t, got[0].ID, got[1].ID, "ids keep the order")
	assert.Zero(t, store.Len("sess-old"))
}

func TestClearForgetsAConversation(t *testing.T) {
	store, _ := openStore(t)
	_, err := store.Append("sess-1", Message{Kind: KindYou, Body: "hello"})
	require.NoError(t, err)

	require.NoError(t, store.Clear("sess-1"))
	assert.Zero(t, store.Len("sess-1"))
	assert.NoError(t, store.Clear("sess-1"), "clearing an empty conversation is not an error")
}

func TestANilStoreRemembersNothingAndPanicsNever(t *testing.T) {
	var store *Store
	assert.Empty(t, store.List("sess-1"))
	assert.Zero(t, store.Len("sess-1"))
	assert.Empty(t, store.Sessions())
	assert.Empty(t, store.Path())
	assert.NoError(t, store.Remove("sess-1", "x"))
	assert.NoError(t, store.Rename("a", "b"))
	assert.NoError(t, store.Clear("a"))
	assert.NoError(t, store.Close())
	_, err := store.Append("sess-1", Message{})
	assert.Error(t, err, "a write to nowhere has to say so")
}

func TestAppendIsSafeFromSeveralGoroutines(t *testing.T) {
	store, _ := openStore(t)

	done := make(chan struct{})
	for range 8 {
		go func() {
			defer func() { done <- struct{}{} }()
			for range 5 {
				_, _ = store.Append("sess-1", Message{Kind: KindYou, Body: "concurrent", At: time.Now()})
			}
		}()
	}
	for range 8 {
		<-done
	}
	assert.Equal(t, 40, store.Len("sess-1"))
}

func TestPathIsUnderTheGitDir(t *testing.T) {
	assert.Equal(t, filepath.Join("/repo/.git", "ade", "chat.db"), Path("/repo/.git"))
}

func TestASecondOpenOnTheSameFileFailsFastRatherThanHanging(t *testing.T) {
	store, path := openStore(t)
	_ = store

	// bbolt takes an exclusive lock. Waiting forever on it would make a server
	// that never starts and never says why.
	start := time.Now()
	_, err := Open(path)
	require.Error(t, err)
	assert.Less(t, time.Since(start), openTimeout*3)
}

func TestAFoldedPromptStaysMarkedAcrossAReopen(t *testing.T) {
	store, path := openStore(t)
	first, err := store.Append("sess-1", Message{Kind: KindYou, Body: "fix the test"})
	require.NoError(t, err)
	second, err := store.Append("sess-1", Message{Kind: KindYou, Body: "and the fixture"})
	require.NoError(t, err)
	second.FoldedInto = first.ID
	require.NoError(t, store.Update("sess-1", second))
	require.NoError(t, store.Close())

	again, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = again.Close() }()

	got := again.List("sess-1")
	require.Len(t, got, 2)
	assert.Empty(t, got[0].FoldedInto)
	assert.Equal(t, first.ID, got[1].FoldedInto)
}
