//go:build linux

package sandbox

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/owenrumney/ade/internal/session"
)

// Design: .git/ade/docs/sandbox-file-ownership.md.
func hostUser(socket string) (string, []string) {
	uid, gid := told()
	user := fmt.Sprintf("%d:%d", uid, gid)
	group, ok := socketGroup(socket)
	if !ok || group == gid {
		return user, nil
	}
	return user, []string{strconv.Itoa(group)}
}

// told prefers the ids a worker was given: ade there is root in a container,
// so its own are 0:0 and the checkout belongs to the ssh user.
func told() (int, int) {
	if uid, gid, ok := guest(); ok {
		return uid, gid
	}
	return os.Getuid(), os.Getgid()
}

// guest is the user a sandbox runs as, when ade was told one.
func guest() (int, int, bool) {
	uid, uok := number(os.Getenv(session.HostUserEnv))
	gid, gok := number(os.Getenv(session.HostGroupEnv))
	return uid, gid, uok && gok
}

// handToGuest gives what ade pre-created to the user the sandbox runs as.
// Best effort: see .git/ade/docs/sandbox-file-ownership.md.
func handToGuest(paths ...string) {
	uid, gid, ok := guest()
	if !ok {
		return
	}
	for _, path := range paths {
		if owned(path, uid) {
			continue
		}
		// Best effort: the run may still work, and refusing it over this would
		// take away the only session that could tell you.
		if err := filepath.WalkDir(path, func(p string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Lchown(p, uid, gid)
		}); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("could not hand ade's sandbox state to the host user", "path", path, "err", err)
		}
	}
}

// owned skips a path already theirs, so a filled cache is walked once.
func owned(path string, uid int) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == uid
}

func number(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil && n >= 0
}

func socketGroup(path string) (int, bool) {
	if path == "" {
		return 0, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(stat.Gid), true
}
