package remote

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkerCredentialsWinAndFallbackFillsTheRest(t *testing.T) {
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		if strings.Contains(command, "test") && strings.Contains(command, "claude") {
			return []byte(sentinel), nil, nil
		}
		if strings.Contains(command, "test") && strings.Contains(command, "codex") {
			return nil, nil, errors.New("missing")
		}
		return []byte(sentinel), nil, nil
	}}
	host := testHost(t, rec)

	err := host.PrepareCredentials(context.Background(), fhsRoots, Credentials{Claude: []byte("laptop-claude"), Codex: []byte("laptop-codex")})
	require.NoError(t, err)
	assert.Len(t, rec.calls, 4)
	assert.Empty(t, rec.input[1], "the worker's Claude login was retained")
	assert.Equal(t, "laptop-codex", string(rec.input[3]))
	assert.NotContains(t, rec.calls[3][len(rec.calls[3])-1], "laptop-codex")
}

// The box is where a session's token refreshes, so a laptop reconnecting with
// an older copy must not put it back over the live one.
func TestAnOlderLaptopCredentialDoesNotOverwriteTheBox(t *testing.T) {
	newer := time.Now()
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		// The box copy is the newer one for claude, older for codex.
		if strings.Contains(command, "stat") {
			if strings.Contains(command, "claude") {
				return []byte(sentinel), nil, nil
			}
			return nil, nil, errors.New("older")
		}
		return []byte(sentinel), nil, nil
	}}
	host := testHost(t, rec)

	err := host.PrepareCredentials(context.Background(), fhsRoots, Credentials{
		Claude: []byte("older-claude"), ClaudeAt: newer.Add(-time.Hour),
		Codex: []byte("newer-codex"), CodexAt: newer,
	})
	require.NoError(t, err)
	for i, input := range rec.input {
		assert.NotEqual(t, "older-claude", string(input), "call %d put an older login back", i)
	}
	assert.Equal(t, "newer-codex", string(rec.input[len(rec.input)-1]))
}

// The worker runs as root and refreshes these files, so the copy on the box is
// root's. A laptop that wrote into it instead of replacing it was refused.
func TestACredentialTheWorkerOwnsIsReplacedNotWrittenTo(t *testing.T) {
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		if strings.Contains(argv[len(argv)-1], "test") {
			return nil, nil, errors.New("missing")
		}
		return []byte(sentinel), nil, nil
	}}
	require.NoError(t, testHost(t, rec).PrepareCredentials(context.Background(), fhsRoots,
		Credentials{Claude: []byte("laptop-claude")}))

	var wrote bool
	for _, call := range rec.calls {
		command := call[len(call)-1]
		if strings.Contains(command, ".credentials.json") && strings.Contains(command, "mv -f") {
			wrote = true
		}
		assert.NotContains(t, command, "cat > \"$1\"\n", "a root-owned file cannot be opened for writing")
	}
	assert.True(t, wrote, "the laptop's copy goes to a new name and is moved over")
}
