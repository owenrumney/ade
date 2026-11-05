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
	"POST /api/threads":               "the thread store follows the view",
	"POST /api/threads/{id}/reply":    "the thread store follows the view",
	"POST /api/threads/{id}/resolve":  "the thread store follows the view",
	"DELETE /api/threads/{id}":        "the thread store follows the view",
	"POST /api/threads/reset":         "the thread store follows the view",
	"DELETE /api/threads":             "the thread store follows the view",
	"POST /api/diff/root":             "this is the switch itself",
	"PUT /api/diff/folders":           "it changes what the switch may pick",
	"POST /api/terminal/close":        "the shell is the machine's, not the diff's",
	"POST /api/agent/terminal":        "the agent's own interface opens in the session's checkout, as the shell does",
	"POST /api/agent/terminal/close":  "gives the conversation back; touches no checkout",
	"POST /api/agent/seen":            "clears the agent's badge; touches no checkout",
	"POST /api/agent/conversation":    "points the session's own agent at another conversation; touches no checkout",
	"POST /api/agent/findings":        "writes under the session's own .git/ade and types at its agent",
	"POST /api/agent/brief":           "types one line at the session's own agent; touches no checkout",
	"POST /api/review":                "reads only, and its run takes the diff's root",
	"POST /api/commit-message":        "reads only, and its run takes the diff's root",
	"POST /api/commit":                "git acts on the directory it is given",
	"POST /api/stage":                 "git acts on the directory it is given",
	"POST /api/unstage":               "git acts on the directory it is given",
	"POST /api/push":                  "git acts on the directory it is given",
	"POST /api/branch":                "git acts on the directory it is given",
	"POST /api/base/pull":             "git acts on the directory it is given",
	"POST /api/base/rebase":           "git acts on the directory it is given",
	"POST /api/scope":                 "scope is per root",
	"POST /api/cancel":                "stopping a run must never be refused",
	"POST /api/launcher":              "starts the launcher, not this checkout",
	"POST /api/sessions/open":         "opens another session",
	"POST /api/reviews/open":          "opens a review elsewhere",
	"POST /api/reviews/mute":          "the dashboard, not this diff",
	"POST /api/notifications/dismiss": "the dashboard, not this diff",
	"POST /api/linear/refresh":        "the dashboard, not this diff",
	"POST /api/linear/start":          "starts work in another repository",
	"POST /api/permissions":           "the rules are ade's, not a checkout's",
	"POST /api/bus/answer":            "the team's question, not this diff",
	"POST /api/bus/ask":               "puts a question to a person, not this diff",
	"POST /api/bus/presence":          "how you look to the team, not this diff",
	"POST /api/bus/claim":             "takes a review elsewhere",
	"POST /api/bus/result":            "reports a review back, not this diff",
	"POST /api/bus/request-review":    "asks the team about a pull request",
	"POST /api/bus/dismiss":           "clears an answer that has been read",
	"POST /api/bus/refresh":           "re-reads the roster, not this diff",
	"POST /api/bus/here":              "the editor's window, not this diff",
	"POST /api/exit":                  "the door",
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
