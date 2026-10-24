package dash

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A place asks the one service behind it: refreshing Issues should not also
// spend two GitHub round trips, and should not say it asked GitHub.
func TestARefreshAsksOnlyTheServiceItNamed(t *testing.T) {
	all := []string{"reviews", "notifications", "tickets"}
	for only, want := range map[string][]string{
		"tickets":       {"tickets"},
		"reviews":       {"reviews"},
		"notifications": {"notifications"},
		"":              all,
		"nonsense":      all,
	} {
		assert.Equal(t, want, ReadsFor(only), "only=%q", only)
	}
}
