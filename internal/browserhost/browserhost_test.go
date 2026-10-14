package browserhost

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/sessions"
)

func TestSlug(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		want string
	}{
		{"https", "https://github.com/owenrumney/ade.git", "owenrumney/ade"},
		{"https without suffix", "https://github.com/owenrumney/ade", "owenrumney/ade"},
		{"ssh scp form", "git@github.com:owenrumney/ade.git", "owenrumney/ade"},
		{"ssh url form", "ssh://git@github.com/owenrumney/ade.git", "owenrumney/ade"},
		{"case is not part of it", "https://github.com/OwenRumney/Ade.git", "owenrumney/ade"},
		{"enterprise host", "https://github.example.com/acme/widget.git", "acme/widget"},
		{"not a repository", "https://github.com", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, slug(tc.url))
		})
	}
}

func TestRemotesReadsEveryRemote(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(`[core]
	url = not-a-remote
[remote "origin"]
	url = git@github.com:owenrumney/ade.git
	fetch = +refs/heads/*:refs/remotes/origin/*
[remote "fork"]
	url = https://github.com/someone/ade.git
`), 0o600))

	assert.Equal(t,
		[]string{"git@github.com:owenrumney/ade.git", "https://github.com/someone/ade.git"},
		remotes(dir),
	)
}

func TestRemotesOfSomethingThatIsNotACheckout(t *testing.T) {
	assert.Nil(t, remotes(t.TempDir()))
}

func TestServeAnswersAPing(t *testing.T) {
	out := &bytes.Buffer{}
	require.NoError(t, Serve(context.Background(), message(t, Request{Action: "ping"}), out, "v1.2.3"))

	got := reply(t, out)
	assert.True(t, got.OK)
	assert.Equal(t, "v1.2.3", got.Version)
}

func TestServeRejectsAnUnknownAction(t *testing.T) {
	out := &bytes.Buffer{}
	require.NoError(t, Serve(context.Background(), message(t, Request{Action: "rm -rf"}), out, "v1"))

	got := reply(t, out)
	assert.False(t, got.OK)
	assert.Contains(t, got.Error, "unknown action")
}

func TestServeRefusesAPullRequestWithoutANumber(t *testing.T) {
	out := &bytes.Buffer{}
	req := Request{Action: "open", Owner: "owenrumney", Repo: "ade"}
	require.NoError(t, Serve(context.Background(), message(t, req), out, "v1"))

	got := reply(t, out)
	assert.False(t, got.OK)
	assert.Contains(t, got.Error, "number")
}

func TestServeAnswersEveryMessageOnTheStream(t *testing.T) {
	// A connectNative port stays open, so the host has to keep reading rather
	// than answering once and exiting.
	in := &bytes.Buffer{}
	in.Write(framed(t, Request{Action: "ping"}))
	in.Write(framed(t, Request{Action: "ping"}))

	out := &bytes.Buffer{}
	require.NoError(t, Serve(context.Background(), in, out, "v1"))

	for i := 0; i < 2; i++ {
		assert.True(t, reply(t, out).OK, "response %d", i+1)
	}
	assert.Zero(t, out.Len(), "nothing but the two responses")
}

func TestServeStopsOnAMessageThatIsNotOurs(t *testing.T) {
	var header [4]byte
	binary.NativeEndian.PutUint32(header[:], maxMessage+1)

	err := Serve(context.Background(), bytes.NewReader(header[:]), &bytes.Buffer{}, "v1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not one of ours")
}

func TestResolveWantsARepository(t *testing.T) {
	_, err := Resolve("", "")
	require.Error(t, err)
}

func message(t *testing.T, req Request) *bytes.Reader {
	t.Helper()
	return bytes.NewReader(framed(t, req))
}

func framed(t *testing.T, req Request) []byte {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)

	var header [4]byte
	binary.NativeEndian.PutUint32(header[:], uint32(len(body)))
	return append(header[:], body...)
}

func reply(t *testing.T, out *bytes.Buffer) Response {
	t.Helper()
	var header [4]byte
	_, err := out.Read(header[:])
	require.NoError(t, err)

	body := make([]byte, binary.NativeEndian.Uint32(header[:]))
	_, err = out.Read(body)
	require.NoError(t, err)

	var resp Response
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp
}

func TestInstallWritesAManifestPointingAtTheShim(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	done, err := Install()
	require.NoError(t, err)

	shim, err := os.ReadFile(done.Shim)
	require.NoError(t, err)
	assert.Contains(t, string(shim), "browser host", "Firefox cannot pass the subcommand itself")

	info, err := os.Stat(done.Shim)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&0o100, "Firefox has to be able to run it")

	body, err := os.ReadFile(done.Manifest)
	require.NoError(t, err)

	var got manifest
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, HostName, got.Name)
	assert.Equal(t, "stdio", got.Type)
	assert.Equal(t, done.Shim, got.Path)
	assert.Equal(t, []string{ExtensionID}, got.AllowedExtensions)
	assert.Equal(t, HostName+".json", filepath.Base(done.Manifest))
}

