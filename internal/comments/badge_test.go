package comments

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The badge is how bad it is, said before the words that say why, in the
// one vocabulary the reader already has from the list.
func TestEachSeverityWearsItsOwnBadge(t *testing.T) {
	for _, each := range []struct{ severity, want string }{
		{SeverityCritical, "![Critical](https://img.shields.io/badge/Critical-8B0000)"},
		{SeverityHigh, "![High](https://img.shields.io/badge/High-red)"},
		{SeverityMedium, "![Medium](https://img.shields.io/badge/Medium-orange)"},
		{SeverityLow, "![Low](https://img.shields.io/badge/Low-yellow)"},
		// Unset is medium, as it is everywhere else.
		{"", "![Medium](https://img.shields.io/badge/Medium-orange)"},
		{"nonsense", "![Medium](https://img.shields.io/badge/Medium-orange)"},
	} {
		assert.Equal(t, each.want, Badge(each.severity), each.severity)
	}
}

func TestTheBadgeGoesOnTopAndOnlyOnce(t *testing.T) {
	body := Badged(SeverityHigh, "This leaks the token into the log.")
	assert.Equal(t,
		"![High](https://img.shields.io/badge/High-red)\n\nThis leaks the token into the log.",
		body)
	assert.Equal(t, body, Badged(SeverityHigh, body), "a second pass adds nothing")
	// One the agent wrote with a badge of its own is already headed.
	written := "![Critical](https://img.shields.io/badge/Critical-8B0000)\n\nsomething"
	assert.Equal(t, written, Badged(SeverityLow, written))
	assert.Equal(t, "", Badged(SeverityHigh, ""))
}
