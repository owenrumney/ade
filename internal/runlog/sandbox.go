package runlog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/owenrumney/ade/internal/statedir"
)

type choice struct {
	Sandbox bool `json:"sandbox"`
}

// Sandbox is whether this checkout last ran its agent in a container, and
// whether it ever said. A checkout that has not chosen reports false, false.
func (l *Log) Sandbox() (on bool, known bool) {
	path := l.sandboxPath()
	if path == "" {
		return false, false
	}
	data, err := os.ReadFile(path) // #nosec G304 -- derived from the home state dir
	if err != nil {
		return false, false
	}
	var held choice
	if err := json.Unmarshal(data, &held); err != nil {
		return false, false
	}
	return held.Sandbox, true
}

// SetSandbox remembers the choice, so a restart does not put the agent back in
// a container the user took it out of.
func (l *Log) SetSandbox(on bool) error {
	path := l.sandboxPath()
	if path == "" {
		return nil
	}
	data, err := json.Marshal(choice{Sandbox: on})
	if err != nil {
		return err
	}
	return writeFile(path, data)
}

// sandboxPath keeps the choice out of agent.json: the checkout and its git dir
// are mounted writable in the box, so an agent could let itself out at the next start.
func (l *Log) sandboxPath() string {
	if l == nil || l.path == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(l.path))
	return filepath.Join(statedir.Home(home), "sandbox", hex.EncodeToString(sum[:])+".json")
}
