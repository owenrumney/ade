// Package wiki owns ADE's canonical, read-only checkout of the shared wiki.
package wiki

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/owenrumney/ade/internal/authstore"
)

const (
	branch     = "main"
	gitTimeout = 20 * time.Second
	// maxPage caps a single read: wiki content comes from a remote anyone
	// with push rights can grow without bound.
	maxPage = 2 << 20
)

var errTooLarge = errors.New("wiki page is too large")

// Status is the durable, user-visible condition of the canonical checkout.
type Status struct {
	Root      string    `json:"root"`
	Remote    string    `json:"remote"`
	Revision  string    `json:"revision,omitempty"`
	LastFetch time.Time `json:"last_fetch,omitempty"`
	LastError string    `json:"last_error,omitempty"`
	Repair    string    `json:"repair,omitempty"`
	Dirty     bool      `json:"dirty"`
}

// Hit is one Markdown line that matched a launcher search. Scope and Updated
// are what tells two hits apart; the file name rarely does, since every
// repository's notes are called the same thing.
type Hit struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
	Scope   string `json:"scope"`
	Updated string `json:"updated,omitempty"`
}

// Store manages one canonical clone below ADE's state directory.
type Store struct {
	root   string
	remote string
	mu     sync.Mutex
	// pub is the publish lock of the design: it covers publishing only, so a
	// slow writer never holds up search or a background fetch.
	pub sync.Mutex
	now func() time.Time
	run func(context.Context, ...string) (string, error)
}

// New prepares a store. An empty remote leaves it disabled.
func New(stateDir, remote string) *Store {
	return &Store{
		root:   filepath.Join(stateDir, "wiki"),
		remote: strings.TrimSpace(remote),
		now:    time.Now,
		run:    runGit,
	}
}

func (s *Store) Root() string  { return s.root }
func (s *Store) Enabled() bool { return s.remote != "" }

// CachedStatus reads only ADE's state file, so a launcher request never waits
// on Git or the network.
func (s *Store) CachedStatus() Status {
	st := s.read()
	st.Root, st.Remote = s.root, s.remote
	return st
}

const (
	maxHits     = 50
	hitsPerPage = 5
)

// Search scans the local canonical clone only. It never invokes Git.
func (s *Store) Search(query string) []Hit {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || !s.Enabled() {
		return []Hit{}
	}
	root, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return []Hit{}
	}
	hits, dates := []Hit{}, s.pageDates()
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || len(hits) == maxHits {
			return filepath.SkipDir
		}
		name := entry.Name()
		if path != root && strings.HasPrefix(name, ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(name), ".md") {
			return nil
		}
		real, err := filepath.EvalSymlinks(path)
		if err != nil || !within(real, root) {
			return nil
		}
		data, err := readCapped(real)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		// A few hits per page, or one long log fills the list and a page that
		// says the thing once, further down the walk, is never shown.
		inFile := 0
		for line, text := range strings.Split(string(data), "\n") {
			if !strings.Contains(strings.ToLower(text), query) {
				continue
			}
			slash := filepath.ToSlash(rel)
			hits = append(hits, Hit{Path: slash, Line: line + 1, Snippet: clip(text, 160), Scope: scope(slash), Updated: dates[slash]})
			if len(hits) == maxHits {
				return filepath.SkipDir
			}
			if inFile++; inFile == hitsPerPage {
				break
			}
		}
		return nil
	})
	return hits
}

// Page reads one safe, repository-relative Markdown path for the viewer.
func (s *Store) Page(path string) ([]byte, error) {
	if !s.Enabled() || !strings.EqualFold(filepath.Ext(path), ".md") {
		return nil, errors.New("wiki page not found")
	}
	root, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return nil, errors.New("wiki page not found")
	}
	path = filepath.Clean(filepath.FromSlash(path))
	if path == "." || filepath.IsAbs(path) || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return nil, errors.New("wiki page not found")
	}
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		if strings.HasPrefix(part, ".") {
			return nil, errors.New("wiki page not found")
		}
	}
	real, err := filepath.EvalSymlinks(filepath.Join(root, path))
	if err != nil || !within(real, root) {
		return nil, errors.New("wiki page not found")
	}
	return readCapped(real)
}

func within(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path) // #nosec G304 -- callers check the path is beneath the canonical root
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxPage+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPage {
		return nil, errTooLarge
	}
	return data, nil
}

func clip(text string, max int) string {
	if len(text) <= max {
		return text
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// Init creates the canonical clone. It is deliberately the only operation
// that may choose or replace origin.
func (s *Store) Init(ctx context.Context, remote string) error {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return errors.New("--remote is required")
	}
	if err := checkRemote(remote); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lockCanonical()
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := os.Stat(s.root); err == nil {
		if _, err := s.git(ctx, "remote", "set-url", "origin", remote); err != nil {
			return fmt.Errorf("set wiki origin: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat wiki root: %w", err)
	} else {
		if err := os.MkdirAll(filepath.Dir(s.root), 0o700); err != nil {
			return fmt.Errorf("create wiki parent: %w", err)
		}
		if _, err := s.run(ctx, "clone", "--branch", branch, "--single-branch", "--", remote, s.root); err != nil {
			return err
		}
	}
	s.remote = remote
	return s.write(Status{Root: s.root, Remote: remote, LastFetch: s.now()})
}

// Status reports state without changing a Git checkout.
func (s *Store) Status(ctx context.Context) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.read()
	st.Root, st.Remote = s.root, s.remote
	if !s.Enabled() {
		return st
	}
	if unlock, err := s.lockCanonical(); err == nil {
		defer unlock()
	}
	if repair := s.validate(ctx); repair != "" {
		st.Repair = repair
		st.Dirty = strings.Contains(repair, "dirty")
		return st
	}
	st.Repair = ""
	st.Dirty = false
	if rev, err := s.git(ctx, "rev-parse", "--verify", "HEAD"); err == nil {
		st.Revision = strings.TrimSpace(rev)
	}
	return st
}

// Sync fetches and fast-forwards a valid canonical clone. The file lock keeps
// a second ade process out of the same working tree.
func (s *Store) Sync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.Enabled() {
		return errors.New("wiki is disabled; run ade wiki init --remote <url>")
	}
	unlock, err := s.lockCanonical()
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.ensure(ctx); err != nil {
		s.recordError(err)
		return err
	}
	if err := s.syncLocked(ctx); err != nil {
		s.recordError(err)
		return err
	}
	return nil
}

