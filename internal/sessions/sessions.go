// Package sessions lists the agent conversations on this machine and starts a
// ade in the checkout one of them belongs to.
//
// It is shared by the server's switcher and by the standalone launcher, which
// are the same list reached from different places.
package sessions

import (
	"bytes"
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
	"regexp"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/linear"
)

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

	Ticket string   `json:"ticket"`
	Tags   []string `json:"tags"`
	// Brief is the newest file under the checkout's .git/ade/docs.
	Brief string `json:"brief"`

	// Resumable is false for agents ade cannot resume. Their directory still
	// opens; the conversation does not come with it.
	Resumable bool `json:"resumable"`

	// Here marks the session belonging to the checkout you are already looking
	// at. The launcher belongs to none, and passes an empty here.
	Here bool `json:"here"`

	// Archived is one you have put away. It is still listed, so it can be
	// brought back, but the page hides it unless asked.
	Archived bool `json:"archived,omitempty"`

	// Live is a session whose checkout has a server running.
	Live bool `json:"live,omitempty"`
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
	return OpenWith(ctx, dir, Start{Agent: agent, Model: model, Session: sessionID})
}

// Start is what a session begins on, beyond the checkout it is in.
type Start struct {
	Agent   string
	Model   string
	Session string
}

func OpenWith(ctx context.Context, dir string, st Start) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find the ade binary: %w", err)
	}

	args := openArgs(st.Agent, st.Model, st.Session)

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

// Work is what `ade create` is asked for beyond the name: what the session
// starts on, and the other checkouts it may edit.
type Work struct {
	Name string
	// Ticket starts the work from a tracker reference instead of a name:
	// FIX-623 for Linear, #12 for a GitHub issue of this repository.
	Ticket string
	Agent  string
	Model  string
	// Base branches the worktree from this ref rather than a fetched main.
	Base string
	// Also are checkouts that get a worktree each on the same branch.
	Also []string
}

// CreateNamed runs `ade create <name>` in a checkout: the document, the
// worktree on its branch and the session. It returns the URL and the worktree.
func CreateNamed(ctx context.Context, dir, name, agent, model string) (string, string, error) {
	return CreateWork(ctx, dir, Work{Name: name, Agent: agent, Model: model})
}

// CreateNamedFrom is CreateNamed with the worktree branched from base rather
// than a freshly fetched main; HEAD is the checkout as it stands.
func CreateNamedFrom(ctx context.Context, dir, name, agent, model, base string) (string, string, error) {
	return CreateWork(ctx, dir, Work{Name: name, Agent: agent, Model: model, Base: base})
}

// issueRefRE is a GitHub issue of this repository: #12, or the bare number.
var issueRefRE = regexp.MustCompile(`^#?([0-9]{1,9})$`)

// TicketArgs is the `ade create` flag a ticket reference means, or nothing for
// an empty one. This reaches a command line, so the shape is the boundary.
func TicketArgs(ticket string) ([]string, error) {
	ticket = strings.TrimSpace(ticket)
	switch {
	case ticket == "":
		return nil, nil
	case linear.ValidRef(ticket):
		return []string{"--linear", strings.ToUpper(ticket)}, nil
	default:
		if m := issueRefRE.FindStringSubmatch(ticket); m != nil && m[1] != "0" {
			return []string{"--github", m[1]}, nil
		}
		return nil, fmt.Errorf("%q is not a ticket: give a Linear reference like FIX-623, or a GitHub issue like #12", ticket)
	}
}

// CreateWork is CreateNamed with everything the new-session screen can ask
// for. It returns the URL and the home worktree.
func CreateWork(ctx context.Context, dir string, w Work) (string, string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("find the ade binary: %w", err)
	}
	name := w.Name
	from, err := TicketArgs(w.Ticket)
	if err != nil {
		return "", "", err
	}
	args := append([]string{"create"}, openArgs(w.Agent, w.Model, "")...)
	if w.Base != "" {
		args = append(args, "--base", w.Base)
	}
	for _, also := range w.Also {
		args = append(args, "--also", also)
	}
	if len(from) > 0 {
		// The flag names the document itself, so there is no positional name.
		name = strings.TrimSpace(w.Ticket)
		args = append(args, from...)
	} else {
		// After --, so a name cannot be read as a flag.
		args = append(args, "--", name)
	}
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
	// 4 MB because a design document is relayed through here, and that is the
	// size docs allows one; a lower cap truncates it into a parse error.
	answer, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
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
