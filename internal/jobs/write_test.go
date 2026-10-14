package jobs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestASavedJobIsOneTheLoaderReadsBack(t *testing.T) {
	home := t.TempDir()
	saved, err := Save(home, Draft{
		Name:        "morning-check",
		Description: "What broke overnight",
		Schedule:    "0 8 * * 1-5",
		Continuity:  Summary,
		Prompt:      "/catchup\n",
	}, Create)
	require.NoError(t, err)
	assert.Equal(t, ScopeUser, saved.Scope)

	list := Load("", home)
	require.Len(t, list, 1)
	assert.Equal(t, "morning-check", list[0].Name)
	assert.Equal(t, "What broke overnight", list[0].Description)
	assert.Equal(t, "0 8 * * 1-5", list[0].Schedule)
	assert.Equal(t, Summary, list[0].Continuity)
	assert.Equal(t, Reads, list[0].Allow)
	assert.Equal(t, "/catchup", list[0].Prompt)
}

func TestSaveDefaultsTheFieldsAFormMayNotAsk(t *testing.T) {
	home := t.TempDir()
	saved, err := Save(home, Draft{Name: "nightly", Schedule: "@every 2h", Prompt: "/catchup"}, Create)
	require.NoError(t, err)
	assert.Equal(t, Fresh, saved.Continuity)
	assert.Equal(t, Reads, saved.Allow)
}

func TestSaveRefusesWhatTheLoaderWouldSkip(t *testing.T) {
	home := t.TempDir()
	for _, tc := range []struct {
		name  string
		draft Draft
		says  string
	}{
		{"no name", Draft{Schedule: "@every 1h", Prompt: "/catchup"}, "job name"},
		{"a path for a name", Draft{Name: "../evil", Schedule: "@every 1h", Prompt: "/x"}, "job name"},
		{"no schedule", Draft{Name: "nightly", Prompt: "/catchup"}, "no schedule"},
		{"a schedule nobody parses", Draft{Name: "nightly", Schedule: "0 8 * *", Prompt: "/x"}, "cron takes 5"},
		{"no prompt", Draft{Name: "nightly", Schedule: "@every 1h"}, "no prompt"},
		{"an interval under the floor", Draft{Name: "nightly", Schedule: "@every 10s", Prompt: "/x"}, "floor"},
		{"a project that is not a remote", Draft{
			Name: "nightly", Schedule: "@every 1h", Project: "ade", Prompt: "/x",
		}, "not a remote"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Save(home, tc.draft, Create)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.says)
		})
	}
	entries, err := os.ReadDir(userDir(home))
	require.True(t, err != nil || len(entries) == 0, "a refused draft wrote a file")
}

// A new job landing on a name already taken destroyed the old definition and
// inherited its history, with no confirm anywhere.
func TestCreatingAJobWillNotLandOnOneThatIsThere(t *testing.T) {
	home := t.TempDir()
	_, err := Save(home, Draft{Name: "nightly", Schedule: "@every 1h", Prompt: "/catchup"}, Create)
	require.NoError(t, err)

	_, err = Save(home, Draft{Name: "nightly", Schedule: "0 2 * * *", Prompt: "/review"}, Create)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already a job called nightly")

	list := Load("", home)
	require.Len(t, list, 1)
	assert.Equal(t, "@every 1h", list[0].Schedule, "the old definition survived")
	assert.Equal(t, "/catchup", list[0].Prompt)
}

func TestReplacingAJobNeedsOneToReplace(t *testing.T) {
	home := t.TempDir()
	_, err := Save(home, Draft{Name: "nightly", Schedule: "@every 1h", Prompt: "/catchup"}, Replace)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no job called nightly")
	assert.Empty(t, Load("", home))

	_, err = Save(home, Draft{Name: "nightly", Schedule: "@every 1h", Prompt: "/catchup"}, Create)
	require.NoError(t, err)
	_, err = Save(home, Draft{Name: "nightly", Schedule: "0 2 * * *", Prompt: "/review"}, Replace)
	require.NoError(t, err)

	list := Load("", home)
	require.Len(t, list, 1)
	assert.Equal(t, "0 2 * * *", list[0].Schedule)
	assert.Equal(t, "/review", list[0].Prompt)
}

func TestSaveRefusesAModeItDoesNotKnow(t *testing.T) {
	home := t.TempDir()
	_, err := Save(home, Draft{Name: "nightly", Schedule: "@every 1h", Prompt: "/x"}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown save mode")
}

func TestSaveRefusesAPromptNobodyCouldHaveMeant(t *testing.T) {
	home := t.TempDir()
	_, err := Save(home, Draft{
		Name: "nightly", Schedule: "@every 1h", Prompt: strings.Repeat("x", maxPrompt+1),
	}, Create)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at most")
	assert.Empty(t, Load("", home))
}

// StatePath keys state by project, so an edited project used to leave the
// history behind and hand the job a fresh, unpaused one.
func TestEditingTheProjectTakesTheHistoryWithIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	job, err := Save(home, Draft{
		Name: "nightly", Schedule: "@every 1h", Prompt: "/catchup",
		Project: "github.com/o/a",
	}, Create)
	require.NoError(t, err)
	_, err = SetPaused(job, true, time.Now())
	require.NoError(t, err)
	oldPath, err := StatePath(job)
	require.NoError(t, err)
	require.FileExists(t, oldPath)

	moved, err := Save(home, Draft{
		Name: "nightly", Schedule: "@every 1h", Prompt: "/catchup",
		Project: "github.com/o/b",
	}, Replace)
	require.NoError(t, err)

	newPath, err := StatePath(moved)
	require.NoError(t, err)
	assert.NotEqual(t, oldPath, newPath)
	assert.NoFileExists(t, oldPath, "the old project kept an orphan")
	assert.True(t, ReadState(moved).Paused, "a paused job came back unpaused")
}

