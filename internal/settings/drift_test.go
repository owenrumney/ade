package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWaivedRequirementComesBackWhenItIsReworded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	require.NoError(t, SetDismissedRequirement("acme/app", "docs/x.md", "parse#b1", "aaaa"))

	aside := DismissedRequirements()
	assert.True(t, IsRequirementDismissed(aside, "acme/app", "docs/x.md", "parse#b1", "aaaa"))
	assert.False(t, IsRequirementDismissed(aside, "acme/app", "docs/x.md", "parse#b1", "bbbb"), "reworded")
	assert.False(t, IsRequirementDismissed(aside, "acme/app", "docs/x.md", "parse#b1", ""), "no hash, no waiver")
}

func TestTheSameBulletInAnotherRepositoryIsNotWaived(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	require.NoError(t, SetDismissedRequirement("acme/app", "docs/x.md", "tests#b1", "aaaa"))

	aside := DismissedRequirements()
	assert.False(t, IsRequirementDismissed(aside, "other/app", "docs/x.md", "tests#b1", "aaaa"), "another repo")
	assert.False(t, IsRequirementDismissed(aside, "acme/app", "docs/y.md", "tests#b1", "aaaa"), "another document")
}

func TestAnEmptyHashBringsAWaivedRequirementBack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	require.NoError(t, SetDismissedRequirement("acme/app", "docs/x.md", "parse#b1", "aaaa"))
	require.NoError(t, SetDismissedRequirement("acme/app", "docs/x.md", "parse#b1", ""))

	assert.Empty(t, DismissedRequirements())
}
