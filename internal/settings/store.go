package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// This is the little state ade keeps beside its settings: which sessions are
// put away, what each is called, which pull requests and notifications you have
// waved off. Each lives in its own file under ~/.ade.
//
// One file each rather than one file for all of them, on purpose. The launcher
// and any number of servers write these, and a write is a read-modify-write of
// the whole file: two processes changing different things in one file would
// lose one of the changes, where two processes changing different files cannot.
// Keeping them apart bounds that to "the same kind of thing at the same moment".
//
// The duplication that would otherwise come with six files lives here instead:
// a store is its filename, and the two shapes below are all that is on top.

// store is one file's worth of state: where it is, and a lock over the
// read-modify-write. Package-level, one per file, so every caller in this
// process takes the same lock.
type store struct {
	name string
	mu   sync.Mutex
}

// path is the file, under ade's own directory.
func (s *store) path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, s.name), nil
}

// load reads the file into whatever shape the caller wants.
//
// A missing or unreadable file leaves the value alone rather than erroring:
// none of these existing is the ordinary first run, and a file corrupted by
// something else is not a reason to refuse to work.
func (s *store) load(into any) {
	file, err := s.path()
	if err != nil {
		return
	}
	data, err := os.ReadFile(file) // #nosec G304 -- ade's own state directory
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, into); err != nil {
		return
	}
}

// save writes it back, atomically, so two processes writing together cannot
// leave half a file behind.
func (s *store) save(from any) error {
	file, err := s.path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(from, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", s.name, err)
	}
	return replace(file, data, ".ade-"+s.name+"-*")
}

// flags is a store of "which of these": a JSON array of ids, read as a set.
type flags struct{ store }

// all is every id in the file.
func (f *flags) all() map[string]bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.read()
}

// set adds an id or takes it out. Setting what is already set writes nothing.
func (f *flags) set(id string, on bool) error {
	if id == "" {
		return fmt.Errorf("%s: nothing named", f.name)
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	have := f.read()
	if have[id] == on {
		return nil
	}
	if on {
		have[id] = true
	} else {
		delete(have, id)
	}
	// Sorted, so the file reads the same way twice.
	ids := make([]string, 0, len(have))
	for id := range have {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return f.save(ids)
}

func (f *flags) read() map[string]bool {
	var ids []string
	f.load(&ids)
	have := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id != "" {
			have[id] = true
		}
	}
	return have
}

// notes is a store of id-to-value: a JSON object.
type notes struct{ store }

// all is every entry in the file.
func (n *notes) all() map[string]string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.read()
}

// set records a value against an id. An empty value forgets the id.
func (n *notes) set(id, value string) error {
	if id == "" {
		return fmt.Errorf("%s: nothing named", n.name)
	}
	n.mu.Lock()
	defer n.mu.Unlock()

	have := n.read()
	if have[id] == value {
		return nil
	}
	if value == "" {
		delete(have, id)
	} else {
		have[id] = value
	}
	// A map marshals with its keys sorted, so this reads the same way twice
	// without any help.
	return n.save(have)
}

// keep drops every id that is not wanted, and writes only if one was.
func (n *notes) keep(wanted map[string]bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	have := n.read()
	dropped := false
	for id := range have {
		if !wanted[id] {
			delete(have, id)
			dropped = true
		}
	}
	if !dropped {
		return nil
	}
	return n.save(have)
}

func (n *notes) read() map[string]string {
	have := map[string]string{}
	n.load(&have)
	return have
}

// replace writes a file atomically, so two processes writing together cannot
// leave half a file behind.
func replace(file string, data []byte, pattern string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(file), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), pattern)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", file, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", file, err)
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		return fmt.Errorf("replace %s: %w", file, err)
	}
	return nil
}
