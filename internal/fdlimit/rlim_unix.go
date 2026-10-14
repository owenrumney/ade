//go:build !windows

package fdlimit

import "syscall"

// rlim converts to whatever width this platform's Rlimit uses.
func rlim(v uint64) uint64 { return v }

var _ = syscall.RLIMIT_NOFILE
