package bridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeArgsPassAutoCompactOnlyWhenAsked(t *testing.T) {
	assert.NotContains(t, claudeArgs(Opts{}), "--autocompact")
	args := claudeArgs(Opts{AutoCompact: "120000", Resume: "abc"})
	at := slices.Index(args, "--autocompact")
	require.Positive(t, at)
	assert.Equal(t, "120000", args[at+1])
	assert.Contains(t, args, "--resume")
}

func TestOneShotArgs(t *testing.T) {
	args := oneShotArgs(OneShotOpts{Prompt: "hello"})
	// The prompt is on stdin, not here: Linux caps one argument at 128 KiB.
	assert.Equal(t, []string{"-p", "--input-format", "text", "--output-format", "stream-json", "--verbose"}, args)
	assert.NotContains(t, args, "hello")

	// Nothing asked for, nothing passed.
	for _, unwanted := range []string{"--model", "--allowedTools", "--dangerously-skip-permissions", "--mcp-config"} {
		assert.NotContainsf(t, args, unwanted, "%s appeared unasked", unwanted)
	}
}

// A one-shot has no stdin left open to answer a permission prompt, so it is
// gated by its allowlist. Skipping permissions instead would hand a reviewer
// every tool on the machine to save a prompt it never sees.
func TestAOneShotIsGatedByItsAllowlistRatherThanSkippingPermissions(t *testing.T) {
	args := oneShotArgs(OneShotOpts{Prompt: "x", AllowedTools: []string{"Read", "Bash"}})
	assert.NotContains(t, args, "--dangerously-skip-permissions")
	at := slices.Index(args, "--allowedTools")
	require.Positive(t, at)
	assert.Equal(t, []string{"Read", "Bash"}, args[at+1:])
}

func TestOneShotArgsRunsAsANamedAgent(t *testing.T) {
	args := oneShotArgs(OneShotOpts{Prompt: "x", Agent: "staff-reviewer", PermissionMode: "acceptEdits"})

	at := slices.Index(args, "--agent")
	require.Positive(t, at)
	assert.Equal(t, "staff-reviewer", args[at+1])
	at = slices.Index(args, "--permission-mode")
	require.Positive(t, at)
	assert.Equal(t, "acceptEdits", args[at+1])
}

func TestOneShotArgsRestrictsTheToolSet(t *testing.T) {
	args := oneShotArgs(OneShotOpts{Prompt: "x", Tools: []string{"Read", "Grep"}})
	at := slices.Index(args, "--tools")
	require.Positive(t, at)
	assert.Equal(t, []string{"Read", "Grep"}, args[at+1:at+3])

	// Nil is Claude Code's default set; empty is none at all.
	assert.NotContains(t, oneShotArgs(OneShotOpts{Prompt: "x"}), "--tools")
	none := oneShotArgs(OneShotOpts{Prompt: "x", Tools: []string{}})
	at = slices.Index(none, "--tools")
	require.Positive(t, at)
	assert.Equal(t, "", none[at+1])
}

func TestOneShotArgsPassesEachToolSeparately(t *testing.T) {
	args := oneShotArgs(OneShotOpts{Prompt: "x", AllowedTools: []string{"Read", "Grep"}})

	// One space-joined string is read as a single bogus tool name and allows
	// nothing, which is worse than allowing everything.
	at := slices.Index(args, "--allowedTools")
	assert.Equal(t, []string{"Read", "Grep"}, args[at+1:at+3])
}

func TestNoMCPStartsWithoutTheConfiguredServers(t *testing.T) {
	args := oneShotArgs(OneShotOpts{Prompt: "x", NoMCP: true})

	// Connecting to them is most of the startup cost, and a turn that only
	// transforms the text it was given has nothing to ask them.
	at := slices.Index(args, "--mcp-config")
	assert.Positive(t, at)
	assert.Equal(t, `{"mcpServers":{}}`, args[at+1])
	// Without strict, the usual config files are merged back in on top.
	assert.Contains(t, args, "--strict-mcp-config")
}

func TestAForkResumesACopyAndNeverTheSessionItself(t *testing.T) {
	pin(t, "")
	args := oneShotArgs(OneShotOpts{Prompt: "x", Fork: "sess-1", Tools: []string{"Read"}})

	at := slices.Index(args, "--resume")
	require.GreaterOrEqual(t, at, 0)
	assert.Equal(t, []string{"--resume", "sess-1", "--fork-session"}, args[at:at+3])
	assert.NotContains(t, oneShotArgs(OneShotOpts{Prompt: "x"}), "--resume")

	prompt := after(args, "--append-system-prompt")
	assert.Contains(t, prompt, writingRules, "an answer to the user, so the writing rules apply")
	assert.NotContains(t, prompt, houseRules, "a fork edits nothing")
}

