// Package comments stores review threads anchored to lines of a diff.
package comments

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/statedir"
)

// Comment authors. AuthorDraft is a reply the agent suggested you might send to
// a reviewer: it is kept distinct from AuthorAgent so the UI can say plainly
// that it has not been, and will not be, posted anywhere.
const (
	AuthorYou   = "you"
	AuthorAgent = "agent"
	AuthorDraft = "draft"
)

// Thread scopes. A repo-scoped thread has no file or line.
const (
	ScopeLine = "line"
	ScopeRepo = "repo"
)

// How badly a finding wants your attention, worst first. Unset reads as
// SeverityMedium: a finding somebody bothered to file is not nothing.
const (
	SeverityCritical = "critical"
	SeverityHigh     = "high"
	SeverityMedium   = "medium"
	SeverityLow      = "low"
)

var severityRank = map[string]int{
	SeverityCritical: 4,
	SeverityHigh:     3,
	SeverityMedium:   2,
	SeverityLow:      1,
}

// Severity normalises a reported severity to one of the four, defaulting
// anything unrecognised to medium.
func Severity(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if _, ok := severityRank[s]; ok {
		return s
	}
	return SeverityMedium
}

// Worse reports whether a is more severe than b.
func Worse(a, b string) bool { return severityRank[Severity(a)] > severityRank[Severity(b)] }

// ErrNotFound is returned when a thread id does not exist.
var ErrNotFound = errors.New("thread not found")

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Thread is one conversation pinned to a line, mirroring a GitHub review thread.
type Thread struct {
	ID       string    `json:"id"`
	Scope    string    `json:"scope,omitempty"`
	File     string    `json:"file"`
	Side     string    `json:"side"`
	Line     int       `json:"line"`
	Anchor   Anchor    `json:"anchor"`
	Comments []Comment `json:"comments"`
	Resolved bool      `json:"resolved"`
	Orphaned bool      `json:"orphaned"`

	// OrphanedAt is when the line this was pinned to went. A pointer so a zero
	// time cannot read as "orphaned in year 1" and sweep it on sight.
	OrphanedAt *time.Time `json:"orphanedAt,omitempty"`
	Category   string     `json:"category,omitempty"`
	Severity   string     `json:"severity,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`

	// Disputed, Withdrawn and DuplicateOf are what a rebuttal round made of
	// this finding: another reviewer objected, more objected than defended it,
	// or it repeats the thread DuplicateOf names. A
	// verdict marks a thread and never removes one — the objection is a
	// reply, and both have to stay readable next to the claim.
	Disputed    bool   `json:"disputed,omitempty"`
	Withdrawn   bool   `json:"withdrawn,omitempty"`
	DuplicateOf string `json:"duplicateOf,omitempty"`

	// ExternalID ties a thread to an upstream review comment so importing twice
	// does not duplicate it.
	ExternalID string `json:"externalId,omitempty"`

	// Outgoing is this finding rewritten as something you would say to the
	// author, waiting for you to read it and decide. Held here rather than in
	// memory so what you approved is what gets sent, even across a restart.
	Outgoing string `json:"outgoing,omitempty"`

	// SentAt is when it went to GitHub. Read it through Sent rather than
	// directly: a zero time must never count.
	//
	//
	// A pointer because omitempty does nothing for a time.Time: a zero one is a
	// struct, not an empty value, so it marshals as year 1 and every thread
	// reaches the browser looking like it has already been sent.
	SentAt *time.Time `json:"sentAt,omitempty"`
}

// Sent reports whether this thread has gone to GitHub. A zero time is not a
// time, whatever an older store wrote.
func (t Thread) Sent() bool {
	return t.SentAt != nil && !t.SentAt.IsZero()
}

// A claim's fate: what became of it, not merely whether it is closed. A
// resolved flag cannot tell fixed from dismissed.
const (
	FateOpen      = "open"
	FateFixed     = "fixed"
	FateDeclined  = "declined"
	FateUnclear   = "unclear"
	FateWithdrawn = "withdrawn"
	FateDuplicate = "duplicate"
)

// Fate reads what is already recorded. The rebuttal round outranks the
// resolver: a withdrawn claim was never owed a fix.
func (t Thread) Fate() string {
	switch {
	case t.Withdrawn:
		return FateWithdrawn
	case t.DuplicateOf != "":
		return FateDuplicate
	}
	for i := len(t.Comments) - 1; i >= 0; i-- {
		switch t.Comments[i].Outcome {
		case "":
			continue
		case resolvedFixed:
			return FateFixed
		case resolvedDeclined:
			return FateDeclined
		default:
			return FateUnclear
		}
	}
	if t.Resolved {
		return FateFixed
	}
	return FateOpen
}

