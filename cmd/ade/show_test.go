package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShowWritesAnInlineImageTheTerminalReads(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, "shot.png")
	raw := []byte("\x89PNG\r\n\x1a\n not really a png, but bytes are bytes")
	require.NoError(t, os.WriteFile(at, raw, 0o600))

	var out bytes.Buffer
	require.NoError(t, showImage(&out, at, "auto"))

	got := out.String()
	assert.True(t, strings.HasPrefix(got, "\x1b]1337;File=inline=1;"), got[:40])
	assert.Contains(t, got, "size=46;")
	assert.Contains(t, got, base64.StdEncoding.EncodeToString(raw))
	assert.True(t, strings.HasSuffix(got, "\a\n"))
}

func TestShowRefusesWhatTheTerminalCannotDecode(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(at, []byte("hello"), 0o600))

	err := showImage(&bytes.Buffer{}, at, "auto")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only png, jpeg and gif")
}

func TestShowSaysWhichFileIsMissing(t *testing.T) {
	err := showImage(&bytes.Buffer{}, filepath.Join(t.TempDir(), "gone.png"), "auto")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gone.png")
}
