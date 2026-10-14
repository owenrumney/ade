package memory

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/owenrumney/ade/internal/transcripts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checkout is a git repository with origin set to remote.
func checkout(t *testing.T, dir, remote string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	for _, args := range [][]string{
		{"init", "-b", "main", "-q"},
		{"remote", "add", "origin", remote},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	return dir
}

// transcript writes a claude JSONL with one user prompt per line of text.
func transcript(t *testing.T, dir, id, cwd string, prompts ...string) *transcripts.Session {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	path := filepath.Join(dir, id+".jsonl")

	var b strings.Builder
	for i, text := range prompts {
		at := time.Now().Add(-time.Duration(len(prompts)-i) * time.Minute).Format(time.RFC3339)
		fmt.Fprintf(&b, `{"type":"user","timestamp":%q,"message":{"role":"user","content":%q}}`+"\n", at, text)
	}
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))

	return &transcripts.Session{
		Agent: transcripts.AgentClaude, ID: id, Path: path, CWD: cwd,
		LastUsed: time.Now(), Title: "session " + id,
	}
}

func TestATranscriptResolvesToTheProjectItRanIn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	main := checkout(t, filepath.Join(home, "code", "ade"), "git@github.com:owenrumney/ade.git")
	other := checkout(t, filepath.Join(home, "code", "other"), "git@github.com:owenrumney/other.git")

	mine := transcript(t, filepath.Join(home, "t"), "a", main, "how do we run the tests")
	theirs := transcript(t, filepath.Join(home, "t"), "b", other, "unrelated")

	id := Identity{Slug: "github.com-owenrumney-ade", Remote: "github.com/owenrumney/ade"}
	corpus := Corpus(t.Context(), id, []*transcripts.Session{mine, theirs})
	require.Len(t, corpus, 1)
	assert.Equal(t, "a", corpus[0].ID)
}

func TestATranscriptFromARemovedWorktreeIsStillOurs(t *testing.T) {
	// The whole reason the ledger exists: the directory is gone, so nothing
	// can be asked of git.
	home := t.TempDir()
	t.Setenv("HOME", home)
	id := Identity{Slug: "github.com-owenrumney-ade", Remote: "github.com/owenrumney/ade"}
	gone := filepath.Join(home, "code", "ade-fix-623")
	require.NoError(t, Record(gone, id))

	session := transcript(t, filepath.Join(home, "t"), "a", gone, "the fix for 623")
	corpus := Corpus(t.Context(), id, []*transcripts.Session{session})
	require.Len(t, corpus, 1)
	assert.Equal(t, "a", corpus[0].ID)
}

func TestResolvingALiveCheckoutWritesItDownForLater(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	main := checkout(t, filepath.Join(home, "code", "ade"), "git@github.com:owenrumney/ade.git")

	id := Identity{Slug: "github.com-owenrumney-ade", Remote: "github.com/owenrumney/ade"}
	session := transcript(t, filepath.Join(home, "t"), "a", main, "anything")
	Corpus(t.Context(), id, []*transcripts.Session{session})

	assert.Equal(t, id.Slug, Ledger()[main], "so it survives the checkout being removed")
}

func TestASiblingNamedLikeAWorktreeIsOfferedNotAdopted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	id := Identity{Slug: "github.com-owenrumney-ade", Remote: "github.com/owenrumney/ade"}
	code := filepath.Join(home, "code")
	require.NoError(t, Record(filepath.Join(code, "ade"), id))

	looksRight := transcript(t, filepath.Join(home, "t"), "a", filepath.Join(code, "ade-fix-623"), "x")
	elsewhere := transcript(t, filepath.Join(home, "t"), "b", filepath.Join(home, "elsewhere", "ade-fix-1"), "x")
	unrelated := transcript(t, filepath.Join(home, "t"), "c", filepath.Join(code, "coast"), "x")
	sessions := []*transcripts.Session{looksRight, elsewhere, unrelated}

	assert.Equal(t, []string{filepath.Join(code, "ade-fix-623")}, Siblings(id, sessions))
	assert.Empty(t, Corpus(t.Context(), id, sessions), "and none of them is in the corpus")
}

func TestOutstandingIsWhatHasNotBeenReadYet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s := remembered(t)

	id := s.Identity()
	dir := filepath.Join(home, "code", "ade")
	require.NoError(t, Record(dir, id))
	session := transcript(t, filepath.Join(home, "t"), "a", dir, "how do we run the tests")

	out := s.Outstanding(t.Context(), []*transcripts.Session{session})
	require.Len(t, out, 1)
	assert.Zero(t, out[0].From)
	assert.Positive(t, out[0].Bytes())

	require.NoError(t, s.MarkRead(out...))
	assert.Empty(t, s.Outstanding(t.Context(), []*transcripts.Session{session}), "read, so nothing outstanding")
}

func TestATranscriptThatGrowsIsReadOnlyFromWhereItStopped(t *testing.T) {
	// "The session ended" is a guess. The watermark is a size so guessing
	// early costs one extra small pass rather than losing the rest.
	home := t.TempDir()
	t.Setenv("HOME", home)
	s := remembered(t)

	dir := filepath.Join(home, "code", "ade")
	require.NoError(t, Record(dir, s.Identity()))
	session := transcript(t, filepath.Join(home, "t"), "a", dir, "first question")

	first := s.Outstanding(t.Context(), []*transcripts.Session{session})
	require.Len(t, first, 1)
	require.NoError(t, s.MarkRead(first...))

	grown := transcript(t, filepath.Join(home, "t"), "a", dir, "first question", "and a second")
	again := s.Outstanding(t.Context(), []*transcripts.Session{grown})
	require.Len(t, again, 1)
	assert.Equal(t, first[0].Size, again[0].From, "resumes from the recorded size")
	assert.Positive(t, again[0].Bytes())
}

func TestAKnownCheckoutIsAnsweredWithoutGit(t *testing.T) {
	// Every repository list asks this, so it must not shell out for a checkout
	// ade has already placed. The directory does not even exist here.
	home := t.TempDir()
	t.Setenv("HOME", home)
	id := Identity{Slug: "github.com-owenrumney-ade", Remote: "github.com/owenrumney/ade"}
	path := filepath.Join(home, "code", "ade")
	require.NoError(t, Record(path, id))

	found, ok := Lookup(path)
	require.True(t, ok)
	assert.Equal(t, id.Slug, found.Slug)

	_, ok = Lookup(filepath.Join(home, "code", "never-seen"))
	assert.False(t, ok, "an unseen checkout still has to be resolved")
}

func TestAStoreOpenedBySlugStillKnowsItsRemote(t *testing.T) {
	// Lookup answers with a slug and nothing else, so project.json has to
	// carry the rest or the launcher would show a project with no name.
	home := t.TempDir()
	t.Setenv("HOME", home)
	full := remembered(t)

	bare, err := Open(Identity{Slug: full.Identity().Slug})
	require.NoError(t, err)
	assert.True(t, bare.Remembered())
	assert.Equal(t, "github.com/owenrumney/ade", bare.Project().Remote)
}
