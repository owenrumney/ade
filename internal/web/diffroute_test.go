package web

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// viewSafe is every mutating route that may run while the Diff tab is showing
// another repository. Everything else must be mounted behind guardHome.
//
//nolint:gochecknoglobals // a table, not state
var viewSafe = map[string]string{
	"POST /api/attach":                      "a selection waiting on the composer, which belongs to the conversation",
	"DELETE /api/attach":                    "clears what the composer already took",
	"POST /api/threads":                     "the thread store follows the view",
	"POST /api/threads/{id}/reply":          "the thread store follows the view",
	"POST /api/threads/{id}/resolve":        "the thread store follows the view",
	"DELETE /api/threads/{id}":              "the thread store follows the view",
	"POST /api/threads/reset":               "the thread store follows the view",
	"DELETE /api/threads":                   "the thread store follows the view",
	"POST /api/diff/root":                   "this is the switch itself",
	"POST /api/diagnose/net":                "asks the network, touches no checkout",
	"POST /api/terminal/close":              "the shell is the machine's, not the diff's",
	"POST /api/agent/terminal":              "the agent's own interface opens in the session's checkout, as the shell does",
	"POST /api/agent/terminal/close":        "gives the conversation back; touches no checkout",
	"POST /api/resolve":                     "a one-shot, and its run takes the diff's root",
	"POST /api/review":                      "reads only, and its run takes the diff's root",
	"POST /api/explain":                     "reads only, and its run takes the diff's root",
	"POST /api/commit-message":              "reads only, and its run takes the diff's root",
	"POST /api/answer":                      "reads only, in the agent's own directory, and writes nothing",
	"POST /api/sharpen":                     "reads the prompt it is sent and the open design document, and writes nothing",
	"POST /api/commit":                      "git acts on the directory it is given",
	"POST /api/stage":                       "git acts on the directory it is given",
	"POST /api/unstage":                     "git acts on the directory it is given",
	"POST /api/push":                        "git acts on the directory it is given",
	"POST /api/branch":                      "git acts on the directory it is given",
	"POST /api/base/pull":                   "git acts on the directory it is given",
	"POST /api/base/rebase":                 "git acts on the directory it is given",
	"POST /api/scope":                       "scope is per root",
	"POST /api/cancel":                      "stopping a run must never be refused",
	"POST /api/sides/{id}/cancel":           "stopping a run must never be refused",
	"POST /api/graph/cancel":                "stopping a run must never be refused",
	"PUT /api/queue/{id}":                   "the queue is the conversation's",
	"DELETE /api/queue/{id}":                "the queue is the conversation's",
	"POST /api/queue/run":                   "the queue is the conversation's",
	"POST /api/queue/{id}/urgent":           "the queue is the conversation's",
	"POST /api/chat/{id}/separate":          "the queue is the conversation's; the run it stops has the diff's root",
	"POST /api/launcher":                    "starts the launcher, not this checkout",
	"POST /api/sessions/open":               "opens another session",
	"POST /api/sessions/archive":            "the session list, not this diff",
	"POST /api/sessions/delete":             "the session list, not this diff",
	"POST /api/sessions/combine":            "the session list, not this diff",
	"POST /api/sessions/uncombine":          "the session list, not this diff",
	"POST /api/sessions/suggestion/dismiss": "the session list, not this diff",
	"POST /api/reviews/open":                "opens a review elsewhere",
	"POST /api/reviews/mute":                "the dashboard, not this diff",
	"POST /api/notifications/dismiss":       "the dashboard, not this diff",
	"POST /api/linear/refresh":              "the dashboard, not this diff",
	"POST /api/linear/start":                "starts work in another repository",
	"PUT /api/sandbox":                      "this is what the root list is built from",
	"PUT /api/sandbox/on":                   "a session setting",
	"PUT /api/preview":                      "a session setting",
	"POST /api/chat":                        "the agent runs at home either way",
	"POST /api/chat/new":                    "the conversation is the session's",
	"POST /api/chat/clear":                  "the conversation is the session's",
	"POST /api/chat/resume":                 "the conversation is the session's",
	"POST /api/chat/model":                  "the conversation is the session's",
	"POST /api/chat/answer":                 "the conversation is the session's",
	"POST /api/chat/decide":                 "the conversation is the session's",
	"POST /api/chat/free":                   "the conversation is the session's",
	"POST /api/hook/pretooluse":             "the gate decides this session's own tool calls",
	"POST /api/permissions":                 "the rules are ade's, not a checkout's",
	"POST /api/chat/switch":                 "the conversation is the session's",
	"POST /api/investigate":                 "the conversation is the session's",
	"POST /api/design":                      "the design document is the session's",
	"PUT /api/design":                       "the design document is the session's",
	"DELETE /api/design":                    "the design document is the session's",
	"DELETE /api/design/doc":                "the design document is the session's",
	"POST /api/design/push":                 "the design document is the session's",
	"POST /api/design/seed":                 "drafts from a ticket, not from the diff",
	"POST /api/design/fold":                 "the design document is the session's",
	"POST /api/design/condense":             "the design document is the session's",
	"PUT /api/project":                      "project memory is the session's",
	"POST /api/project/entry":               "project memory is the session's",
	"POST /api/project/distil":              "project memory is the session's",
	"DELETE /api/files":                     "the session's own .git/ade files, which do not move with the diff root",
	"POST /api/internal/delegate":           "a subagent talking back, not a diff action",
	"POST /api/internal/delegate/doc":       "the design document is the session's",
	"POST /api/bus/answer":                  "the team's question, not this diff",
	"POST /api/bus/ask":                     "puts a question to a person, not this diff",
	"POST /api/bus/presence":                "how you look to the team, not this diff",
	"POST /api/bus/claim":                   "takes a review elsewhere",
	"POST /api/bus/result":                  "reports a review back, not this diff",
	"POST /api/bus/request-review":          "asks the team about a pull request",
	"POST /api/bus/dismiss":                 "clears an answer that has been read",
	"POST /api/bus/refresh":                 "re-reads the roster, not this diff",
	"POST /api/bus/here":                    "the editor's window, not this diff",
	"POST /api/transcribe":                  "audio in, text back; it reads and writes nothing",
	"POST /api/exit":                        "the door",
	"POST /api/verify":                      "the check list is the repository's, not one diff's",
}

