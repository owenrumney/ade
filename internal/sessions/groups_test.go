package sessions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

func groupedRepos() []Repo {
	then := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return []Repo{{Checkouts: []Dir{
		{Path: "/code/widget", Sessions: []Session{
			{ID: "old", Dir: "/code/widget", Title: "initial investigation", LastUsed: then},
			{ID: "new", Dir: "/code/widget", Title: "review follow-up", LastUsed: then.Add(time.Hour)},
			{ID: "live", Dir: "/code/widget", Live: true, LastUsed: then.Add(2 * time.Hour)},
		}},
		{Path: "/code/other", Sessions: []Session{{ID: "other", Dir: "/code/other", LastUsed: then}}},
	}}}
}

func TestCombineOrdersMembersAndUsesTheNewestTitle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	group, err := Combine(groupedRepos(), "/code/widget", []string{"new", "old"})
	require.NoError(t, err)
	assert.Equal(t, []string{"old", "new"}, group.Members)
	assert.Equal(t, "review follow-up", group.Title)
	assert.Equal(t, []string{"old", "new"}, settings.SessionGroups()[0].Members)
}

func TestCombineRejectsAnythingButEligibleMembers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	for _, ids := range [][]string{
		{"old"},
		{"old", "old"},
		{"old", "other"},
		{"old", "live"},
	} {
		_, err := Combine(groupedRepos(), "/code/widget", ids)
		assert.Error(t, err, ids)
		assert.Empty(t, settings.SessionGroups(), "a rejected request wrote a group")
	}

	repos := groupedRepos()
	repos[0].Checkouts[0].Sessions[0].Group = "already"
	_, err := Combine(repos, "/code/widget", []string{"old", "new"})
	assert.Error(t, err)
	assert.Empty(t, settings.SessionGroups())
}

func TestCandidatesAndSuggestionsExcludeLiveAndGroupedSessions(t *testing.T) {
	dir := groupedRepos()[0].Checkouts[0]
	dir.Sessions[1].Group = "group-1"
	candidates := Candidates(dir)
	require.Len(t, candidates, 1)
	assert.Equal(t, "old", candidates[0].ID)
	assert.Equal(t,
		SuggestionFingerprint([]string{"old", "new"}),
		SuggestionFingerprint([]string{"new", "old"}),
	)
}

func TestAttachGroupsUsesTheStoredMembersWithoutRewritingSessions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	record, err := settings.CreateSessionGroup("/code/widget", []string{"old", "new"})
	require.NoError(t, err)
	dir := groupedRepos()[0].Checkouts[0]

	attachGroups(&dir, settings.SessionGroups())
	require.Len(t, dir.Groups, 1)
	assert.Equal(t, record.ID, dir.Sessions[0].Group)
	assert.Equal(t, record.ID, dir.Sessions[1].Group)
	assert.Empty(t, dir.Sessions[2].Group)
	assert.Equal(t, "review follow-up", dir.Groups[0].Title)
}

// A member past the scan's age cutoff leaves one visible session and no card
// to draw. Stamping it anyway is a row the list has nowhere to put.
func TestAMemberThatFellOutOfTheScanLeavesTheSurvivorOnItsOwn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := settings.CreateSessionGroup("/code/widget", []string{"gone", "new"})
	require.NoError(t, err)
	dir := groupedRepos()[0].Checkouts[0]

	attachGroups(&dir, settings.SessionGroups())
	assert.Empty(t, dir.Groups)
	assert.Empty(t, dir.Sessions[1].Group, "the survivor must still render as itself")
	assert.Contains(t, Candidates(dir), dir.Sessions[1], "and still be combinable")
}

func TestCandidatesLeaveOutArchivedSessions(t *testing.T) {
	dir := groupedRepos()[0].Checkouts[0]
	dir.Sessions[0].Archived = true

	candidates := Candidates(dir)
	require.Len(t, candidates, 1)
	assert.Equal(t, "new", candidates[0].ID, "an archived session was put away on purpose")
}

func TestUncombineRestoresTheIndividualSessions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repos := groupedRepos()
	group, err := Combine(repos, "/code/widget", []string{"old", "new"})
	require.NoError(t, err)

	require.NoError(t, Uncombine(repos, group.ID))
	assert.Empty(t, settings.SessionGroups())
	dir := groupedRepos()[0].Checkouts[0]
	attachGroups(&dir, settings.SessionGroups())
	assert.Empty(t, dir.Groups)
	assert.Empty(t, dir.Sessions[0].Group)
	assert.Empty(t, dir.Sessions[1].Group)
}

// The group store is machine-wide, so an id on its own would otherwise reach
// a group belonging to a repository this server cannot see.
func TestUncombineRefusesAGroupFromAnotherCheckout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	group, err := Combine(groupedRepos(), "/code/widget", []string{"old", "new"})
	require.NoError(t, err)

	elsewhere := []Repo{{Checkouts: []Dir{{Path: "/code/other"}}}}
	require.Error(t, Uncombine(elsewhere, group.ID))
	assert.Len(t, settings.SessionGroups(), 1)
}
