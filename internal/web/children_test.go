package web

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Lines as Claude Code writes a sub-agent's transcript: one content block each.
const (
	childCall   = `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/x/agent.go"}}]}}` + "\n"
	childThinks = `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hm"}]}}` + "\n"
	childReport = `{"type":"assistant","message":{"content":[{"type":"text","text":"done, here is what I found"}]}}` + "\n"
	childPrompt = `{"type":"user","message":{"role":"user","content":"Repository: /x. Find things."}}` + "\n"
)

func childrenIn(dir string) (*children, *[]string) {
	var amended []string
	c := newChildren()
	c.dir = func() string { return dir }
	c.amend = func(msgID, doing string, steps int) {
		amended = append(amended, fmt.Sprintf("%s %d %s", msgID, steps, doing))
	}
	return c, &amended
}

func TestAChildIsFollowedThroughItsTranscript(t *testing.T) {
	dir := t.TempDir()
	c, amended := childrenIn(dir)
	c.open("tool-1", "msg-1", "Inventory ade features")
	file := filepath.Join(dir, "agent-abc.jsonl")
	require.NoError(t, os.WriteFile(file, []byte(childPrompt+childThinks+childCall), 0o600))

	require.True(t, c.poll())
	kids := c.snapshot()
	require.Len(t, kids, 1)
	assert.Equal(t, "Inventory ade features", kids[0].Name)
	assert.Equal(t, "Read: agent.go", kids[0].Doing)
	assert.Equal(t, 1, kids[0].Steps)
	assert.Equal(t, []string{"msg-1 1 Read: agent.go"}, *amended)

	// Only what was appended is read, and a line not yet whole waits.
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(childCall + `{"type":"assistant","mess`)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.True(t, c.poll())
	assert.Equal(t, 2, c.snapshot()[0].Steps)
	require.True(t, c.poll())
	assert.Equal(t, 2, c.snapshot()[0].Steps, "a half-written line is not a step")
}

func TestFilesAreClaimedInCallOrder(t *testing.T) {
	dir := t.TempDir()
	c, _ := childrenIn(dir)
	c.open("tool-1", "msg-1", "first")
	c.open("tool-2", "msg-2", "second")
	now := time.Now()
	first := filepath.Join(dir, "agent-x.jsonl")
	second := filepath.Join(dir, "agent-y.jsonl")
	require.NoError(t, os.WriteFile(first, []byte(childCall), 0o600))
	require.NoError(t, os.WriteFile(second, []byte(childCall+childCall), 0o600))
	require.NoError(t, os.Chtimes(first, now, now))
	require.NoError(t, os.Chtimes(second, now, now.Add(time.Second)))

	require.True(t, c.poll())
	kids := c.snapshot()
	require.Len(t, kids, 2)
	assert.Equal(t, 1, kids[0].Steps, "the first call took the older file")
	assert.Equal(t, 2, kids[1].Steps)
}

func TestAFileFromBeforeTheCallIsNotItsChild(t *testing.T) {
	dir := t.TempDir()
	c, _ := childrenIn(dir)
	old := filepath.Join(dir, "agent-old.jsonl")
	require.NoError(t, os.WriteFile(old, []byte(childCall), 0o600))
	stale := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(old, stale, stale))
	c.open("tool-1", "msg-1", "look")

	require.True(t, c.poll())
	assert.Equal(t, 0, c.snapshot()[0].Steps)
}

func TestALaunchNamesTheChildsTranscript(t *testing.T) {
	dir := t.TempDir()
	c, _ := childrenIn(dir)
	c.open("tool-1", "msg-1", "look")
	c.answered("tool-1", "msg-1", "look", "Async agent launched successfully. (internal)\nagentId: abc123 (internal ID)\noutput_file: /tmp/x")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-abc123.jsonl"), []byte(childCall+childCall), 0o600))

	require.True(t, c.poll())
	assert.Equal(t, 2, c.snapshot()[0].Steps)
}

func TestAChildEndsOnItsCallersResult(t *testing.T) {
	dir := t.TempDir()
	c, amended := childrenIn(dir)
	c.open("tool-1", "msg-1", "look")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-abc.jsonl"), []byte(childCall), 0o600))
	require.True(t, c.poll())

	c.answered("tool-1", "msg-1", "look", "the report, in full")
	assert.False(t, c.poll(), "nothing left to follow")
	assert.Empty(t, c.snapshot())
	assert.Equal(t, "msg-1 1 ", (*amended)[len(*amended)-1], "the row keeps the count and loses the step")
}

func TestAChildEndsOnTheNoticeThatNamesItsCall(t *testing.T) {
	dir := t.TempDir()
	c, _ := childrenIn(dir)
	c.open("tool-1", "msg-1", "look")
	c.noticed("<task-notification>\n<task-id>abc</task-id>\n<tool-use-id>tool-1</tool-use-id>\n<status>completed</status>\n</task-notification>")
	assert.False(t, c.poll())
	assert.Empty(t, c.snapshot())
}

func TestAReportWithNothingAfterItIsTheEnd(t *testing.T) {
	was := childSettle
	childSettle = 0
	defer func() { childSettle = was }()
	dir := t.TempDir()
	c, _ := childrenIn(dir)
	c.open("tool-1", "msg-1", "look")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-a.jsonl"), []byte(childCall+childReport), 0o600))

	assert.False(t, c.poll())
	assert.Empty(t, c.snapshot())
}

