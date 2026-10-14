//go:build linux

package session

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// HandToHost gives ade's state directory to the user the host runs as.
// Design: .git/ade/docs/sandbox-file-ownership.md.
func HandToHost(dir string) {
	uid, gid, ok := hostIDs()
	if !ok {
		return
	}
	// Every time, not once: root keeps adding files to it.
	if err := filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(path, uid, gid)
	}); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("could not hand ade's state directory to the host user", "dir", dir, "err", err)
	}
}

func hostIDs() (int, int, bool) {
	uid, uok := id(os.Getenv(HostUserEnv))
	gid, gok := id(os.Getenv(HostGroupEnv))
	return uid, gid, uok && gok
}

func id(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil && n >= 0
}
