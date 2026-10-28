package window

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTheWindowIsNotOpenedWhenItIsNotInstalled(t *testing.T) {
	// Said and not silent: `ade .` opening nothing at all is the worst of it.
	windowApp = filepath.Join(t.TempDir(), "ADE.app")
	assert.ErrorIs(t, Open(t.TempDir()), ErrNone)
}
