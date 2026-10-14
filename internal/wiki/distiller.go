package wiki

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/authstore"
	"github.com/owenrumney/ade/internal/transcripts"
)

// DistillAgent is who a distilled commit names as its author.
const DistillAgent = "ade-distill"

// DistillQuiet is how recently a transcript may have been written and still be
// read on a schedule. A turn inside it is still going, or has only just ended.
const DistillQuiet = 10 * time.Minute

// maxDistillFailures is how many goes the same prose gets. The watermark holds
// on a failure, so without a limit one bad batch fails the same way for ever.
const maxDistillFailures = 3

// errBeforeTheModel marks a failure the model had no part in, so the brake
// leaves the repository alone.
var errBeforeTheModel = errors.New("failed before the model ran")

// ErrNotTheModel is the same for a caller: a login that expired is the
// machine's fault, and must not hold a repository for days over it.
var ErrNotTheModel = errBeforeTheModel

// DistillModel runs the toolless one-shot and answers its reply.
type DistillModel func(ctx context.Context, prompt string) (reply string, costUSD float64, err error)

// DistillRun is one repository's run.
type DistillRun struct {
	// Repo is owner/repo, the wiki scope written to.
	Repo string
	// Sessions are the repository's transcripts, oldest first.
	Sessions []*transcripts.Session
	// Manual is the button or the command line: no quiet period, and the prose
	// is never given up on, since this is how a give-up is retried.
	Manual bool
	DryRun bool
	Model  DistillModel
	Now    time.Time
}

// DistillOutcome is what came of one repository's run.
type DistillOutcome struct {
	Repo     string   `json:"repo"`
	State    string   `json:"state"`
	Sessions int      `json:"sessions,omitempty"`
	Pages    []string `json:"pages,omitempty"`
	Commit   string   `json:"commit,omitempty"`
	CostUSD  float64  `json:"costUsd,omitempty"`
	Error    string   `json:"error,omitempty"`
	// Prompt is filled on a dry run only, so the caller can say what it cost.
	Prompt string        `json:"-"`
	Reply  *DistillReply `json:"-"`
}

// Distill states beyond the ones Submit records.
const (
	DistillNothing = "nothing"
	DistillHeld    = "held"
	DistillSkipped = "skipped"
	DistillDry     = "dry-run"
)

// Distill reads a repository's unread prose, has the model rewrite the pages
// it touches, and publishes them. The watermark only moves once they are out.
func (s *Store) Distill(ctx context.Context, run DistillRun) (DistillOutcome, error) {
	out := DistillOutcome{Repo: run.Repo}
	if !s.Enabled() {
		return out, errors.New("wiki is disabled")
	}
	if !validRepo(run.Repo) {
		return out, fmt.Errorf("%q is not a plain owner/repo", run.Repo)
	}
	if run.Now.IsZero() {
		run.Now = time.Now()
	}
	// One run per repository across every process: the draft name is shared, and
	// a second reader of the watermark would distil the same prose twice.
	unlock, held, err := authstore.TryLock(s.distillRunPath(run.Repo))
	if err != nil {
		return out, err
	}
	if !held {
		out.State = DistillHeld
		out.Error = "another distillation of " + run.Repo + " is already running"
		return out, nil
	}
	defer unlock()

	state, err := s.distillState()
	if err != nil {
		return out, err
	}
	spoken, read := s.unread(run, state)
	out.Sessions = len(spoken)
	if len(spoken) == 0 {
		out.State = DistillNothing
		if run.DryRun {
			return out, nil
		}
		return out, s.moveMarks(run.Repo, read, true, false, false, "")
	}

	out, err = s.distillOnce(ctx, run, spoken, out)
	if run.DryRun {
		return out, err
	}
	ok := err == nil && (out.State == "published" || out.State == "unchanged" || out.State == DistillNothing)
	if err != nil && out.Error == "" {
		out.Error = err.Error()
	}
	// A blamed run that says nothing slows the repository for no readable reason.
	if !ok && out.Error == "" {
		out.Error = "failed in state " + quoted(out.State) + " without saying why"
	}
	// The brake is for a model that keeps misreading the task. A failed fetch or
	// a run the clock cut short is neither, and must not hold the repository.
	blame := !ok && ctx.Err() == nil && !errors.Is(err, errBeforeTheModel)
	// Past its goes, the prose is what the model keeps failing on: read past it.
	// Never on the button, which is the way to retry what was given up on.
	skip := blame && !run.Manual && state.Failures[run.Repo]+1 >= maxDistillFailures
	if skip {
		out.State = DistillSkipped
		out.Error = fmt.Sprintf("gave up on %d sessions after %d failures: %s",
			len(spoken), maxDistillFailures, out.Error)
	}
	markErr := s.moveMarks(run.Repo, read, ok, blame, skip, out.Error)
	// Given up on, not failed: the run is over and the next one starts clean.
	// A watermark that did not move is still a failure, and is reported.
	if skip {
		err = nil
	}
	if markErr != nil && err == nil {
		err = markErr
	}
	return out, err
}

