package remote

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// said is every argv the recorder saw, as one string to assert against.
func said(rec *recorder) string {
	var out strings.Builder
	for _, call := range rec.calls {
		out.WriteString(strings.Join(call, " "))
		out.WriteString("\n")
	}
	return out.String()
}

func TestTheWorkerIsToldWhoCommits(t *testing.T) {
	rec := &recorder{}
	who := Identity{Name: "Owen Rumney", Email: "owen@example.com"}
	require.NoError(t, testHost(t, rec).PrepareGitIdentity(context.Background(), who))

	require.Len(t, rec.calls, 2, "a name and an email, and nothing about signing")
	for _, word := range []string{workerName, "user.name", "user.email", "Owen", "owen@example.com"} {
		assert.Contains(t, said(rec), word)
	}
}

func TestASigningKeyGoesOverStdinNotArgv(t *testing.T) {
	rec := &recorder{}
	who := Identity{
		Name: "Owen", Email: "owen@example.com",
		SigningKey: []byte("PRIVATE-KEY-BODY"), SigningPub: []byte("ssh-ed25519 AAAA"),
	}
	require.NoError(t, testHost(t, rec).PrepareGitIdentity(context.Background(), who))

	assert.NotContains(t, said(rec), "PRIVATE-KEY-BODY", "a private key must never reach a process list")
	var sent bool
	for _, in := range rec.input {
		sent = sent || strings.Contains(string(in), "PRIVATE-KEY-BODY")
	}
	assert.True(t, sent, "the key is sent, just not in argv")
	for _, word := range []string{"gpg.format", "user.signingkey", "commit.gpgsign", "tag.gpgsign"} {
		assert.Contains(t, said(rec), word)
	}
}

func TestAMachineThatDoesNotSignSendsNoKey(t *testing.T) {
	rec := &recorder{}
	who := Identity{Name: "Owen", Email: "owen@example.com", SigningPub: []byte("ssh-ed25519 AAAA")}
	require.NoError(t, testHost(t, rec).PrepareGitIdentity(context.Background(), who))
	assert.NotContains(t, said(rec), "commit.gpgsign", "half a key pair signs nothing")
}

func TestAnIdentityThisMachineDoesNotHaveIsNotSent(t *testing.T) {
	for _, who := range []Identity{{}, {Name: "Owen"}, {Email: "owen@example.com"}, {Name: " ", Email: " "}} {
		rec := &recorder{}
		require.NoError(t, testHost(t, rec).PrepareGitIdentity(context.Background(), who))
		assert.Empty(t, rec.calls, "half an identity is worse than git's own message")
	}
}

func TestAFailedIdentityNamesTheMachineAndTheKey(t *testing.T) {
	h := &Host{Name: "pi", ssh: func(context.Context, []string, []byte) ([]byte, []byte, error) {
		return nil, nil, errors.New("no such container")
	}}
	err := h.PrepareGitIdentity(context.Background(), Identity{Name: "O", Email: "o@e.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user.name")
	assert.Contains(t, err.Error(), "pi")
}

func TestPushCredentialsComeFromTheWorkersGh(t *testing.T) {
	rec := &recorder{}
	require.NoError(t, testHost(t, rec).PrepareGitCredentials(context.Background()))
	for _, word := range []string{workerName, "gh", "auth", "setup-git"} {
		assert.Contains(t, said(rec), word)
	}
}

func TestTheSameKeyIsPointedAtForPushing(t *testing.T) {
	rec := &recorder{}
	who := Identity{
		Name: "Owen", Email: "owen@example.com",
		SigningKey: []byte("PRIVATE"), SigningPub: []byte("ssh-ed25519 AAAA"),
		KnownHosts: []byte("github.com ssh-ed25519 AAAAHOST\n"),
	}
	require.NoError(t, testHost(t, rec).PrepareGitIdentity(context.Background(), who))

	all := said(rec)
	assert.Contains(t, all, "core.sshCommand", "ssh ignores a key it was not told about")
	assert.Contains(t, all, "ssh -F $HOME/.ssh/config", "ssh expands ~ from the passwd file, not HOME")
	assert.Contains(t, all, "known_hosts")

	var hosts bool
	for _, in := range rec.input {
		hosts = hosts || strings.Contains(string(in), "AAAAHOST")
	}
	assert.True(t, hosts, "the laptop's trust is what the worker should use")
}

func TestNoKnownHostsIsNotAnEmptyFile(t *testing.T) {
	rec := &recorder{}
	who := Identity{
		Name: "Owen", Email: "owen@example.com",
		SigningKey: []byte("PRIVATE"), SigningPub: []byte("ssh-ed25519 AAAA"),
	}
	require.NoError(t, testHost(t, rec).PrepareGitIdentity(context.Background(), who))
	assert.NotContains(t, said(rec), `cat > "$HOME/.ssh/known_hosts"`,
		"writing an empty one would replace whatever the machine already trusts")
}

func TestAnSSHRemoteOnTheWorkerGoesOverHttps(t *testing.T) {
	rec := &recorder{}
	require.NoError(t, testHost(t, rec).PrepareGitCredentials(context.Background()))
	all := said(rec)
	for _, word := range []string{"--unset-all", "--add", "url.https://github.com/.insteadOf",
		"git@github.com:", "ssh://git@github.com/"} {
		assert.Contains(t, all, word)
	}
	assert.Less(t, strings.Index(all, "--unset-all"), strings.Index(all, "--add"),
		"an unset after the adds would wipe what it just wrote")
	assert.Contains(t, all, `'^(git@github\.com:|ssh://git@github\.com/)$'`,
		"only ade's own spellings go; a rewrite of the user's own under the same key stays")
}

func TestTheSSHKeyIsNamedWhereSSHWillLookForIt(t *testing.T) {
	rec := &recorder{}
	who := Identity{
		Name: "Owen", Email: "owen@example.com",
		SigningKey: []byte("PRIVATE"), SigningPub: []byte("ssh-ed25519 AAAA"),
	}
	require.NoError(t, testHost(t, rec).PrepareGitIdentity(context.Background(), who))
	all := said(rec)
	assert.Contains(t, all, "IdentityFile $HOME/.ssh/ade_signing", "~ would name root's home, not the worker's")
	assert.NotContains(t, all, "IdentityFile ~/")
}

func TestTheSigningKeyIsOfferedToGithubOnly(t *testing.T) {
	rec := &recorder{}
	who := Identity{
		Name: "Owen", Email: "owen@example.com",
		SigningKey: []byte("PRIVATE"), SigningPub: []byte("ssh-ed25519 AAAA"),
	}
	require.NoError(t, testHost(t, rec).PrepareGitIdentity(context.Background(), who))
	all := said(rec)
	assert.Contains(t, all, "Host github.com", "a global -i sends ade's key to gitlab too")
	assert.NotContains(t, all, "ssh -i ", "and suppresses every key the worker holds for anywhere else")
}
