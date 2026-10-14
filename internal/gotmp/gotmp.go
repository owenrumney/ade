// Package gotmp keeps GOTMPDIR off macOS's temporary volume: Gatekeeper kills
// the test binary `go test` links into /var/folders and execs from there.
package gotmp

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Apply points GOTMPDIR somewhere the binaries survive. Off macOS, or when
// the caller set one, env comes back unchanged.
func Apply(env []string) []string { return apply(env, runtime.GOOS, Dir) }

// Dir is that directory, made if absent. The Go build cache already lives
// under UserCacheDir, so it is known not to be the policed volume.
func Dir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "ade", "gobuild")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func apply(env []string, goos string, dir func() (string, error)) []string {
	if goos != "darwin" || has(env, "GOTMPDIR") {
		return env
	}
	path, err := dir()
	if err != nil {
		return env
	}
	// Cloned: the caller's slice has spare capacity, and a plain append would
	// write this into the environment the next call is handed.
	return append(slices.Clone(env), "GOTMPDIR="+path)
}

func has(env []string, key string) bool {
	for _, kv := range env {
		if name, _, ok := strings.Cut(kv, "="); ok && name == key {
			return true
		}
	}
	return false
}
