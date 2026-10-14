package browserhost

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/owenrumney/ade/internal/settings"
)

// manifest is Firefox's native messaging host manifest.
type manifest struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Path              string   `json:"path"`
	Type              string   `json:"type"`
	AllowedExtensions []string `json:"allowed_extensions"`
}

// Installed is where install put things, so the command can say so.
type Installed struct {
	Manifest string
	Shim     string
}

// HostName is what the extension calls. It has to match the manifest's file
// name and the name inside it.
const HostName = "io.ade.host"

// ExtensionID is the add-on allowed to talk to the host. Firefox checks it, so
// the extension pins the same id in browser_specific_settings.
//
// Frozen at the pre-ADE name: changing it makes a new add-on on AMO.
const ExtensionID = "sow@owenrumney.github.io"

// Install writes the native messaging manifest, and the shim it points at.
//
// Firefox runs the manifest's path with its own arguments and no way to add
// ours, so it cannot point at ade directly: the shim is what turns that call
// into `ade browser host`.
func Install() (Installed, error) {
	exe, err := os.Executable()
	if err != nil {
		return Installed{}, fmt.Errorf("find ade: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	shim, err := writeShim(exe)
	if err != nil {
		return Installed{}, err
	}

	dir, err := manifestDir()
	if err != nil {
		return Installed{}, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return Installed{}, fmt.Errorf("create %s: %w", dir, err)
	}

	body, err := json.MarshalIndent(manifest{
		Name:              HostName,
		Description:       "Open a pull request in ADE",
		Path:              shim,
		Type:              "stdio",
		AllowedExtensions: []string{ExtensionID},
	}, "", "  ")
	if err != nil {
		return Installed{}, err
	}

	path := filepath.Join(dir, HostName+".json")
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		return Installed{}, fmt.Errorf("write %s: %w", path, err)
	}
	return Installed{Manifest: path, Shim: shim}, nil
}

// Uninstall removes the manifest, so Firefox stops offering the host.
func Uninstall() (string, error) {
	dir, err := manifestDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, HostName+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("remove %s: %w", path, err)
	}
	return path, nil
}

func writeShim(exe string) (string, error) {
	dir, err := settings.Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}

	path := filepath.Join(dir, "browser-host.sh")
	script := fmt.Sprintf("#!/bin/sh\nexec %q browser host\n", exe)
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { // #nosec G306 -- Firefox executes it
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

func manifestDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find your home directory: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Mozilla", "NativeMessagingHosts"), nil
	case "linux", "freebsd", "openbsd":
		return filepath.Join(home, ".mozilla", "native-messaging-hosts"), nil
	default:
		return "", fmt.Errorf("ade does not know where Firefox keeps native messaging hosts on %s", runtime.GOOS)
	}
}
