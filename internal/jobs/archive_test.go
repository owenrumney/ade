package jobs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

func TestTheDesignArchiveIsScheduledByDefault(t *testing.T) {
	job, found := Find(WithBuiltins(nil, settings.Settings{}), KindDesignArchive)
	require.True(t, found, "the backlog is swept whether or not a wiki is configured")
	assert.Equal(t, Reads, job.Allow, "archiving touches untracked state and a remote, never a branch")
	assert.Equal(t, ScopeBuiltin, job.Scope)
	assert.False(t, job.Every.Manual())
}

func TestADesignArchiveWrittenByHandWins(t *testing.T) {
	mine := Job{Name: KindDesignArchive, Kind: KindDesignArchive, Scope: ScopeUser, Schedule: "0 8 * * 1"}
	job, found := Find(WithBuiltins([]Job{mine}, settings.Settings{}), KindDesignArchive)
	require.True(t, found)
	assert.Equal(t, mine, job)
}
