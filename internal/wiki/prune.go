package wiki

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/authstore"
)

// settleWindow leaves a just-touched draft alone, so pruning does not pull a
// mount out from under a session that is still running.
const settleWindow = time.Hour

// PruneReport is what a prune found. Only Removed and Clones were deleted;
// Retained is reported for the user to discard by hand.
type PruneReport struct {
	Removed  []string `json:"removed"`
	Retained []Draft  `json:"retained"`
	Clones   []string `json:"clones"`
}

// Prune removes clean abandoned drafts and expired publish clones. It never
// removes a draft with unpublished changes, nor one a conflict is holding.
func (s *Store) Prune(ctx context.Context) (PruneReport, error) {
	report := PruneReport{Removed: []string{}, Retained: []Draft{}, Clones: []string{}}
	drafts, err := s.Drafts()
	if err != nil {
		return report, err
	}
	unlock, err := authstore.Lock(s.recordsPath())
	if err != nil {
		return report, err
	}
	defer unlock()
	records := s.draftRecords()
	for _, draft := range drafts {
		record := records[draft.Session]
		if record.State == "conflict" || recentlyTouched(draft.Root) {
			report.Retained = append(report.Retained, draft)
			continue
		}
		changed, err := s.draftChanged(ctx, draft)
		if err != nil || changed {
			report.Retained = append(report.Retained, draft)
			continue
		}
		if err := os.RemoveAll(draft.Root); err != nil {
			return report, err
		}
		if record.Publish != "" {
			_ = os.RemoveAll(record.Publish)
		}
		delete(records, draft.Session)
		report.Removed = append(report.Removed, draft.Session)
	}
	if err := s.writeDraftRecords(records); err != nil {
		return report, err
	}
	clones, err := s.expireClones(records)
	if err != nil {
		return report, err
	}
	report.Clones = clones
	return report, nil
}

// expireClones removes publish clones no live record claims. A conflict's
// clone is claimed, and survives until the user discards its draft.
func (s *Store) expireClones(records map[string]draftRecord) ([]string, error) {
	root := filepath.Join(filepath.Dir(s.root), "wiki-publish")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	claimed := map[string]bool{}
	for _, record := range records {
		if record.Publish != "" {
			claimed[filepath.Base(record.Publish)] = true
		}
	}
	gone := []string{}
	for _, entry := range entries {
		if !entry.IsDir() || claimed[entry.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return nil, err
		}
		gone = append(gone, entry.Name())
	}
	sort.Strings(gone)
	return gone, nil
}

// draftChanged reports whether the draft differs from the canonical tree at
// its base revision, in either direction.
func (s *Store) draftChanged(ctx context.Context, draft Draft) (bool, error) {
	if draft.Base == "" || !validRepo(draft.Repo) {
		return true, nil
	}
	seen := map[string]bool{}
	for _, dir := range scopes(draft.Repo) {
		root := filepath.Join(draft.Root, dir)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil || entry.IsDir() {
				return err
			}
			rel := relDraft(draft.Root, path)
			seen[rel] = true
			if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
				return errChanged
			}
			data, err := os.ReadFile(path) // #nosec G304 -- beneath this draft's root
			if err != nil {
				return err
			}
			was, err := s.git(ctx, "show", draft.Base+":"+rel)
			if err != nil || was != string(data) {
				return errChanged
			}
			return nil
		})
		if errors.Is(err, errChanged) {
			return true, nil
		}
		if err != nil {
			return true, err
		}
		listed, err := s.git(ctx, "ls-tree", "-r", "--name-only", draft.Base, "--", filepath.ToSlash(dir)+"/")
		if err != nil {
			return true, nil
		}
		for _, name := range strings.Split(strings.TrimSpace(listed), "\n") {
			if name = strings.TrimSpace(name); name != "" && !seen[name] {
				return true, nil
			}
		}
	}
	return false, nil
}

var errChanged = errors.New("draft differs from its base")

func scopes(repo string) []string {
	return []string{"general", filepath.Join("repos", filepath.FromSlash(repo))}
}

func recentlyTouched(root string) bool {
	newest := time.Time{}
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := entry.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return time.Since(newest) < settleWindow
}
