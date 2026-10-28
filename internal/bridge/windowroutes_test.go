package bridge

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routesIn is every `/api/...` path a Go file registers a handler for.
func routesIn(t *testing.T, file string) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../..", file))
	require.NoError(t, err)
	out := map[string]bool{}
	found := regexp.MustCompile(`HandleFunc\("[A-Z]+ (/api/[^"]*)"`).FindAllSubmatch(body, -1)
	for _, m := range found {
		out[string(m[1])] = true
	}
	return out
}

// asked is every `/api/...` path the window names, from the files that
// name them as string literals.
func asked(t *testing.T, files ...string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, file := range files {
		body, err := os.ReadFile(filepath.Join("../..", file))
		require.NoError(t, err)
		for _, m := range regexp.MustCompile(`"(/api/[a-z0-9/-]*)`).FindAllSubmatch(body, -1) {
			path := string(m[1])
			out[path] = append(out[path], file)
		}
	}
	return out
}

// TestTheWindowOnlyAsksForRoutesThatExist is here because it did not.
// `/api/active` was asked for by the window and served by nothing: the
// launcher calls that route `/api/serving`, and a 404 at run time is a
// poor way to learn it.
func TestTheWindowOnlyAsksForRoutesThatExist(t *testing.T) {
	// Every file that mounts one: the launcher, the session server, and
	// the settings API both of them embed.
	have := map[string]bool{}
	for _, file := range []string{
		"internal/launcher/launcher.go",
		"internal/web/web.go",
		"internal/settingsapi/settingsapi.go",
	} {
		for path, ok := range routesIn(t, file) {
			have[path] = ok
		}
	}

	// A path with a slot in it — /api/queue/{id}/urgent — is matched by
	// its first two segments, which is as far as a literal gets us.
	prefix := map[string]bool{}
	for path := range have {
		if cut := strings.SplitN(strings.TrimPrefix(path, "/api/"), "/", 2); len(cut) > 0 {
			prefix["/api/"+cut[0]] = true
		}
	}

	for path, where := range asked(t,
		"app/src/main.ts",
		"app/renderer/doing.ts",
		"app/renderer/panels.ts",
		"app/renderer/sources.ts",
	) {
		head := path
		if cut := strings.SplitN(strings.TrimPrefix(path, "/api/"), "/", 2); len(cut) > 0 {
			head = "/api/" + cut[0]
		}
		assert.Truef(t, have[path] || prefix[head],
			"%s asks for %s and no server registers it", strings.Join(where, ", "), path)
	}
}