var routeLine = regexp.MustCompile(`mux\.HandleFunc\("(POST|PUT|DELETE) ([^"]+)", ([^\n]*)\)`)

// TestEveryMutatingRouteDecidesAboutTheViewRoot reads the mount table rather
// than the handlers, so a route added without a decision fails the build.
func TestEveryMutatingRouteDecidesAboutTheViewRoot(t *testing.T) {
	source, err := os.ReadFile("web.go")
	require.NoError(t, err)

	var undecided []string
	mounted := map[string]bool{}
	for _, match := range routeLine.FindAllStringSubmatch(string(source), -1) {
		route := match[1] + " " + match[2]
		mounted[route] = true
		if strings.Contains(match[3], "guardHome") {
			continue
		}
		if _, ok := viewSafe[route]; ok {
			continue
		}
		undecided = append(undecided, route)
	}
	require.NotEmpty(t, mounted, "the mount table should have been read")

	sort.Strings(undecided)
	assert.Emptyf(t, undecided, "these mutate but say nothing about a foreign diff root:\n%s\n\n"+
		"Mount behind s.guardHome, or add it to viewSafe with the reason it is safe.",
		strings.Join(undecided, "\n"))

	var stale []string
	for route := range viewSafe {
		if !mounted[route] {
			stale = append(stale, route)
		}
	}
	sort.Strings(stale)
	assert.Emptyf(t, stale, "viewSafe names routes that are not mounted:\n%s", strings.Join(stale, "\n"))
}

// TestTheRouteScanSeesTheRoutes guards the scan itself: a mount table written
// another way would make the test above pass by reading nothing.
func TestTheRouteScanSeesTheRoutes(t *testing.T) {
	source, err := os.ReadFile("web.go")
	require.NoError(t, err)
	found := routeLine.FindAllStringSubmatch(string(source), -1)
	assert.Greater(t, len(found), 50, fmt.Sprintf("only %d mutating routes matched", len(found)))
}

// Every diff the browser is handed goes through both display passes, highlight
// first: it assigns the tokens outright, so marking before it erases the marks.
func TestEveryDiffIsHighlightedAndWordMarked(t *testing.T) {
	sites := 0
	err := filepath.WalkDir("../..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path) // #nosec G304 -- a walk of this repository
		require.NoError(t, err)

		lit := offsetsOf(string(body), "highlight.Apply(")
		marks := offsetsOf(string(body), "words.Apply(")
		sites += len(lit)
		if !assert.Equalf(t, len(lit), len(marks),
			"%s highlights a diff without marking the words that changed", path) {
			return nil
		}
		for i := range lit {
			assert.Lessf(t, lit[i], marks[i],
				"%s marks the words before it highlights, which erases every mark", path)
		}
		return nil
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, sites, 5, "found no diff builders to check")
}

func offsetsOf(body, needle string) []int {
	var out []int
	for at := 0; ; {
		i := strings.Index(body[at:], needle)
		if i < 0 {
			return out
		}
		out = append(out, at+i)
		at += i + len(needle)
	}
}
