package design

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A reviewer has a shell. `go test -c` writes pkg.test into the checkout and
// the user finds a binary in their diff.
func TestEveryPromptWithAShellForbidsBuildArtifacts(t *testing.T) {
	for name, prompt := range map[string]string{
		"review":  DiffReviewPrompt("security", "", "DIFF"),
		"explain": ExplainPrompt("eli5", "", "DIFF"),
		"@name":   SubagentPrompt("security", "", "", "", "look at this", ""),
		"handoff": HandoffPrompt("tester", "security", "", "look at this", ""),
	} {
		assert.Contains(t, prompt, "go test -c", name)
	}
}
