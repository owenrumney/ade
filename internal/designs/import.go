package designs

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Imported is one document moved into the store, or the reason it was not.
type Imported struct {
	From string `json:"from"`
	Name string `json:"name"`
	Why  string `json:"why,omitempty"`
}

// Took reports whether the document went in.
func (i Imported) Took() bool { return i.Why == "" }

// Import copies a checkout's documents into the store, naming the repository
// in each. A copy, so the originals stay until you are happy.
func Import(root, from, repo string) ([]Imported, error) {
	found, err := os.ReadDir(from)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Imported
	for _, each := range found {
		if each.IsDir() || !strings.HasSuffix(each.Name(), ".md") {
			continue
		}
		out = append(out, ImportFile(root, filepath.Join(from, each.Name()), repo))
	}
	return out, nil
}

// ImportFile copies one document into the store.
func ImportFile(root, path, repo string) Imported {
	name := strings.TrimSuffix(filepath.Base(path), ".md")
	got := Imported{From: path, Name: Slug(name)}
	raw, err := os.ReadFile(path) // #nosec G304 -- a checkout's own docs directory
	if err != nil {
		got.Why = err.Error()
		return got
	}
	meta, body, err := Split(raw)
	if err != nil {
		got.Why = err.Error()
		return got
	}
	if repo != "" && !has(meta.Repos, repo) {
		meta.Repos = append(meta.Repos, repo)
	}
	if meta.Title == "" {
		meta.Title = title(body, name)
	}
	if _, err := Write(root, name, meta, body); err != nil {
		got.Why = err.Error()
	}
	return got
}

// title is the document's first heading, which is what it calls itself.
func title(body []byte, fallback string) string {
	for _, line := range bytes.Split(body, []byte("\n")) {
		if rest, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("# ")); ok {
			return strings.TrimSpace(string(rest))
		}
	}
	return fallback
}

func has(list []string, want string) bool {
	for _, each := range list {
		if strings.EqualFold(each, want) {
			return true
		}
	}
	return false
}

// Summary is what the import says it did.
func Summary(all []Imported) string {
	took, left := 0, 0
	for _, each := range all {
		if each.Took() {
			took++
			continue
		}
		left++
	}
	return fmt.Sprintf("%d imported, %d left behind", took, left)
}
