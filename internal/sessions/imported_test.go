package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

// plant writes a transcript for a session in cwd and backdates it, since
// LastUsed is the file's own mtime rather than anything written inside it.
func plant(t *testing.T, home, cwd, id string, when time.Time) {
	t.Helper()
	slug := "-" + filepath.Base(filepath.Dir(cwd)) + "-" + filepath.Base(cwd)
	project := filepath.Join(home, ".claude", "projects", slug)
	require.NoError(t, os.MkdirAll(project, 0o750))
	path := filepath.Join(project, id+".jsonl")
	at := when.UTC().Format(time.RFC3339)
	body := fmt.Sprintf(
		`{"type":"user","timestamp":%q,"cwd":%q,"message":{"role":"user","content":"ship the thing"}}`+"\n"+
			`{"type":"assistant","timestamp":%q,"requestId":"r1","message":{"id":"m1","role":"assistant","model":"claude-opus-5","content":[],"usage":{"input_tokens":10,"output_tokens":2}}}`+"\n",
		at, cwd, at)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	require.NoError(t, os.Chtimes(path, when, when))
}

func listed(in []Session) map[string]Session {
	out := make(map[string]Session, len(in))
	for _, one := range in {
		out[one.ID] = one
	}
	return out
}

func TestAPinnedSessionOutlivesTheAgeWindow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()
	old := time.Now().Add(-90 * 24 * time.Hour)
	plant(t, home, work, "11111111-1111-1111-1111-111111111111", old)

	assert.NotContains(t, listed(List("")), "11111111-1111-1111-1111-111111111111",
		"older than the window and not pinned")

	require.NoError(t, Pin("11111111-1111-1111-1111-111111111111", work))
	found, ok := listed(List(""))["11111111-1111-1111-1111-111111111111"]
	require.True(t, ok, "a pin is the way back to one the window drops")
	assert.True(t, found.Pinned)
	assert.Equal(t, work, found.Dir)
	assert.Empty(t, found.From, "listed where it was recorded")

	// Unpinning hands it back to the window.
	require.NoError(t, Unpin("11111111-1111-1111-1111-111111111111"))
	assert.NotContains(t, listed(List("")), "11111111-1111-1111-1111-111111111111")
}

func TestAPinnedSessionDoesNotSpendTheRowCap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()
	now := time.Now()
	recent := "22222222-2222-2222-2222-222222222222"
	older := "33333333-3333-3333-3333-333333333333"
	plant(t, home, work, recent, now.Add(-time.Hour))
	plant(t, home, work, older, now.Add(-48*time.Hour))

	one := Window{Max: 1, Age: age}
	assert.Len(t, ListIn("", one), 1, "the cap holds without a pin")

	require.NoError(t, Pin(older, work))
	t.Cleanup(func() { _ = Unpin(older) })

	got := listed(ListIn("", one))
	assert.Contains(t, got, recent, "the cap still admits the newest")
	assert.Contains(t, got, older, "and the pin is found past it")
	assert.Len(t, got, 2)
}

func TestAnAdoptedSessionListsUnderTheCheckoutThatTookIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	recorded := t.TempDir()
	adopter := t.TempDir()
	id := "44444444-4444-4444-4444-444444444444"
	plant(t, home, recorded, id, time.Now().Add(-time.Hour))

	require.NoError(t, Pin(id, adopter))
	t.Cleanup(func() { _ = Unpin(id) })

	found, ok := listed(List(""))[id]
	require.True(t, ok)
	assert.Equal(t, adopter, found.Dir, "opened where it was adopted")
	assert.Equal(t, filepath.Base(adopter), found.Name)
	assert.Equal(t, recorded, found.From, "and says where it came from")

	// Here follows the adopting checkout, not the recorded one.
	assert.True(t, listed(List(adopter))[id].Here)
	assert.False(t, listed(List(recorded))[id].Here)
}

func TestAdoptingSurvivesTheRecordedDirectoryGoing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	recorded := t.TempDir()
	adopter := t.TempDir()
	id := "55555555-5555-5555-5555-555555555555"
	plant(t, home, recorded, id, time.Now().Add(-time.Hour))
	require.NoError(t, os.RemoveAll(recorded))

	// Unpinned it is dropped, because there is nowhere to start a server.
	assert.NotContains(t, listed(List("")), id)

	require.NoError(t, Pin(id, adopter))
	t.Cleanup(func() { _ = Unpin(id) })
	assert.Contains(t, listed(List("")), id, "the adopting checkout is somewhere to open")
}

func TestResolveRefusesWhatIsNotASession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()

	for _, id := range []string{"", "   ", "not-a-uuid", "../../etc/passwd", "*"} {
		_, err := Resolve(id, "")
		assert.ErrorIsf(t, err, ErrNotAnID, "accepted %q", id)
	}

	_, err := Resolve("66666666-6666-6666-6666-666666666666", "")
	assert.ErrorIs(t, err, ErrNoSuchID)

	plant(t, home, work, "77777777-7777-7777-7777-777777777777", time.Now())
	found, err := Resolve("77777777-7777-7777-7777-777777777777", "")
	require.NoError(t, err)
	assert.Equal(t, work, found.Dir)
	assert.True(t, found.Pinned)
	assert.True(t, found.Resumable)
}

func TestResolveFindsATranscriptRecordedElsewhere(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	recorded := t.TempDir()
	adopter := t.TempDir()
	id := "88888888-8888-8888-8888-888888888888"
	plant(t, home, recorded, id, time.Now())

	found, err := Resolve(id, adopter)
	require.NoError(t, err)
	assert.Equal(t, adopter, found.Dir)
	assert.Equal(t, recorded, found.From)

	// Nothing was written: resolving is a read, and the pin is a separate act.
	assert.Empty(t, settings.Imported()[id])
}

func TestResolveRefusesADirectoryThatIsGone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	recorded := t.TempDir()
	id := "99999999-9999-9999-9999-999999999999"
	plant(t, home, recorded, id, time.Now())
	require.NoError(t, os.RemoveAll(recorded))

	_, err := Resolve(id, "")
	assert.ErrorIs(t, err, ErrGone, "nowhere to start a server")
}
