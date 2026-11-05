package sessions

import (
	"cmp"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/runlog"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/settings"
)

// maxDepth is how far below a scan root to look. Repositories live at
// ~/code/repo or ~/code/owner/repo; going deeper finds vendored copies rather
// than work.
const maxDepth = 3

// Dir is a checkout you can open, with the conversations that happened in it.
type Dir struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Owner  string `json:"owner,omitempty"`
	Branch string `json:"branch,omitempty"`

	// LastUsed is the newest session in this checkout, or zero for one you have
	// never run an agent in. Those are the interesting ones to start something
	// new in, so they are listed rather than hidden.
	LastUsed time.Time `json:"lastUsed,omitempty"`

	Sessions []Session `json:"sessions"`

	// Main is the repository's main checkout, which is this path unless this is
	// a worktree. It is what groups worktrees under the thing they are of.
	Main string `json:"main,omitempty"`

	// Worktree marks a checkout that is one, so the level showing them can say
	// which is the repository itself.
	Worktree bool `json:"worktree,omitempty"`

	// Here marks the checkout you are already looking at. The launcher belongs
	// to none, so nothing it lists is ever here.
	Here bool `json:"here,omitempty"`

	// Scratch marks a checkout ade made for work with nowhere to live yet, so
	// the list can say which of these are still waiting to be moved somewhere.
	Scratch bool `json:"scratch,omitempty"`
}

// Repo is a git repository: its main checkout, and every worktree of it.
//
// A worktree is a place to work on this repository, not a repository of its
// own, so it belongs one level down rather than beside its parent.
type Repo struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Owner  string `json:"owner,omitempty"`
	Branch string `json:"branch,omitempty"`

	// LastUsed is the newest session anywhere in the repository, and Sessions
	// the total across its checkouts, archived ones aside, so the top level can
	// be ordered and counted without opening it.
	LastUsed time.Time `json:"lastUsed,omitempty"`
	Sessions int       `json:"sessions"`

	// Checkouts is the main one first, then its worktrees.
	Checkouts []Dir `json:"checkouts"`

	// Here marks the repository holding the checkout you are already looking
	// at. The launcher belongs to none, so nothing it lists is ever here.
	Here bool `json:"here,omitempty"`

	// Scratch marks a repository ade made for work with nowhere to live yet.
	Scratch bool `json:"scratch,omitempty"`

	// Remembered is whether ade keeps a project memory for this repository.
	// One flag for the repository, not the checkout: every worktree shares it.
	Remembered bool `json:"remembered,omitempty"`
}

// Repos lists the repositories under roots, most recently worked in first,
// each carrying its worktrees and their sessions.
//
// The roots decide what is on the list. A conversation that happened somewhere
// else — a scratch repository under the temp directory, most often — attaches
// to nothing and is left out; widen the roots if you want it.
func Repos(roots []string, ignored func(string) bool) []Repo {
	dirs := Dirs(roots, ignored)

	byMain := map[string]*Repo{}
	var order []string
	for _, dir := range dirs {
		main := dir.Main
		repo, seen := byMain[main]
		if !seen {
			repo = &Repo{Path: main, Name: filepath.Base(main), Owner: ownerOf(main)}
			byMain[main] = repo
			order = append(order, main)
		}
		repo.Checkouts = append(repo.Checkouts, dir)
		// Counted without the archived ones: the number says how much is going
		// on here, and what you put away is not.
		for _, session := range dir.Sessions {
			if !session.Archived {
				repo.Sessions++
			}
		}
		if dir.LastUsed.After(repo.LastUsed) {
			repo.LastUsed = dir.LastUsed
		}
		// The repository is named by its main checkout, so that is the branch
		// worth showing beside it.
		if dir.Path == main {
			repo.Branch = dir.Branch
			repo.Owner = dir.Owner
			repo.Scratch = dir.Scratch
		}
	}

	out := make([]Repo, 0, len(order))
	for _, main := range order {
		repo := byMain[main]
		sort.SliceStable(repo.Checkouts, func(i, j int) bool {
			// The main checkout leads; the worktrees follow it by recency.
			if (repo.Checkouts[i].Path == main) != (repo.Checkouts[j].Path == main) {
				return repo.Checkouts[i].Path == main
			}
			if repo.Checkouts[i].LastUsed.Equal(repo.Checkouts[j].LastUsed) {
				return strings.ToLower(repo.Checkouts[i].Path) < strings.ToLower(repo.Checkouts[j].Path)
			}
			return repo.Checkouts[i].LastUsed.After(repo.Checkouts[j].LastUsed)
		})
		out = append(out, *repo)
	}
	sort.SliceStable(out, func(i, j int) bool {
		// Somewhere you were working beats somewhere you might; among the rest,
		// alphabetical is the only order that means anything.
		if out[i].LastUsed.Equal(out[j].LastUsed) {
			return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path)
		}
		return out[i].LastUsed.After(out[j].LastUsed)
	})
	return out
}

// MarkRepoHere flags the repository holding a server's checkout, and the
// checkout itself, so its switcher can say where you already are.
func MarkRepoHere(repos []Repo, root string) []Repo {
	if root == "" {
		return repos
	}
	for i := range repos {
		for j := range repos[i].Checkouts {
			if repos[i].Checkouts[j].Path == root {
				repos[i].Checkouts[j].Here = true
				repos[i].Here = true
			}
		}
	}
	return repos
}

// FindCheckout picks a checkout out of the repositories, so a request can only
// name somewhere that was actually offered.
func FindCheckout(repos []Repo, path string) (Dir, bool) {
	for _, repo := range repos {
		for _, dir := range repo.Checkouts {
			if dir.Path == path {
				return dir, true
			}
		}
	}
	return Dir{}, false
}

