package transcripts

import (
	"bufio"
	"errors"
	"fmt"
	"os"
)

// maxLine bounds a single JSONL record. Claude writes whole file contents into
// tool results, so lines of several megabytes are normal.
const maxLine = 32 << 20

// indexLineCap bounds how far an index pass reads looking for metadata, so one
// pathological transcript cannot stall startup.
const indexLineCap = 2000

// errStopScan ends a scan early, for index passes that have what they need.
var errStopScan = errors.New("stop scan")

// scanJSONL calls fn for each non-blank line. fn returning errStopScan ends the
// scan cleanly; oversized lines are skipped rather than failing the file.
func scanJSONL(path string, fn func(line []byte) error) error {
	file, err := os.Open(path) // #nosec G304 -- path comes from globbing the agents' own session dirs.
	if err != nil {
		// A session created but never used has no transcript yet: nothing to
		// read, but nothing wrong either.
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLine)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := fn(line); err != nil {
			if errors.Is(err, errStopScan) {
				return nil
			}
			return err
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, bufio.ErrTooLong) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

func modTime(path string) (int64, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return info.ModTime().Unix(), true
}
