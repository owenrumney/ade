package netcheck

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnOpenSSHPortIsReachable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	got := dialHost(context.Background(), "pi", listener.Addr().String(), "darwin", "Visual Studio Code")
	assert.True(t, got.Reachable)
	assert.Empty(t, got.Err)
	assert.Empty(t, got.Hint)
	assert.Contains(t, got.Line(), "pi ssh port open")
}

func TestAClosedPortIsTheMachineAndGetsNoHint(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())

	got := dialHost(context.Background(), "pi", address, "darwin", "Visual Studio Code")
	assert.False(t, got.Reachable)
	assert.NotEmpty(t, got.Err)
	assert.Empty(t, got.Hint, "connection refused is the far end answering, not macOS")
}

func TestTheRefusalHintNamesTheEditorAndNeverAde(t *testing.T) {
	noRoute := &net.OpError{Op: "dial", Err: syscall.EHOSTUNREACH}
	tests := []struct {
		name string
		goos string
		err  error
		app  string
		want []string
	}{
		{name: "the editor's row", goos: "darwin", err: noRoute, app: "Visual Studio Code",
			want: []string{"turn on Visual Studio Code", "Local Network", "nc -vz -G 3 192.168.1.9 22", "ade has no row"}},
		{name: "no app above ade", goos: "darwin", err: noRoute,
			want: []string{"the application that started ade", "Local Network"}},
		{name: "wrapped", goos: "darwin", err: fmt.Errorf("dial: %w", noRoute), app: "Cursor",
			want: []string{"turn on Cursor"}},
		{name: "not a mac", goos: "linux", err: noRoute, app: "Code"},
		{name: "a timeout is not a refusal", goos: "darwin", err: context.DeadlineExceeded, app: "Code"},
		{name: "a closed port is not a refusal", goos: "darwin", err: syscall.ECONNREFUSED, app: "Code"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := refusalHint(tt.goos, tt.err, tt.app, "192.168.1.9:22")
			if len(tt.want) == 0 {
				assert.Empty(t, got)
				return
			}
			for _, want := range tt.want {
				assert.Contains(t, got, want)
			}
			assert.NotContains(t, got, "turn on ade")
		})
	}
}

func TestTheResponsibleAppIsTheOutermostBundle(t *testing.T) {
	tests := []struct {
		comm string
		want string
	}{
		{"/Applications/Visual Studio Code.app/Contents/MacOS/Electron", "Visual Studio Code"},
		{"/Applications/Visual Studio Code.app/Contents/Frameworks/Code Helper (Plugin).app/Contents/MacOS/Code Helper (Plugin)", "Visual Studio Code"},
		{"/System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal", "Terminal"},
		{"/Users/owen/go/bin/ade", ""},
		{"-zsh", ""},
	}
	for _, tt := range tests {
		t.Run(tt.comm, func(t *testing.T) {
			assert.Equal(t, tt.want, appOf(tt.comm))
		})
	}
}

func TestAnAliasDialsWhereSSHWouldGo(t *testing.T) {
	config := "user owen\nhostname 192.168.1.9\nport 2222\nidentityfile ~/.ssh/id\n"
	assert.Equal(t, "192.168.1.9:2222", addressFrom(config, "pi"))
	assert.Equal(t, "pi:22", addressFrom("", "pi"), "no answer from ssh falls back to the name")
}
