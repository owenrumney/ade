package settings

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkipReviewByAuthorRepoAndAge(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	old := time.Now().UTC().AddDate(0, 0, -600).Format(time.RFC3339)

	rules := Ignore{Authors: []string{"dependabot[bot]"}, Repos: []string{"acme/legacy"}, OlderThanDays: 90}

	assert.True(t, rules.SkipReview("acme/app", "dependabot[bot]", now), "by author")
	assert.True(t, rules.SkipReview("acme/app", "DEPENDABOT[BOT]", now), "author, whatever the case")
	assert.True(t, rules.SkipReview("ACME/Legacy", "sam", now), "repo, whatever the case")
	assert.True(t, rules.SkipReview("acme/app", "sam", old), "600 days is older than 90")
	assert.False(t, rules.SkipReview("acme/app", "sam", now), "a live one from a person stays")

	none := Ignore{}
	assert.False(t, none.SkipReview("acme/app", "dependabot[bot]", old), "no rules, nothing skipped")

	ageless := Ignore{Authors: []string{"x"}}
	assert.False(t, ageless.SkipReview("acme/app", "sam", old), "zero older_than_days is no age rule")
}

func TestABotIsIgnoredWhicheverWayItIsWritten(t *testing.T) {
	// github.com shows "dependabot[bot]"; the GraphQL search answers
	// "dependabot". Both spellings of the rule have to work, or the rule looks
	// broken to whoever wrote it.
	now := time.Now().UTC().Format(time.RFC3339)
	for _, rule := range []string{"dependabot[bot]", "dependabot", "Dependabot[Bot]"} {
		rules := Ignore{Authors: []string{rule}}
		for _, author := range []string{"dependabot", "dependabot[bot]"} {
			assert.Truef(t, rules.SkipReview("acme/app", author, now), "rule %q against author %q", rule, author)
		}
		assert.False(t, rules.SkipReview("acme/app", "sam", now), "a person is not a bot")
	}
}

func TestSkipReviewKeepsWhatItCannotDate(t *testing.T) {
	// Hiding a real request because its timestamp was odd is the worse failure.
	rules := Ignore{OlderThanDays: 1}
	assert.False(t, rules.SkipReview("acme/app", "sam", ""), "no time given")
	assert.False(t, rules.SkipReview("acme/app", "sam", "not-a-time"), "a time we cannot read")
}

func TestSkipNotificationByRepoAndReason(t *testing.T) {
	rules := Ignore{Repos: []string{"acme/legacy"}, Reasons: []string{"comment", "subscribed"}}

	assert.True(t, rules.SkipNotification("acme/app", "comment"))
	assert.True(t, rules.SkipNotification("acme/app", "Subscribed"))
	assert.True(t, rules.SkipNotification("acme/legacy", "review_requested"))
	assert.False(t, rules.SkipNotification("acme/app", "review_requested"))
	assert.False(t, rules.SkipNotification("", ""), "nothing to match on is not a match")
}

func TestIgnoreRulesSurviveTheSettingsFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	want := Settings{
		ScanDirs: []string{"/tmp/code"},
		Ignore:   Ignore{Authors: []string{"dependabot[bot]"}, Reasons: []string{"comment"}, OlderThanDays: 90},
	}
	require.NoError(t, Save(want))

	got, err := Load()
	require.NoError(t, err)
	assert.Equal(t, want.Ignore, got.Ignore)
	assert.True(t, got.Ignore.SkipNotification("any/repo", "comment"))
}

func TestDismissalLastsUntilTheThreadMovesOn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	at := "2026-09-01T08:15:23Z"
	require.NoError(t, SetDismissed("2539", at))

	set := Dismissed()
	assert.True(t, IsDismissed(set, "2539", at), "the same state stays put aside")
	assert.True(t, IsDismissed(set, "2539", "2026-08-30T00:00:00Z"), "older than the dismissal, still aside")
	assert.False(t, IsDismissed(set, "2539", "2026-09-02T00:00:00Z"),
		"someone replied after it was dismissed, so it comes back")
	assert.False(t, IsDismissed(set, "other", at), "a thread never dismissed")

	require.NoError(t, SetDismissed("2539", ""), "an empty time brings it back")
	assert.False(t, IsDismissed(Dismissed(), "2539", at))

	assert.Error(t, SetDismissed("  ", at), "an id is required")
}

func TestDismissalsAreKeptForManyThreads(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for i := range 20 {
		require.NoError(t, SetDismissed(fmt.Sprintf("id-%d", i), "2026-09-01T08:00:00Z"))
	}
	assert.Len(t, Dismissed(), 20)
}

func TestIgnoreRepoTakesAWholeOwner(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)

	// The three ways anyone would write "nothing from aquasecurity".
	for _, rule := range []string{"aquasecurity/*", "aquasecurity", "AquaSecurity/*"} {
		rules := Ignore{Repos: []string{rule}}
		for _, repo := range []string{"aquasecurity/trivy", "aquasecurity/tfsec", "AquaSecurity/Trivy"} {
			assert.Truef(t, rules.SkipNotification(repo, "comment"), "rule %q against repo %q", rule, repo)
			assert.Truef(t, rules.SkipReview(repo, "sam", now), "rule %q against repo %q", rule, repo)
		}
		assert.Falsef(t, rules.SkipNotification("owenrumney/ade", "comment"), "rule %q leaves others alone", rule)
	}
}

func TestIgnoreRepoPatterns(t *testing.T) {
	for _, tc := range []struct {
		rule string
		repo string
		want bool
	}{
		{"acme/legacy-*", "acme/legacy-api", true},
		{"acme/legacy-*", "acme/current", false},
		{"*/sandbox", "anyone/sandbox", true},
		{"*/sandbox", "anyone/other", false},
		{"acme/thing", "acme/thing", true},
		{"acme/thing", "acme/thingy", false},
		// "*" stops at a slash, so an owner rule cannot swallow everything.
		{"acme/*", "other/repo", false},
	} {
		t.Run(tc.rule+" vs "+tc.repo, func(t *testing.T) {
			rules := Ignore{Repos: []string{tc.rule}}
			assert.Equal(t, tc.want, rules.SkipNotification(tc.repo, "comment"))
		})
	}
}

func TestIgnoreAuthorsAndReasonsTakePatterns(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)

	bots := Ignore{Authors: []string{"*-bot"}}
	assert.True(t, bots.SkipReview("a/b", "release-bot", now))
	assert.False(t, bots.SkipReview("a/b", "sam", now))

	// The bracket in a bot name is a name, never a character class: this is the
	// case that would silently stop matching if brackets were treated as glob.
	named := Ignore{Authors: []string{"dependabot[bot]"}}
	assert.True(t, named.SkipReview("a/b", "dependabot", now))
	assert.False(t, named.SkipReview("a/b", "dependabott", now), "not a character class")

	kinds := Ignore{Reasons: []string{"ci_*"}}
	assert.True(t, kinds.SkipNotification("a/b", "ci_activity"))
	assert.False(t, kinds.SkipNotification("a/b", "comment"))
}

func TestABadPatternMatchesNothingRatherThanEverything(t *testing.T) {
	// path.Match errors on this; the rule should do nothing, not match all.
	rules := Ignore{Repos: []string{"acme/[a-"}}
	assert.False(t, rules.SkipNotification("acme/thing", "comment"))
}
