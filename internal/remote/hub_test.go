package remote

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hasArgs reports every word appearing as its own quoted argument of the
// login-shell command ssh is handed.
func hasArgs(command string, args ...string) bool {
	for _, arg := range args {
		if !strings.Contains(command, `'\''`+arg+`'\''`) {
			return false
		}
	}
	return true
}

// hubReplies answers docker for a hub that is, or is not, already serving.
func hubReplies(running bool, calls *[]string) func([]string) ([]byte, []byte, error) {
	return func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		*calls = append(*calls, command)
		switch {
		case strings.Contains(command, "'pull'"):
			return []byte(sentinel), nil, nil
		case hasArgs(command, "image", "inspect"):
			return []byte(sentinel + "sha256:same\n"), nil, nil
		case strings.Contains(command, "Config.Labels"):
			if !running {
				return nil, nil, errors.New("no such container")
			}
			return []byte(sentinel + "sha256:same\n"), nil, nil
		case hasArgs(command, "inspect", "ade-hub"):
			if !running {
				return nil, []byte("no such container"), errors.New("exit status 1")
			}
			return []byte(sentinel + "true\n"), nil, nil
		case hasArgs(command, "network", "inspect"):
			return nil, []byte("not found"), errors.New("exit status 1")
		case strings.Contains(command, "'exec'"):
			if !running {
				return nil, nil, errors.New("no such container")
			}
			return []byte(sentinel + `{"mcpHub":true,"pid":1}`), nil, nil
		case strings.Contains(command, "'run'"):
			running = true
		}
		return []byte(sentinel), nil, nil
	}
}

func TestStartHubAdoptsAHubThatAnswers(t *testing.T) {
	var calls []string
	rec := &recorder{reply: hubReplies(true, &calls)}
	host := testHost(t, rec)
	require.NoError(t, host.StartHub(context.Background(), fhsRoots, Login{User: "owen", Token: "token"}))
	for _, call := range calls {
		assert.NotContains(t, call, "'run'", "a serving hub is kept, not replaced")
	}
}

func TestStartHubMakesTheNetworkAndStartsOnIt(t *testing.T) {
	was := workerWait
	workerWait = 200 * time.Millisecond
	t.Cleanup(func() { workerWait = was })
	var calls []string
	rec := &recorder{reply: hubReplies(false, &calls)}
	host := testHost(t, rec)
	require.NoError(t, host.StartHub(context.Background(), fhsRoots, Login{User: "owenrumney", Token: "github-token"}))

	joined := strings.Join(calls, "\n")
	assert.True(t, hasArgs(joined, "network", "create", "ade"), joined)
	run := ""
	for i, call := range calls {
		if strings.Contains(call, "'run'") {
			run = call
			assert.Equal(t, "", string(rec.input[i]), "nothing secret rides on the run's stdin")
		}
		if strings.Contains(call, "'login'") {
			assert.Equal(t, "github-token", string(rec.input[i]), "the registry token goes on stdin")
			assert.NotContains(t, call, "github-token")
			assert.True(t, hasArgs(call, "-u", "owenrumney"), "the real login, which a private package checks")
		}
	}
	require.NotEmpty(t, run)
	assert.Contains(t, run, HubImage)
	assert.True(t, hasArgs(run, "--network", "ade"), run)
	assert.Contains(t, run, "MCP_HUB_HOME=/srv/ade/.hub")
	assert.True(t, hasArgs(run, "127.0.0.1:4867"), "the hub stays on loopback")
	assert.True(t, hasArgs(run, "serve"), "the image's entrypoint is mcp-hub, so it is not named again")
	assert.False(t, hasArgs(run, "mcp-hub"), "naming the binary after the image runs mcp-hub mcp-hub")
	assert.False(t, hasArgs(run, "-p"), "the hub's port is never published")
}

// hubRun answers docker for a hub that is not there yet, with the run under
// the caller's control: that is where a name conflict shows up.
func hubRun(run func() ([]byte, []byte, error), inspect func() ([]byte, []byte, error), serving *bool) func([]string) ([]byte, []byte, error) {
	return func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		switch {
		case hasArgs(command, "container", "inspect"):
			return inspect()
		case strings.Contains(command, "'run'"):
			return run()
		case hasArgs(command, "image", "inspect"):
			return []byte(sentinel + "sha256:same\n"), nil, nil
		case strings.Contains(command, "'exec'"):
			if !*serving {
				return nil, nil, errors.New("no such container")
			}
			return []byte(sentinel + `{"mcpHub":true,"pid":1}`), nil, nil
		case hasArgs(command, "inspect", hubName), hasArgs(command, "network", "inspect"):
			return nil, []byte("no such container"), errors.New("exit status 1")
		}
		return []byte(sentinel), nil, nil
	}
}

