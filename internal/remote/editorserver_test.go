package remote

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArchIsTheNameTheDownloadTemplateUses(t *testing.T) {
	tests := []struct {
		uname string
		arch  string
		fails string
	}{
		{uname: "Linux aarch64", arch: "arm64"},
		{uname: "Linux x86_64", arch: "x64"},
		{uname: "Linux armv7l", fails: "arm64 and x64 only"},
		{uname: "Darwin arm64", fails: "arm64 and x64 only"},
		{uname: "Linux", fails: "did not say what it is"},
	}
	for _, tc := range tests {
		t.Run(tc.uname, func(t *testing.T) {
			host := testHost(t, &recorder{reply: says(tc.uname + "\n")})
			system, arch, err := host.Arch(context.Background())
			if tc.fails != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.fails)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "linux", system)
			assert.Equal(t, tc.arch, arch)
		})
	}
}

func TestTheServerIsStartedOnTheHostsLoopbackWithItsTokenOffArgv(t *testing.T) {
	rec := &recorder{reply: says("pid=4120\nport=39107\ntoken=abc\n")}
	host := testHost(t, rec)

	server, err := host.EditorServer(context.Background(), EditorServerWant{
		Commit: "08d4889f9ec4a1685d257b9b95de036c8e1ce1e5",
		URL:    "https://github.com/owenrumney/ade/releases/download/1.135.06783/ade-reh-linux-arm64-1.135.06783.tar.gz",
	})
	require.NoError(t, err)
	assert.Equal(t, 39107, server.Port)
	assert.Equal(t, "abc", server.Token)

	command := rec.calls[0][len(rec.calls[0])-1]
	assert.Contains(t, command, "--host 127.0.0.1")
	assert.Contains(t, command, "--port 0")
	assert.Contains(t, command, "--connection-token-file")
	assert.Contains(t, command, "--enable-remote-auto-shutdown")
	assert.NotContains(t, command, "--disable-client-validation")
	// The token the server will read is written from stdin, so it is in
	// neither this argv nor the host's process list.
	require.Len(t, rec.input, 1)
	fresh := strings.TrimSpace(string(rec.input[0]))
	assert.Len(t, fresh, 64)
	assert.NotContains(t, command, fresh)
}

func TestAnUnvalidatedConnectIsOneFlagOnOneResolve(t *testing.T) {
	rec := &recorder{reply: says("pid=1\nport=39107\ntoken=abc\n")}
	host := testHost(t, rec)

	_, err := host.EditorServer(context.Background(), EditorServerWant{
		Commit: "1a46a584", URL: "https://example.invalid/x.tar.gz", Unvalidated: true,
	})
	require.NoError(t, err)
	assert.Contains(t, rec.calls[0][len(rec.calls[0])-1], "--disable-client-validation")
}

func TestACommitThatWouldNotNameATreeIsRefusedBeforeAnySsh(t *testing.T) {
	rec := &recorder{reply: says("")}
	host := testHost(t, rec)

	for _, bad := range []string{"", "../../etc", "head", "08d4889f; rm -rf /"} {
		_, err := host.EditorServer(context.Background(), EditorServerWant{Commit: bad, URL: "https://x/y"})
		assert.Error(t, err, bad)
	}
	assert.Empty(t, rec.calls)
}

func TestAServerThatNeverSaysWhereItIsListeningIsAnError(t *testing.T) {
	host := testHost(t, &recorder{reply: says("pid=4120\n")})
	_, err := host.EditorServer(context.Background(), EditorServerWant{Commit: "08d4889", URL: "https://x/y"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listening")
}

func TestThePruneKeepsThisCommitThePreviousOneAndAnythingLive(t *testing.T) {
	const current = "08d4889f9ec4a1685d257b9b95de036c8e1ce1e5"
	const previous = "1a46a5840000000000000000000000000000000f"
	const older = "cafe123cafe123cafe123cafe123cafe123cafe1"
	const live = "beef456beef456beef456beef456beef456beef4"

	listing := current + " live\n" + previous + " dead\n" + live + " live\n" + older + " dead\n"
	assert.Equal(t, []string{older}, editorServersToDrop(current, listing))

	// Nothing of this commit on the host yet: the newest tree is still the one
	// before it, so reopening the last window does not re-download.
	assert.Equal(t, []string{older}, editorServersToDrop(current, previous+" dead\n"+older+" dead\n"))

	// A tree whose name is not a commit is left alone rather than removed.
	assert.Empty(t, editorServersToDrop(current, "notacommit dead\n"))
	assert.Empty(t, editorServersToDrop(current, ""))
}

func TestThePruneDeletesByNameRatherThanByGlob(t *testing.T) {
	const current = "08d4889f9ec4a1685d257b9b95de036c8e1ce1e5"
	const previous = "1a46a5840000000000000000000000000000000f"
	const stale = "cafe123cafe123cafe123cafe123cafe123cafe1"
	rec := &recorder{reply: says(current + " live\n" + previous + " dead\n" + stale + " dead\n")}
	host := testHost(t, rec)

	require.NoError(t, host.PruneEditorServers(context.Background(), current))
	require.Len(t, rec.calls, 2)
	command := rec.calls[1][len(rec.calls[1])-1]
	assert.True(t, hasArgs(command, stale), command)
	assert.NotContains(t, command, "*")
}

func TestAHostWithNothingToPruneRunsNoRemove(t *testing.T) {
	rec := &recorder{reply: says("")}
	require.NoError(t, testHost(t, rec).PruneEditorServers(context.Background(), "08d4889"))
	assert.Len(t, rec.calls, 1)
}

func TestStoppingTheServerChecksItIsTheOneThisCommitStarted(t *testing.T) {
	rec := &recorder{reply: says("")}
	host := testHost(t, rec)
	require.NoError(t, host.StopEditorServer(context.Background(), "08d4889"))
	command := rec.calls[0][len(rec.calls[0])-1]
	assert.Contains(t, command, "/proc/$pid/cmdline")
	assert.Contains(t, command, "kill")
}
