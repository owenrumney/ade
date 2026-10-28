package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/transcripts"
)

func TestListSkipsDirectoriesThatAreGone(t *testing.T) {
	// A transcript outlives the directory it was recorded in: rename a repo or
	// remove a worktree and it points at nothing. Offering one would start a
	// server somewhere that does not exist.
	for _, found := range List("") {
		info, err := os.Stat(found.Dir)
		require.NoErrorf(t, err, "%s is listed but gone", found.Dir)
		assert.True(t, info.IsDir())
		assert.NotEmpty(t, found.Name, "a session needs something to click on")
		assert.Equal(t, found.Agent == "claude" || found.Agent == "codex", found.Resumable, found.Agent)
	}
}

func TestListMarksNothingHereWithoutACheckout(t *testing.T) {
	// The launcher belongs to no checkout, so nothing in its list is "here".
	for _, found := range List("") {
		assert.False(t, found.Here)
	}
}

func TestFindOnlyMatchesWhatWasScanned(t *testing.T) {
	list := []Session{
		{Dir: "/repo/one", ID: "a"},
		{Dir: "/repo/two", ID: "b"},
	}

	found, ok := Find(list, "/repo/two", "b")
	require.True(t, ok)
	assert.Equal(t, "b", found.ID)

	// An empty id matches on the directory alone, which is how a row with no
	// transcript still opens.
	_, ok = Find(list, "/repo/one", "")
	assert.True(t, ok)

	// The request names a directory to start a process in, so anything the scan
	// did not return has to be refused.
	for _, tc := range [][2]string{{"/etc", ""}, {"/", ""}, {"/repo/one", "b"}, {"", ""}} {
		_, ok := Find(list, tc[0], tc[1])
		assert.Falsef(t, ok, "accepted %q %q", tc[0], tc[1])
	}
}

func TestLastLine(t *testing.T) {
	// ade prints its URL last; a created document or worktree path comes first.
	assert.Equal(t, "http://127.0.0.1:1/?t=a",
		LastLine("/some/doc.md\n/some/worktree\nhttp://127.0.0.1:1/?t=a\n"))
	assert.Equal(t, "one", LastLine("one"))
	assert.Empty(t, LastLine("   \n  "))
}

// Choosing a provider never chooses a boundary: the container is the default
// for both, and only --sandbox=false asks for otherwise.
func TestTheLauncherNeverDropsTheSandboxForAProvider(t *testing.T) {
	assert.NotContains(t, openArgs("codex", "gpt-5.6-terra", ""), "--sandbox=false")
	assert.NotContains(t, openArgs("claude", "claude-opus-5", ""), "--sandbox=false")
}

// A ticket reaches a command line, so the shape is the boundary.
func TestATicketIsEitherALinearKeyOrAnIssueNumber(t *testing.T) {
	for want, refs := range map[string][]string{
		"--linear": {"FIX-623", "fix-623", " TRK-1072 "},
		"--github": {"#12", "12"},
	} {
		for _, ref := range refs {
			args, err := TicketArgs(ref)
			require.NoErrorf(t, err, "refused %q", ref)
			assert.Equalf(t, want, args[0], "%q", ref)
		}
	}
	assert.Equal(t, []string{"--linear", "FIX-623"}, mustArgs(t, "fix-623"))
	assert.Equal(t, []string{"--github", "12"}, mustArgs(t, "#12"))

	none, err := TicketArgs("  ")
	require.NoError(t, err)
	assert.Nil(t, none)

	for _, bad := range []string{"#0", "cancel-running-process", "FIX-623-x", "https://linear.app/x/issue/FIX-623"} {
		_, err := TicketArgs(bad)
		assert.Errorf(t, err, "accepted %q", bad)
	}
}

func mustArgs(t *testing.T, ref string) []string {
	t.Helper()
	args, err := TicketArgs(ref)
	require.NoError(t, err)
	return args
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "abc", truncate("abc", 5))
	assert.Equal(t, "abc…", truncate("abcdef", 3))
}

func TestANamedAgentsRunIsNotARecentSession(t *testing.T) {
	// ade calls @staff-reviewer and friends on the checkout you are working in.
	// Claude Code gives each a top-level transcript, so every review turned
	// into three more rows in Recent sessions, all in the same checkout.
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()

	project := filepath.Join(home, ".claude", "projects", "-work")
	require.NoError(t, os.MkdirAll(project, 0o750))
	at := time.Now().UTC().Format(time.RFC3339)
	write := func(name, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(project, name), []byte(body), 0o600))
	}
	write("aaaa-1111.jsonl", fmt.Sprintf(
		`{"type":"user","timestamp":%q,"cwd":%q,"message":{"role":"user","content":"cache the cart"}}`+"\n"+
			`{"type":"assistant","timestamp":%q,"requestId":"r1","message":{"id":"m1","role":"assistant","model":"claude-opus-5","content":[],"usage":{"input_tokens":10,"output_tokens":2}}}`+"\n",
		at, work, at))
	write("bbbb-2222.jsonl", `{"type":"agent-setting","agentSetting":"staff-reviewer"}`+"\n"+fmt.Sprintf(
		`{"type":"user","timestamp":%q,"cwd":%q,"message":{"role":"user","content":"You are @staff-reviewer"}}`+"\n", at, work))

	var ids []string
	models := map[string]string{}
	for _, found := range List("") {
		ids = append(ids, found.ID)
		models[found.ID] = found.Model
	}
	assert.Contains(t, ids, "aaaa-1111", "the conversation is still listed")
	assert.Equal(t, "claude-opus-5", models["aaaa-1111"], "the model travels with a resumable session")
	assert.NotContains(t, ids, "bbbb-2222", "a reviewer's run is not somewhere to go back to")
}

// The row said "We are working on this repository together in ade. - There is
// no design doc…" for every session in the list. What Owen typed is in there.
func TestATitleIsWhatTheUserAskedNotTheWrapperAdeSentWithIt(t *testing.T) {
	wrapped := "We are working on this repository together in ade. " +
		"- Answer what is asked. If it needs a change, make it. They said: why is the commit panel empty?"
	assert.Equal(t, "why is the commit panel empty?",
		Title(&transcripts.Session{FirstPrompt: wrapped}))

	// A run ade launched keeps its opening sentence: it names the work.
	assert.Equal(t, "Read ticket trk-1093 using the tools you have for it.",
		Title(&transcripts.Session{FirstPrompt: "Read ticket trk-1093 using the tools you have for it. Then draft the design in /x/y"}))

	// Anything else is itself, and a title already recorded always wins.
	assert.Equal(t, "plain question", Title(&transcripts.Session{FirstPrompt: "plain question"}))
	assert.Equal(t, "named", Title(&transcripts.Session{Title: "named", FirstPrompt: "ignored"}))
}

func TestTheSearchesWindowIsWiderThanTheSwitchers(t *testing.T) {
	assert.Equal(t, max, Recent().Max)
	assert.Equal(t, age, Recent().Age)
	assert.Zero(t, Everything().Max, "a search is not capped")
	assert.Zero(t, Everything().Age, "nor is it a fortnight")
}
