package transcripts

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// breakdownFixture is a session that went one way and then another: two tools
// with very different appetites, a subagent doing its own reading, and a
// compaction in the middle that dropped the first half of it.
const breakdownFixture = `{"type":"user","uuid":"u1","message":{"role":"user","content":"look at the pricing table"}}
{"type":"assistant","uuid":"a1","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/work/pricing.go"}}]}}
{"type":"user","uuid":"u2","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"AAAAAAAAAAAAAAAA"}]}}
{"type":"user","uuid":"s1","isSidechain":true,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"}]}}
{"type":"system","subtype":"compact_boundary","uuid":"c1","compactMetadata":{"preservedSegment":{"headUuid":"a2"}}}
{"type":"user","uuid":"sum1","isCompactSummary":true,"message":{"role":"user","content":"SSSSSSSS"}}
{"type":"assistant","uuid":"a2","message":{"role":"assistant","content":[{"type":"text","text":"rrrr"},{"type":"tool_use","id":"t2","name":"Bash","input":{}}]}}
{"type":"user","uuid":"u3","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"CCCCCCCCCCCCCCCCCCCCCCCC"}]}}
{"type":"user","uuid":"u4","message":{"role":"user","content":"and the discounts"}}
`

func breakdownFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeFixture(t, path, body)
	return path
}

func breakdownOf(t *testing.T, body string) map[string]int {
	t.Helper()
	byLabel := map[string]int{}
	for _, part := range ContextBreakdown(breakdownFile(t, body)) {
		byLabel[part.Label] = part.Tokens
	}
	return byLabel
}

func TestABreakdownGroupsToolOutputByTheToolThatProducedIt(t *testing.T) {
	parts := breakdownOf(t, breakdownFixture)
	assert.Equal(t, 6, parts["Bash"], "24 characters of result and an empty call")
	assert.Equal(t, 2, parts["compacted summary"])
	assert.Equal(t, 1, parts["replies"])
}

func TestABreakdownForgetsWhatACompactionReplaced(t *testing.T) {
	parts := breakdownOf(t, breakdownFixture)
	assert.NotContains(t, parts, "Read", "the Read happened before the compaction that dropped it")
}

// The preserved segment survives its compaction: the boundary names where the
// tail the summary did not replace begins, and that tail is still in the window.
func TestABreakdownKeepsWhatACompactionPreserved(t *testing.T) {
	kept := strings.Replace(breakdownFixture, `"headUuid":"a2"`, `"headUuid":"a1"`, 1)
	parts := breakdownOf(t, kept)
	assert.Equal(t, 12, parts["Read"], "16 characters of result, and the 32 of the call that asked for it")
}

// A subagent's conversation is its own. Only what it hands back arrives here,
// and that arrives as an ordinary tool result on this conversation.
func TestABreakdownIgnoresASubagentsOwnConversation(t *testing.T) {
	one := strings.Replace(breakdownFixture, `"headUuid":"a2"`, `"headUuid":"a1"`, 1)
	assert.Equal(t, 12, breakdownOf(t, one)["Read"], "the sidechain's own 40 characters are not this window's")
}

func TestABreakdownPutsTheBiggestContributorFirst(t *testing.T) {
	parts := ContextBreakdown(breakdownFile(t, breakdownFixture))
	require.NotEmpty(t, parts)
	for i := 1; i < len(parts); i++ {
		assert.GreaterOrEqual(t, parts[i-1].Tokens, parts[i].Tokens, "%v", parts)
	}
	assert.Equal(t, "Bash", parts[0].Label)
}

// A result whose call is no longer in the transcript still occupies the window.
// It has no tool to be grouped under, so it goes in with the rest of them.
func TestABreakdownStillCountsAResultWhoseCallItNeverSaw(t *testing.T) {
	orphan := `{"type":"user","uuid":"u1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"gone","content":"DDDDDDDD"}]}}` + "\n"
	assert.Equal(t, 2, breakdownOf(t, orphan)["tool output"])
}

func TestABreakdownOfAMissingTranscriptIsEmpty(t *testing.T) {
	assert.Empty(t, ContextBreakdown(filepath.Join(t.TempDir(), "nope.jsonl")))
}