// distillOnce is the part of a run that can fail: sync, seed, ask, write,
// submit. Whatever happens, no draft of its own is left behind.
func (s *Store) distillOnce(ctx context.Context, run DistillRun, spoken []Spoken, out DistillOutcome) (DistillOutcome, error) {
	if err := s.Sync(ctx); err != nil {
		out.State = "failed"
		return out, fmt.Errorf("sync the wiki: %w: %w", errBeforeTheModel, err)
	}
	session := distillSession(run.Repo)
	_ = s.Discard(session)
	draft, err := s.seedDraft(session, run.Repo)
	if err != nil {
		out.State = "failed"
		return out, fmt.Errorf("%w: %w", errBeforeTheModel, err)
	}
	defer func() {
		if _, err := os.Stat(draft.Root); err == nil {
			_ = s.Discard(session)
		}
	}()

	shown, named := pagesToShow(draft.Root, run.Repo, s.pageDates())
	text := distillPrompt(run.Repo, run.Now, shown, named, spoken)
	if run.DryRun {
		out.Prompt = text
	}
	reply, cost, err := run.Model(ctx, text)
	out.CostUSD = cost
	if err != nil {
		out.State = "failed"
		return out, fmt.Errorf("run the model: %w", err)
	}
	isShown := map[string]int{}
	for _, page := range shown {
		isShown[page.Path] = len(strings.TrimSpace(page.Body))
	}
	parsed, err := ParseDistill(reply, run.Repo, isShown, func(p string) bool {
		_, err := os.Stat(filepath.Join(draft.Root, filepath.FromSlash(p)))
		return err == nil
	})
	if err != nil {
		out.State = "rejected"
		return out, err
	}
	for _, page := range parsed.Pages {
		out.Pages = append(out.Pages, page.Path)
	}
	if run.DryRun {
		out.State, out.Reply = DistillDry, &parsed
		return out, nil
	}
	if len(parsed.Pages) == 0 && parsed.Log == "" {
		out.State = DistillNothing
		return out, nil
	}

	if err := writeDistilled(draft.Root, run.Repo, parsed, run.Now); err != nil {
		out.State = "failed"
		return out, err
	}
	result, err := s.Submit(ctx, draft, DistillAgent)
	out.State, out.Commit = result.State, result.Commit
	if result.State == "unchanged" {
		return out, nil
	}
	return out, err
}

