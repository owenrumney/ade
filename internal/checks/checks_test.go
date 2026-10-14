package checks

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/owenrumney/ade/internal/ghpr"
)

func TestPromptInsistsOnReadingTheFailure(t *testing.T) {
	prompt := Prompt(42, []ghpr.Check{
		{Name: "test", Workflow: "CI", Bucket: "fail", Description: "2 failed", Link: "https://ci/1"},
	})

	assert.Contains(t, prompt, "#42")
	assert.Contains(t, prompt, "test")
	assert.Contains(t, prompt, "CI")
	assert.Contains(t, prompt, "2 failed")
	assert.Contains(t, prompt, "https://ci/1")

	// The whole point is that it reads the log rather than guessing from the name.
	assert.Contains(t, prompt, "rather than inferring the cause")
	assert.Contains(t, prompt, "Do NOT run git commit")
	assert.Contains(t, prompt, "say so and leave it alone")
}

func TestPromptNumbersEveryCheck(t *testing.T) {
	prompt := Prompt(1, []ghpr.Check{
		{Name: "lint", Bucket: "fail"},
		{Name: "build", Bucket: "fail"},
		{Name: "e2e", Bucket: "fail"},
	})

	for _, want := range []string{"1. lint", "2. build", "3. e2e"} {
		assert.Contains(t, prompt, want)
	}
	assert.Equal(t, 1, strings.Count(prompt, "Failing checks:"))
}

func TestPromptOmitsMissingFields(t *testing.T) {
	prompt := Prompt(1, []ghpr.Check{{Name: "lint", Bucket: "fail"}})

	assert.Contains(t, prompt, "1. lint")
	assert.NotContains(t, prompt, "()", "a check with no workflow gains no empty parentheses")
	assert.NotContains(t, prompt, "http")
}
