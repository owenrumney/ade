package filetime

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreatedIsNeverLaterThanTheTruth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.md")
	before := time.Now().Add(-time.Second)
	require.NoError(t, os.WriteFile(path, []byte("# one\n"), 0o600))

	// Touched an hour on, the creation date must not move with it.
	later := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(path, later, later))
	info, err := os.Lstat(path)
	require.NoError(t, err)

	at := Created(info)
	assert.False(t, at.IsZero())
	assert.False(t, at.Before(before), "created %s is before the file existed", at)
	if _, ok := birth(info); ok {
		assert.True(t, at.Before(later), "a platform with a birth time must not report the touch")
	}
}
