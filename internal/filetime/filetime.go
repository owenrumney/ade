// Package filetime reads when a file came into being.
package filetime

import (
	"os"
	"time"
)

// Created is the file's birth time where the platform records one, and its
// modification time where it does not: never later than the truth.
func Created(info os.FileInfo) time.Time {
	if at, ok := birth(info); ok && !at.IsZero() {
		return at
	}
	return info.ModTime()
}
