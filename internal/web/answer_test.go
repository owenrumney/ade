package web

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAnswerPromptAsksForOneLine(t *testing.T) {
	got := answerPrompt("  which port does the worker bind?  ")
	assert.Contains(t, got, "They asked:\n\nwhich port does the worker bind?")
	assert.Contains(t, got, "one or two short lines")
	assert.Contains(t, got, "Change nothing", "a fork must not edit the checkout")
}
