package term

import "syscall"

// killGroup kills the shell and everything it started. The pty made the shell a
// session leader, so the negative pid reaches the whole group — killing the
// shell alone would leave a running server or editor orphaned.
func killGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}
