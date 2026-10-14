package remote

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

// LocalName is the host name this machine answers to in the picker.
const LocalName = "local"

// NewLocal is this machine as a Host. The ssh transport is the only thing that
// differs: every capability check and container start above it is unchanged.
func NewLocal() *Host {
	return &Host{Name: LocalName, local: true, ssh: localRunner}
}

// Local reports a host that is this machine.
func (h *Host) Local() bool { return h != nil && h.local }

// localRunner runs what the ssh runner would have sent, without ssh. It is
// handed the same argv, so it unwraps the login-shell wrapper `login` built.
func localRunner(ctx context.Context, argv []string, stdin []byte) ([]byte, []byte, error) {
	command := localCommand(argv)
	if command == "" {
		// A control-socket operation: there is no master to check or close.
		return nil, nil, nil
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command) // #nosec G204 -- argv built here, as for ssh
	var out, errOut bytes.Buffer
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	return out.Bytes(), errOut.Bytes(), err
}

// localCommand is the shell command inside an ssh argv, or "" when the argv is
// ssh talking to its own control socket rather than running anything.
func localCommand(argv []string) string {
	for i, arg := range argv {
		if arg == "--" && i+1 < len(argv) {
			return unwrapLogin(argv[i+1])
		}
	}
	return ""
}

// unwrapLogin strips the login-shell wrapper and the sentinel printf. Running
// a login shell here would read this machine's profile twice.
func unwrapLogin(command string) string {
	const prefix = `exec "${SHELL:-/bin/sh}" -lc `
	inner := command
	if stripped, ok := strings.CutPrefix(command, prefix); ok {
		inner = unquote(stripped)
	}
	if _, after, found := strings.Cut(inner, "printf %s "+quote(sentinel)+"; "); found {
		return after
	}
	return inner
}

// unquote reverses quote for a single fully-quoted word.
func unquote(s string) string {
	if len(s) < 2 || s[0] != '\'' || s[len(s)-1] != '\'' {
		return s
	}
	return strings.ReplaceAll(s[1:len(s)-1], `'\''`, "'")
}
