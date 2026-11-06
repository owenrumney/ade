package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShowPutsThePictureInTheOutbox(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, "shot.png")
	raw := []byte("\x89PNG\r\n\x1a\n not really a png, but bytes are bytes")
	require.NoError(t, os.WriteFile(at, raw, 0o600))
	box := filepath.Join(dir, "outbox")

	to, err := showImage(box, at)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(box, "shot.png"), to)
	got, err := os.ReadFile(to)
	require.NoError(t, err)
	assert.Equal(t, raw, got)
}

// The same name twice is the same picture again, not a second one.
func TestShowOverwritesThePictureOfTheSameName(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, "shot.png")
	box := filepath.Join(dir, "outbox")
	require.NoError(t, os.WriteFile(at, []byte("first"), 0o600))
	_, err := showImage(box, at)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(at, []byte("second"), 0o600))
	to, err := showImage(box, at)
	require.NoError(t, err)

	got, err := os.ReadFile(to)
	require.NoError(t, err)
	assert.Equal(t, []byte("second"), got)
	left, err := os.ReadDir(box)
	require.NoError(t, err)
	assert.Len(t, left, 1)
}

func TestShowRefusesWhatTheTabCannotDraw(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(at, []byte("hello"), 0o600))

	_, err := showImage(filepath.Join(dir, "outbox"), at)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only png, jpeg and gif")
}

func TestShowSaysWhichFileIsMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := showImage(filepath.Join(dir, "outbox"), filepath.Join(dir, "gone.png"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gone.png")
}
