package preview

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		want string
	}{
		{"localhost", "http://localhost:3000", "http://localhost:3000"},
		{"ipv4", "https://127.0.0.1:3000/app", "https://127.0.0.1:3000/app"},
		{"ipv6", "http://[::1]:3000", "http://[::1]:3000"},
		{"empty disables", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidURL(tc.url)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestValidURLRefusesAnythingButAnUnauthenticatedLoopbackURL(t *testing.T) {
	for _, raw := range []string{
		"example.com",
		"ftp://localhost:3000",
		"https://example.com",
		"http://localhost.evil.example",
		"http://user:pass@localhost:3000",
	} {
		_, err := ValidURL(raw)
		assert.Error(t, err, raw)
	}
}

func TestCaptureUsesAnIsolatedChromeProfile(t *testing.T) {
	dir := t.TempDir()
	chrome := filepath.Join(dir, "chrome")
	args := filepath.Join(dir, "args")
	require.NoError(t, os.WriteFile(chrome, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS\"\nfor arg do case $arg in --screenshot=*) printf png > \"${arg#--screenshot=}\";; esac; done\n"), 0o700))
	t.Setenv("ADE_CHROME", chrome)
	t.Setenv("ARGS", args)

	image, err := Capture(context.Background(), "http://localhost:5173")
	require.NoError(t, err)
	assert.Equal(t, []byte("png"), image)
	got, err := os.ReadFile(args)
	require.NoError(t, err)
	assert.Contains(t, string(got), "--user-data-dir=")
	assert.Contains(t, string(got), "--window-size=1280,800")
	assert.True(t, strings.Contains(string(got), "--headless=new"))
}

func TestChromeIsFoundOnThePath(t *testing.T) {
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, name)
			require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700))
			t.Setenv("ADE_CHROME", "")
			t.Setenv("PATH", dir)

			found, err := findChrome()
			require.NoError(t, err)
			assert.Equal(t, path, found)
		})
	}
}

func TestNoChromeAnywhereSaysSo(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("a real Chrome under /Applications would be found")
	}
	t.Setenv("ADE_CHROME", "")
	t.Setenv("PATH", t.TempDir())

	_, err := findChrome()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Chrome or Chromium is needed")
}

func TestSaveAndLoad(t *testing.T) {
	file := filepath.Join(t.TempDir(), "ade", "preview.json")
	want, err := Save(file, Config{URL: "http://localhost:5173"})
	require.NoError(t, err)
	got, err := Load(file)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestReachableIsFalseWhenNothingIsListening(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	live := "http://" + listener.Addr().String()
	assert.True(t, Reachable(live), "a dev server that is up is worth a browser")

	require.NoError(t, listener.Close())
	assert.False(t, Reachable(live), "one that is not is skipped rather than failed")
	assert.False(t, Reachable("not a url at all"))
}