// Dirs lists every checkout under roots, worktrees included and unaggregated.
func Dirs(roots []string, ignored func(string) bool) []Dir {
	if ignored == nil {
		ignored = func(string) bool { return false }
	}

	found := map[string]*Dir{}
	for _, root := range roots {
		for _, path := range checkouts(root, ignored) {
			if _, seen := found[path]; !seen {
				dir := newDir(path)
				// A repository sitting directly in a scan root has no owner;
				// naming the root as one would say "code" against everything.
				if filepath.Dir(path) == root {
					dir.Owner = ""
				}
				found[path] = dir
			}
		}
	}

	titles, archived := settings.Titles(), settings.Archived()
	out := make([]Dir, 0, len(found))
	for _, dir := range found {
		if held, ok := sessionIn(dir, titles, archived); ok {
			dir.Sessions, dir.LastUsed = []Session{held}, held.LastUsed
		}
		out = append(out, *dir)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].LastUsed.Equal(out[j].LastUsed) {
			return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path)
		}
		return out[i].LastUsed.After(out[j].LastUsed)
	})
	return out
}

// FindDir picks a checkout out of a list, so a request can only name one that
// was actually offered.
func FindDir(list []Dir, path string) (Dir, bool) {
	for _, dir := range list {
		if dir.Path == path {
			return dir, true
		}
	}
	return Dir{}, false
}

func newDir(path string) *Dir {
	dir := &Dir{Path: path, Name: filepath.Base(path), Branch: branchOf(path), Main: path, Scratch: settings.IsScratch(path)}
	if main, ok := mainOf(path); ok {
		dir.Main, dir.Worktree = main, true
	}
	dir.Owner = ownerOf(path)
	return dir
}

// ownerOf is the directory a checkout sits in.
func ownerOf(path string) string {
	parent := filepath.Base(filepath.Dir(path))
	if parent == "." || parent == string(filepath.Separator) {
		return ""
	}
	return parent
}

// checkouts walks a root for git checkouts, stopping at each one: what is
// inside a repository is that repository's business.
func checkouts(root string, ignored func(string) bool) []string {
	var out []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxDepth || ignored(dir) {
			return
		}
		if isCheckout(dir) {
			out = append(out, dir)
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			walk(filepath.Join(dir, entry.Name()), depth+1)
		}
	}
	walk(root, 0)
	return out
}

func isCheckout(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// branchOf reads HEAD rather than shelling out to git, since this runs for
// every checkout on the machine. A detached head has no branch to report.
func branchOf(dir string) string {
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return ""
	}
	// A worktree's .git is a file pointing at the real directory.
	if pointer, ok := gitPointer(dir); ok {
		gitDir = pointer
	}

	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD")) // #nosec G304 -- the checkout's own git dir
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(head))
	if !strings.HasPrefix(ref, "ref: refs/heads/") {
		return ""
	}
	return strings.TrimPrefix(ref, "ref: refs/heads/")
}

// MainCheckout resolves a worktree to the checkout it belongs to, for a caller
// holding a path and no scan. False for a checkout that is one.
func MainCheckout(dir string) (string, bool) { return mainOf(dir) }

// mainOf resolves a worktree to the checkout it belongs to. A worktree's .git
// is a file naming <main>/.git/worktrees/<name>, which is the only reliable
// link back: names like coast-fix-625 are a convention, not a fact.
func mainOf(dir string) (string, bool) {
	pointer, ok := gitPointer(dir)
	if !ok {
		return "", false
	}
	// .../<main>/.git/worktrees/<name> → .../<main>
	parent := filepath.Dir(pointer)
	if filepath.Base(parent) != "worktrees" {
		return "", false
	}
	gitDir := filepath.Dir(parent)
	if filepath.Base(gitDir) != ".git" {
		return "", false
	}
	return filepath.Dir(gitDir), true
}

// gitPointer reads a worktree's .git file. A checkout with a real .git
// directory is not a worktree and has none.
// GitDir is a checkout's own git directory: .git for a main checkout, and
// the directory its .git file points at for a worktree. It is where ade
// keeps the checkout's server record.
func GitDir(dir string) string {
	if pointer, ok := gitPointer(dir); ok {
		return pointer
	}
	return filepath.Join(dir, ".git")
}

func gitPointer(dir string) (string, bool) {
	path := filepath.Join(dir, ".git")
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	data, err := os.ReadFile(path) // #nosec G304 -- inside a checkout we just found
	if err != nil {
		return "", false
	}
	pointer := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
	if pointer == "" {
		return "", false
	}
	return strings.TrimSpace(pointer), true
}

// sessionIn is the session ade recorded for a checkout. No record, no session.
func sessionIn(dir *Dir, titles map[string]string, archived map[string]bool) (Session, bool) {
	gitDir := GitDir(dir.Path)
	held := runlog.Read(runlog.Path(gitDir))
	if held.Session == "" {
		return Session{}, false
	}
	rec, serving := session.Read(session.Path(gitDir))
	return Session{
		ID:        held.Session,
		Agent:     cmp.Or(held.Provider, "claude"),
		Dir:       dir.Path,
		Name:      dir.Name,
		Branch:    dir.Branch,
		Title:     titles[held.Session],
		LastUsed:  held.Used,
		Ticket:    held.Ticket,
		Tags:      append([]string{}, held.Tags...),
		Brief:     runlog.Brief(gitDir, filepath.Join(dir.Main, ".git")),
		Resumable: true,
		Archived:  archived[held.Session],
		Live:      serving && session.Running(rec.PID),
	}, true
}
