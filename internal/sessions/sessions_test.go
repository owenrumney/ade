package sessions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
