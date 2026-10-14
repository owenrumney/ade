package verify

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Detect is the list ade offers a repository that has none. A test command is
// never offered, whatever the repository has: see the design for why.
func Detect(root string) []Check {
	if root == "" {
		return nil
	}
	var out []Check
	if exists(filepath.Join(root, "go.mod")) {
		out = append(out,
			Check{Name: "build", Run: "go build ./..."},
			Check{Name: "vet", Run: "go vet ./..."})
	}
	if script := nodeBuild(filepath.Join(root, "package.json")); script != "" {
		out = append(out, Check{Name: "build", Run: script})
	}
	if exists(filepath.Join(root, "Cargo.toml")) {
		out = append(out, Check{Name: "check", Run: "cargo check"})
	}
	return out
}

// nodeBuild is the package manager's build command, when there is a build
// script to run. The lock file names the manager; npm is the fallback.
func nodeBuild(manifest string) string {
	data, err := os.ReadFile(manifest) // #nosec G304 -- the checkout's own manifest, read not run
	if err != nil {
		return ""
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil || pkg.Scripts["build"] == "" {
		return ""
	}
	dir := filepath.Dir(manifest)
	switch {
	case exists(filepath.Join(dir, "pnpm-lock.yaml")):
		return "pnpm run build"
	case exists(filepath.Join(dir, "yarn.lock")):
		return "yarn build"
	default:
		return "npm run build"
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
