package bridge

import (
	"os/exec"
	"syscall"

	"github.com/owenrumney/ade/internal/spawncap"
)

// ownGroup puts a subprocess in a process group of its own, so what it starts
// can be signalled with it. Without this a tool run outlives the agent.
func ownGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	// CommandContext would otherwise signal the leader alone, which is the
	// one process that was never the problem.
	cmd.Cancel = func() error {
		killGroup(cmd)
		return nil
	}
}

// killGroup signals the whole tree. A agent's own exit says nothing about the
// `go test` it left running, which is what wedges the machine.
func killGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	// The group id is the leader's pid, which ownGroup made this process.
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill()
	}
}

// capped is a container's environment with the go throttle on it. The map is
// the spec's own, so it is copied.
func capped(env map[string]string) map[string]string {
	return withEnv(env, []string{"GOFLAGS=" + spawncap.GoFlags(env["GOFLAGS"])})
}

// groupOf is the process group a command leads, or zero when it leads none:
// a container's client is not the tree, and --cpus holds that one.
func groupOf(cmd *exec.Cmd) int {
	if cmd == nil || cmd.Process == nil || cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		return 0
	}
	return cmd.Process.Pid
}

func (c *Claude) Group() int { return groupOf(c.cmd) }

func (c *Codex) Group() int { return groupOf(c.cmd) }
