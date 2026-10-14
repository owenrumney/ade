package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/lineage"
	"github.com/owenrumney/ade/internal/statedir"
)

// turn is one exchange in flight, and the files it has written held as they
// were before it touched them. The agent announces a tool call before it runs,
// so when Edit or Write is seen the file on disk is still the old one.
type turn struct {
	session   string
	messageID string
	summary   string
	prompt    string
	at        time.Time

	// before is repo-relative path to content, one entry per file this turn has
	// written and only the first time it writes each.
	before  map[string]string
	preview bool
}

// pendingTurns bounds the turns held open. A prompt is shown the moment you
// send it, and a queued one waits its turn without writing anything, so a turn
// with no edits yet cannot be dropped. This caps what that can accumulate to.
const pendingTurns = 8

// writingTools are the tools that change a file. Anything else a turn calls —
// Bash included — is not attributed: a turn is credited only with what its own
// Edit and Write calls touched, and a line credited to an agent that did not
// write it makes the whole store worthless.
var writingTools = map[string]bool{
	"Edit":            true,
	"MultiEdit":       true,
	"Write":           true,
	"NotebookEdit":    true,
	"edit":            true,
	"write":           true,
	"functions.edit":  true,
	"functions.write": true,
}

// beginTurn opens a turn for a message. Called for every prompt you send, from
// say, so chat, build and draft all get lineage from the one hook.
func (s *Server) beginTurn(messageID, summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns = append(s.turns, &turn{
		session:   s.agent.Current(),
		messageID: messageID,
		summary:   firstLine(summary),
		prompt:    summary,
		at:        time.Now(),
		before:    map[string]string{},
	})
	if len(s.turns) > pendingTurns {
		s.turns = s.turns[len(s.turns)-pendingTurns:]
	}
}

// beginRunTurn opens a turn for a run that had no prompt of its own, so its
// edits are not credited to whatever you last said. There is no message to jump
// to, only the summary.
func (s *Server) beginRunTurn(summary string) {
	s.beginTurn("", summary)
}

// noteEdits snapshots each file a turn is about to write. Read before the tool
// runs, so this is the content the turn started from.
func (s *Server) noteEdits(uses []bridge.ToolUse) {
	root := s.space().diffRoot()
	if root == "" {
		return
	}
	for _, use := range uses {
		if !writingTools[use.Name] {
			continue
		}
		path := editedPath(use.Input)
		if path == "" {
			continue
		}
		rel, ok := relTo(root, path)
		if !ok {
			continue
		}
		s.agent.noteTouched(rel)

		s.mu.Lock()
		live := s.liveTurn()
		if live != nil && isUIFile(rel) {
			live.preview = true
		}
		seen := live != nil && hasBefore(live, rel)
		s.mu.Unlock()
		if live == nil || seen {
			continue
		}

		// Missing is not an error: Write creates files, and "" is the right
		// before for one that did not exist.
		content, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil && !os.IsNotExist(err) {
			slog.Debug("snapshot for lineage", "file", rel, "err", err)
			continue
		}

		s.mu.Lock()
		if live == s.liveTurn() {
			live.before[rel] = string(content)
		}
		s.mu.Unlock()
	}
}

// settleTurn attributes what each finished turn changed and records it. Called
// after the diff has been refreshed, so lines are anchored against the diff the
// browser is about to be shown.
func (s *Server) settleTurn() {
	// The conversation is where it was left, so this is when a transcript is
	// worth counting as finished. Off the caller's goroutine: it scans.
	s.spawn(func() { s.refreshDebt() })

	s.mu.Lock()
	diff := s.diff
	var done []*turn
	var kept []*turn
	for _, t := range s.turns {
		if len(t.before) == 0 {
			kept = append(kept, t)
			continue
		}
		done = append(done, t)
	}
	s.turns = kept
	for _, t := range done {
		s.previewPending = s.previewPending || t.preview
	}
	s.mu.Unlock()

	if len(done) == 0 {
		return
	}
	store := s.lineageStore()
	if store == nil {
		return
	}
	root := s.space().diffRoot()

	for _, t := range done {
		var ranges []lineage.Range
		for rel, before := range t.before {
			after, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil && !os.IsNotExist(err) {
				slog.Debug("read after lineage", "file", rel, "err", err)
				continue
			}
			spans, err := lineage.Changed(s.ctx, before, string(after))
			if err != nil {
				slog.Debug("diff for lineage", "file", rel, "err", err)
				continue
			}
			for _, span := range spans {
				anchor, ok := comments.Locate(diff, rel, comments.SideRight, span.Start)
				if !ok {
					// The line is not in the diff: reverted, or outside what
					// ade is showing. Nothing to point a gutter at.
					continue
				}
				ranges = append(ranges, lineage.Range{
					File: rel, Start: span.Start, Lines: span.Lines, Anchor: anchor,
				})
			}
		}
		if err := store.Add(lineage.Entry{
			Session:   t.session,
			MessageID: t.messageID,
			At:        t.at,
			Summary:   t.summary,
			Prompt:    t.prompt,
			Ranges:    ranges,
		}); err != nil {
			slog.Warn("could not record lineage", "err", err)
		}
	}

	if err := store.Prune(diff); err != nil {
		slog.Debug("prune lineage", "err", err)
	}
	s.hub.Broadcast("lineage", nil)
}

