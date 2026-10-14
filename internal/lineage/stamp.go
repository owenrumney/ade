package lineage

import (
	"strings"

	"github.com/owenrumney/ade/internal/gitdiff"
)

// Stamp writes sha on every unstamped range whose lines added (a commit's
// added lines by file) contains. A stamped range keeps the sha it has.
func (s *Store) Stamp(diff *gitdiff.Diff, sha string, added map[string][]string) (int, error) {
	if s == nil || strings.TrimSpace(sha) == "" || len(added) == 0 {
		return 0, nil
	}

	sets := make(map[string]map[string]bool, len(added))
	for file, lines := range added {
		set := make(map[string]bool, len(lines))
		for _, line := range lines {
			set[line] = true
		}
		sets[file] = set
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stamped := 0
	for i := range s.entries {
		for j := range s.entries[i].Ranges {
			r := &s.entries[i].Ranges[j]
			if r.Commit != "" {
				continue
			}
			if !inCommit(diff, *r, sets[r.File]) {
				continue
			}
			r.Commit = sha
			stamped++
		}
	}
	if stamped == 0 {
		return 0, nil
	}
	return stamped, s.save()
}

// Restamp moves every stamp of old onto sha, which is what an amend needs: the
// commit it rewrote is gone and the ranges it carried are now in the new one.
func (s *Store) Restamp(old, sha string) (int, error) {
	if s == nil || old == "" || sha == "" || old == sha {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	moved := 0
	for i := range s.entries {
		for j := range s.entries[i].Ranges {
			if s.entries[i].Ranges[j].Commit != old {
				continue
			}
			s.entries[i].Ranges[j].Commit = sha
			moved++
		}
	}
	if moved == 0 {
		return 0, nil
	}
	return moved, s.save()
}

// ForCommit is the turns that wrote a line of file into sha, newest first. This
// is the lookup blame leads to: a sha, and the asks behind it.
func (s *Store) ForCommit(sha, file string) []Turn {
	if s == nil || sha == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []Turn
	for i := len(s.entries) - 1; i >= 0; i-- {
		e := s.entries[i]
		for _, r := range e.Ranges {
			if r.Commit != sha || (file != "" && r.File != file) {
				continue
			}
			out = append(out, Turn{
				Session:   e.Session,
				MessageID: e.MessageID,
				At:        e.At,
				Summary:   e.Summary,
				Prompt:    e.Prompt,
				Commit:    r.Commit,
			})
			break
		}
	}
	return out
}

// inCommit reports whether every line the range covers is one the commit
// added. An all-blank range matches nothing; it would match almost anything.
func inCommit(diff *gitdiff.Diff, r Range, added map[string]bool) bool {
	if len(added) == 0 {
		return false
	}
	start, ok := where(diff, r)
	if !ok {
		return false
	}
	content, ok := rightLines(diff, r.File, start, r.Lines)
	if !ok {
		return false
	}
	real := 0
	for _, line := range content {
		if strings.TrimSpace(line) != "" {
			real++
		}
		if !added[line] {
			return false
		}
	}
	return real > 0
}

// rightLines is the new-side content of lines [start, start+count) of file, and
// false if any of them is not in the diff.
func rightLines(diff *gitdiff.Diff, file string, start, count int) ([]string, bool) {
	if diff == nil || count <= 0 {
		return nil, false
	}
	byLine := map[int]string{}
	for i := range diff.Files {
		if diff.Files[i].Path != file {
			continue
		}
		for _, hunk := range diff.Files[i].Hunks {
			for _, row := range hunk.Rows {
				if row.Right != nil {
					byLine[row.Right.Line] = row.Right.Content
				}
			}
		}
	}
	out := make([]string, 0, count)
	for n := start; n < start+count; n++ {
		line, ok := byLine[n]
		if !ok {
			return nil, false
		}
		out = append(out, line)
	}
	return out, true
}
