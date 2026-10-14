package wiki

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/owenrumney/ade/internal/authstore"
	"github.com/owenrumney/ade/internal/gitdiff"
)

var (
	sessionName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	repoSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

// validRepo reports whether repo is the plain owner/repo pair scopes() may
// join beneath repos/.
func validRepo(repo string) bool {
	owner, name, ok := strings.Cut(repo, "/")
	return ok && repoSegment.MatchString(owner) && repoSegment.MatchString(name)
}

// Draft is one session's private, plain-text workspace.
type Draft struct {
	Session string `json:"session"`
	Root    string `json:"root"`
	Repo    string `json:"repo"`
	Base    string `json:"base"`
	State   string `json:"state,omitempty"`
	Commit  string `json:"commit,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Submission is the durable outcome of a requested publish.
type Submission struct {
	Session string `json:"session"`
	State   string `json:"state"`
	Commit  string `json:"commit,omitempty"`
	Error   string `json:"error,omitempty"`
}

type draftRecord struct {
	Repo   string `json:"repo"`
	Base   string `json:"base"`
	State  string `json:"state,omitempty"`
	Commit string `json:"commit,omitempty"`
	Error  string `json:"error,omitempty"`
	// Publish is the retained clone a conflict left behind, which only the
	// user may clear.
	Publish string    `json:"publish,omitempty"`
	At      time.Time `json:"at,omitempty"`
}

// Draft creates and seeds a session directory from the canonical revision.
func (s *Store) Draft(ctx context.Context, session, repoRoot string) (Draft, error) {
	if !s.Enabled() {
		return Draft{}, errors.New("wiki is disabled; run ade wiki init --remote <url>")
	}
	if !sessionName.MatchString(session) {
		return Draft{}, errors.New("invalid wiki session id")
	}
	owner, repo := gitdiff.Origin(ctx, repoRoot)
	if owner == "" || repo == "" {
		return Draft{}, errors.New("wiki drafts require an origin owner/repo")
	}
	// origin is whatever the checkout says it is, and scopes() joins these
	// into a path: a separator or a dot-dot here escapes repos/.
	if !repoSegment.MatchString(owner) || !repoSegment.MatchString(repo) {
		return Draft{}, errors.New("wiki drafts require a plain origin owner/repo")
	}
	// A seeded draft is answered without touching Git: a run must not wait on
	// a fetch to get its mount.
	if held, err := s.draftOf(session); err == nil {
		if info, err := os.Stat(held.Root); err == nil && info.IsDir() {
			// An untouched draft behind the wiki is re-seeded, so a long session
			// reads what others published rather than the day it started.
			if held.Base == s.CachedStatus().Revision || touched(held.Root) {
				return held, nil
			}
			if err := os.RemoveAll(held.Root); err != nil {
				return held, nil
			}
		}
	}
	// Seeding needs a base revision, not a current one: fetch only when no
	// revision is cached, so a run never waits on an unreachable remote.
	st := s.CachedStatus()
	if st.Revision == "" {
		if err := s.Sync(ctx); err != nil {
			return Draft{}, err
		}
		st = s.CachedStatus()
	}
	return s.seedDraft(session, owner+"/"+repo)
}

// seedDraft copies the canonical scopes for repo into the session's draft,
// at the cached revision. An existing directory is kept as it is.
func (s *Store) seedDraft(session, repo string) (Draft, error) {
	st := s.CachedStatus()
	if st.Revision == "" {
		return Draft{}, errors.New("canonical wiki has no revision")
	}
	draft := Draft{Session: session, Root: s.DraftRoot(session), Repo: repo, Base: st.Revision}
	if info, err := os.Stat(draft.Root); err == nil {
		if !info.IsDir() {
			return Draft{}, errors.New("wiki draft path is not a directory")
		}
		return draft, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Draft{}, err
	}
	for _, dir := range scopes(draft.Repo) {
		if err := copyMarkdown(filepath.Join(s.root, dir), filepath.Join(draft.Root, dir)); err != nil {
			return Draft{}, err
		}
	}
	if err := os.WriteFile(filepath.Join(draft.Root, ".ade-wiki-base"), []byte(draft.Base+"\n"), 0o600); err != nil {
		return Draft{}, err
	}
	if err := os.WriteFile(filepath.Join(draft.Root, ".ade-wiki-repo"), []byte(draft.Repo+"\n"), 0o600); err != nil {
		return Draft{}, err
	}
	if err := s.saveDraft(draft); err != nil {
		return Draft{}, err
	}
	return draft, nil
}

// touched reports a draft with a page newer than its seed marker.
func touched(root string) bool {
	marker, err := os.Stat(filepath.Join(root, ".ade-wiki-base"))
	if err != nil {
		return true
	}
	changed := false
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || changed || entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		if info, err := entry.Info(); err == nil && info.ModTime().After(marker.ModTime()) {
			changed = true
		}
		return nil
	})
	return changed
}

// changedSince reports a draft with a page written after an earlier outcome.
// An unknown time counts as changed: a retained draft is not proof of a push.
func changedSince(root string, at time.Time) bool {
	if at.IsZero() {
		return true
	}
	changed := false
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || changed || entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		if info, err := entry.Info(); err == nil && info.ModTime().After(at) {
			changed = true
		}
		return nil
	})
	return changed
}

// Drafts lists retained drafts without reading or changing Git state.
func (s *Store) Drafts() ([]Draft, error) {
	root := filepath.Join(filepath.Dir(s.root), "wiki-drafts")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []Draft{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]Draft, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !sessionName.MatchString(entry.Name()) {
			continue
		}
		draft := Draft{Session: entry.Name(), Root: filepath.Join(root, entry.Name())}
		if record, ok := s.draftRecords()[draft.Session]; ok {
			draft.Base, draft.Repo = record.Base, record.Repo
			draft.State, draft.Commit, draft.Error = record.State, record.Commit, record.Error
		}
		out = append(out, draft)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Session < out[j].Session })
	return out, nil
}

// draftOf rebuilds a draft from ADE's own record. The agent names no path.
func (s *Store) draftOf(session string) (Draft, error) {
	if !sessionName.MatchString(session) {
		return Draft{}, errors.New("invalid wiki session id")
	}
	record, ok := s.draftRecords()[session]
	if !ok {
		return Draft{}, errors.New("this session has no wiki draft")
	}
	return Draft{Session: session, Root: filepath.Join(filepath.Dir(s.root), "wiki-drafts", session), Repo: record.Repo, Base: record.Base}, nil
}

// DraftRoot is where a session's draft lives, whether or not it exists yet.
func (s *Store) DraftRoot(session string) string {
	return filepath.Join(filepath.Dir(s.root), "wiki-drafts", session)
}

// Discard removes one retained draft. Callers must name the exact session.
func (s *Store) Discard(session string) error {
	if !sessionName.MatchString(session) {
		return errors.New("invalid wiki session id")
	}
	root := filepath.Join(filepath.Dir(s.root), "wiki-drafts", session)
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return errors.New("wiki draft not found")
	} else if err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	var removeErr error
	err := s.withRecords(func(records map[string]draftRecord) {
		if record, ok := records[session]; ok && record.Publish != "" {
			removeErr = os.RemoveAll(record.Publish)
		}
		delete(records, session)
	})
	if removeErr != nil {
		return removeErr
	}
	return err
}

// Submit validates and publishes one draft without exposing Git to its agent.
func (s *Store) Submit(ctx context.Context, draft Draft, agent string) (Submission, error) {
	if !s.Enabled() {
		return Submission{}, errors.New("wiki is disabled")
	}
	if !sessionName.MatchString(draft.Session) || draft.Root != filepath.Join(filepath.Dir(s.root), "wiki-drafts", draft.Session) {
		return Submission{}, errors.New("invalid wiki draft")
	}
	base, err := os.ReadFile(filepath.Join(draft.Root, ".ade-wiki-base"))
	if err != nil || strings.TrimSpace(string(base)) == "" {
		return Submission{}, errors.New("wiki draft has no recorded base revision")
	}
	draft.Base = strings.TrimSpace(string(base))
	record, ok := s.draftRecords()[draft.Session]
	if !ok || record.Repo == "" {
		return Submission{}, errors.New("wiki draft has no recorded repository")
	}
	if !validRepo(record.Repo) {
		return Submission{}, errors.New("wiki draft has an unusable recorded repository")
	}
	draft.Repo = record.Repo
	if draft.Base != record.Base {
		return Submission{}, errors.New("wiki draft base does not match host record")
	}
	// A published commit whose canonical refresh is still pending must not be
	// committed a second time. Written since, and it is not that commit's draft.
	if record.State == "published" && record.Commit != "" && !changedSince(draft.Root, record.At) {
		return Submission{Session: draft.Session, State: "published", Commit: record.Commit}, nil
	}
	if err := s.validateDraft(draft); err != nil {
		return s.record(draft, Submission{Session: draft.Session, State: "rejected", Error: err.Error()}), err
	}
	out, err := s.publish(ctx, draft, agent)
	return s.record(draft, out), err
}

// record keeps the last outcome beside the draft so ade wiki status can
// report it without re-running Git. A published draft is already gone.
func (s *Store) record(draft Draft, out Submission) Submission {
	_ = s.withRecords(func(records map[string]draftRecord) {
		held, ok := records[draft.Session]
		if !ok {
			return
		}
		held.State, held.Commit, held.Error, held.At = out.State, out.Commit, out.Error, time.Now().UTC()
		records[draft.Session] = held
	})
	return out
}

func (s *Store) validateDraft(draft Draft) error {
	for _, dir := range scopes(draft.Repo) {
		root := filepath.Join(draft.Root, dir)
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if entry.Type()&fs.ModeSymlink != 0 || !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
				return fmt.Errorf("%s: only regular Markdown files may be published", relDraft(draft.Root, path))
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if rule := secretRule(string(data)); rule != "" {
				return fmt.Errorf("%s: suspected secret (%s)", relDraft(draft.Root, path), rule)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// readRegular opens without following a symlink, so a draft the sandbox swaps
// under us between the walk and the read cannot point outside itself.
func readRegular(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0) // #nosec G304 -- beneath this draft's root
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: only regular Markdown files may be published", filepath.Base(path))
	}
	return io.ReadAll(f)
}

func relDraft(root, path string) string {
	rel, _ := filepath.Rel(root, path)
	return filepath.ToSlash(rel)
}

var secretPatterns = []struct {
	rule string
	re   *regexp.Regexp
}{
	{"private-key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"github-token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`)},
	{"aws-access-key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"credential-assignment", regexp.MustCompile(`(?i)\b(password|secret|api[_-]?key|token)\s*[:=]\s*["']?[A-Za-z0-9_./+=-]{16,}`)},
	{"url-credentials", regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s/:@]+:[^\s/@]{6,}@`)},
	{"provider-key", regexp.MustCompile(`\b(sk|rk)-(ant|proj|live|test)?-?[A-Za-z0-9_-]{20,}\b`)},
	{"slack-token", regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}\b`)},
	{"google-api-key", regexp.MustCompile(`\bAIza[A-Za-z0-9_-]{35}\b`)},
	{"json-web-token", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)},
}

