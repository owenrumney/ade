//go:build !darwin

package filetime

import (
	"os"
	"time"
)

// Linux records a birth time but only reaches it through statx, which takes a
// path rather than the FileInfo every caller here already holds.
func birth(os.FileInfo) (time.Time, bool) { return time.Time{}, false }
