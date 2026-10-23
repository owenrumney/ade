package concierge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/owenrumney/ade/internal/settings"
)

// DeskFile is the one window's workspace, kept beside the desk's checkout.
const DeskFile = "desk.code-workspace"

// DeskName is the first folder's name, which is how the extension tells the
// desk's checkout from one brought in to work on.
const DeskName = "Desk"

type folder struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
}

// Desk ensures the one window's workspace file, with the desk's checkout
// first and dir among its folders, and answers the file to open.
func Desk(ctx context.Context, dir string) (string, error) {
	home, err := Ensure(ctx)
	if err != nil {
		return "", err
	}
	base, err := settings.Dir()
	if err != nil {
		return "", err
	}
	file := filepath.Join(base, DeskFile)
	raw, err := os.ReadFile(file) // #nosec G304 -- ade's own state directory
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read the desk's workspace: %w", err)
	}
	out, err := withDesk(raw, base, home, dir)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(file, out, 0o600); err != nil {
		return "", fmt.Errorf("write the desk's workspace: %w", err)
	}
	return file, nil
}

// withDesk rewrites a workspace file's folders: the desk first, dir added
// once, every other key kept. The editor writes paths relative to at.
func withDesk(raw []byte, at, home, dir string) ([]byte, error) {
	keys := map[string]json.RawMessage{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &keys); err != nil {
			return nil, fmt.Errorf("the desk's workspace does not parse: %w", err)
		}
	}
	var had []folder
	if held, ok := keys["folders"]; ok {
		if err := json.Unmarshal(held, &had); err != nil {
			return nil, fmt.Errorf("the desk's workspace folders do not parse: %w", err)
		}
	}
	same := func(a, b string) bool {
		if !filepath.IsAbs(a) {
			a = filepath.Join(at, a)
		}
		if !filepath.IsAbs(b) {
			b = filepath.Join(at, b)
		}
		return filepath.Clean(a) == filepath.Clean(b)
	}
	folders := []folder{{Path: home, Name: DeskName}}
	for _, f := range had {
		if !same(f.Path, home) {
			folders = append(folders, f)
		}
	}
	if dir != "" && !same(dir, home) {
		held := false
		for _, f := range folders {
			held = held || same(f.Path, dir)
		}
		if !held {
			folders = append(folders, folder{Path: dir})
		}
	}
	encoded, err := json.Marshal(folders)
	if err != nil {
		return nil, err
	}
	keys["folders"] = encoded
	if _, ok := keys["settings"]; !ok {
		keys["settings"] = json.RawMessage(`{}`)
	}
	return json.MarshalIndent(keys, "", "  ")
}