// The resolver's status strings. Named here rather than imported: resolve
// already depends on this package, so the arrow cannot point back.
const (
	resolvedFixed    = "fixed"
	resolvedDeclined = "declined"
)

// Answered reports whether the resolver has already ruled on this thread and
// nobody has spoken since: a declined verdict is an answer, not work waiting.
func (t Thread) Answered() bool {
	if len(t.Comments) == 0 {
		return false
	}
	return t.Comments[len(t.Comments)-1].Outcome != ""
}

type Comment struct {
	ID     string    `json:"id"`
	Author string    `json:"author"`
	Body   string    `json:"body"`
	At     time.Time `json:"at"`

	// ExternalID is the GitHub comment ID this reply mirrors, empty for
	// anything written in ade. It is how MergeExternalReplies tells a reply
	// already recorded from one that just landed on GitHub.
	ExternalID string `json:"externalId,omitempty"`

	// SentAt is when this comment went to GitHub as a reply. A pointer for the
	// same reason Thread.SentAt is: omitempty does nothing for a time.Time, so
	// a zero one would reach the browser looking sent.
	SentAt *time.Time `json:"sentAt,omitempty"`

	// Outcome is the resolver's verdict when this reply is one, so a refusal
	// does not read as a fix. One of resolve's statuses, or empty.
	Outcome string `json:"outcome,omitempty"`
}

// Sent reports whether this comment has gone to GitHub.
func (c Comment) Sent() bool {
	return c.SentAt != nil && !c.SentAt.IsZero()
}

// Mine reports whether a comment was written on this side — by you, or drafted
// for you — rather than imported from GitHub or written by a reviewing agent.
// Only these can be sent as a reply.
func (c Comment) Mine() bool {
	return c.ExternalID == "" && (c.Author == AuthorYou || c.Author == AuthorDraft)
}

// PendingReply is what this thread has to say back to GitHub: its own unsent
// comments, oldest first. Only those after the last imported one — anything
// earlier was already answered by the reply that followed it.
func (t Thread) PendingReply() []Comment {
	last := -1
	for i, c := range t.Comments {
		if c.ExternalID != "" {
			last = i
		}
	}

	var out []Comment
	for _, c := range t.Comments[last+1:] {
		if c.Mine() && !c.Sent() {
			out = append(out, c)
		}
	}
	return out
}

// Store is the on-disk set of threads for one branch.
type Store struct {
	mu      sync.Mutex
	path    string
	threads []*Thread
}

// Open loads the thread store for a branch, creating nothing until first write.
// State lives under the git dir so it is never committed and never staged.
func Open(gitDir, branch string) (*Store, error) {
	path := filepath.Join(statedir.Git(gitDir), Slug(branch)+".json")
	s := &Store{path: path}

	data, err := os.ReadFile(path) // #nosec G304 -- path is derived from the repo's own git dir
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read thread store: %w", err)
	}
	if err := json.Unmarshal(data, &s.threads); err != nil {
		return nil, fmt.Errorf("parse thread store %s: %w", path, err)
	}
	healSentAt(s.threads)
	restartOrphanClocks(s.threads, time.Now())
	return s, nil
}

// restartOrphanClocks makes orphanGrace time this session has been up. Elapsed
// while nothing was running, it is a grace the user was never given.
func restartOrphanClocks(threads []*Thread, now time.Time) {
	for _, thread := range threads {
		if thread.OrphanedAt != nil {
			when := now
			thread.OrphanedAt = &when
		}
	}
}

// healSentAt clears the year-1 timestamps an earlier version wrote for every
// thread, sent or not. Left alone they read as "sent to GitHub" for comments
// that have gone nowhere, which is the worst thing this file could be wrong
// about.
func healSentAt(threads []*Thread) {
	for _, thread := range threads {
		if thread.SentAt != nil && thread.SentAt.IsZero() {
			thread.SentAt = nil
		}
	}
}

// Path reports where threads are persisted.
func (s *Store) Path() string { return s.path }

// List returns every thread, newest last.
func (s *Store) List() []Thread {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Thread, 0, len(s.threads))
	for _, t := range s.threads {
		out = append(out, t.clone())
	}
	return out
}

// Unresolved returns the open threads, which are what Resolve acts on.
func (s *Store) Unresolved() []Thread {
	var out []Thread
	for _, t := range s.List() {
		if !t.Resolved {
			out = append(out, t)
		}
	}
	return out
}