func (s *Store) ensure(ctx context.Context) error {
	_, err := os.Stat(s.root)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat wiki root: %w", err)
	}
	if err := checkRemote(s.remote); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.root), 0o700); err != nil {
		return fmt.Errorf("create wiki parent: %w", err)
	}
	_, err = s.run(ctx, "clone", "--branch", branch, "--single-branch", "--", s.remote, s.root)
	return err
}

// checkRemote refuses a remote git would read as an option or a command.
func checkRemote(remote string) error {
	if strings.HasPrefix(remote, "-") {
		return errors.New("wiki remote may not start with '-'")
	}
	if strings.HasPrefix(strings.ToLower(remote), "ext::") {
		return errors.New("wiki remote may not use the ext:: transport")
	}
	return nil
}

// lockCanonical serialises canonical Git across processes: the launcher, each
// web server and the CLI all drive this one working tree.
func (s *Store) lockCanonical() (func(), error) {
	return authstore.Lock(s.root)
}

func (s *Store) validate(ctx context.Context) string {
	inside, err := s.git(ctx, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(inside) != "true" {
		return "canonical clone is corrupt"
	}
	status, err := s.git(ctx, "status", "--porcelain")
	if err != nil {
		return "canonical clone is unreadable"
	}
	if strings.TrimSpace(status) != "" {
		return "canonical clone is dirty"
	}
	head, err := s.git(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "canonical clone is detached"
	}
	if strings.TrimSpace(head) != branch {
		return "canonical clone is on unexpected branch " + strings.TrimSpace(head)
	}
	// config, not `remote get-url`: that applies url.<base>.insteadOf, so a
	// gitconfig rewriting https to ssh would fail a clone ade made itself.
	remote, err := s.git(ctx, "config", "--get", "remote.origin.url")
	if err != nil {
		return "canonical clone has no origin; run ade wiki init --remote " + s.remote
	}
	if found := strings.TrimSpace(remote); found != s.remote {
		return fmt.Sprintf("canonical clone's origin is %s, not the configured %s; run ade wiki init --remote %s to point it at the configured one", found, s.remote, s.remote)
	}
	upstream, err := s.git(ctx, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil || strings.TrimSpace(upstream) != "origin/"+branch {
		return "canonical clone does not track origin/main"
	}
	if _, err := s.git(ctx, "rev-parse", "--verify", "origin/"+branch); err != nil {
		return "canonical clone is missing origin/main"
	}
	return ""
}

func (s *Store) git(ctx context.Context, args ...string) (string, error) {
	argv := append([]string{"-C", s.root}, args...)
	return s.run(ctx, argv...)
}

func (s *Store) statePath() string { return filepath.Join(filepath.Dir(s.root), "wiki-status.json") }

func (s *Store) read() Status {
	data, err := os.ReadFile(s.statePath())
	if err != nil {
		return Status{}
	}
	var st Status
	if json.Unmarshal(data, &st) != nil {
		return Status{}
	}
	return st
}

func (s *Store) write(st Status) error {
	if err := os.MkdirAll(filepath.Dir(s.statePath()), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.statePath(), append(data, '\n'), 0o600)
}

func (s *Store) recordError(err error) {
	st := s.read()
	st.Root, st.Remote, st.LastError = s.root, s.remote, err.Error()
	_ = s.write(st)
}

func runGit(ctx context.Context, args ...string) (string, error) {
	limited, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(limited, "git", args...) // #nosec G204 -- arguments are constructed by this package
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", scrub(strings.Join(args, " ")), err, scrub(gitReason(string(out))))
	}
	return string(out), nil
}

// reasonLine is a line of Git output that says why, rather than what Git was
// doing at the time. "remote:" is left out: GitHub writes its progress there.
var reasonLine = regexp.MustCompile(`(?i)^(fatal|error|warning):|^(permission denied|host key verification failed)`)

// maxReason caps what one failure writes into a state file.
const maxReason = 2000

// gitReason keeps the lines saying why, on one line: a verdict shows the first
// line of an error, and a clone's first line is always "Cloning into ...".
func gitReason(out string) string {
	var kept []string
	last := ""
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		last = line
		if reasonLine.MatchString(line) {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		if last == "" {
			return ""
		}
		kept = []string{last}
	}
	return clip(strings.Join(kept, "; "), maxReason)
}

var urlCredentials = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@\s]*@`)

// scrub keeps a remote's embedded credentials out of an error, which reaches
// the sandboxed agent as a tool result and ADE's state files as text.
func scrub(text string) string {
	return urlCredentials.ReplaceAllString(text, "$1***@")
}