// stampCommit marks the ranges a commit carried with its sha. replacing is the
// sha an amend rewrote, whose stamps move onto the new one.
func (s *Server) stampCommit(ctx context.Context, sha, replacing string) {
	store := s.lineageStore()
	if store == nil || sha == "" {
		return
	}
	root := s.space().diffRoot()

	if replacing != "" {
		if _, err := store.Restamp(replacing, sha); err != nil {
			slog.Warn("could not move an amended commit's lineage", "err", err)
		}
	}

	added, err := gitdiff.AddedLines(ctx, root, sha)
	if err != nil {
		slog.Debug("added lines for lineage", "sha", sha, "err", err)
		return
	}
	// Branch scope, not s.diff: on an uncommitted-scope view the lines just
	// committed have left the diff and nothing would match.
	diff, err := gitdiff.Load(ctx, gitdiff.Options{Repo: root, Scope: gitdiff.ScopeBranch})
	if err != nil {
		slog.Debug("branch diff for lineage", "err", err)
		return
	}
	stamped, err := store.Stamp(diff, sha, added)
	if err != nil {
		slog.Warn("could not stamp lineage", "err", err)
		return
	}
	if stamped > 0 {
		slog.Debug("stamped lineage", "sha", sha, "ranges", stamped)
		s.hub.Broadcast("lineage", nil)
	}

	// Publishing clones and pushes, so it never runs on the caller's goroutine.
	go s.recordCommit(context.WithoutCancel(ctx), sha, replacing)
}

// drainCommits stamps the commits the post-commit hook queued, which is how a
// commit made in a terminal rather than through the modal is picked up.
func (s *Server) drainCommits(ctx context.Context) {
	w := s.space()
	if w == nil || w.gitDir == "" || w.snapshot {
		return
	}
	shas, err := lineage.Drain(w.gitDir)
	if err != nil {
		slog.Debug("drain commit queue", "err", err)
		return
	}
	for _, sha := range shas {
		s.stampCommit(ctx, sha, "")
	}
}

// installCommitHook wires the post-commit hook up, once, at startup. A hook
// ade did not write is left alone, and the reason is worth one line.
func (s *Server) installCommitHook(ctx context.Context) {
	w := s.space()
	if w == nil || w.gitDir == "" || w.snapshot {
		return
	}
	if err := lineage.InstallHook(ctx, w.diffRoot()); err != nil {
		slog.Info("commits made in a terminal will not be stamped", "err", err)
	}
}

// handleLineage answers which turns changed a line.
// lineageResponse is the turns that changed one line, newest first.
type lineageResponse struct {
	Turns []lineage.Turn `json:"turns"`
}

// lineageMarks is which lines of each file some turn changed, keyed by path, so
// the gutter costs one request rather than one per line.
type lineageMarks struct {
	Files map[string][]int `json:"files"`
}

func (s *Server) handleLineage(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	line, err := strconv.Atoi(r.URL.Query().Get("line"))
	if file == "" || err != nil || line < 1 {
		writeError(w, http.StatusBadRequest, "a file and a line are needed")
		return
	}
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()

	turns := s.lineageStore().At(diff, file, line)
	if turns == nil {
		turns = []lineage.Turn{}
	}
	writeJSON(w, http.StatusOK, lineageResponse{Turns: turns})
}

// handleLineageMarks answers which lines have lineage at all, so the diff can
// draw its gutter in one request rather than one per line.
func (s *Server) handleLineageMarks(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, lineageMarks{Files: s.lineageStore().Marks(diff)})
}

// lineageStore is the current workspace's lineage, opened once and kept. The
// workspace can be swapped for a worktree or a pull request, and each keeps its
// own, so they are cached by path rather than held on the workspace.
func (s *Server) lineageStore() *lineage.Store {
	w := s.space()
	if w == nil || w.gitDir == "" || w.snapshot {
		return nil
	}
	path := filepath.Join(statedir.Git(w.gitDir), comments.Slug(w.branch)+".lineage.json")

	s.mu.RLock()
	store := s.lineages[path]
	s.mu.RUnlock()
	if store != nil {
		return store
	}

	store, err := lineage.Open(w.gitDir, w.branch)
	if err != nil {
		slog.Warn("could not open lineage", "err", err)
		return nil
	}
	s.mu.Lock()
	if have := s.lineages[path]; have != nil {
		store = have
	} else {
		if s.lineages == nil {
			s.lineages = map[string]*lineage.Store{}
		}
		s.lineages[path] = store
	}
	s.mu.Unlock()
	return store
}

// hasBefore reports whether a turn already snapshotted a file. Caller holds
// s.mu.
func hasBefore(t *turn, rel string) bool {
	_, ok := t.before[rel]
	return ok
}

// liveTurn is the newest turn open. Caller holds s.mu.
func (s *Server) liveTurn() *turn {
	if len(s.turns) == 0 {
		return nil
	}
	return s.turns[len(s.turns)-1]
}

// editedPath is the file a writing tool call names.
func editedPath(input json.RawMessage) string {
	var call struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Path         string `json:"path"`
	}
	if err := json.Unmarshal(input, &call); err != nil {
		return ""
	}
	if call.FilePath != "" {
		return call.FilePath
	}
	if call.NotebookPath != "" {
		return call.NotebookPath
	}
	return call.Path
}

// relTo makes a tool's path relative to the checkout, refusing anything outside
// it: a turn that wrote to /tmp has nothing to do with the diff.
func relTo(root, path string) (string, bool) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func isUIFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".css", ".html", ".jsx", ".scss", ".svelte", ".tsx", ".vue":
		return true
	default:
		return false
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}