func TestAReportFollowedByACallIsNotTheEnd(t *testing.T) {
	was := childSettle
	childSettle = 0
	defer func() { childSettle = was }()
	dir := t.TempDir()
	c, _ := childrenIn(dir)
	c.open("tool-1", "msg-1", "look")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-a.jsonl"), []byte(childReport+childCall), 0o600))

	assert.True(t, c.poll())
	assert.Equal(t, 1, c.snapshot()[0].Steps)
}

// The result of a backgrounded command says where it writes, which is how
// one is followed: the turn that started it ends, the command does not.
const taskStarted = "Command running in background with ID: bkj9. Output is being written to: %s. You will be notified when it completes."

func TestABackgroundedCommandIsFollowedThroughItsOutput(t *testing.T) {
	dir := t.TempDir()
	c, amended := childrenIn(dir)
	out := filepath.Join(dir, "bkj9.output")
	require.NoError(t, os.WriteFile(out, []byte("go: downloading\nok internal/bridge\n"), 0o600))
	c.answered("tool-1", "msg-1", "go test ./...", fmt.Sprintf(taskStarted, out))

	require.True(t, c.poll())
	kids := c.snapshot()
	require.Len(t, kids, 1)
	assert.Equal(t, childTask, kids[0].Kind)
	assert.Equal(t, "go test ./...", kids[0].Name)
	assert.Equal(t, "ok internal/bridge", kids[0].Doing, "the last line it printed")
	assert.Equal(t, 0, kids[0].Steps, "a command has no steps to count")
	assert.Equal(t, []string{"msg-1 0 ok internal/bridge"}, *amended)

	c.noticed("<task-notification>\n<tool-use-id>tool-1</tool-use-id>\n<status>completed</status>\n</task-notification>")
	assert.False(t, c.poll())
	assert.Empty(t, c.snapshot())
}

func TestACommandThatAlreadyFinishedIsNotFollowed(t *testing.T) {
	c, _ := childrenIn(t.TempDir())
	c.answered("tool-1", "msg-1", "ls", "a.go\nb.go\n")
	assert.Empty(t, c.snapshot())
	assert.False(t, c.poll())
}

func TestAnAgentAndACommandAreCountedApart(t *testing.T) {
	dir := t.TempDir()
	c, _ := childrenIn(dir)
	c.open("tool-1", "msg-1", "Inventory ade features")
	out := filepath.Join(dir, "bkj9.output")
	require.NoError(t, os.WriteFile(out, []byte("building\n"), 0o600))
	c.answered("tool-2", "msg-2", "make ui", fmt.Sprintf(taskStarted, out))

	require.True(t, c.poll())
	kids := c.snapshot()
	require.Len(t, kids, 2)
	assert.Equal(t, childAgent, kids[0].Kind)
	assert.Equal(t, childTask, kids[1].Kind)
	assert.Equal(t, "building", kids[1].Doing)
}

// A sub-agent runs inside the turn, so cancelling the turn ends it. Without
// this nothing ever reports it finished and the badge says it is still
// working, for as long as the server is up.
func TestCancellingATurnEndsItsSubAgents(t *testing.T) {
	dir := t.TempDir()
	c, amended := childrenIn(dir)
	c.open("tool-1", "msg-1", "Inventory ade features")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-a.jsonl"), []byte(childCall), 0o600))
	require.True(t, c.poll())
	require.Len(t, c.snapshot(), 1)

	assert.Equal(t, 1, c.stop(childAgent))
	assert.Empty(t, c.snapshot())
	assert.Equal(t, "msg-1 1 ", (*amended)[len(*amended)-1], "the row keeps its count and loses the step")
	assert.Equal(t, 0, c.stop(childAgent), "there is nothing left to stop")
}

// A backgrounded command is its own process and outlives the turn that
// started it, which is the whole point of backgrounding it.
func TestCancellingATurnLeavesItsCommandsRunning(t *testing.T) {
	dir := t.TempDir()
	c, _ := childrenIn(dir)
	out := filepath.Join(dir, "bkj9.output")
	require.NoError(t, os.WriteFile(out, []byte("still going\n"), 0o600))
	c.answered("tool-1", "msg-1", "go test ./...", fmt.Sprintf(taskStarted, out))
	c.open("tool-2", "msg-2", "Explore")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-a.jsonl"), []byte(childCall), 0o600))
	require.True(t, c.poll())

	assert.Equal(t, 1, c.stop(childAgent))
	kids := c.snapshot()
	require.Len(t, kids, 1)
	assert.Equal(t, childTask, kids[0].Kind)
}

// The agent leads the process group its children run in, so its going takes
// them with it whatever their own files last said.
func TestNothingSurvivesTheAgentItRanIn(t *testing.T) {
	dir := t.TempDir()
	c, _ := childrenIn(dir)
	out := filepath.Join(dir, "bkj9.output")
	require.NoError(t, os.WriteFile(out, []byte("going\n"), 0o600))
	c.answered("tool-1", "msg-1", "go test ./...", fmt.Sprintf(taskStarted, out))
	c.open("tool-2", "msg-2", "Explore")
	require.True(t, c.poll())
	require.Len(t, c.snapshot(), 2)

	alive := true
	c.alive = func() bool { return alive }
	require.True(t, c.poll(), "an agent still up changes nothing")
	alive = false
	assert.False(t, c.poll())
	assert.Empty(t, c.snapshot())
}
