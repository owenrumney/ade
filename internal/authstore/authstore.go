// Package authstore writes the login files ade shares with the agent CLIs:
// atomically, and behind a lock, because a CLI reads them as ade writes.
package authstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Write replaces path with data, 0600, through a temp file in the same
// directory: a truncating write lets a reader see a torn file.
func Write(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".authstore-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	// A rename can be durable before the data it points at is.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return syncDir(dir)
}

// syncDir makes the rename itself survive a crash.
func syncDir(dir string) error {
	d, err := os.Open(dir) // #nosec G304 -- the directory the target is in
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}

// Lock covers path with an exclusive lock, held until the returned function
// is called. It is a file beside the target, never the target itself.
func Lock(path string) (func(), error) {
	unlock, _, err := lockFile(path, syscall.LOCK_EX)
	return unlock, err
}

// TryLock is Lock without the wait: held is false when someone else has it.
func TryLock(path string) (unlock func(), held bool, err error) {
	return lockFile(path, syscall.LOCK_EX|syscall.LOCK_NB)
}

func lockFile(path string, how int) (func(), bool, error) {
	lock := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lock), 0o700); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- beside a path the caller owns
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("lock %s: %w", lock, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, true, nil
}
