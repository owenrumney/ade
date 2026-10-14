package remote

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallingAgentsRunsInTheWorkerBox(t *testing.T) {
	var ran string
	h := &Host{Name: "pi", ssh: func(_ context.Context, argv []string, _ []byte) ([]byte, []byte, error) {
		ran = strings.Join(argv, " ")
		return nil, nil, nil
	}}

	require.NoError(t, h.PrepareAgents(context.Background()))
	// Run quotes every word for the login shell, so the tokens are checked
	// one at a time rather than as a line.
	for _, word := range []string{"docker", "exec", workerName, "ade", "agents", "install"} {
		assert.Contains(t, ran, word)
	}
	assert.Less(t, strings.Index(ran, workerName), strings.Index(ran, "install"),
		"the container is named before the command it runs")
}

func TestAFailedAgentInstallNamesTheMachine(t *testing.T) {
	h := &Host{Name: "pi", ssh: func(context.Context, []string, []byte) ([]byte, []byte, error) {
		return nil, nil, errors.New("no such container")
	}}

	err := h.PrepareAgents(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pi", "a warning that does not say which machine is no use")
}
