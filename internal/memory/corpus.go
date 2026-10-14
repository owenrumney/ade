package memory

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/transcripts"
)

// ledgerFile maps a checkout path to its project: the only way to place a
// transcript from a worktree that has since been removed.
const ledgerFile = "paths.json"

var ledgerMu sync.Mutex

// Record notes that a checkout belongs to a project. Never pruned — a dead
// path's only value is answering this question later.
func Record(path string, id Identity) error {
	if path == "" || id.Slug == "" {
		return nil
	}
	ledgerMu.Lock()
	defer ledgerMu.Unlock()

	file, err := ledgerPath()
	if err != nil {
		return err
	}
	ledger := map[string]string{}
	readJSON(file, &ledger)
	if ledger[filepath.Clean(path)] == id.Slug {
		return nil
	}
	ledger[filepath.Clean(path)] = id.Slug
	return writeJSON(file, ledger)
}

// Ledger is every checkout path ade has seen, and which project it was.
func Ledger() map[string]string {
	ledgerMu.Lock()
	defer ledgerMu.Unlock()

	file, err := ledgerPath()
	if err != nil {
		return map[string]string{}
	}
	ledger := map[string]string{}
	readJSON(file, &ledger)
	return ledger
}

// Lookup is the project a checkout is already known to belong to, from the
// ledger alone. No git, so listing many checkouts costs no subprocesses.
func Lookup(path string) (Identity, bool) {
	if path == "" {
		return Identity{}, false
	}
	slug, ok := Ledger()[filepath.Clean(path)]
	if !ok || slug == "" {
		return Identity{}, false
	}
	return Identity{Slug: slug}, true
}

// Consumed is how much of a transcript a distillation run has read.
type Consumed struct {
	Size int64     `json:"size"`
	At   time.Time `json:"at"`
}

// Unread is one transcript with work left in it.
type Unread struct {
	Session string    `json:"session"`
	Path    string    `json:"path"`
	Title   string    `json:"title"`
	At      time.Time `json:"at"`
	// From is the byte already read; Size is what is on disk now.
	From int64 `json:"from"`
	Size int64 `json:"size"`
}

// Bytes is what a run would have to read.
func (u Unread) Bytes() int64 { return u.Size - u.From }

// Corpus is the transcripts belonging to a project, oldest first. Resolving a
// live directory writes it to the ledger, so the answer outlives it.
func Corpus(ctx context.Context, id Identity, sessions []*transcripts.Session) []*transcripts.Session {
	ledger := Ledger()
	resolved := map[string]string{}

	var mine []*transcripts.Session
	for _, s := range sessions {
		dir := filepath.Clean(s.CWD)
		if dir == "" || dir == "." {
			continue
		}
		slug, known := resolved[dir]
		if !known {
			slug = slugFor(ctx, dir, ledger)
			resolved[dir] = slug
		}
		if slug == id.Slug {
			mine = append(mine, s)
		}
	}
	sort.SliceStable(mine, func(i, j int) bool { return mine[i].LastUsed.Before(mine[j].LastUsed) })
	return mine
}

// Outstanding is the project's transcripts with unread bytes, oldest first. A
// stat per file, so how far behind a project is costs nothing to ask.
func (s *Store) Outstanding(ctx context.Context, sessions []*transcripts.Session) []Unread {
	consumed := s.consumed()
	var out []Unread
	for _, session := range Corpus(ctx, s.id, sessions) {
		info, err := os.Stat(session.Path)
		if err != nil {
			continue
		}
		was := consumed[session.Path]
		if info.Size() <= was.Size {
			continue
		}
		out = append(out, Unread{
			Session: session.ID, Path: session.Path, Title: session.Title,
			At: session.LastUsed, From: was.Size, Size: info.Size(),
		})
	}
	return out
}

// MarkRead records how much of each transcript a run read. A size, not a done
// flag: a session resumed afterwards resumes from where it stopped.
func (s *Store) MarkRead(read ...Unread) error {
	writeMu.Lock()
	defer writeMu.Unlock()

	consumed := s.consumed()
	now := time.Now()
	for _, u := range read {
		consumed[u.Path] = Consumed{Size: u.Size, At: now}
	}
	return writeJSON(s.path(consumedFile), consumed)
}

func (s *Store) consumed() map[string]Consumed {
	consumed := map[string]Consumed{}
	readJSON(s.path(consumedFile), &consumed)
	return consumed
}

// slugFor resolves a directory to a project: the ledger first, since a removed
// worktree has nothing else, then git for one that is still there.
func slugFor(ctx context.Context, dir string, ledger map[string]string) string {
	if slug, ok := ledger[dir]; ok {
		return slug
	}
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	id, err := Resolve(ctx, dir)
	if err != nil || id.Slug == "" {
		return ""
	}
	ledger[dir] = id.Slug
	if err := Record(dir, id); err != nil {
		return id.Slug
	}
	return id.Slug
}

// Siblings are directories named like a removed worktree of this project but
// never resolved to it. A convention, not a fact, so they are only offered.
func Siblings(id Identity, sessions []*transcripts.Session) []string {
	name := id.RepoName()
	if name == "" {
		return nil
	}
	ledger := Ledger()
	// Only beside a checkout already known to be this project. The naming
	// convention alone would match any repository called ade-something.
	parents := map[string]bool{}
	for path, slug := range ledger {
		if slug == id.Slug {
			parents[filepath.Dir(path)] = true
		}
	}

	seen, out := map[string]bool{}, []string{}
	for _, s := range sessions {
		dir := filepath.Clean(s.CWD)
		if dir == "" || seen[dir] || ledger[dir] != "" || !parents[filepath.Dir(dir)] {
			continue
		}
		if !strings.HasPrefix(filepath.Base(dir), name+"-") {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	sort.Strings(out)
	return out
}

func ledgerPath() (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ledgerFile), nil
}
