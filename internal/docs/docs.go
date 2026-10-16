// Package docs manages the markdown design documents ade renders and edits.
package docs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/owenrumney/ade/internal/filetime"
	"github.com/owenrumney/ade/internal/render"
	"github.com/owenrumney/ade/internal/safepath"
	"github.com/owenrumney/ade/internal/statedir"
)

// scratchLink is the per-repo scratch directory, usually a symlink into the
// watchtower store. It is preferred when present so documents land where the
// rest of a repo's scratch work already lives.
const scratchLink = ".scratch"

// maxDocBytes bounds a saved document, since it arrives from the browser.
const maxDocBytes = 4 << 20

// ticketRE matches a Linear-style reference in a branch name. The digits must
// not run into a dot, so a dependency bump like undici-6.27.0 is not read as a
// ticket.
var ticketRE = regexp.MustCompile(`(?i)\b([a-z]{2,6})-(\d+)(?:[^0-9.]|$)`)

// Ticket pulls the issue reference out of a branch name, or returns empty when
// there is not one. feat/dev-82-add-the-lsp gives DEV-82.
func Ticket(branch string) string {
	match := ticketRE.FindStringSubmatch(branch)
	if match == nil {
		return ""
	}
	return strings.ToUpper(match[1]) + "-" + match[2]
}

// SearchDirs lists every directory a design document might live in, most
// specific first. Unlike Dir it creates nothing, and skips what is not there.
//
// commonGitDir is the repository's shared git dir, so the main checkout's
// .scratch is searched too: a worktree has no .scratch of its own, and the
// document it was created for was written in the checkout it came from.
func SearchDirs(root, commonGitDir, override string) []string {
	candidates := []string{override, filepath.Join(root, scratchLink)}
	if main := mainRoot(commonGitDir); main != "" && main != root {
		candidates = append(candidates, filepath.Join(main, scratchLink))
	}
	candidates = append(candidates, filepath.Join(statedir.Git(commonGitDir), "docs"))

	var dirs []string
	seen := map[string]bool{}
	for _, dir := range candidates {
		if dir == "" || !filepath.IsAbs(dir) || seen[dir] || !isDir(dir) {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	return dirs
}

// mainRoot is the checkout that owns the shared git dir, which is its parent
// when that dir is a plain .git.
func mainRoot(commonGitDir string) string {
	if commonGitDir == "" || filepath.Base(commonGitDir) != ".git" {
		return ""
	}
	return filepath.Dir(commonGitDir)
}

// Find locates the document belonging to a branch, returning the directory that
// holds it so the caller knows where to read from. A branch with no matching
// document is the ordinary case and reports false.
// What this session last had open wins: a document named for what it designs
// cannot be matched by name. Another session's open document is not a match.
func Find(dirs []string, commonGitDir, branch, session string) (string, string, bool) {
	if file, ok := Opened(commonGitDir, branch, session); ok {
		if file == "" {
			return "", "", false
		}
		if dir, found := locate(dirs, file); found {
			return dir, file, true
		}
	}

	ticket := Ticket(branch)
	name := lastSegment(branch)

	// Most specific first. The ticket is a deliberate reference where a name can
	// be coincidence, and a file named exactly for the ticket beats one that
	// merely mentions it.
	for _, match := range []func(string) bool{
		func(file string) bool { return ticket != "" && strings.EqualFold(stem(file), ticket) },
		func(file string) bool { return ticket != "" && strings.Contains(strings.ToUpper(file), ticket) },
		func(file string) bool { return name != "" && strings.EqualFold(stem(file), name) },
	} {
		for _, dir := range dirs {
			files, err := markdownFiles(dir)
			if err != nil {
				continue
			}
			for _, file := range files {
				if match(file) {
					return dir, file, true
				}
			}
		}
	}
	return "", "", false
}

func locate(dirs []string, file string) (string, bool) {
	for _, dir := range dirs {
		full, err := Resolve(dir, file)
		if err != nil {
			continue
		}
		if _, err := os.Stat(full); err == nil {
			return dir, true
		}
	}
	return "", false
}

func lastSegment(branch string) string {
	if i := strings.LastIndex(branch, "/"); i >= 0 {
		return branch[i+1:]
	}
	return branch
}

func stem(file string) string {
	return strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
}

// Dir resolves where a new design document is written, creating it if needed. It
// prefers the repo's .scratch directory and otherwise keeps them beside the
// thread store under the git dir, where they are never committed.
func Dir(root, commonGitDir, override string) (string, error) {
	// The same order SearchDirs uses, so a document is created where the next
	// run will look for it. Diverging here is what put one in .scratch and then
	// hunted for it under the git dir.
	var dir string
	switch existing := SearchDirs(root, commonGitDir, override); {
	case override != "":
		dir = override
	case len(existing) > 0:
		dir = existing[0]
	case commonGitDir != "":
		dir = filepath.Join(statedir.Git(commonGitDir), "docs")
	default:
		return "", fmt.Errorf("no git dir to store design documents in; pass an explicit directory")
	}
	// A relative path would resolve against the process working directory, which
	// is not necessarily the repository.
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("design directory %q must be absolute", dir)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create docs dir %s: %w", dir, err)
	}
	return dir, nil
}

// Tree lists the markdown files under dir, marking current as selected.
func Tree(dir, current string) ([]render.FileEntry, error) {
	files, err := markdownFiles(dir)
	if err != nil {
		return nil, err
	}
	made := map[string]time.Time{}
	for _, file := range files {
		if info, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(file))); err == nil {
			made[file] = filetime.Created(info)
		}
	}
	return buildTree(files, current, made), nil
}