func secretRule(text string) string {
	for _, pattern := range secretPatterns {
		if pattern.re.MatchString(text) {
			return pattern.rule
		}
	}
	return ""
}

func copyMarkdown(from, to string) error {
	return filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil || rel == "." {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		dest := filepath.Join(to, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0o700)
		}
		if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o600)
	})
}

func (s *Store) publish(ctx context.Context, draft Draft, agent string) (Submission, error) {
	s.pub.Lock()
	defer s.pub.Unlock()
	if err := checkRemote(s.remote); err != nil {
		return Submission{Session: draft.Session, State: "failed", Error: err.Error()}, err
	}
	tmp := filepath.Join(filepath.Dir(s.root), "wiki-publish", draft.Session+"-"+draftKey(draft.Base))
	if err := os.MkdirAll(filepath.Dir(tmp), 0o700); err != nil {
		return Submission{}, err
	}
	if _, err := s.run(ctx, "clone", "--", s.remote, tmp); err != nil {
		return Submission{Session: draft.Session, State: "failed", Error: err.Error()}, err
	}
	retain := false
	defer func() {
		if !retain {
			_ = os.RemoveAll(tmp)
			return
		}
		s.retainPublish(draft.Session, tmp)
	}()
	if _, err := s.gitAt(ctx, tmp, "checkout", "--detach", draft.Base); err != nil {
		return Submission{Session: draft.Session, State: "failed", Error: err.Error()}, err
	}
	changed, err := copyChanges(draft, tmp)
	if err != nil {
		return Submission{Session: draft.Session, State: "rejected", Error: err.Error()}, err
	}
	if len(changed) == 0 {
		return Submission{Session: draft.Session, State: "unchanged"}, nil
	}
	if _, err := s.gitAt(ctx, tmp, append([]string{"add", "--"}, changed...)...); err != nil {
		return Submission{Session: draft.Session, State: "failed", Error: err.Error()}, err
	}
	message := "wiki: update " + strings.Join(changed, ", ")
	if _, err := s.gitAt(ctx, tmp, "-c", "user.name=ADE", "-c", "user.email=ade@localhost", "commit", "-m", message, "-m", "ADE-Session: "+draft.Session, "-m", "ADE-Agent: "+strings.TrimSpace(agent)); err != nil {
		return Submission{Session: draft.Session, State: "failed", Error: err.Error()}, err
	}
	commit, err := s.gitAt(ctx, tmp, "rev-parse", "HEAD")
	if err != nil {
		return Submission{}, err
	}
	commit = strings.TrimSpace(commit)
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := s.gitAt(ctx, tmp, "fetch", "origin", branch); err != nil {
			return Submission{Session: draft.Session, State: "failed", Commit: commit, Error: err.Error()}, err
		}
		if _, err := s.gitAt(ctx, tmp, "rebase", "origin/"+branch); err != nil {
			_ = s.gitAtIgnore(ctx, tmp, "rebase", "--abort")
			retain = true
			return Submission{Session: draft.Session, State: "conflict", Commit: commit, Error: err.Error()}, err
		}
		commit, err = s.gitAt(ctx, tmp, "rev-parse", "HEAD")
		if err != nil {
			return Submission{Session: draft.Session, State: "failed", Error: err.Error()}, err
		}
		commit = strings.TrimSpace(commit)
		if _, err := s.gitAt(ctx, tmp, "push", "origin", "HEAD:"+branch); err == nil {
			// Published either way; a refresh that did not land still fails
			// the call, because the draft is retained and search has not.
			if err := s.Sync(ctx); err != nil {
				retain = true
				return Submission{Session: draft.Session, State: "published", Commit: commit, Error: err.Error()}, err
			}
			if err := s.canonicalHas(ctx, commit); err != nil {
				retain = true
				pending := errors.New("published, but the canonical clone has not caught up")
				return Submission{Session: draft.Session, State: "published", Commit: commit, Error: pending.Error()}, pending
			}
			if err := os.RemoveAll(draft.Root); err != nil {
				retain = true
				return Submission{Session: draft.Session, State: "published", Commit: commit, Error: err.Error()}, err
			}
			_ = s.withRecords(func(records map[string]draftRecord) {
				delete(records, draft.Session)
			})
			return Submission{Session: draft.Session, State: "published", Commit: commit}, nil
		}
	}
	return Submission{Session: draft.Session, State: "failed", Commit: commit, Error: "push rejected after 3 retries"}, errors.New("wiki push rejected after 3 retries")
}