func TestEditingTheProjectWillNotStandOnAnotherHistory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	there, err := Save(home, Draft{
		Name: "nightly", Schedule: "@every 1h", Prompt: "/b", Project: "github.com/o/b",
	}, Create)
	require.NoError(t, err)
	_, err = SetPaused(there, true, time.Now())
	require.NoError(t, err)
	require.NoError(t, Remove(home, "nightly"))

	// Put b's history back without its definition, the way a deleted job that
	// was mid-fire would.
	require.NoError(t, WriteState(there, State{Name: "nightly", Paused: true}))

	job, err := Save(home, Draft{
		Name: "nightly", Schedule: "@every 1h", Prompt: "/a", Project: "github.com/o/a",
	}, Create)
	require.NoError(t, err)
	_, err = SetPaused(job, true, time.Now())
	require.NoError(t, err)

	_, err = Save(home, Draft{
		Name: "nightly", Schedule: "@every 1h", Prompt: "/a", Project: "github.com/o/b",
	}, Replace)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "its own history")
}

// The frontmatter names the state file, so a file whose two names disagree
// would read and write another job's history.
func TestSaveRefusesADefinitionWhoseNamesDisagree(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(userDir(home), 0o750))
	// Written by hand: name: nightly in a file called old.md.
	require.NoError(t, os.WriteFile(filepath.Join(userDir(home), "old.md"),
		[]byte("---\nname: nightly\nschedule: \"@every 1h\"\n---\n\n/catchup\n"), 0o600))

	_, err := Save(home, Draft{Name: "old", Schedule: "@every 1h", Prompt: "/x"}, Replace)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "holds a job called nightly")
}

// Remove used to take the state of the frontmatter's name, which can be
// another job's.
func TestRemoveTakesOnlyItsOwnHistory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mine, err := Save(home, Draft{Name: "nightly", Schedule: "@every 1h", Prompt: "/catchup"}, Create)
	require.NoError(t, err)
	_, err = SetPaused(mine, true, time.Now())
	require.NoError(t, err)
	mineState, err := StatePath(mine)
	require.NoError(t, err)
	require.FileExists(t, mineState)

	// A hand-written old.md that claims to be nightly.
	require.NoError(t, os.WriteFile(filepath.Join(userDir(home), "old.md"),
		[]byte("---\nname: nightly\nschedule: \"@every 1h\"\n---\n\n/catchup\n"), 0o600))

	require.NoError(t, Remove(home, "old"))
	assert.FileExists(t, mineState, "removing old.md took nightly's history")
}

func TestRemoveTakesTheStateWithTheJob(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	job, err := Save(home, Draft{Name: "nightly", Schedule: "@every 1h", Prompt: "/catchup"}, Create)
	require.NoError(t, err)
	_, err = SetPaused(job, true, time.Now())
	require.NoError(t, err)
	state, err := StatePath(job)
	require.NoError(t, err)
	require.FileExists(t, state)

	require.NoError(t, Remove(home, "nightly"))
	assert.Empty(t, Load("", home))
	assert.NoFileExists(t, state)
}

func TestRemoveWillNotBeTalkedOutOfTheJobsDirectory(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(userDir(home), 0o750))
	outside := filepath.Join(home, "keep.md")
	require.NoError(t, os.WriteFile(outside, []byte("mine"), 0o600))

	require.Error(t, Remove(home, "../keep"))
	assert.FileExists(t, outside)
}

// A description with a colon or a newline in it must not become frontmatter
// the parser reads as another field, or prompt.
func TestRenderKeepsAFieldOnOneLine(t *testing.T) {
	home := t.TempDir()
	saved, err := Save(home, Draft{
		Name:        "nightly",
		Description: "check: prod\nschedule: 0 0 * * *",
		Schedule:    "@every 1h",
		Prompt:      "/catchup",
	}, Create)
	require.NoError(t, err)
	assert.Equal(t, "@every 1h", saved.Schedule)
	assert.Equal(t, "check: prod schedule: 0 0 * * *", saved.Description)
	assert.Equal(t, "/catchup", saved.Prompt)
}

func TestAManualJobSavesAndReadsBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	saved, err := Save(home, Draft{Name: "prod-triage", Schedule: "manual", Prompt: "/prod-triage"}, Create)
	require.NoError(t, err)
	assert.Equal(t, Manual, saved.Schedule)
	assert.True(t, saved.Every.Manual())

	list := Load("", home)
	require.Len(t, list, 1)
	assert.True(t, list[0].Every.Manual(), "the file is the definition, and it read back the same")
	// No project: it runs wherever the launcher is, which is no checkout.
	assert.Empty(t, list[0].Project)
	assert.Equal(t, Manual, Viewed(list[0], ReadState(list[0])).Schedule)
	assert.Nil(t, Viewed(list[0], ReadState(list[0])).NextDue, "nothing to count down to")
}
