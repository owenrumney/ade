// Package sessions lists the agent conversations on this machine and starts a
// ade in the checkout one of them belongs to.
//
// It is shared by the server's switcher and by the standalone launcher, which
// are the same list reached from different places.
package sessions

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/transcripts"
)

// max bounds the list. It is a way back to something you were doing, not an
// archive; sm itself is the archive.
const max = 60

// age is how far back to look. Older than this and you are not switching back
// to it, you are going looking for it.
const age = 14 * 24 * time.Hour

// Session is one agent conversation you could return to.
type Session struct {
	ID       string    `json:"id"`
	Agent    string    `json:"agent"`
	Model    string    `json:"model,omitempty"`
	Dir      string    `json:"dir"`
	Name     string    `json:"name"`
	Branch   string    `json:"branch,omitempty"`
	Title    string    `json:"title,omitempty"`
	LastUsed time.Time `json:"lastUsed"`

	// Path is the transcript on disk, for a reader that needs what was said
	// rather than what is listed. Not sent to the browser.
	Path string `json:"-"`

	// Resumable is false for agents ade cannot resume. Their directory still
	// opens; the conversation does not come with it.
	Resumable bool `json:"resumable"`

	// Here marks the session belonging to the checkout you are already looking
	// at. The launcher belongs to none, and passes an empty here.
	Here bool `json:"here"`

	// Archived is one you have put away. It is still listed, so it can be
	// brought back, but the page hides it unless asked.
	Archived bool `json:"archived,omitempty"`

	// Live sessions still have an agent attached and cannot be combined.
	Live bool `json:"live,omitempty"`

	// Group names the combined-session card this session is shown under.
	Group string `json:"group,omitempty"`
}

// Window bounds what List answers; zero in a field is unbounded. A switcher
// wants the shortlist, a search what the shortlist did not have.
type Window struct {
	Max int
	Age time.Duration
}

func Recent() Window     { return Window{Max: max, Age: age} }
func Everything() Window { return Window{} }

// List returns the recent sessions, newest first.
func List(here string) []Session { return ListIn(here, Recent()) }

func ListIn(here string, window Window) []Session {
	cutoff := time.Time{}
	if window.Age > 0 {
		cutoff = time.Now().Add(-window.Age)
	}
	archived := settings.Archived()
	hidden := settings.Hidden()

	titles := settings.Titles()
	scanned := transcripts.Scan()
	live := transcripts.Liveness(scanned)
	var out []Session
	for _, found := range scanned {
		if found.CWD == "" || found.LastUsed.Before(cutoff) || hidden[found.ID] {
			continue
		}
		// A named agent's run has a transcript of its own, but it is a reply
		// inside a conversation rather than one to go back to.
		if found.Subagent != "" {
			continue
		}
		// So is a one-shot ade wrote the whole of: 18 of 60 rows, measured.
		if transcripts.Machine(found) {
			continue
		}
		// A transcript outlives the directory it was recorded in — a rename or a
		// removed worktree leaves one behind. Offering it would start a server
		// somewhere that is not there.
		if info, err := os.Stat(found.CWD); err != nil || !info.IsDir() {
			continue
		}
		out = append(out, Session{
			ID:        found.ID,
			Agent:     string(found.Agent),
			Model:     found.Model,
			Path:      found.Path,
			Dir:       found.CWD,
			Name:      filepath.Base(found.CWD),
			Branch:    found.Branch,
			Title:     cmp.Or(titles[found.ID], Title(found)),
			LastUsed:  found.LastUsed,
			Resumable: found.Agent == transcripts.AgentClaude || found.Agent == transcripts.AgentCodex,
			Here:      here != "" && found.CWD == here,
			Archived:  archived[found.ID],
			Live:      live[found.Path],
		})
		if window.Max > 0 && len(out) == window.Max {
			break
		}
	}
	return out
}

// Find picks a session out of a list. Callers match against what the scan
// actually returned, so a request cannot name any directory on the machine and
// have a process started in it.
func Find(list []Session, dir, id string) (Session, bool) {
	for _, found := range list {
		if found.Dir == dir && (id == "" || found.ID == id) {
			return found, true
		}
	}
	return Session{}, false
}

// Open runs this binary in the session's checkout, picking the conversation up
// where it left off when that is possible.
func Open(ctx context.Context, target Session) (string, error) {
	id := ""
	if target.Resumable {
		id = target.ID
	}
	return OpenOn(ctx, target.Dir, target.Agent, target.Model, id)
}

// OpenIn runs this binary in a checkout and returns its URL. An empty
// sessionID starts without a conversation, which is what opening somewhere you
// have never run an agent means.
//
// ade backgrounds itself and prints its URL, and hands back the running one's
// when there already is one, so this is the same command you would type there.
func OpenIn(ctx context.Context, dir, sessionID string) (string, error) {
	return OpenAgentIn(ctx, dir, "", sessionID)
}

// OpenAgentIn starts a checkout with a specific agent provider and session.
func OpenAgentIn(ctx context.Context, dir, agent, sessionID string) (string, error) {
	return OpenOn(ctx, dir, agent, "", sessionID)
}