// retainPublish names the clone a conflict or a pending refresh left, so
// prune expires it and discard removes it.
func (s *Store) retainPublish(session, path string) {
	_ = s.withRecords(func(records map[string]draftRecord) {
		held, ok := records[session]
		if !ok {
			return
		}
		held.Publish = path
		records[session] = held
	})
}

// withRecords is the only way to change the records file: the launcher, every
// web server and the CLI are separate processes over the one file.
func (s *Store) withRecords(change func(map[string]draftRecord)) error {
	unlock, err := authstore.Lock(s.recordsPath())
	if err != nil {
		return err
	}
	defer unlock()
	records := s.draftRecords()
	change(records)
	return s.writeDraftRecords(records)
}

func (s *Store) recordsPath() string {
	return filepath.Join(filepath.Dir(s.root), "wiki-drafts.json")
}

func (s *Store) draftRecords() map[string]draftRecord {
	data, err := os.ReadFile(s.recordsPath())
	if err != nil {
		return map[string]draftRecord{}
	}
	var records map[string]draftRecord
	if json.Unmarshal(data, &records) != nil || records == nil {
		return map[string]draftRecord{}
	}
	return records
}

func (s *Store) saveDraft(draft Draft) error {
	return s.withRecords(func(records map[string]draftRecord) {
		records[draft.Session] = draftRecord{Repo: draft.Repo, Base: draft.Base}
	})
}