// ByExternal finds the thread already imported for an upstream root comment
// ID, if any.
func (s *Store) ByExternal(externalID string) (Thread, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.threads {
		if t.ExternalID != "" && t.ExternalID == externalID {
			return t.clone(), true
		}
	}
	return Thread{}, false
}

// SetExternal records the GitHub comment ID a sent thread became, so a later
// import recognises it as already known rather than a new comment.
func (s *Store) SetExternal(id, externalID string) (Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := s.find(id)
	if t == nil {
		return Thread{}, ErrNotFound
	}
	t.ExternalID = externalID
	if err := s.save(); err != nil {
		return Thread{}, err
	}
	return t.clone(), nil
}

// MergeExternalReplies appends any of a GitHub thread's comments this store
// has not already recorded, matched by ExternalID. It reopens a resolved
// thread: a new reply means the point was not settled after all. Reports how
// many were new.
func (s *Store) MergeExternalReplies(threadID string, incoming []Comment) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := s.find(threadID)
	if t == nil {
		return 0, ErrNotFound
	}
	known := make(map[string]bool, len(t.Comments)+1)
	// The root is already on the thread: as the comment imported with it, or as
	// the text you wrote and sent, which carries no GitHub ID to match on.
	known[t.ExternalID] = true
	for _, c := range t.Comments {
		if c.ExternalID != "" {
			known[c.ExternalID] = true
		}
	}

	added := 0
	for _, c := range incoming {
		if c.ExternalID == "" || known[c.ExternalID] {
			continue
		}
		c.ID = newID()
		t.Comments = append(t.Comments, c)
		added++
	}
	if added == 0 {
		return 0, nil
	}
	// A new reply is a reopening: the point was not settled after all.
	t.Resolved = false
	if err := s.save(); err != nil {
		return 0, err
	}
	return added, nil
}