// OpenOn is that, on a named model. Empty model leaves the CLI's own default.
func OpenOn(ctx context.Context, dir, agent, model, sessionID string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find the ade binary: %w", err)
	}

	args := openArgs(agent, model, sessionID)

	cmd := exec.CommandContext(ctx, self, args...) // #nosec G204 -- this binary, in a directory the scan found
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// What ade said is the diagnosis; the exit status alone reads as nothing.
		if said := LastLine(stderr.String()); said != "" {
			return "", fmt.Errorf("start ade in %s: %s", dir, said)
		}
		return "", fmt.Errorf("start ade in %s: %w", dir, err)
	}

	url := LastLine(string(out))
	if !strings.HasPrefix(url, "http://") {
		return "", fmt.Errorf("ade in %s did not report a url", dir)
	}
	return url, nil
}

// None is what --session takes to mean "start a conversation", as against an
// empty id, which reopens whatever the checkout was last in.
const None = "none"

func openArgs(agent, model, sessionID string) []string {
	args := []string{"--no-open"}
	if agent != "" {
		args = append(args, "--provider", agent)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if sessionID != "" {
		args = append(args, "--session", sessionID)
	}

	return args
}

// CreateIn runs `ade create --linear <ticket>` in a repository's main
// checkout and returns the URL of the ade it started: the document named
// after the ticket, a worktree beside the checkout on the ticket's branch,
// and the design drafted from the ticket once the server is up. The same
// command you would type there, so the same result.
func CreateIn(ctx context.Context, dir, ticket string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find the ade binary: %w", err)
	}
	cmd := exec.CommandContext(ctx, self, "create", "--linear", ticket, "--no-open") // #nosec G204 -- this binary, in a directory the scan found
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return "", fmt.Errorf("start work on %s in %s: %s", ticket, dir, LastLine(string(exit.Stderr)))
		}
		return "", fmt.Errorf("start work on %s in %s: %w", ticket, dir, err)
	}
	url := LastLine(string(out))
	if !strings.HasPrefix(url, "http://") {
		return "", fmt.Errorf("ade in %s did not report a url", dir)
	}
	return url, nil
}

// CreateNamed runs `ade create <name>` in a checkout: the document, the
// worktree on its branch and the session. It returns the URL and the worktree.
func CreateNamed(ctx context.Context, dir, name, agent, model string) (string, string, error) {
	return CreateNamedFrom(ctx, dir, name, agent, model, "")
}

// CreateNamedFrom is CreateNamed with the worktree branched from base rather
// than a freshly fetched main; HEAD is the checkout as it stands.
func CreateNamedFrom(ctx context.Context, dir, name, agent, model, base string) (string, string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("find the ade binary: %w", err)
	}
	args := append([]string{"create"}, openArgs(agent, model, "")...)
	if base != "" {
		args = append(args, "--base", base)
	}
	// After --, so a name cannot be read as a flag.
	args = append(args, "--", name)
	cmd := exec.CommandContext(ctx, self, args...) // #nosec G204 -- this binary, in a directory the scan found
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return "", "", fmt.Errorf("start %s in %s: %s", name, dir, LastLine(string(exit.Stderr)))
		}
		return "", "", fmt.Errorf("start %s in %s: %w", name, dir, err)
	}
	url, path := createdAt(string(out))
	if url == "" || path == "" {
		return "", "", fmt.Errorf("ade in %s did not report a url and a worktree", dir)
	}
	return url, path, nil
}

// createdAt reads what create printed: the address last, and the worktree as
// the last absolute path that is a directory, marked (existing) or not.
func createdAt(out string) (url, path string) {
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), "(existing)"))
		if strings.HasPrefix(line, "http://") {
			url = line
			continue
		}
		if info, err := os.Stat(line); filepath.IsAbs(line) && err == nil && info.IsDir() {
			path = line
		}
	}
	return url, path
}

// Ask makes one request of a running ade, with the token off its recorded
// URL. It is how one ade hands a document, a session or a first prompt to
// another rather than starting a rival for the same checkout.
func Ask(ctx context.Context, recURL, method, path, body string) error {
	_, err := AskFor(ctx, recURL, method, path, body, 10*time.Second)
	return err
}

// AskFor is Ask, with the answer — connecting to a host needs the URL that
// comes back. wait bounds it, since dialling over ssh is not a loopback hop.
func AskFor(ctx context.Context, recURL, method, path, body string, wait time.Duration) ([]byte, error) {
	parsed, err := url.Parse(recURL)
	if err != nil {
		return nil, err
	}
	token := parsed.Query().Get("t")
	if token == "" {
		return nil, fmt.Errorf("no token in the recorded url")
	}

	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, parsed.Scheme+"://"+parsed.Host+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Ade-Token", token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	answer, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		if msg := errorIn(answer); msg != "" {
			return answer, fmt.Errorf("%s", msg)
		}
		return answer, fmt.Errorf("the running ade answered %s", res.Status)
	}
	return answer, nil
}

// errorIn pulls the message out of ade's error shape, so a caller relaying one
// says what went wrong rather than which status code carried it.
func errorIn(body []byte) string {
	var shape struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &shape); err != nil {
		return ""
	}
	return shape.Error
}

// LastLine is where ade prints its URL; a created document or worktree path
// comes before it.
func LastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// Title is what a session is called when nobody has named it.
func Title(found *transcripts.Session) string {
	if t := strings.TrimSpace(found.Title); t != "" {
		return t
	}
	return truncate(said(found.FirstPrompt), 120)
}

// said is what the user asked, out of the prompt ade wrapped it in. Untitled
// rows read "We are working on this repository together in ade" without it.
func said(prompt string) string {
	for _, part := range transcripts.Unwrap(prompt) {
		if part.Role == transcripts.RoleUser && strings.TrimSpace(part.Body) != "" {
			return strings.TrimSpace(part.Body)
		}
	}
	return strings.TrimSpace(prompt)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
