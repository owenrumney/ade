package bridge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A tool run is a grandchild: the agent starts a shell, the shell starts the
// build. Killing the agent alone leaves it, which is how a laptop wedges.
func TestKillingTheAgentTakesWhatItStarted(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "alive")
	script := "sh -c 'while :; do touch " + marker + "; sleep 0.1; done' & sleep 60"

	cmd := exec.CommandContext(context.Background(), "sh", "-c", script)
	ownGroup(cmd)
	require.NoError(t, cmd.Start())

	require.Eventually(t, func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}, 3*time.Second, 50*time.Millisecond, "the grandchild never started")

	killGroup(cmd)
	_ = cmd.Wait()

	// It touches the marker ten times a second, so a gap means it is gone.
	require.NoError(t, os.Remove(marker))
	time.Sleep(500 * time.Millisecond)
	_, err := os.Stat(marker)
	require.ErrorIs(t, err, os.ErrNotExist, "the grandchild outlived the agent")
}

func TestTheAgentGetsAGroupOfItsOwn(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "sleep", "5")
	ownGroup(cmd)
	require.NoError(t, cmd.Start())
	defer func() { killGroup(cmd); _ = cmd.Wait() }()

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	require.NoError(t, err)
	require.Equal(t, cmd.Process.Pid, pgid, "the agent did not lead its own group")
	require.NotEqual(t, syscall.Getpid(), pgid, "it shares ade's group, so a kill would take ade too")
}

func TestAHostAgentLeadsAGroupAndAContainerClientDoesNot(t *testing.T) {
	host := exec.CommandContext(context.Background(), "sleep", "30")
	ownGroup(host)
	require.NoError(t, host.Start())
	defer killGroup(host)
	require.Equal(t, host.Process.Pid, groupOf(host))

	boxed := exec.CommandContext(context.Background(), "sleep", "30")
	require.NoError(t, boxed.Start())
	defer func() { _ = boxed.Process.Kill() }()
	require.Zero(t, groupOf(boxed))
	require.Zero(t, groupOf(nil))
}

func TestAContainerGetsTheGoThrottleWithoutTouchingTheSpec(t *testing.T) {
	spec := map[string]string{"GOFLAGS": "-mod=mod"}
	got := capped(spec)
	require.Contains(t, got["GOFLAGS"], "-mod=mod -p=")
	require.Equal(t, "-mod=mod", spec["GOFLAGS"])
}
