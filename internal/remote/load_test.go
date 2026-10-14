package remote

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAMachineReportsWhatItIsUsing(t *testing.T) {
	rec := &recorder{reply: says("2.50\n8\n16384000\n4096000\n3\n")}
	load, err := testHost(t, rec).Load(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "pc", load.Name)
	assert.Equal(t, 8, load.Cores)
	assert.InDelta(t, 31.25, load.CPU(), 0.01, "two and a half cores of eight")
	assert.Equal(t, int64(16384000*1024), load.MemTotal, "meminfo counts kibibytes")
	assert.Equal(t, int64(12288000*1024), load.MemUsed, "available is free, not used")
	assert.Equal(t, 3, load.Agents)
}

func TestAMachineWithoutProcIsNotGuessedAt(t *testing.T) {
	for _, out := range []string{"", "0.1\n", "0.1\n8\n0\n0\n0\n", "load\n8\n16\n8\n0\n"} {
		rec := &recorder{reply: says(out)}
		_, err := testHost(t, rec).Load(context.Background())
		assert.Error(t, err, "a Mac has no /proc, and a zero would read as an idle machine")
	}
}

func TestLoadWithoutCoresIsNotDividedByZero(t *testing.T) {
	assert.Zero(t, Load{Load1: 3}.CPU())
}

func TestAMachineThatCannotCountItsCoresIsNotIdle(t *testing.T) {
	for _, out := range []string{"2.5\nN/A\n16\n8\n0\n", "2.5\n0\n16\n8\n0\n"} {
		rec := &recorder{reply: says(out)}
		_, err := testHost(t, rec).Load(context.Background())
		assert.Error(t, err, "load over no cores draws a saturated machine at zero")
	}
}

func TestTheWorkerAndTheHubAreNotAgents(t *testing.T) {
	// The awk itself, over a docker ps that holds both and one real agent.
	out, err := exec.Command("sh", "-c", "printf '%s\\n' "+
		workerName+" "+hubName+" ade-foo postgres | "+countAgents).Output()
	require.NoError(t, err)
	assert.Equal(t, "1", strings.TrimSpace(string(out)),
		"infrastructure counted as an agent reads as work nobody started")
}