// unread is each session's prose since its watermark, within the run's
// budget, and where each transcript's watermark goes if the run succeeds.
func (s *Store) unread(run DistillRun, state distillState) ([]Spoken, map[string]int64) {
	var spoken []Spoken
	read := map[string]int64{}
	left := DistillBudget
	for _, session := range run.Sessions {
		if session.Subagent != "" || left <= 0 {
			continue
		}
		info, err := os.Stat(session.Path)
		if err != nil {
			continue
		}
		from := state.Read[session.Path].Size
		if info.Size() <= from {
			continue
		}
		if !run.Manual && run.Now.Sub(info.ModTime()) < DistillQuiet {
			continue
		}
		said, end, err := transcripts.Prose(session, from, min(DistillPerSession, left))
		if err != nil {
			continue
		}
		read[session.Path] = end
		text := render(said)
		if text == "" {
			continue
		}
		left -= len(text)
		spoken = append(spoken, Spoken{Session: session.ID, Branch: session.Branch, Text: text})
	}
	return spoken, read
}

// render is prose as the prompt reads it: one speaker per line, uncut. The
// watermark is past all Prose returned, so a cut here is prose lost.
func render(said []transcripts.Text) string {
	var b strings.Builder
	for _, t := range said {
		fmt.Fprintf(&b, "%s: %s\n", t.Role, t.Body)
	}
	return strings.TrimSpace(b.String())
}

// writeDistilled puts the reply's pages into the draft and the log line at the
// top of the repository's log.
func writeDistilled(root, repo string, reply DistillReply, now time.Time) error {
	for _, page := range reply.Pages {
		dest := filepath.Join(root, filepath.FromSlash(page.Path))
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		body := strings.TrimRight(page.Body, "\n") + "\n"
		if err := os.WriteFile(dest, []byte(body), 0o600); err != nil {
			return err
		}
	}
	if reply.Log == "" {
		return nil
	}
	log := filepath.Join(root, "repos", filepath.FromSlash(repo), "log.md")
	existing, err := os.ReadFile(log)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(log), 0o700); err != nil {
		return err
	}
	return os.WriteFile(log, []byte(prependLog(string(existing), reply.Log, now)), 0o600)
}

// moveMarks records the run: on success the watermarks advance and the failure
// count clears, otherwise only the count goes up, and only where blame is due.
func (s *Store) moveMarks(repo string, read map[string]int64, ok, blame, skip bool, why string) error {
	if !ok && !blame {
		return nil
	}
	return s.withDistillState(func(state *distillState) {
		if !ok && !skip {
			state.Failures[repo]++
			if state.LastError == nil {
				state.LastError = map[string]string{}
			}
			state.LastError[repo] = firstLine(why)
			return
		}
		delete(state.Failures, repo)
		delete(state.LastError, repo)
		// The only record of what the wiki never got.
		if skip {
			if state.Skipped == nil {
				state.Skipped = map[string]string{}
			}
			state.Skipped[repo] = firstLine(why)
		} else {
			delete(state.Skipped, repo)
		}
		now := time.Now().UTC()
		for p, end := range read {
			state.Read[p] = distillMark{Size: end, At: now}
		}
	})
}

// distillSession is the draft a repository's runs use: one name per
// repository, so a leftover is found and thrown away by the next run.
func distillSession(repo string) string {
	sum := sha256.Sum256([]byte(repo))
	return fmt.Sprintf("distill-%x", sum[:6])
}

// distillRunPath is what a run holds while it is going, beside the watermark
// and never it: that file is locked and written from inside a run.
func (s *Store) distillRunPath(repo string) string {
	return s.distillPath() + "." + distillSession(repo)
}

func quoted(state string) string {
	if strings.TrimSpace(state) == "" {
		return "(none)"
	}
	return state
}

// heldWhy is what the last run said, for a hold that names only a count.
func heldWhy(why string) string {
	if strings.TrimSpace(why) == "" {
		return "no reason recorded"
	}
	return why
}

// firstLine keeps a failure short enough to sit in a job's one-line outcome.
func firstLine(why string) string {
	why, _, _ = strings.Cut(strings.TrimSpace(why), "\n")
	if len(why) <= 200 {
		return why
	}
	// On a rune boundary: half a character is invalid UTF-8 by the time it is
	// written out, and reads as a replacement mark mid-word.
	end := 0
	for i := range why {
		if i > 200 {
			break
		}
		end = i
	}
	return why[:end] + "…"
}
