package wiki

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// datesFile is when each page was last changed, by repository-relative path.
// Written once per sync so Search can name a date without invoking Git.
const datesFile = "wiki-dates.json"

func (s *Store) datesPath() string { return filepath.Join(filepath.Dir(s.root), datesFile) }

// scope is the source a page belongs to: `owner/repo` for a repository's
// notes, `general` for the shared ones. It is what a search result is read by.
func scope(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if parts[0] == "repos" && len(parts) > 2 {
		return parts[1] + "/" + parts[2]
	}
	return parts[0]
}

// indexDates records each path's last commit date. One `git log` over the
// whole tree, newest first, so the first sighting of a path is its date.
func (s *Store) indexDates(ctx context.Context) error {
	out, err := s.git(ctx, "log", "--format=%x00%cI", "--name-only")
	if err != nil {
		return err
	}
	dates, when := map[string]string{}, ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if after, found := strings.CutPrefix(line, "\x00"); found {
			when = after
			continue
		}
		if line == "" || when == "" || dates[line] != "" {
			continue
		}
		dates[line] = when
	}
	data, err := json.Marshal(dates)
	if err != nil {
		return err
	}
	return os.WriteFile(s.datesPath(), data, 0o600)
}

// pageDates reads the cached index. A clone made before the index existed has
// none, and a hit from it simply carries no date.
func (s *Store) pageDates() map[string]string {
	data, err := os.ReadFile(s.datesPath())
	if err != nil {
		return map[string]string{}
	}
	var dates map[string]string
	if json.Unmarshal(data, &dates) != nil || dates == nil {
		return map[string]string{}
	}
	return dates
}
