// Package browser opens URLs in the user's browser.
package browser

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Open launches url in the default browser, honouring AGENTBRIDGE_BROWSER.
func Open(url string) error {
	return OpenWith(url, os.Getenv("AGENTBRIDGE_BROWSER"))
}

// OpenWith launches url in the named browser, falling back to the OS default
// when preferred is empty.
func OpenWith(url, preferred string) error {
	var cmd *exec.Cmd
	if preferred = strings.TrimSpace(preferred); preferred != "" {
		if runtime.GOOS == "darwin" {
			cmd = exec.Command("open", "-a", preferred, url) // #nosec G204 -- explicit user configuration
		} else {
			cmd = exec.Command(preferred, url) // #nosec G204 -- explicit user configuration
		}
	} else {
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url) // #nosec G204 -- url is internally constructed
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url) // #nosec G204
		default:
			cmd = exec.Command("xdg-open", url) // #nosec G204
		}
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}
