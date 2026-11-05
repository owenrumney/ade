package main

import (
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
)

func TestChildAlwaysRunsInTheForeground(t *testing.T) {
	// ade backgrounds itself by default. The child it spawns IS the background
	// process, so if it did not know that, every server would fork another and
	// keep going.
	args := childArgs(settings{idle: 30 * time.Minute})

	require.Contains(t, args, "--foreground")
	assert.Equal(t, "--foreground", args[0], "before anything that could fail to parse")
	assert.NotContains(t, args, "--detach")
	assert.NotContains(t, args, "-d")
}

func TestChildArgsCarryTheRunAcross(t *testing.T) {
	cfg := settings{
		sessionID: "sess-1",
		model:     "haiku",
		idle:      5 * time.Minute,
		prNumber:  7,
		diff:      gitdiff.Options{Scope: gitdiff.ScopeCommitted, Base: "main", Context: 5, Rev: "abc"},
	}
	args := childArgs(cfg)

	for _, pair := range [][2]string{
		{"--session", "sess-1"},
		{"--model", "haiku"},
		{"--idle", "5m0s"},
		{"--base", "main"},
		{"--context", "5"},
		{"--pr", "7"},
	} {
		i := slices.Index(args, pair[0])
		require.GreaterOrEqualf(t, i, 0, "%s missing", pair[0])
		require.Lessf(t, i+1, len(args), "%s has no value", pair[0])
		assert.Equalf(t, pair[1], args[i+1], "%s", pair[0])
	}

	assert.Contains(t, args, "--committed")
	assert.Contains(t, args, "--no-open", "the parent opens the browser; the child has no terminal to report to")
	assert.Equal(t, "abc", args[len(args)-1], "a revspec is positional and goes last")
}

func TestAgentSelectsProvider(t *testing.T) {
	for _, test := range []struct {
		name     string
		agent    string
		legacy   string
		agentSet bool
		want     string
		err      string
	}{
		{name: "default", agent: "claude", want: "claude"},
		{name: "codex", agent: "codex", agentSet: true, want: "codex"},
		{name: "legacy spelling", agent: "codex", legacy: "pi", agentSet: true, want: "codex"},
		{name: "conflict", agent: "codex", legacy: "claude", agentSet: true, err: "conflicts"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := providerFor(test.agent, test.legacy, test.agentSet)
			if test.err != "" {
				require.ErrorContains(t, err, test.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestChildNamesTheSelectedAgent(t *testing.T) {
	args := childArgs(settings{provider: "codex", idle: time.Minute})
	i := slices.Index(args, "--agent")
	require.GreaterOrEqual(t, i, 0)
	require.Less(t, i+1, len(args))
	assert.Equal(t, "codex", args[i+1])
}

func TestChildArgsOmitWhatWasNotAsked(t *testing.T) {
	args := childArgs(settings{idle: 30 * time.Minute})

	for _, unwanted := range []string{"--session", "--model", "--pr", "--base", "--committed"} {
		assert.NotContainsf(t, args, unwanted, "%s should not be passed when unset", unwanted)
	}
	// The flag is gone: a run on the host is gated, and the ask has somewhere
	// to land.
	assert.NotContains(t, args, "--skip-permissions=false")
}

func TestTheAgentDoesNotInheritTheCallersConversation(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "the-callers-session")

	// Run ade from inside a Claude Code session and the agent used to resume
	// that conversation. The transcript only grows, so a long one replayed into
	// the agent's smaller window and compacted every few minutes — and both
	// processes then appended to the same session file.
	for _, args := range [][]string{
		{"--no-open"},
		{"create", "cart-redesign", "--no-open"},
	} {
		cmd := rootCmd()
		cmd.SetArgs(args)
		require.NoError(t, cmd.ParseFlags(args))

		session, err := cmd.Flags().GetString("session")
		require.NoError(t, err)
		assert.Emptyf(t, session, "ade %v picked up the caller's session", args)
	}
}

func TestASessionCanStillBeResumedOnPurpose(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "the-callers-session")

	// The launcher resumes a checkout's own conversation this way, which is a
	// deliberate choice rather than an accident of where ade was started.
	args := []string{"--no-open", "--session", "a-checkouts-session"}
	cmd := rootCmd()
	cmd.SetArgs(args)
	require.NoError(t, cmd.ParseFlags(args))

	session, err := cmd.Flags().GetString("session")
	require.NoError(t, err)
	assert.Equal(t, "a-checkouts-session", session)
}

func TestIssueDocName(t *testing.T) {
	for _, tc := range []struct{ title, want string }{
		{"Cache the token", "412-cache-the-token"},
		{"  Cache   the token!! ", "412-cache-the-token"},
		{"Bug: /auth returns 500", "412-bug-auth-returns-500"},
		{"", "issue-412"},
		{"✨✨", "issue-412"},
	} {
		assert.Equalf(t, tc.want, issueDocName(412, tc.title), "%q", tc.title)
	}

	// A title is a sentence; a branch name is not.
	long := issueDocName(412, strings.Repeat("long ", 30))
	assert.LessOrEqual(t, len(long), 52)
	assert.True(t, strings.HasPrefix(long, "412-long-long-"))

	// The name becomes a branch name, so nothing may survive that git would
	// refuse or a shell would read as something else.
	for _, title := range []string{"a/b", "../../etc/passwd", "a b; rm -rf /", "-leading", "trailing-"} {
		name := issueDocName(412, title)
		assert.NotContains(t, name, "/", title)
		assert.NotContains(t, name, "..", title)
		assert.Regexp(t, `^[a-z0-9][a-z0-9-]*$`, name, title)
	}
}

func TestCreateRefusesTwoStartingPoints(t *testing.T) {
	newCreate := func() *cobra.Command {
		var s, provider, model string
		agent := "claude"
		var noOpen, foreground bool
		var idle time.Duration
		return newCreateCommand(&s, &agent, &provider, &model, &noOpen, &foreground, &idle)
	}
	for _, tc := range []struct {
		name, wants string
		args        []string
	}{
		{"issue and ticket", "pick one", []string{"--github", "412", "--linear", "FIX-1"}},
		{"issue and a name", "already names the work", []string{"--github", "412", "cart-redesign"}},
		{"nothing at all", "give the work a name", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newCreate()
			cmd.SetArgs(tc.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wants)
		})
	}
}