func TestUninstallIsFineWithNothingInstalled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, err := Uninstall()
	require.NoError(t, err)
	assert.Contains(t, path, HostName)
}

func TestEnvironReplacesThePath(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("ADE_MARKER", "kept")

	got := environ("/opt/homebrew/bin:/usr/bin")
	assert.Contains(t, got, "PATH=/opt/homebrew/bin:/usr/bin")
	assert.Contains(t, got, "ADE_MARKER=kept")
	assert.NotContains(t, got, "PATH=/usr/bin", "the old one is replaced, not added to")
}

func TestEnvironLeavesTheEnvironmentAloneWithNoPath(t *testing.T) {
	// Nothing beats inheriting: a PATH we could not work out is not an
	// improvement on the one we have.
	assert.Nil(t, environ(""))
}

func TestLoginPathReadsTheShellsPath(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	got := loginPath()

	require.NotEmpty(t, got)
	assert.NotContains(t, got, sentinel, "the fence is not part of the value")
	assert.Contains(t, got, "/bin")
}

func TestLoginPathIgnoresWhatTheProfilePrints(t *testing.T) {
	// A profile that says hello would otherwise be read as the PATH.
	shell := filepath.Join(t.TempDir(), "chatty.sh")
	require.NoError(t, os.WriteFile(shell, []byte("#!/bin/sh\necho hello from your profile\nshift 2\nexec /bin/sh -c \"$@\" --\n"), 0o700))
	t.Setenv("SHELL", shell)

	assert.Equal(t, os.Getenv("PATH"), loginPath())
}

func TestLoginPathGivesUpWithoutAShell(t *testing.T) {
	t.Setenv("SHELL", "")
	assert.Empty(t, loginPath())
}

func TestRenamedFrom(t *testing.T) {
	cases := []struct {
		name        string
		dir         string
		owner, repo string
		found       string
		want        bool
	}{
		{
			name: "the same owner under the repository's old name",
			dir:  "/Users/owen/code/owenrumney/ade", owner: "owenrumney", repo: "ade",
			found: "owenrumney/diffy", want: true,
		},
		{
			name: "the directory settles it, not the remote",
			dir:  "/Users/owen/code/owenrumney/diffy", owner: "owenrumney", repo: "ade",
			found: "owenrumney/diffy", want: false,
		},
		{
			name: "another owner's repository of the same name is not yours",
			dir:  "/Users/owen/code/someoneelse/ade", owner: "owenrumney", repo: "ade",
			found: "someoneelse/ade", want: false,
		},
		{
			name: "case does not matter",
			dir:  "/Users/owen/code/owenrumney/ADE", owner: "OwenRumney", repo: "ade",
			found: "owenrumney/diffy", want: true,
		},
		{
			name: "a remote that is not owner/repo",
			dir:  "/Users/owen/code/owenrumney/ade", owner: "owenrumney", repo: "ade",
			found: "", want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, renamedFrom(tc.dir, tc.owner, tc.repo, tc.found))
		})
	}
}

func TestAddressIsOnlyAURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  string
		want string
	}{
		{"the url ade printed", "starting…\nhttp://127.0.0.1:8731/\n", "http://127.0.0.1:8731/"},
		{"a last line that is not one", "did the thing\n", ""},
		{"nothing at all", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, address(tc.out))
		})
	}
}

func TestSessionCheckoutReturnsTheClosedWorktree(t *testing.T) {
	main := "/code/acme/widget"
	worktree := "/code/acme/widget-fix-login"
	dirs := []sessions.Dir{
		{Path: main, Main: main, Branch: "main"},
		{Path: worktree, Main: main, Branch: "fix/login", Worktree: true},
	}

	assert.Equal(t, worktree, sessionCheckout(main, "fix/login", dirs))
	assert.Equal(t, main, sessionCheckout(main, "gone", dirs))
	assert.Equal(t, main, sessionCheckout(main, "", dirs))
}

func TestServeStartingASessionNeedsARepository(t *testing.T) {
	// A session is asked for by owner/repo, never by directory: the host looks
	// the checkout up itself, so the page cannot name one.
	out := &bytes.Buffer{}
	require.NoError(t, Serve(context.Background(), message(t, Request{Action: "session"}), out, "v1"))

	got := reply(t, out)
	assert.False(t, got.OK)
	assert.Contains(t, got.Error, "repository")
}
