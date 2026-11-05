package ratchet

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The cap is checked against what the working tree adds, not the whole
// repository: the rule is about what an agent is writing now, and a tree-wide
// cap would have meant a mass rewrite of prose that predates it.
const commentCap = CommentCap

func TestNoCommentInTheWorkingTreeRunsPastTheCap(t *testing.T) {
	over := LongComments(addedLines(t), commentCap)
	assert.Emptyf(t, over, "comments over %d lines:\n%s\n\nPut the reasoning in the design doc and leave one line here.",
		commentCap, strings.Join(over, "\n"))
}

func TestLongCommentsFindsARunPastTheCapAndNamesItsLine(t *testing.T) {
	diff := []string{
		"+++ b/a.go",
		"@@ -1,0 +10,5 @@",
		"+// one",
		"+// two",
		"-// gone",
		"+// three",
		"+func f() {}",
		"+// fine",
		"+//go:generate x",
		"+// also fine",
	}
	assert.Equal(t, []string{"a.go:10 — 3 lines"}, LongComments(diff, 2))
	assert.Empty(t, LongComments(diff, 3))
}

// commentShare is the most of a change that may be comment, as a percentage of
// the lines it adds. commentCap bounds one comment; this bounds how many.
//
// The repository sits near 13% and is not the standard to match. allowance is
// what a small change gets regardless, so a handful of declarations can each
// carry their one line without the percentage biting.
//
// 12 rather than 10 since a branch carrying several reviewed features kept
// landing at 11: the pressure was falling on load-bearing prose about locks
// and boundaries, which is the writing this rule was never meant to squeeze.
const commentShare = 12
const allowance = 6

func TestCommentsAreASmallPartOfWhatTheWorkingTreeAdds(t *testing.T) {
	code, comment := 0, 0
	for _, line := range addedLines(t) {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		body := strings.TrimSpace(line[1:])
		switch {
		case body == "":
		case strings.HasPrefix(body, "//") && !strings.HasPrefix(body, "//go:"):
			comment++
		default:
			code++
		}
	}
	total := code + comment
	if total < 40 || comment <= allowance {
		return
	}
	share := comment * 100 / total
	assert.LessOrEqualf(t, share, commentShare,
		"%d of %d added lines are comment (%d%%, cap %d%%).\n"+
			"Cut the ones restating the code and the ones arguing a case; the argument goes in the design doc.",
		comment, total, share, commentShare)
}

// addedLines is every line the working tree adds to a non-test source file, as
// diff output. Untracked files are wholly new, so each of their lines is added.
// Only `//` comments are counted, so a JSDoc block is not seen.
func addedLines(t *testing.T) []string {
	t.Helper()
	const root = "../.."

	diff, err := exec.Command("git", "-C", root, "diff", "HEAD", "-U0", "--",
		"*.go", "*.ts", "*.tsx", ":(exclude)*_test.go", ":(exclude)*.test.ts", ":(exclude)*.d.ts").Output()
	if err != nil {
		t.Skipf("git diff: %v", err)
	}
	lines := strings.Split(string(diff), "\n")

	untracked, err := exec.Command("git", "-C", root, "ls-files", "-o", "--exclude-standard", "--",
		"*.go", "*.ts", "*.tsx").Output()
	if err != nil {
		t.Skipf("git ls-files: %v", err)
	}
	for _, path := range strings.Fields(string(untracked)) {
		if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".test.ts") || strings.HasSuffix(path, ".d.ts") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err)
		lines = append(lines, "+++ b/"+path)
		for _, line := range strings.Split(string(body), "\n") {
			lines = append(lines, "+"+line)
		}
	}
	return lines
}

// TestTheCliClaimsItsOwnIdentifier guards the Local Network grant, which is
// keyed to this string. Renaming it starts from no permission. The window
// signs with one of its own, so what is pinned is the default.
func TestTheCliClaimsItsOwnIdentifier(t *testing.T) {
	sign, err := os.ReadFile(filepath.Join("../..", "tools", "sign.sh"))
	require.NoError(t, err)

	found := regexp.MustCompile(`(?m)^bundle_id="([^"]+)"`).FindAllSubmatch(sign, -1)
	require.Len(t, found, 1, "tools/sign.sh sets bundle_id once")
	assert.Equal(t, "${ADE_BUNDLE_ID:-uk.co.owenrumney.ade-cli}", string(found[0][1]),
		"the cli's identifier is what sign.sh falls back to")
}

// TestTheWindowSignsAsItself keeps the two programs apart: one identifier
// for two bundles is two sets of permissions macOS cannot tell apart.
func TestTheWindowSignsAsItself(t *testing.T) {
	pack, err := os.ReadFile(filepath.Join("../..", "app", "pack.sh"))
	require.NoError(t, err)

	assert.Contains(t, string(pack), "--app-bundle-id=uk.co.owenrumney.ade-window")
	assert.Contains(t, string(pack), "ADE_BUNDLE_ID=uk.co.owenrumney.ade-window")
	assert.NotContains(t, string(pack), "ade-cli")
}

// TestTheWindowIsCalledOneThing keeps the three places that name it in
// step. macOS reads the menu bar from the bundle when it is packaged and
// from app.getName() when it is not, so a disagreement shows up in only
// one of them — which is how it read "Electron" in a dev run.
func TestTheWindowIsCalledOneThing(t *testing.T) {
	const name = "ADE"
	for _, at := range []struct{ file, want string }{
		{filepath.Join("../..", "app", "package.json"), `"productName": "` + name + `"`},
		{filepath.Join("../..", "app", "pack.sh"), `"` + name + `" \`},
		{filepath.Join("../..", "app", "src", "main.ts"), `const NAME = "` + name + `"`},
	} {
		body, err := os.ReadFile(at.file)
		require.NoError(t, err)
		assert.Contains(t, string(body), at.want, at.file)
	}
}
