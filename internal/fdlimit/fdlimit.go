// Package fdlimit raises this process's open-file ceiling off the 256 a macOS
// shell hands down. See .git/ade/docs/log-viewer.md.
package fdlimit

import (
	"fmt"
	"syscall"
)

// ceiling is as high as this asks for: darwin returns EINVAL above
// kern.maxfilesperproc rather than clamping.
const ceiling = 10240

// Raise lifts the soft limit towards the hard one. A failure is not fatal:
// the old limit works, for less long.
func Raise() (from, to uint64, err error) {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return 0, 0, fmt.Errorf("read the open-file limit: %w", err)
	}
	from = uint64(lim.Cur)
	want := uint64(lim.Max)
	if want > ceiling {
		want = ceiling
	}
	if want <= from {
		return from, from, nil
	}

	next := lim
	next.Cur = rlim(want)
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &next); err != nil {
		return from, from, fmt.Errorf("raise the open-file limit to %d: %w", want, err)
	}
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return from, want, nil
	}
	return from, uint64(lim.Cur), nil
}
