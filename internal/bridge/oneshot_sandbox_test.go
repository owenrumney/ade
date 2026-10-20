package bridge

import (
	"context"
	"testing"

	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRuntime struct{}

func (fakeRuntime) Name() string            { return "fake" }
func (fakeRuntime) Bin() string             { return "/bin/echo" }
func (fakeRuntime) HasArgs(string) []string { return []string{"true"} }

// Args is the real runtimes' shape: the command, then what the spec carries.
func (fakeRuntime) Args(spec sandbox.Spec) []string {
	return append(append([]string{}, spec.Argv...), spec.Args...)
}

func TestAOneShotWithASandboxRunsTheRuntimeNotTheHostBinary(t *testing.T) {
	opts := OneShotOpts{
		Bin:     "/no/such/claude",
		Prompt:  "hello",
		Model:   "opus",
		Agent:   "security",
		Sandbox: &sandbox.Spec{Image: "img"},
		Runtime: fakeRuntime{},
	}
	cmd, err := claudeCommand(context.Background(), box{opts.Sandbox, opts.Runtime, opts.Bin, opts.Dir, nil}, oneShotArgs(opts))
	require.NoError(t, err)
	assert.Equal(t, "/bin/echo", cmd.Path)
	// The image's claude, and the same arguments the host run would get.
	assert.Contains(t, cmd.Args, "claude")
	assert.NotContains(t, cmd.Args, "/no/such/claude")
	assert.Contains(t, cmd.Args, "--agent")
	assert.Contains(t, cmd.Args, "security")
}

func TestASandboxedNoMCPRunDoesNotGetTheBoxesServers(t *testing.T) {
	opts := OneShotOpts{Prompt: "hello", NoMCP: true}
	spec := &sandbox.Spec{Image: "img", Args: []string{"--mcp-config", "/box/ade-mcp.json"}}
	cmd, err := claudeCommand(context.Background(), box{spec: spec, runtime: fakeRuntime{}}, oneShotArgs(opts))
	require.NoError(t, err)
	assert.NotContains(t, cmd.Args, "/box/ade-mcp.json", "a second config merges the servers back in")
	strict := 0
	for _, arg := range cmd.Args {
		if arg == "--strict-mcp-config" {
			strict++
		}
	}
	assert.Equal(t, 1, strict)
}

func TestASandboxedRunKeepsTheBoxesServersWhenItAskedForThem(t *testing.T) {
	spec := &sandbox.Spec{Image: "img", Args: []string{"--mcp-config", "/box/ade-mcp.json"}}
	cmd, err := claudeCommand(context.Background(), box{spec: spec, runtime: fakeRuntime{}}, oneShotArgs(OneShotOpts{Prompt: "hello"}))
	require.NoError(t, err)
	assert.Contains(t, cmd.Args, "/box/ade-mcp.json")
}

func TestASandboxWithoutARuntimeIsRefused(t *testing.T) {
	_, err := claudeCommand(context.Background(), box{spec: &sandbox.Spec{Image: "img"}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs a runtime")
}