// MarkReplySent records that named comments of a thread have gone to GitHub, so
// they are not offered or posted a second time.
func (s *Store) MarkReplySent(threadID string, ids []string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := s.find(threadID)
	if t == nil {
		return ErrNotFound
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	for i := range t.Comments {
		if want[t.Comments[i].ID] {
			when := at
			t.Comments[i].SentAt = &when
		}
	}
	return s.save()
}

// Get returns one thread by id.
func (s *Store) Get(id string) (Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.find(id)
	if t == nil {
		return Thread{}, ErrNotFound
	}
	return t.clone(), nil
}

// Add starts a new thread on a line.
func (s *Store) Add(t Thread) (Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.add(t)
}

// AddOrReply files a finding, folding it into an existing agent thread about
// the same lines rather than starting a second one there. Three reviewers
// seeing the same thing is one conversation with three voices, not three
// comments to read past. merged reports which happened, so a caller that may
// withdraw the finding knows it does not own the thread.
func (s *Store) AddOrReply(t Thread) (thread Thread, merged bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	body := ""
	if len(t.Comments) > 0 {
		body = t.Comments[0].Body
	}
	if into := s.mergeInto(t); into != nil && strings.TrimSpace(body) != "" {
		author := t.Comments[0].Author
		if author == "" {
			author = AuthorAgent
		}
		into.Comments = append(into.Comments, Comment{ID: newID(), Author: author, Body: body, At: time.Now()})
		// The thread is as bad as the worst thing said on it.
		if Worse(t.Severity, into.Severity) {
			into.Severity = Severity(t.Severity)
		}
		if err := s.save(); err != nil {
			return Thread{}, false, err
		}
		return into.clone(), true, nil
	}

	added, err := s.add(t)
	return added, false, err
}

// mergeWindow is how far apart two findings can be and still be the same
// point. Reviewers of one bug rarely land on the same line: one names the
// dereference, another the check above it, a third the signature. Requiring
// an exact match is what put three comments saying one thing in the diff.
const mergeWindow = 10

// mergeInto is the open agent thread this finding belongs on, if there is one.
// Anything on the line itself qualifies, whatever it is about; further off, up
// to mergeWindow, only the same category does. Two reviewers on one line are
// talking about that line, but ten lines away a performance note and a
// correctness bug are two points, and folding one under the other reads as a
// reply to something nobody said.
//
// Your own threads and imported ones are left alone: an agent must not append
// to a conversation somebody else started.
func (s *Store) mergeInto(t Thread) *Thread {
	if t.Scope == ScopeRepo || strings.TrimSpace(t.File) == "" || t.Line <= 0 {
		return nil
	}
	var into *Thread
	for _, other := range s.threads {
		if other.Resolved || other.Sent() || other.ExternalID != "" {
			continue
		}
		if other.File != t.File || other.Side != t.Side {
			continue
		}
		gap := absDiff(other.Line, t.Line)
		if gap > 0 && (gap > mergeWindow || !strings.EqualFold(other.Category, t.Category)) {
			continue
		}
		if len(other.Comments) == 0 || !fromReviewer(other.Comments[0].Author) {
			continue
		}
		// Nearest wins, so a finding on the line itself is not folded into one
		// ten lines away that happened to be filed first.
		if into == nil || gap < absDiff(into.Line, t.Line) {
			into = other
		}
	}
	return into
}

// fromReviewer reports whether a comment came from a reviewing agent — the
// cold one, or a named one under its own name — rather than from you.
func fromReviewer(author string) bool {
	return author != "" && author != AuthorYou && author != AuthorDraft
}

func (s *Store) add(t Thread) (Thread, error) {
	if t.Scope == "" {
		t.Scope = ScopeLine
	}
	switch t.Scope {
	case ScopeRepo:
		// A repo comment is about the change as a whole, so it carries no anchor.
		t.File, t.Side, t.Line, t.Anchor = "", "", 0, Anchor{}
	case ScopeLine:
		if strings.TrimSpace(t.File) == "" || t.Line <= 0 {
			return Thread{}, errors.New("thread needs a file and a line")
		}
		if t.Side != SideLeft && t.Side != SideRight {
			return Thread{}, fmt.Errorf("invalid side %q", t.Side)
		}
	default:
		return Thread{}, fmt.Errorf("invalid scope %q", t.Scope)
	}
	if len(t.Comments) == 0 || strings.TrimSpace(t.Comments[0].Body) == "" {
		return Thread{}, errors.New("thread needs a first comment")
	}

	t.ID = newID()
	t.CreatedAt = time.Now()
	if t.Severity != "" {
		t.Severity = Severity(t.Severity)
	}
	for i := range t.Comments {
		t.Comments[i].ID = newID()
		t.Comments[i].At = t.CreatedAt
		if t.Comments[i].Author == "" {
			t.Comments[i].Author = AuthorYou
		}
	}

	s.threads = append(s.threads, &t)
	if err := s.save(); err != nil {
		return Thread{}, err
	}
	return t.clone(), nil
}

// Reply appends a comment to an existing thread and reopens it.
func (s *Store) Reply(id, author, body string) (Thread, error) {
	return s.ReplyOutcome(id, author, body, "")
}

// ReplyOutcome is Reply carrying the resolver's verdict on the thread.
func (s *Store) ReplyOutcome(id, author, body, outcome string) (Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(body) == "" {
		return Thread{}, errors.New("empty comment")
	}
	t := s.find(id)
	if t == nil {
		return Thread{}, ErrNotFound
	}
	if author == "" {
		author = AuthorYou
	}
	t.Comments = append(t.Comments, Comment{ID: newID(), Author: author, Body: body, At: time.Now(), Outcome: outcome})
	// A reply is a reopening: the point was not settled after all.
	t.Resolved = false
	if err := s.save(); err != nil {
		return Thread{}, err
	}
	return t.clone(), nil
}

// SetResolved marks a thread resolved or reopens it.
func (s *Store) SetResolved(id string, resolved bool) (Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := s.find(id)
	if t == nil {
		return Thread{}, ErrNotFound
	}
	t.Resolved = resolved
	if err := s.save(); err != nil {
		return Thread{}, err
	}
	return t.clone(), nil
}

// Verdict is what a rebuttal round concluded about one thread. It is applied
// in one call rather than a field at a time, because the round decides all
// three together and a half-applied verdict reads as a different one.
type Verdict struct {
	Disputed    bool
	Withdrawn   bool
	DuplicateOf string
}

// SetVerdict records a rebuttal round's conclusion about a thread.
func (s *Store) SetVerdict(id string, v Verdict) (Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := s.find(id)
	if t == nil {
		return Thread{}, ErrNotFound
	}
	t.Disputed, t.Withdrawn, t.DuplicateOf = v.Disputed, v.Withdrawn, v.DuplicateOf
	if err := s.save(); err != nil {
		return Thread{}, err
	}
	return t.clone(), nil
}

// Delete removes a thread.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, t := range s.threads {
		if t.ID == id {
			s.threads = append(s.threads[:i], s.threads[i+1:]...)
			return s.save()
		}
	}
	return ErrNotFound
}

