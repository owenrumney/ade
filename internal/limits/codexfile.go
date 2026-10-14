package limits

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/owenrumney/ade/internal/settings"
)

// A reading is shared through a file beside ade's other state, so that every
// ade on the machine makes one request between them rather than one each.
var writeOne sync.Mutex

func filePath(provider string) (string, error) {
	dir, err := settings.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, provider+"-limits.json"), nil
}

func readShared(provider string) (Limits, bool) {
	path, err := filePath(provider)
	if err != nil {
		return Limits{}, false
	}
	data, err := os.ReadFile(path) // #nosec G304 -- ade's own state directory
	if err != nil {
		return Limits{}, false
	}
	var out Limits
	if err := json.Unmarshal(data, &out); err != nil || len(out.Windows) == 0 {
		return Limits{}, false
	}
	return out, true
}

// writeShared records a reading; a failure is dropped, the number is a nicety.
func writeShared(l Limits) {
	writeOne.Lock()
	defer writeOne.Unlock()
	path, err := filePath(l.Provider)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		slog.Debug("keep the plan limits", "err", err)
		return
	}
	data, err := json.Marshal(l)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ade-limits-*")
	if err != nil {
		slog.Debug("keep the plan limits", "err", err)
		return
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		slog.Debug("keep the plan limits", "err", err)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		slog.Debug("keep the plan limits", "err", err)
	}
}