func (s *Store) writeDraftRecords(records map[string]draftRecord) error {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	path := s.recordsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func (s *Store) gitAt(ctx context.Context, dir string, args ...string) (string, error) {
	return s.run(ctx, append([]string{"-C", dir}, args...)...)
}

func (s *Store) gitAtIgnore(ctx context.Context, dir string, args ...string) error {
	_, err := s.gitAt(ctx, dir, args...)
	return err
}

// canonicalHas reports whether the canonical clone already contains commit.
// It reads the working tree, so it takes the same locks a sync does.
func (s *Store) canonicalHas(ctx context.Context, commit string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lockCanonical()
	if err != nil {
		return err
	}
	defer unlock()
	_, err = s.git(ctx, "merge-base", "--is-ancestor", commit, "HEAD")
	return err
}

// syncLocked fetches and fast-forwards. Callers hold s.mu and the canonical
// file lock.
func (s *Store) syncLocked(ctx context.Context) error {
	if repair := s.validate(ctx); repair != "" {
		return fmt.Errorf("wiki needs repair: %s", repair)
	}
	if _, err := s.git(ctx, "fetch", "origin", branch); err != nil {
		return err
	}
	if _, err := s.git(ctx, "merge", "--ff-only", "origin/"+branch); err != nil {
		return err
	}
	st := s.read()
	st.Root, st.Remote, st.LastFetch, st.LastError, st.Repair = s.root, s.remote, s.now(), "", ""
	if rev, err := s.git(ctx, "rev-parse", "--verify", "HEAD"); err == nil {
		st.Revision = strings.TrimSpace(rev)
	}
	// Here rather than on the read path: search must never invoke Git.
	if err := s.indexDates(ctx); err != nil {
		slog.Debug("index wiki dates", "err", err)
	}
	return s.write(st)
}

func copyChanges(draft Draft, clone string) ([]string, error) {
	var changed []string
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
			// The draft is mounted read-write in a live sandbox, so what is
			// copied here is re-checked rather than trusted from validateDraft.
			if entry.Type()&fs.ModeSymlink != 0 || !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
				return fmt.Errorf("%s: only regular Markdown files may be published", rel)
			}
			data, err := readRegular(path)
			if err != nil {
				return err
			}
			if rule := secretRule(string(data)); rule != "" {
				return fmt.Errorf("%s: suspected secret (%s)", rel, rule)
			}
			was, wasErr := os.ReadFile(filepath.Join(clone, filepath.FromSlash(rel)))
			if wasErr == nil && bytes.Equal(data, was) {
				return nil
			}
			if wasErr != nil && !errors.Is(wasErr, os.ErrNotExist) {
				return wasErr
			}
			dest := filepath.Join(clone, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(dest, data, 0o600); err != nil {
				return err
			}
			changed = append(changed, rel)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, dir := range scopes(draft.Repo) {
		root := filepath.Join(clone, dir)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil || entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
				return err
			}
			rel := relDraft(clone, path)
			if _, err := os.Stat(filepath.Join(draft.Root, filepath.FromSlash(rel))); errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%s: deletes are not allowed", rel)
			} else if err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(changed)
	return changed, nil
}

func draftKey(base string) string {
	sum := sha256.Sum256([]byte(base + time.Now().UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(sum[:6])
}