// Resettable is every thread a reset would clear: your own and the reviewer's,
// but not one imported from a pull request or already posted to one. Those two
// exist upstream, where this store cannot put them back.
func (s *Store) Resettable() []Thread {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []Thread
	for _, t := range s.threads {
		if keepOnReset(t) {
			continue
		}
		out = append(out, t.clone())
	}
	return out
}

// Reset clears the threads Resettable lists and reports how many went.
func (s *Store) Reset() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	kept := make([]*Thread, 0, len(s.threads))
	for _, t := range s.threads {
		if keepOnReset(t) {
			kept = append(kept, t)
		}
	}
	gone := len(s.threads) - len(kept)
	if gone == 0 {
		return 0, nil
	}
	s.threads = kept
	if err := s.save(); err != nil {
		return 0, err
	}
	return gone, nil
}

func keepOnReset(t *Thread) bool { return t.ExternalID != "" || t.Sent() }

// SetOutgoing records the rewritten text for a thread, or clears it with "".
func (s *Store) SetOutgoing(id, text string) (Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	thread := s.find(id)
	if thread == nil {
		return Thread{}, fmt.Errorf("no thread %s", id)
	}
	thread.Outgoing = text
	if err := s.save(); err != nil {
		return Thread{}, err
	}
	return *thread, nil
}

// MarkSent records that a thread's text reached GitHub. Sending is not
// reversible, so a thread that has gone is never offered again.
func (s *Store) MarkSent(ids []string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range ids {
		if thread := s.find(id); thread != nil {
			when := at
			thread.SentAt = &when
		}
	}
	return s.save()
}

// Sendable is every open thread of your own that could go to GitHub: not
// already sent, and not one imported from there in the first place.
func (s *Store) Sendable() []Thread {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []Thread
	for _, thread := range s.threads {
		if thread.Resolved || thread.Orphaned || thread.ExternalID != "" {
			continue
		}
		if thread.Sent() || thread.Scope == ScopeRepo {
			continue
		}
		if thread.File == "" || thread.Line <= 0 {
			continue
		}
		out = append(out, *thread)
	}
	return out
}

// Reanchor repositions every thread against a fresh diff and drops the ones
// whose code has been gone a while. It reports what it dropped.
func (s *Store) Reanchor(diff *gitdiff.Diff) []Thread {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	was := s.threads
	kept := make([]*Thread, 0, len(s.threads))
	var gone []Thread
	for _, t := range s.threads {
		reanchor(t, diff, now)
		if swept(t, diff, now) {
			gone = append(gone, t.clone())
			continue
		}
		kept = append(kept, t)
	}
	if len(gone) == 0 {
		_ = s.save()
		return nil
	}

	// A deletion that only happened in memory would be persisted by the next
	// method that saves, so the file is what decides whether it happened.
	s.threads = kept
	if err := s.save(); err != nil {
		s.threads = was
		return nil
	}
	return gone
}

// orphanGrace is how long a comment outlives the line it was pinned to, counted
// from the pass that first missed it and read on the first pass after that.
const orphanGrace = time.Hour

// swept reports whether an orphan has been gone long enough to drop. Only on the
// whole-branch view with its file still in it; anything else is a change of view.
func swept(t *Thread, diff *gitdiff.Diff, now time.Time) bool {
	if !t.Orphaned || t.OrphanedAt == nil || keepOnReset(t) {
		return false
	}
	if diff == nil || diff.Mode != gitdiff.ModeWorktree {
		return false
	}
	if !HasFile(diff, t.File) {
		return false
	}
	return now.Sub(*t.OrphanedAt) >= orphanGrace
}

// save writes the store atomically so an interrupted write cannot corrupt it.
func (s *Store) save() error {
	if len(s.threads) == 0 {
		if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove thread store: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("create thread store dir: %w", err)
	}
	data, err := json.MarshalIndent(s.threads, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".ade-*")
	if err != nil {
		return fmt.Errorf("create temp thread store: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write thread store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("replace thread store: %w", err)
	}
	return nil
}

// clone deep-copies a thread so callers cannot mutate stored state.
func (t *Thread) clone() Thread {
	out := *t
	out.Comments = append([]Comment(nil), t.Comments...)
	return out
}

func (s *Store) find(id string) *Thread {
	for _, t := range s.threads {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func newID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// slug makes a branch name safe to use as a filename.
// Slug is a branch name made safe for a filename, and is how branch-keyed
// state under the git dir is named. Lineage keys itself the same way.
func Slug(branch string) string {
	s := unsafeName.ReplaceAllString(strings.TrimSpace(branch), "-")
	s = strings.Trim(s, "-.")
	if s == "" {
		return "detached"
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}
