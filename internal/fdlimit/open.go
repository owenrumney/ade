package fdlimit

import (
	"os"
	"syscall"
)

// State is where this process stands against its ceiling.
type State struct {
	Open  int    `json:"open"`
	Limit uint64 `json:"limit"`
}

// Near is whether the ceiling could be why a connection failed. A count that
// could not be taken is not evidence that it was.
func (s State) Near() bool { return s.Open >= 0 && s.Limit > 0 && uint64(s.Open) > s.Limit*8/10 }

// Now counts open descriptors against the limit, via /dev/fd. Open stays -1
// where there is no /dev/fd to read, which is not the same as none open.
func Now() State {
	out := State{Open: -1}
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err == nil {
		out.Limit = uint64(lim.Cur)
	}
	if entries, err := os.ReadDir("/dev/fd"); err == nil {
		out.Open = len(entries)
	}
	return out
}
