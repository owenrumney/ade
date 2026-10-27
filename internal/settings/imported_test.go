package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPinningASessionRecordsTheCheckoutHoldingIt(t *testing.T) {
	atHome(t)

	require.NoError(t, SetImported("sess-1", "/code/coast"))
	require.NoError(t, SetImported("sess-2", "/code/infra"))
	assert.Equal(t, map[string]string{"sess-1": "/code/coast", "sess-2": "/code/infra"}, Imported())

	// Adopting again moves the pin rather than keeping both.
	require.NoError(t, SetImported("sess-1", "/code/coast-trk-1"))
	assert.Equal(t, "/code/coast-trk-1", Imported()["sess-1"])

	// An empty directory unpins, which takes the id out of the file.
	require.NoError(t, SetImported("sess-1", ""))
	assert.Equal(t, map[string]string{"sess-2": "/code/infra"}, Imported())
}

func TestPinningNeedsAnId(t *testing.T) {
	atHome(t)
	assert.Error(t, SetImported("", "/code/coast"))
	assert.Error(t, SetImported("   ", "/code/coast"))
}

func TestAnUnpinnedIdIsNotAnError(t *testing.T) {
	atHome(t)
	require.NoError(t, SetImported("never-pinned", ""))
	assert.Empty(t, Imported())
}
