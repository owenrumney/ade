// Package preview keeps the local app URL ade captures after visual work.
package preview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/owenrumney/ade/internal/statedir"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	Width  = 1280
	Height = 800
)

// DefaultURL is where a dev server usually is, so a repository that says
// nothing is still captured.
const DefaultURL = "http://localhost:5173"

type Config struct {
	URL string `json:"url"`
}

func Path(gitDir string) string {
	if gitDir == "" {
		return ""
	}
	return filepath.Join(statedir.Git(gitDir), "preview.json")
}

func Load(file string) (Config, error) {
	if file == "" {
		return Config{}, nil
	}
	data, err := os.ReadFile(file) // #nosec G304 -- ade state under .git
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read preview: %w", err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("parse preview: %w", err)
	}
	config.URL, err = ValidURL(config.URL)
	if err != nil {
		return Config{}, err
	}
	return config, nil
}

func Save(file string, config Config) (Config, error) {
	if file == "" {
		return Config{}, errors.New("no git directory to keep preview settings in")
	}
	url, err := ValidURL(config.URL)
	if err != nil {
		return Config{}, err
	}
	config.URL = url
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return Config{}, err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		return Config{}, err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return Config{}, err
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return Config{}, err
	}
	return config, nil
}

func ValidURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("preview needs a complete URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("preview URL must use HTTP or HTTPS")
	}
	if parsed.User != nil {
		return "", errors.New("preview URL cannot contain credentials")
	}
	if !isLoopback(parsed.Hostname()) {
		return "", errors.New("preview URL must point to localhost")
	}
	return parsed.String(), nil
}

// Reachable is whether something is listening there now. A dev server that is
// not running is the common case, and not worth starting a browser for.
func Reachable(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	port := parsed.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[parsed.Scheme]
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(parsed.Hostname(), port), time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func Capture(ctx context.Context, raw string) ([]byte, error) {
	url, err := ValidURL(raw)
	if err != nil {
		return nil, err
	}
	chrome, err := findChrome()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "ade-preview-")
	if err != nil {
		return nil, fmt.Errorf("create preview profile: %w", err)
	}
	defer os.RemoveAll(dir)
	output := filepath.Join(dir, "preview.png")
	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--hide-scrollbars",
		"--user-data-dir=" + filepath.Join(dir, "profile"),
		fmt.Sprintf("--window-size=%d,%d", Width, Height),
		"--screenshot=" + output,
		url,
	}
	if output, err := exec.CommandContext(ctx, chrome, args...).CombinedOutput(); err != nil { // #nosec G204 -- chrome is found locally
		return nil, fmt.Errorf("capture preview: %w: %s", err, strings.TrimSpace(string(output)))
	}
	image, err := os.ReadFile(output) // #nosec G304 -- output is our temporary path
	if err != nil {
		return nil, fmt.Errorf("read preview: %w", err)
	}
	return image, nil
}

func findChrome() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("ADE_CHROME")); custom != "" {
		if info, err := os.Stat(custom); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return custom, nil
		}
		return "", fmt.Errorf("ADE_CHROME is not executable: %s", custom)
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
		if found, err := exec.LookPath(name); err == nil {
			return found, nil
		}
	}
	if runtime.GOOS == "darwin" {
		for _, path := range []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		} {
			if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				return path, nil
			}
		}
	}
	return "", errors.New("Chrome or Chromium is needed to capture a preview")
}