// List names the markdown files under dir, in the order the tree draws them.
func List(dir string) ([]string, error) {
	return markdownFiles(dir)
}

// Stem is a document's name without its directory or extension, which is what
// a branch's last segment is matched against.
func Stem(file string) string { return stem(file) }

// LastSegment is the part of a branch name after the last slash.
func LastSegment(branch string) string { return lastSegment(branch) }

// Ensure returns the document for name, scaffolding it only if it is not already
// there. Running the same create twice is asking to carry on with that design,
// not an error.
func Ensure(dir, name, title string) (string, bool, error) {
	file := Filename(name)
	if file == "" {
		return "", false, fmt.Errorf("a document needs a name")
	}
	full, err := Resolve(dir, file)
	if err != nil {
		return "", false, err
	}
	if _, err := os.Stat(full); err == nil {
		return file, false, nil
	}

	created, err := Create(dir, name, title)
	if err != nil {
		return "", false, err
	}
	return created, true, nil
}

// Create writes a new document and returns its path relative to dir. It refuses
// to overwrite, so an existing design is never silently replaced.
func Create(dir, name, title string) (string, error) {
	file := Filename(name)
	if file == "" {
		return "", fmt.Errorf("a document needs a name")
	}
	full, err := Resolve(dir, file)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(full); err == nil {
		return "", fmt.Errorf("%s already exists", file)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return "", fmt.Errorf("create %s: %w", file, err)
	}
	if err := os.WriteFile(full, []byte(scaffold(title, file)), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", file, err)
	}
	return file, nil
}

// Filename turns a user-supplied name into a relative markdown path.
func Filename(name string) string {
	name = clean(name)
	if name == "" {
		return ""
	}
	if !isMarkdown(name) {
		name += ".md"
	}
	return name
}

// Render renders one document to an HTML fragment plus its contents list.
func Render(dir, file string) (render.RenderedDocument, error) {
	full, err := Resolve(dir, file)
	if err != nil {
		return render.RenderedDocument{}, err
	}
	// Plain: a design document is read for its argument, and chroma colours
	// every fence as if it were production code.
	return render.Document(full, render.Options{Theme: render.ThemeDark, Plain: true})
}

// Read returns the raw markdown of a document, for editing.
func Read(dir, file string) (string, error) {
	full, err := Resolve(dir, file)
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(full) // #nosec G304 -- Resolve confines full to dir
	if err != nil {
		return "", fmt.Errorf("read %s: %w", file, err)
	}
	return string(body), nil
}

// Version fingerprints a document's contents, so a watcher can tell that the
// agent has rewritten it. Empty when there is nothing to read, which reads as
// "no document" rather than as a change.
func Version(dir, file string) string {
	body, err := Read(dir, file)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:8])
}

// Save overwrites a document with edited markdown.
func Save(dir, file, body string) error {
	if len(body) > maxDocBytes {
		return fmt.Errorf("document is too large to save")
	}
	full, err := Resolve(dir, file)
	if err != nil {
		return err
	}
	// O_NOFOLLOW as well as Resolve: a link planted in the interval between the
	// two would otherwise be followed by the write.
	handle, err := os.OpenFile(full, os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600) // #nosec G304 -- Resolve confines full to dir
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s does not exist", file)
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", file, err)
	}
	if _, err := handle.Write([]byte(body)); err != nil {
		_ = handle.Close()
		return fmt.Errorf("write %s: %w", file, err)
	}
	if err := handle.Close(); err != nil {
		return fmt.Errorf("write %s: %w", file, err)
	}
	return nil
}

