package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheBuildNeedsNoSiblingCheckouts is the invariant that had quietly broken.
//
// go.mod carried `replace` directives pointing at ../agentbridge, ../mdview and
// ../sm, so building ade needed four checkouts laid out as siblings at those
// exact relative paths. On any other machine it simply did not build. The code
// they pointed at now lives in internal/, and nothing should send the build
// outside this repository again.
func TestTheBuildNeedsNoSiblingCheckouts(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	require.NoError(t, err)
	mod := string(data)

	for _, line := range strings.Split(mod, "\n") {
		trimmed := strings.TrimSpace(line)
		assert.NotContainsf(t, trimmed, "replace ",
			"a replace directive ties the build to whatever is beside it: %q", trimmed)
		assert.NotContainsf(t, trimmed, "../",
			"a relative path ties the build to a directory layout: %q", trimmed)
	}

	// Named individually, because these are the three that were absorbed and a
	// re-added dependency on any of them is the specific mistake to catch.
	for _, absorbed := range []string{
		"github.com/owenrumney/agentbridge",
		"github.com/owenrumney/mdview",
		"github.com/owenrumney/sm",
	} {
		assert.NotContainsf(t, mod, absorbed,
			"%s was absorbed into internal/; depending on it again splits the source", absorbed)
	}
}
