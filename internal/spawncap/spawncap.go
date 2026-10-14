// Package spawncap bounds what one agent can start, so one command cannot
// take the machine. The design is .git/ade/docs/spawn-cap.md.
package spawncap

import (
	"bytes"
	"context"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// Limit is how many processes an agent's group may hold before its turn is
// cancelled.
const Limit = 32

// Jobs is how many things a toolchain or a container may run at once.
func Jobs() int { return jobs(runtime.NumCPU()) }

func jobs(cores int) int { return max(2, cores/2) }

// Apply tells go to run Jobs binaries at a time rather than one per core.
func Apply(env []string) []string { return apply(env, Jobs()) }

// GoFlags is GOFLAGS with the cap on it. A -p already there wins.
func GoFlags(flags string) string { return goFlags(flags, Jobs()) }

// Count is how many processes are in a group. ps and not a syscall: macOS has
// no call that lists a group, and this spelling is the same on Linux.
func Count(ctx context.Context, group int) (int, error) {
	out, err := exec.CommandContext(ctx, "ps", "-A", "-o", "pgid=").Output()
	if err != nil {
		return 0, err
	}
	return count(out, group), nil
}

func apply(env []string, n int) []string {
	at, flags := -1, ""
	for i, kv := range env {
		if name, value, ok := strings.Cut(kv, "="); ok && name == "GOFLAGS" {
			at, flags = i, value
		}
	}
	set := "GOFLAGS=" + goFlags(flags, n)
	out := slices.Clone(env)
	if at < 0 {
		return append(out, set)
	}
	out[at] = set
	return out
}

func goFlags(flags string, n int) string {
	for _, flag := range strings.Fields(flags) {
		if flag == "-p" || strings.HasPrefix(flag, "-p=") {
			return flags
		}
	}
	return strings.TrimSpace(flags + " -p=" + strconv.Itoa(n))
}

func count(out []byte, group int) int {
	want, n := strconv.Itoa(group), 0
	for _, line := range bytes.Split(out, []byte("\n")) {
		if string(bytes.TrimSpace(line)) == want {
			n++
		}
	}
	return n
}
