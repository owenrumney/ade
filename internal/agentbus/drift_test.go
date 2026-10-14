package agentbus

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// specPath is the real protocol, where a local agentbus install keeps it. This
// package is a hand-written mirror, so it drifts silently without a check.
func specPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "agentbus", "src", "docs", "protocol.md")
}

func spec(t *testing.T) string {
	t.Helper()
	path := specPath()
	if path == "" {
		t.Skip("no home directory")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip("agentbus is not installed from source here")
	}
	return string(raw)
}

// A frame ade sends that the spec no longer names is drift that would only
// show as a refusal at runtime. The other direction is fine: protocol.md may
// name frames ade has no use for, and those are deliberately not copied.
func TestEveryFrameAdeSendsIsStillInTheSpec(t *testing.T) {
	text := spec(t)
	named := map[string]bool{}
	for _, m := range regexp.MustCompile(`"type":\s*"([a-z.]+)"`).FindAllStringSubmatch(text, -1) {
		named[m[1]] = true
	}
	require.NotEmpty(t, named, "read no frame types out of protocol.md")

	for _, sent := range []string{
		TypeHello, TypeStatus, TypeWho, TypeGet, TypeAsk, TypeAnswer,
		TypeReviewRequest, TypeReviewClaim, TypeReviewResult, TypeCancel,
	} {
		assert.True(t, named[sent], "ade sends %q and protocol.md does not name it", sent)
	}
}

// The fields of hello are what joins a session to its instance. A rename here
// is the difference between a run showing up under `who` and vanishing.
func TestTheHelloFieldsAdeSetsAreStillInTheSpec(t *testing.T) {
	text := spec(t)
	for _, field := range []string{"instance.id", "session", "accepts", "presence", "pid", "ancestors", "status"} {
		assert.Contains(t, text, "`"+field+"`", "hello.%s is gone from protocol.md", field)
	}
}
