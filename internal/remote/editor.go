package remote

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// EditorLimit caps what is sent. A configuration is text; a plugin tree is the
// box's to clone, and a lua cache is nobody's.
const EditorLimit = 8 << 20

// EditorConfig is the laptop's ~/.config/nvim as a tar.gz, or nil when there
// is none. Symlinks are left out: one points somewhere this tree does not.
func EditorConfig(home string) ([]byte, error) {
	root := filepath.Join(home, ".config", "nvim")
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil, nil
	}
	// A stow or chezmoi setup links the directory itself. WalkDir lstats its
	// root, so walking the link gives an empty archive that wipes the worker's.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	var sent, files int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			return nil
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		files++
		if sent += info.Size(); sent > EditorLimit {
			return fmt.Errorf("%s is over the %d byte cap", root, EditorLimit)
		}
		file, err := os.Open(path) // #nosec G304 -- the user's own configuration
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		_, err = io.Copy(tw, file)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	// Nothing to send rather than an empty archive: unpacking one removes the
	// worker's configuration and puts nothing back.
	if files == 0 {
		return nil, nil
	}
	return buf.Bytes(), nil
}

// unpackEditor replaces the worker's copy. Whole, not merged: a file deleted
// here would otherwise live on over there forever.
const unpackEditor = `set -eu
mkdir -p "$HOME/.config"
rm -rf "$HOME/.config/nvim"
mkdir -p "$HOME/.config/nvim"
tar xzf - -C "$HOME/.config/nvim"`

// PrepareEditorConfig puts it in the worker's HOME, so its editor pane opens
// your setup. Through the worker, as the MCP servers are: root owns that HOME.
func (h *Host) PrepareEditorConfig(ctx context.Context, config []byte) error {
	if len(config) == 0 {
		return nil
	}
	if _, err := h.RunInput(ctx, config, "docker", "exec", "-i", workerName, "sh", "-c", unpackEditor); err != nil {
		return fmt.Errorf("send the editor configuration to %s: %w", h.Name, err)
	}
	return nil
}
