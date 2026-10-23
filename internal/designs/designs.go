// Package designs is the one place design documents live: a directory per
// design, shared between machines over git. See the design doc.
package designs

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/owenrumney/ade/internal/settings"
)

// File is the document inside a design's directory. One name, so a session
// mounting the directory always knows what to open.
const File = "design.md"

// Meta is a design's front matter: what it touches, and what it is for.
type Meta struct {
	// Repos are the repositories a session on this design is started
	// across, as owner/name. The first is home.
	Repos []string `yaml:"repos,omitempty"`
	// Ticket is the issue this came from, when it came from one.
	Ticket string `yaml:"ticket,omitempty"`
	Title  string `yaml:"title,omitempty"`
}

// Design is one document in the store.
type Design struct {
	Name     string    `json:"name"`
	Dir      string    `json:"dir"`
	File     string    `json:"file"`
	Meta     Meta      `json:"meta"`
	Modified time.Time `json:"modified,omitempty"`
}

// Home is where the store lives, whether or not it has been made.
func Home() (string, error) {
	dir, err := settings.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "designs"), nil
}

// unsafeName is everything a directory name may not hold. A design is named
// for its branch, so the two agree.
var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// Slug is the directory a design is kept in, from whatever it was called.
func Slug(name string) string {
	s := unsafeName.ReplaceAllString(strings.TrimSpace(name), "-")
	s = strings.Trim(s, "-.")
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// splitter finds the front matter block, which must open the file.
var splitter = regexp.MustCompile(`(?s)\A---\r?\n(.*?)\r?\n---\r?\n?`)

// Split separates a document's front matter from its body. A file without
// any is all body, which is what every design written before this was.
func Split(raw []byte) (Meta, []byte, error) {
	found := splitter.FindSubmatch(raw)
	if found == nil {
		return Meta{}, raw, nil
	}
	var meta Meta
	if err := yaml.Unmarshal(found[1], &meta); err != nil {
		return Meta{}, raw, fmt.Errorf("the front matter does not parse: %w", err)
	}
	return meta, raw[len(found[0]):], nil
}

// Join writes front matter back above a body, leaving out a block with
// nothing in it rather than writing an empty one.
func Join(meta Meta, body []byte) ([]byte, error) {
	if len(meta.Repos) == 0 && meta.Ticket == "" && meta.Title == "" {
		return body, nil
	}
	head, err := yaml.Marshal(meta)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteString("---\n")
	out.Write(head)
	out.WriteString("---\n")
	out.Write(bytes.TrimLeft(body, "\n"))
	return out.Bytes(), nil
}

// Read is one design, by directory name.
func Read(root, name string) (Design, error) {
	slug := Slug(name)
	if slug == "" {
		return Design{}, errors.New("a design needs a name")
	}
	dir := filepath.Join(root, slug)
	file := filepath.Join(dir, File)
	raw, err := os.ReadFile(file) // #nosec G304 -- ade's own store, under a slug
	if err != nil {
		return Design{}, err
	}
	meta, _, err := Split(raw)
	if err != nil {
		return Design{}, fmt.Errorf("%s: %w", slug, err)
	}
	out := Design{Name: slug, Dir: dir, File: file, Meta: meta}
	if info, err := os.Stat(file); err == nil {
		out.Modified = info.ModTime()
	}
	return out, nil
}

// List is every design in the store, newest first. A directory holding no
// document is not one; the store's own .git is skipped by that rule.
func List(root string) ([]Design, error) {
	found, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Design
	for _, each := range found {
		if !each.IsDir() || strings.HasPrefix(each.Name(), ".") {
			continue
		}
		design, err := Read(root, each.Name())
		if err != nil {
			continue
		}
		out = append(out, design)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Modified.Equal(out[j].Modified) {
			return out[i].Name < out[j].Name
		}
		return out[i].Modified.After(out[j].Modified)
	})
	return out, nil
}

// Write puts a design in the store, making its directory. It refuses to
// replace a document that is already there: that is Save's job, not this.
func Write(root, name string, meta Meta, body []byte) (Design, error) {
	slug := Slug(name)
	if slug == "" {
		return Design{}, errors.New("a design needs a name")
	}
	dir := filepath.Join(root, slug)
	file := filepath.Join(dir, File)
	if _, err := os.Stat(file); err == nil {
		return Design{}, fmt.Errorf("%s is already in the store", slug)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Design{}, err
	}
	if meta.Title == "" {
		meta.Title = slug
	}
	raw, err := Join(meta, scaffold(meta.Title, body))
	if err != nil {
		return Design{}, err
	}
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		return Design{}, err
	}
	return Read(root, slug)
}

// Save replaces a document already in the store. Write makes one and
// refuses an existing name; this is the other half.
func Save(root, name string, meta Meta, body []byte) error {
	slug := Slug(name)
	if slug == "" {
		return errors.New("a design needs a name")
	}
	file := filepath.Join(root, slug, File)
	if _, err := os.Stat(file); err != nil {
		return fmt.Errorf("%s is not in the store", slug)
	}
	if meta.Title == "" {
		meta.Title = slug
	}
	raw, err := Join(meta, body)
	if err != nil {
		return err
	}
	return os.WriteFile(file, raw, 0o600)
}

// scaffold is the body a new design starts with, or the one handed in.
func scaffold(title string, body []byte) []byte {
	if len(bytes.TrimSpace(body)) > 0 {
		return body
	}
	return []byte("# " + title + "\n\n## Problem\n\n## Approach\n\n## Open questions\n")
}
