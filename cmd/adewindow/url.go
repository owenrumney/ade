package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// bodyLimit caps a panel's page, which is the only body this host takes.
const bodyLimit = 4 << 20

// assets is where the window's own files are: beside the binary when
// packaged, and the checkout's app/ when run from source.
func assets() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return "", err
	}
	here := filepath.Dir(self)
	tries := []string{
		filepath.Join(here, "..", "Resources", "ade", "app"),
		filepath.Join(here, "..", "..", "app"),
	}
	if at, err := os.Getwd(); err == nil {
		tries = append(tries, filepath.Join(at, "app"), at)
	}
	for _, at := range tries {
		if _, err := os.Stat(filepath.Join(at, "renderer", "index.html")); err == nil {
			return filepath.Clean(at), nil
		}
	}
	return "", fmt.Errorf("no app/renderer beside %s or under the working directory", here)
}

// opened is the checkout named on the command line, so `ade .` opens the
// window on one rather than on whatever was last used.
func opened() string {
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		at, err := filepath.Abs(arg)
		if err != nil {
			continue
		}
		if info, err := os.Stat(at); err == nil && info.IsDir() {
			return at
		}
	}
	return ""
}

// launcher is where ade is listening and the token it answers to, read
// from the record rather than passed in. Neither is ever logged.
func launcher() (base, token string, err error) {
	at, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	raw, err := os.ReadFile(filepath.Join(at, ".ade", "server.json")) // #nosec G304 -- the user's own
	if err != nil {
		return "", "", err
	}
	var held struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &held); err != nil {
		return "", "", fmt.Errorf("read the record: %w", err)
	}
	parsed, err := url.Parse(held.URL)
	if err != nil {
		return "", "", fmt.Errorf("that record holds no url: %w", err)
	}
	token = parsed.Query().Get("t")
	if token == "" {
		return "", "", fmt.Errorf("the record carries no token")
	}
	return parsed.Scheme + "://" + parsed.Host, token, nil
}

// shell is ade's own page, served from the root. The bundle is untouched:
// what draws here is what `make app-shots` draws from the same file.
func shell(path string) (string, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- our own files
	if err != nil {
		return "", err
	}
	// Relative to the page on disk, absolute once it is served at the root.
	return strings.ReplaceAll(string(raw), "../dist/", "/dist/"), nil
}

func readAll(r *http.Request) (string, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, bodyLimit))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// adeBinary is the cli, found without relying on PATH: an app launched
// from Finder gets a thin one, and `ade` lives in a Go bin directory.
func adeBinary() (string, error) {
	if at, err := exec.LookPath("ade"); err == nil {
		return at, nil
	}
	tries := []string{
		filepath.Join(home(), "go", "bin", "ade"),
		"/usr/local/bin/ade",
		"/opt/homebrew/bin/ade",
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		tries = append(tries, filepath.Join(gopath, "bin", "ade"))
	}
	for _, at := range tries {
		if info, err := os.Stat(at); err == nil && !info.IsDir() {
			return at, nil
		}
	}
	return "", fmt.Errorf("ade is not on the PATH or in a Go bin directory")
}

// shellPath is a login shell's PATH. An app opened from the Dock gets
// launchd's, so claude, gh and mise's shims are not on what it starts.
func shellPath() string {
	const mark = "__ADE_PATH__"
	at := os.Getenv("SHELL")
	if at == "" {
		at = "/bin/zsh"
	}
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	out, err := exec.CommandContext(ctx, at, "-ilc", "printf '"+mark+"%s"+mark+"' \"$PATH\"").Output() // #nosec G204 -- the user's own shell
	if err != nil {
		return ""
	}
	parts := strings.Split(string(out), mark)
	if len(parts) < 3 {
		return ""
	}
	return parts[1]
}

func home() string {
	at, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return at
}
