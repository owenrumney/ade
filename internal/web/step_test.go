package web

import (
	"encoding/json"
	"testing"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/stretchr/testify/assert"
)

func TestStepLineSaysWhatTheToolIsDoing(t *testing.T) {
	cases := []struct {
		name, tool, input, want string
	}{
		{"a command, first line only", "Bash", `{"command":"git diff --cached\necho done"}`, "Bash: git diff --cached"},
		{"an edit names the file", "Edit", `{"file_path":"/r/internal/web/agent.go"}`, "Edit: agent.go"},
		{"a search names the pattern", "Grep", `{"pattern":"noteEdits"}`, "Grep: noteEdits"},
		{"a fetch names the host", "WebFetch", `{"url":"https://open-vsx.org/api/x"}`, "WebFetch: open-vsx.org"},
		{"a tool with nothing to say is its name", "TodoWrite", `{"todos":[]}`, "TodoWrite"},
		{"a long detail is cut", "Bash", `{"command":"0123456789012345678901234567890123456789"}`, "Bash: 0123456789012345678901234567890…"},
		{"a cd into the checkout is not the step", "Bash", `{"command":"cd /Users/owen/code/owenrumney/ade && cat internal/web/step.go"}`, "Bash: cat internal/web/step.go"},
		{"several of them", "Bash", `{"command":"cd /a && cd /b && make ui"}`, "Bash: make ui"},
		{"a cd that is the whole command stands", "Bash", `{"command":"cd /Users/owen/code"}`, "Bash: cd /Users/owen/code"},
		{"what follows the cd is kept whole", "Bash", `{"command":"cd /r && go test ./... && echo ok"}`, "Bash: go test ./... && echo ok"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stepLine(bridge.ToolUse{Name: tc.tool, Input: json.RawMessage(tc.input)})
			assert.Equal(t, tc.want, got)
		})
	}
}
