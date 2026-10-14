package web

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/filetime"
	"github.com/owenrumney/ade/internal/render"
	"github.com/owenrumney/ade/internal/safepath"
)

// FileEntry is one file in the Files tab.
type FileEntry struct {
	Root    string    `json:"root"`
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	At      time.Time `json:"modified"`
	Created time.Time `json:"created"`
	Kind    string    `json:"kind"`
}

// FileGroup is one root and what is in it. Note says why a group is empty or
// read-only, so the pane never shows a bare empty list.
type FileGroup struct {
	Root     string      `json:"root"`
	Label    string      `json:"label"`
	Note     string      `json:"note"`
	ReadOnly bool        `json:"readOnly"`
	Files    []FileEntry `json:"files"`
}

// The roots the tab reaches. An enum, not a path: nothing outside these three
// is listable, readable or deletable through it.
const (
	rootDocs        = "docs"
	rootAttachments = "attachments"
	rootOutbox      = "outbox"
)

// previewCap is the largest file the tab will serve for preview. Rendering a
// log of any size into the page locks the tab up, and Logs is the log viewer.
const previewCap = 2 << 20

// fileKinds decides the preview from the extension; nothing sniffs content.
var fileKinds = map[string]string{
	".md": "markdown", ".markdown": "markdown",
	".png": "image", ".jpg": "image", ".jpeg": "image", ".gif": "image", ".webp": "image",
	".txt": "text", ".log": "text", ".csv": "text", ".json": "text",
	".yml": "text", ".yaml": "text", ".html": "text", ".svg": "text",
	".env": "text", ".toml": "text", ".ini": "text", ".conf": "text",
}

// textTypes are served as text/plain when the attachment allow-list has no
// type of its own. Never .html or .svg: those would run on ade's origin.
var textTypes = map[string]bool{
	".yml": true, ".yaml": true, ".env": true,
	".toml": true, ".ini": true, ".conf": true,
}

// fileRoot is where one group lives, and what may be done to it.
type fileRoot struct {
	dir       string
	label     string
	recursive bool
	readOnly  bool
	note      string
}

// fileRoots resolves the three groups for this session. The docs directory is
// the one the Design tab reads, so both show the same documents.
func (s *Server) fileRoots() map[string]fileRoot {
	dir, _ := s.designPlace()
	gitDir := s.space().gitDir
	out := map[string]fileRoot{
		rootDocs: {dir: dir, label: "Documents", recursive: true,
			note: "shared with every session in this repository"},
		rootAttachments: {dir: chat.AttachDir(gitDir), label: "Attachments"},
		rootOutbox:      {dir: outboxPath(gitDir), label: "Outbox"},
	}
	// The outbox empties itself onto the turn's final reply, so a delete
	// mid-turn races that and can drop a picture the reply refers to.
	if s.agent.State().Running {
		out[rootOutbox] = fileRoot{dir: outboxPath(gitDir), label: "Outbox", readOnly: true,
			note: "the agent is running; the outbox empties onto its reply"}
	}
	return out
}

