package dash

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/linear"
	"github.com/owenrumney/ade/internal/sessions"
)

// A ticket already in a worktree must not be offered as something to start:
// doing it twice makes a second worktree for one piece of work, and the second
// one is the one without the history.
func TestATicketAlreadyInAWorktreeIsReportedAsStarted(t *testing.T) {
	b := New(Deps{Repos: func() ([]sessions.Repo, error) {
		return []sessions.Repo{{
			Name: "ade",
			Checkouts: []sessions.Dir{
				{Path: "/code/ade", Branch: "main"},
				// Newest first, which is the order sessions.List gives them in.
				{Path: "/code/ade-fix-623", Branch: "fix-623", Sessions: []sessions.Session{
					{ID: "newest"},
					{ID: "older"},
				}},
				// Archived sessions are ones you put away, so they are not what
				// resuming should land on.
				{Path: "/code/ade-eng-7", Branch: "eng-7", Sessions: []sessions.Session{
					{ID: "put-away", Archived: true},
					{ID: "live"},
				}},
			},
		}}, nil
	}})

	started := b.Started([]linear.Issue{
		{Identifier: "FIX-623"},
		{Identifier: "ENG-7"},
		{Identifier: "NEW-1"},
	})

	require.Contains(t, started, "FIX-623", "the branch ade create would have made")
	assert.Equal(t, "/code/ade-fix-623", started["FIX-623"].Dir)
	assert.Equal(t, "newest", started["FIX-623"].Session, "the one last used")

	require.Contains(t, started, "ENG-7")
	assert.Equal(t, "live", started["ENG-7"].Session, "an archived session is not resumed")

	assert.NotContains(t, started, "NEW-1", "nothing on the machine is on its branch")
}

// Linear's own branch name is taken when it is set, since a branch made from
// Linear's copy button will not match the slug ade would have chosen.
func TestLinearsOwnBranchNameCounts(t *testing.T) {
	b := New(Deps{Repos: func() ([]sessions.Repo, error) {
		return []sessions.Repo{{Checkouts: []sessions.Dir{
			{Path: "/code/app-owen", Branch: "owen/fix-623-the-thing"},
		}}}, nil
	}})

	started := b.Started([]linear.Issue{
		{Identifier: "FIX-623", BranchName: "owen/fix-623-the-thing"},
	})
	require.Contains(t, started, "FIX-623")
	assert.Equal(t, "/code/app-owen", started["FIX-623"].Dir)
}

func TestTicketBranchMatchesWhatCreateWouldName(t *testing.T) {
	assert.Equal(t, "fix-623", TicketBranch("FIX-623"))
	assert.Equal(t, "eng-1", TicketBranch("eng-1"))
	assert.Empty(t, TicketBranch(""))
}
