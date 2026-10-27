package ghpr

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNotificationsReadsTheInboxAndWhereEachOneGoes(t *testing.T) {
	out := `[
	  {"id":"25399171359","reason":"review_requested","unread":true,"updated_at":"2026-09-01T08:15:23Z",
	   "repository":{"full_name":"owenrumney/ade"},
	   "subject":{"title":"TRK-820 Make RATS CI reliable","type":"PullRequest",
	              "url":"https://api.github.com/repos/owenrumney/ade/pulls/1034"}},
	  {"id":"2","reason":"mention","unread":false,"updated_at":"2026-08-30T10:00:00Z",
	   "repository":{"full_name":"acme/other"},
	   "subject":{"title":"a bug","type":"Issue",
	              "url":"https://api.github.com/repos/acme/other/issues/77"}},
	  {"id":"3","reason":"subscribed","unread":true,"updated_at":"2026-08-29T10:00:00Z",
	   "repository":{"full_name":"acme/other"},
	   "subject":{"title":"v1.2.0","type":"Release",
	              "url":"https://api.github.com/repos/acme/other/releases/9912"}}
	]`

	inbox, err := parseNotifications(out)
	require.NoError(t, err)
	require.Len(t, inbox, 3)

	assert.Equal(t, "25399171359", inbox[0].ID)
	assert.Equal(t, "owenrumney/ade", inbox[0].Repo)
	assert.Equal(t, "review_requested", inbox[0].Reason)
	assert.Equal(t, "PullRequest", inbox[0].Type)
	assert.Equal(t, 1034, inbox[0].Number)
	assert.Equal(t, "https://github.com/owenrumney/ade/pull/1034", inbox[0].URL,
		"pulls in the api is pull in a browser")
	assert.True(t, inbox[0].Unread)

	assert.Equal(t, 77, inbox[1].Number)
	assert.Equal(t, "https://github.com/acme/other/issues/77", inbox[1].URL)
	assert.False(t, inbox[1].Unread)

	assert.Zero(t, inbox[2].Number, "a release has no number of the kind a row shows")
	assert.Equal(t, "https://github.com/acme/other", inbox[2].URL,
		"a subject with nowhere of its own lands on the repository")

	_, err = parseNotifications("nope")
	assert.Error(t, err)
}

func TestSubjectPlaceHandlesWhatTheApiCanSend(t *testing.T) {
	for _, tc := range []struct {
		name   string
		repo   string
		url    string
		want   string
		number int
	}{
		{"a pull request", "a/b", "https://api.github.com/repos/a/b/pulls/12", "https://github.com/a/b/pull/12", 12},
		{"an issue", "a/b", "https://api.github.com/repos/a/b/issues/12", "https://github.com/a/b/issues/12", 12},
		{"a trailing slash", "a/b", "https://api.github.com/repos/a/b/pulls/12/", "https://github.com/a/b/pull/12", 12},
		{"no url at all", "a/b", "", "https://github.com/a/b", 0},
		{"not a number on the end", "a/b", "https://api.github.com/repos/a/b/pulls/head", "https://github.com/a/b", 0},
		{"a shape not seen before", "a/b", "https://api.github.com/repos/a/b/discussions/4", "https://github.com/a/b", 0},
		{"a zero number", "a/b", "https://api.github.com/repos/a/b/pulls/0", "https://github.com/a/b", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url, number := subjectPlace(tc.repo, tc.url)
			assert.Equal(t, tc.want, url)
			assert.Equal(t, tc.number, number)
		})
	}
}

func TestStateQueryAliasesOneLookupEach(t *testing.T) {
	q := stateQuery([]PRRef{{Repo: "a/b", Number: 12}, {Repo: "c/d", Number: 7}})
	assert.Contains(t, q, `a0: repository(owner:"a",name:"b"){ pullRequest(number:12){ state } }`)
	assert.Contains(t, q, `a1: repository(owner:"c",name:"d"){ pullRequest(number:7){ state } }`)
	assert.NotContains(t, strings.ToLower(q), "mutation")
}

func TestReadStatesMapsBackByAlias(t *testing.T) {
	batch := []PRRef{{Repo: "a/b", Number: 12}, {Repo: "c/d", Number: 7}, {Repo: "e/f", Number: 9}}
	out := `{"data":{
	  "a0":{"pullRequest":{"state":"MERGED"}},
	  "a1":{"pullRequest":{"state":"open"}},
	  "a2":{"pullRequest":null}
	}}`

	states := map[string]string{}
	require.NoError(t, readStates(out, batch, states))
	assert.Equal(t, "MERGED", states["a/b#12"])
	assert.Equal(t, "OPEN", states["c/d#7"], "however github cased it")
	assert.NotContains(t, states, "e/f#9", "one that could not be read is left out, not guessed at")

	assert.Error(t, readStates("nope", batch, map[string]string{}))
}

func TestPRStatesRefusesARepoItCannotTrust(t *testing.T) {
	// Nothing reaches gh: every ref is dropped before a query is built, so this
	// answers empty without asking anything.
	states, err := PRStates(context.Background(), t.TempDir(), []PRRef{
		{Repo: `a/b"){ x }`, Number: 1},
		{Repo: "no-slash", Number: 1},
		{Repo: "a/b", Number: 0},
	})
	require.NoError(t, err)
	assert.Empty(t, states)
}

func TestPRRefKey(t *testing.T) {
	assert.Equal(t, "owner/repo#12", PRRef{Repo: "owner/repo", Number: 12}.Key())
}

// What a person pastes is whatever the browser was showing, which is rarely
// the bare url: a tab, an anchor and a query all ride along.
func TestParseRefReadsWhatWasPasted(t *testing.T) {
	for _, c := range []struct {
		in   string
		want PRRef
	}{
		{"https://github.com/infracost/cli/pull/1535", PRRef{"infracost/cli", 1535}},
		{"https://github.com/infracost/cli/pull/1535/files", PRRef{"infracost/cli", 1535}},
		{"https://github.com/infracost/cli/pull/1535#discussion_r1", PRRef{"infracost/cli", 1535}},
		{"https://github.com/infracost/cli/pull/1535?w=1", PRRef{"infracost/cli", 1535}},
		{"  github.com/owner/name/pull/7/commits  ", PRRef{"owner/name", 7}},
		{"infracost/cli#1535", PRRef{"infracost/cli", 1535}},
	} {
		got, ok := ParseRef(c.in)
		assert.Truef(t, ok, "refused %q", c.in)
		assert.Equalf(t, c.want, got, "read %q", c.in)
	}
}

func TestParseRefRefusesWhatIsNotAPullRequest(t *testing.T) {
	for _, in := range []string{
		"", "   ", "https://github.com/infracost/cli",
		"https://github.com/infracost/cli/issues/12",
		"pull/9", "https://github.com/a/b/pull/0",
		"https://github.com/a/b/pull/nope",
		// No owner before the name: nothing to clone from.
		"https://github.com/cli/pull/3",
	} {
		_, ok := ParseRef(in)
		assert.Falsef(t, ok, "accepted %q", in)
	}
}
