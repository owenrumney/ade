package myday

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheEstimatesSurviveBeingWrittenAndReadBack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	made := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)

	require.NoError(t, Save(Cache{"ENG-1": {UpdatedAt: made, MadeAt: made, Low: 2, High: 4, Why: "a migration"}}))
	got := Load()

	require.Contains(t, got, "ENG-1")
	assert.Equal(t, 4.0, got["ENG-1"].High)
	assert.True(t, made.Equal(got["ENG-1"].UpdatedAt))
}

func TestAnUnreadableCacheIsAnEmptyOneRatherThanAFailedMorning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := CachePath()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))

	assert.Empty(t, Load(), "the worst a corrupt cache costs is one morning's estimates made again")
}