func (s *Server) handleFiles(w http.ResponseWriter, _ *http.Request) {
	roots := s.fileRoots()
	groups := make([]FileGroup, 0, len(roots))
	for _, name := range []string{rootDocs, rootAttachments, rootOutbox} {
		root := roots[name]
		files, err := listFiles(name, root)
		if err != nil {
			slog.Debug("list files", "root", name, "err", err)
		}
		groups = append(groups, FileGroup{
			Root: name, Label: root.label, Note: root.note, ReadOnly: root.readOnly, Files: files,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (s *Server) handleFileRaw(w http.ResponseWriter, r *http.Request) {
	path, name, err := s.resolveFile(r.URL.Query().Get("root"), r.URL.Query().Get("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// O_NOFOLLOW and then the handle, not the name: an agent writes these roots,
	// so a link swapped in after an Lstat would be what got served.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0) // #nosec G304 -- resolved inside one of the three roots
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	if info.Size() > previewCap {
		writeError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("%s is %d bytes; the preview stops at %d", name, info.Size(), previewCap))
		return
	}

	// The attachment allow-list, not a new one: an .html or .svg served with
	// its own type would run as a page on ade's origin.
	ext := strings.ToLower(filepath.Ext(name))
	kind, known := attachmentTypes[ext]
	if !known && (textTypes[ext] || envFile(name)) {
		kind, known = "text/plain; charset=utf-8", true
	}
	if !known {
		kind = "application/octet-stream"
		w.Header().Set("Content-Disposition",
			mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(name)}))
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := io.Copy(w, file); err != nil {
		slog.Debug("write file", "err", err)
	}
}

// handleFileRender is a markdown file as HTML. Never Unsafe: markdown an
// agent wrote or a user pasted is not a document this user wrote.
func (s *Server) handleFileRender(w http.ResponseWriter, r *http.Request) {
	path, name, err := s.resolveFile(r.URL.Query().Get("root"), r.URL.Query().Get("name"))
	if err != nil || fileKind(name) != "markdown" {
		http.NotFound(w, r)
		return
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	if info.Size() > previewCap {
		writeError(w, http.StatusRequestEntityTooLarge, name+" is too large to preview")
		return
	}
	doc, err := render.Document(path, render.Options{Theme: render.ThemeDark, Unsafe: false})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"html": string(doc.Body)})
}

// handleFileUses is which documents mention this file, so "is it safe to
// delete?" has an answer. One scan when one file is looked at, not on delete.
func (s *Server) handleFileUses(w http.ResponseWriter, r *http.Request) {
	_, name, err := s.resolveFile(r.URL.Query().Get("root"), r.URL.Query().Get("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	used, err := usedBy(s.docsDir, filepath.Base(name))
	if err != nil {
		slog.Debug("scan documents", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": used})
}

// usedBy is the documents under dir whose text contains name. The base name:
// a document may reach a picture by any relative path.
func usedBy(dir, name string) ([]string, error) {
	out := []string{}
	if dir == "" || name == "" {
		return out, nil
	}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || fileKind(path) != "markdown" {
			return nil
		}
		info, lerr := os.Lstat(path)
		if lerr != nil || !info.Mode().IsRegular() || info.Size() > previewCap {
			return nil
		}
		body, rerr := os.ReadFile(path) // #nosec G304 -- walked from the docs directory
		if rerr != nil || !strings.Contains(string(body), name) {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return nil
		}
		// A document that names itself is not a reference to itself.
		if filepath.Base(rel) != name {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out, ignoreMissing(err)
}

// handleFileDelete removes files from one root, reporting each as deleted or
// failed: an error for the batch leaves the page guessing which half went.
func (s *Server) handleFileDelete(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	names := r.URL.Query()["name"]
	if len(names) == 0 {
		writeError(w, http.StatusBadRequest, "name a file to delete")
		return
	}
	if _, ok := s.fileRoots()[root]; !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%q is not one of the file groups", root))
		return
	}
	type failure struct {
		Name string `json:"name"`
		Why  string `json:"why"`
	}
	deleted, failed := []string{}, []failure{}
	for _, name := range names {
		if err := s.deleteFile(root, name); err != nil {
			failed = append(failed, failure{Name: name, Why: err.Error()})
			continue
		}
		deleted = append(deleted, name)
	}
	if len(deleted) > 0 {
		s.hub.Broadcast("state", nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted, "failed": failed})
}

func (s *Server) deleteFile(root, name string) error {
	if s.fileRoots()[root].readOnly {
		return errors.New("the agent is running; wait for the turn to end")
	}
	// A build reads its brief as it runs, the same as the design routes.
	if root == rootDocs && s.buildingDoc(name) {
		return errors.New("the agent is building this document; wait for it or stop it first")
	}
	path, _, err := s.resolveFile(root, name)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%s is not there", name)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a file", name)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove %s: %w", name, err)
	}
	// A name that no longer exists is not something to hold open.
	if root == rootDocs && sameDoc(name, s.currentDoc()) {
		s.setDoc("")
	}
	return nil
}

// resolveFile turns a root and a name into a path inside that root. The name
// is relative: absolute paths and anything climbing out are refused here.
func (s *Server) resolveFile(root, name string) (string, string, error) {
	place, ok := s.fileRoots()[root]
	if !ok || place.dir == "" {
		return "", "", fmt.Errorf("%q is not one of the file groups", root)
	}
	clean := safepath.Relative(name)
	if clean == "" {
		return "", "", fmt.Errorf("%q is not a file in %s", name, root)
	}
	// Flat roots take a base name and nothing else; only documents nest.
	if !place.recursive {
		clean = filepath.Base(clean)
	}
	// Inside, not a Rel check: a link planted as a directory along the way
	// passes Rel, and only the last part is ever Lstat-ed.
	path, err := safepath.Inside(place.dir, clean)
	if err != nil {
		return "", "", fmt.Errorf("%q is not a file in %s", name, root)
	}
	return path, clean, nil
}

// listFiles is one root's regular files. A symlink is left out, not followed:
// an agent writes the outbox, and a link there would read what it points at.
func listFiles(root string, place fileRoot) ([]FileEntry, error) {
	out := []FileEntry{}
	if place.dir == "" {
		return out, nil
	}
	add := func(rel string, info os.FileInfo) {
		out = append(out, FileEntry{
			Root: root, Name: rel, Size: info.Size(), At: info.ModTime(),
			Created: filetime.Created(info), Kind: fileKind(rel),
		})
	}
	if !place.recursive {
		entries, err := os.ReadDir(place.dir)
		if err != nil {
			return out, ignoreMissing(err)
		}
		for _, entry := range entries {
			info, err := os.Lstat(filepath.Join(place.dir, entry.Name()))
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			add(entry.Name(), info)
		}
		sortFiles(out)
		return out, nil
	}
	err := filepath.WalkDir(place.dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, lerr := os.Lstat(path)
		if lerr != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(place.dir, path)
		if rerr != nil {
			return nil
		}
		add(filepath.ToSlash(rel), info)
		return nil
	})
	sortFiles(out)
	return out, ignoreMissing(err)
}

// sortFiles orders by name, so a document and the picture beside it sit
// together and one shift-click takes both.
func sortFiles(files []FileEntry) {
	sort.SliceStable(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
}

func ignoreMissing(err error) error {
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// envFile is a dotenv file by name. The extension alone misses .env.local.
func envFile(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	return base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".env")
}

func fileKind(name string) string {
	if envFile(name) {
		return "text"
	}
	if kind, ok := fileKinds[strings.ToLower(filepath.Ext(name))]; ok {
		return kind
	}
	return "other"
}