func TestAOneShotCanLeaveNoTranscript(t *testing.T) {
	assert.NotContains(t, oneShotArgs(OneShotOpts{Prompt: "x"}), "--no-session-persistence")
	assert.Contains(t, oneShotArgs(OneShotOpts{Prompt: "x", NoSessionPersistence: true}), "--no-session-persistence")
}

// fakeClaude writes a stub binary that fails to authenticate for the first
// failures runs, then emits a result event.
func fakeClaude(t *testing.T, failures int) (bin string, calls func() int) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "claude")
	counter := filepath.Join(dir, "calls")
	script := fmt.Sprintf(`#!/bin/sh
echo x >> %q
n=$(wc -l < %q | tr -d ' ')
if [ "$n" -le %d ]; then
  echo "Failed to authenticate: OAuth session expired and could not be refreshed" >&2
  exit 1
fi
echo '{"type":"result","result":"done","session_id":"s1"}'
`, counter, counter, failures)
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o700))
	return bin, func() int {
		data, err := os.ReadFile(counter)
		if err != nil {
			return 0
		}
		return strings.Count(string(data), "\n")
	}
}

func TestOneShotRetriesOnceWhenTheLoginFailedBeforeAnythingRan(t *testing.T) {
	bin, calls := fakeClaude(t, 1)
	authRetryDelay = time.Millisecond
	t.Cleanup(func() { authRetryDelay = 2 * time.Second })

	turn, err := OneShot(context.Background(), OneShotOpts{Bin: bin, Prompt: "hi"})

	require.NoError(t, err)
	assert.Equal(t, "done", turn.Text)
	assert.Equal(t, 2, calls())
}

func TestOneShotGivesUpAfterTheSecondLoginFailure(t *testing.T) {
	bin, calls := fakeClaude(t, 2)
	authRetryDelay = time.Millisecond
	t.Cleanup(func() { authRetryDelay = 2 * time.Second })

	_, err := OneShot(context.Background(), OneShotOpts{Bin: bin, Prompt: "hi"})

	require.Error(t, err)
	assert.Equal(t, 2, calls())
}

func TestOneShotDoesNotRetryAFailureThatIsNotTheLogin(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\necho 'disk full' >&2\nexit 1\n"), 0o700))

	_, err := OneShot(context.Background(), OneShotOpts{Bin: bin, Prompt: "hi"})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "oauth")
}

func TestOneShotDoesNotRetryOnceTheAgentHasSaidSomething(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	script := `#!/bin/sh
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"partial"}]}}'
echo "Failed to authenticate: OAuth session expired and could not be refreshed" >&2
exit 1
`
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o700))

	var seen int
	_, err := OneShot(context.Background(), OneShotOpts{Bin: bin, Prompt: "hi", OnEvent: func(Event) { seen++ }})

	require.Error(t, err)
	assert.Equal(t, 1, seen)
}

func TestIsAuthFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"oauth expired", errors.New("OAuth session expired and could not be refreshed"), true},
		{"failed to authenticate", errors.New("Failed to authenticate"), true},
		{"api error", errors.New("authentication_error: bad token"), true},
		{"unrelated", errors.New("no space left on device"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsAuthFailure(tc.err))
		})
	}
}

// A review embeds the diff, and Linux refuses an argv entry over 128 KiB with
// "argument list too long". The prompt travels on stdin instead.
func TestTheOneShotPromptTravelsOnStdinNotInArgv(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	seen := filepath.Join(dir, "stdin")
	script := fmt.Sprintf(`#!/bin/sh
cat > %q
echo '{"type":"result","result":"done","session_id":"s1"}'
`, seen)
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o700))

	huge := strings.Repeat("x", 200*1024)
	_, err := OneShot(context.Background(), OneShotOpts{Bin: bin, Prompt: huge})
	require.NoError(t, err)

	got, err := os.ReadFile(seen)
	require.NoError(t, err)
	assert.Equal(t, huge, string(got))
}

func TestOneShotArgsPassAutoCompactOnlyWhenAsked(t *testing.T) {
	assert.NotContains(t, oneShotArgs(OneShotOpts{Prompt: "x"}), "--autocompact")
	args := oneShotArgs(OneShotOpts{Prompt: "x", AutoCompact: "auto"})
	at := slices.Index(args, "--autocompact")
	require.Positive(t, at)
	assert.Equal(t, "auto", args[at+1])
}