// Put writes a document by name whether or not it is there already. Create
// refuses an existing name and Save an absent one; a pushed document is neither.
func Put(dir, name, body string) (string, error) {
	if len(body) > maxDocBytes {
		return "", fmt.Errorf("document is too large to save")
	}
	file := Filename(name)
	if file == "" {
		return "", fmt.Errorf("a document needs a name")
	}
	// Put creates, so a relative dir would write into the process working
	// directory rather than failing to find a document there.
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("there is nowhere design documents live for this checkout")
	}
	full, err := Resolve(dir, file)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return "", fmt.Errorf("create %s: %w", file, err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", file, err)
	}
	return file, nil
}

// Locate finds a document by name across the directories designs live in,
// returning the full path. The name may be given with or without .md.
func Locate(dirs []string, name string) (string, error) {
	file := Filename(name)
	if file == "" {
		return "", fmt.Errorf("name a document to look for")
	}
	for _, dir := range dirs {
		full, err := Resolve(dir, file)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(full); err == nil {
			return full, nil
		}
	}
	return "", fmt.Errorf("no document called %s", file)
}

// Delete removes a document, returning where it was. Only files inside the given
// directories can be reached, so a name cannot be used to delete anything else.
func Delete(dirs []string, name string) (string, error) {
	full, err := Locate(dirs, name)
	if err != nil {
		return "", err
	}
	if err := os.Remove(full); err != nil {
		return "", fmt.Errorf("remove %s: %w", full, err)
	}
	return full, nil
}

// Resolve turns a relative document name into an absolute path inside dir, and
// refuses anything that would escape it.
func Resolve(dir, file string) (string, error) {
	name := clean(file)
	if name == "" {
		return "", fmt.Errorf("%q is not a document in the design directory", file)
	}
	if !isMarkdown(name) {
		return "", fmt.Errorf("%s is not a markdown file", name)
	}
	// An agent writes this directory, so a link planted in it is a way to read
	// or write anything the user can.
	full, err := safepath.Inside(dir, name)
	if err != nil {
		return "", fmt.Errorf("%q is not a document in the design directory", file)
	}
	return full, nil
}

func clean(name string) string { return safepath.Relative(name) }

func isMarkdown(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// scaffold seeds a new document. The guidance sits in HTML comments so it shows
// while you are editing, renders as nothing, and is not mistaken for a written
// brief by anything deciding whether the design says something yet.
func scaffold(title, file string) string {
	if strings.TrimSpace(title) == "" {
		title = strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	}
	return "# " + title + `

## Problem

<!-- What is wrong today, and who it bites. Concrete beats abstract: the case
     that breaks, not the quality that is missing. -->

## Approach

<!-- What you want to build, in enough detail that someone could disagree with
     it. Name the files or components you expect to touch. -->

## Open questions

<!-- Anything you have not decided. The agent will add to this when it spots
     something you have not covered. -->
`
}

func markdownFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !isMarkdown(path) {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	slices.SortFunc(files, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	return files, nil
}

// treeNode assembles the nested file list the sidebar renders.
type treeNode struct {
	entry    render.FileEntry
	children map[string]*treeNode
}

func buildTree(files []string, current string, made map[string]time.Time) []render.FileEntry {
	root := &treeNode{children: map[string]*treeNode{}}
	for _, file := range files {
		parent := root
		parts := strings.Split(file, "/")
		for i, part := range parts {
			if part == "" {
				continue
			}
			if i == len(parts)-1 {
				parent.children[part] = &treeNode{entry: render.FileEntry{
					Name: part, Path: file, Selected: file == current, Created: made[file],
				}}
				continue
			}
			child := parent.children[part]
			if child == nil {
				child = &treeNode{entry: render.FileEntry{Name: part, Dir: true}, children: map[string]*treeNode{}}
				parent.children[part] = child
			}
			parent = child
		}
	}
	return flatten(root)
}

// flatten sorts each level with directories first, then alphabetically.
func flatten(n *treeNode) []render.FileEntry {
	keys := make([]string, 0, len(n.children))
	for key := range n.children {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if ad, bd := n.children[a].entry.Dir, n.children[b].entry.Dir; ad != bd {
			if ad {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})

	out := make([]render.FileEntry, 0, len(keys))
	for _, key := range keys {
		child := n.children[key]
		entry := child.entry
		if entry.Dir {
			entry.Children = flatten(child)
		}
		out = append(out, entry)
	}
	return out
}