// docker's rm returns when the kill is sent, not when the name is released, so
// the hub's own run can lose to its own cleanup exactly as the worker's does.
func TestTheHubRetriesTheRunThatLostToItsOwnCleanup(t *testing.T) {
	was, wasName := workerWait, nameWait
	workerWait, nameWait = 200*time.Millisecond, time.Second
	t.Cleanup(func() { workerWait, nameWait = was, wasName })

	runs, serving := 0, false
	rec := &recorder{reply: hubRun(func() ([]byte, []byte, error) {
		runs++
		if runs == 1 {
			return nil, []byte(conflictSaid), errors.New("exit status 125")
		}
		serving = true
		return []byte(sentinel + "container-id\n"), nil, nil
	}, func() ([]byte, []byte, error) {
		return nil, []byte("Error: No such container: " + hubName), errors.New("exit status 1")
	}, &serving)}

	require.NoError(t, testHost(t, rec).StartHub(context.Background(), fhsRoots, Login{}))
	assert.Equal(t, 2, runs, "the conflict was retried, not reported")
}

// The name in the advice is the hub's. The two paths were written together, so
// a copied line pointing at ade-worker is the mistake to catch.
func TestAHubNameDockerWillNotFreeSaysWhatToRunOnTheMachine(t *testing.T) {
	was, wasName := workerWait, nameWait
	workerWait, nameWait = 100*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { workerWait, nameWait = was, wasName })

	serving := false
	rec := &recorder{reply: hubRun(func() ([]byte, []byte, error) {
		return nil, []byte(conflictSaid), errors.New("exit status 125")
	}, func() ([]byte, []byte, error) {
		return []byte(sentinel + "ac12011b34b9\n"), nil, nil
	}, &serving)}

	err := testHost(t, rec).StartHub(context.Background(), fhsRoots, Login{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker rm -f "+hubName)
	assert.Contains(t, err.Error(), "restart its docker daemon")
}

func TestPrepareMCPServersKeepsTheRestOfClaudeJSON(t *testing.T) {
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		if hasArgs(argv[len(argv)-1], "exec", "ade-worker") && !strings.Contains(argv[len(argv)-1], "cat >") {
			return []byte(sentinel + `{"hasCompletedOnboarding":true,"mcpServers":{"old":{}}}`), nil, nil
		}
		return []byte(sentinel), nil, nil
	}}
	host := testHost(t, rec)
	servers := map[string]json.RawMessage{"builtin-linear": json.RawMessage(`{"type":"http","url":"http://127.0.0.1:4867/mcp/servers/builtin-linear"}`)}
	require.NoError(t, host.PrepareMCPServers(context.Background(), servers))

	wrote := rec.calls[len(rec.calls)-1]
	assert.True(t, hasArgs(wrote[len(wrote)-1], "docker", "exec", "-i", "ade-worker"),
		"the worker writes its own HOME; the ssh user cannot")

	var written map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.input[len(rec.input)-1], &written))
	assert.JSONEq(t, "true", string(written["hasCompletedOnboarding"]))
	assert.Contains(t, string(written["mcpServers"]), "builtin-linear")
	assert.NotContains(t, string(written["mcpServers"]), `"old"`, "the laptop's list is the list")
}

func TestPrepareHubShipsTheDatabaseWhenTheMachineHasNone(t *testing.T) {
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		if hasArgs(argv[len(argv)-1], "test", "-s") {
			return nil, nil, errors.New("exit status 1")
		}
		return []byte(sentinel), nil, nil
	}}
	host := testHost(t, rec)
	require.NoError(t, host.PrepareHub(context.Background(), fhsRoots, HubState{DB: []byte("sqlite"), WAL: []byte("wal")}))
	var sent []string
	for i, call := range rec.calls {
		if command := call[len(call)-1]; strings.Contains(command, "'cat' >") || strings.Contains(command, "cat > ") {
			sent = append(sent, command+"="+string(rec.input[i]))
		}
	}
	require.Len(t, sent, 2)
	assert.Contains(t, sent[0], "/srv/ade/.hub/mcp-hub.db")
	assert.Contains(t, sent[0], "=sqlite")
	assert.Contains(t, sent[1], "mcp-hub.db-wal")
	assert.Contains(t, sent[1], "=wal")
}

func TestPrepareHubWithNothingToShipRunsNothing(t *testing.T) {
	rec := &recorder{}
	host := testHost(t, rec)
	require.NoError(t, host.PrepareHub(context.Background(), fhsRoots, HubState{}))
	assert.Empty(t, rec.calls)
}
