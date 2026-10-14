package bridge

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// after returns the value following flag, or "" if the flag is absent.
func after(args []string, flag string) string {
	at := slices.Index(args, flag)
	if at < 0 || at+1 >= len(args) {
		return ""
	}
	return args[at+1]
}

// pin replaces the standing instruction for one test.
func pin(t *testing.T, text string) {
	t.Helper()
	was := standing
	standing = func() string { return text }
	t.Cleanup(func() { standing = was })
	pinRole(t, "")
}

func pinRole(t *testing.T, text string) {
	t.Helper()
	was := activeRole
	activeRole = func() string { return text }
	t.Cleanup(func() { activeRole = was })
}

func TestTheStandingInstructionReachesEverySession(t *testing.T) {
	// The point of the setting: a CLAUDE.md is the repository's and is read by
	// whatever chooses to. This is the user's, and ade puts it there.
	pin(t, "Only ever drive the local dev environment.")

	t.Run("a session gets it", func(t *testing.T) {
		prompt := after(claudeArgs(Opts{}), "--append-system-prompt")
		assert.Contains(t, prompt, "Only ever drive the local dev environment.")
		assert.Contains(t, prompt, "The user has set this standing instruction")
	})

	t.Run("it comes before the caller's own prompt and the house rules", func(t *testing.T) {
		prompt := after(claudeArgs(Opts{SystemPrompt: "you are reviewing a migration"}), "--append-system-prompt")
		standingAt := strings.Index(prompt, "Only ever drive")
		require.Positive(t, standingAt+1)
		assert.Less(t, standingAt, strings.Index(prompt, "you are reviewing"))
		assert.Less(t, standingAt, strings.Index(prompt, "Comments in code"))
	})

	t.Run("a one-shot that can write gets it", func(t *testing.T) {
		args := oneShotArgs(OneShotOpts{Prompt: "apply the findings", Tools: []string{"Read", "Edit"}})
		assert.Contains(t, after(args, "--append-system-prompt"), "Only ever drive")
	})

	t.Run("it is labelled as the user's, not as one of ade's rules", func(t *testing.T) {
		assert.Contains(t, userInstruction, "The user has set this")
		assert.Contains(t, userInstruction, "overrides your defaults")
	})
}

func TestNoStandingInstructionLeavesThePromptAsItWas(t *testing.T) {
	pin(t, "   ")
	pinRole(t, "")
	prompt := after(claudeArgs(Opts{}), "--append-system-prompt")
	assert.Equal(t, writingRules+"\n\n"+searchRules+"\n\n"+toolRules+"\n\n"+houseRules+"\n\n"+wikiRules+"\n\n"+showRules+"\n\n"+sessionRules, prompt, "nothing set, nothing added")
}

func TestTheRoleReachesOnlyTheMainChat(t *testing.T) {
	pin(t, "")
	pinRole(t, "Use explicit production profiles.")

	main := after(claudeArgs(Opts{}), "--append-system-prompt")
	assert.Contains(t, main, "Use explicit production profiles.")
	assert.Contains(t, main, roleInstruction)

	named := oneShotSystemPrompt(OneShotOpts{Agent: "security", Tools: []string{"Read"}})
	assert.NotContains(t, named, "Use explicit production profiles.")

	resolve := oneShotSystemPrompt(OneShotOpts{Tools: []string{"Read", "Edit"}})
	assert.NotContains(t, resolve, "Use explicit production profiles.")
}

func TestTheHouseRulesReachEveryTurnThatCanEditCode(t *testing.T) {
	// Without this the machine's own settings would leak into these.
	pin(t, "")

	t.Run("the main agent always gets them", func(t *testing.T) {
		// CLAUDE.md is read once and then sits behind the conversation. The
		// system prompt goes with every turn, which is the point.
		prompt := after(claudeArgs(Opts{}), "--append-system-prompt")
		assert.Contains(t, prompt, "Comments in code you write or edit")
	})

	t.Run("a caller's own prompt is kept, and comes first", func(t *testing.T) {
		prompt := after(claudeArgs(Opts{SystemPrompt: "you are reviewing a migration"}), "--append-system-prompt")
		require.Contains(t, prompt, "you are reviewing a migration")
		require.Contains(t, prompt, "Comments in code you write or edit")
		assert.Less(t, strings.Index(prompt, "you are reviewing"), strings.Index(prompt, "Comments in code"))
	})

	t.Run("a one-shot that can write gets them", func(t *testing.T) {
		args := oneShotArgs(OneShotOpts{Prompt: "apply the findings", Tools: []string{"Read", "Edit"}})
		assert.Contains(t, after(args, "--append-system-prompt"), "Comments in code you write or edit")
	})

	t.Run("a read-only reviewer does not", func(t *testing.T) {
		// Its "comments" are review comments. Telling it to keep those down is
		// the opposite of what it is for.
		args := oneShotArgs(OneShotOpts{Prompt: "review this", Tools: []string{"Read", "Grep", "Glob"}})
		assert.NotContains(t, args, "--append-system-prompt")
	})

	t.Run("a read-only run keeps a system prompt it was given", func(t *testing.T) {
		args := oneShotArgs(OneShotOpts{
			Prompt: "review this", Tools: []string{"Read"}, SystemPrompt: "be adversarial",
		})
		assert.Equal(t, "be adversarial", after(args, "--append-system-prompt"))
	})

	t.Run("a one-shot with Claude Code's default tools does not", func(t *testing.T) {
		// nil Tools means the default set, which is not a statement that this
		// run writes code. The rewrite one-shots are all in this shape.
		args := oneShotArgs(OneShotOpts{Prompt: "rewrite this comment"})
		assert.NotContains(t, args, "--append-system-prompt")
	})
}

func TestEditsCode(t *testing.T) {
	for _, tools := range [][]string{
		{"Write"}, {"Edit"}, {"MultiEdit"}, {"NotebookEdit"}, {"Read", "Grep", "Edit"},
	} {
		assert.Truef(t, editsCode(tools), "%v can write", tools)
	}
	for _, tools := range [][]string{
		nil, {}, {"Read"}, {"Read", "Grep", "Glob", "Bash"},
	} {
		assert.Falsef(t, editsCode(tools), "%v cannot write", tools)
	}
}

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

func TestTheWritingRulesReachEveryTurnIncludingReadOnlyOnes(t *testing.T) {
	pin(t, "")

	reviewer := OneShotOpts{Agent: "staff-reviewer", Tools: []string{"Read", "Grep", "Glob"}}
	assert.Contains(t, oneShotSystemPrompt(reviewer), writingRules)
	assert.NotContains(t, oneShotSystemPrompt(reviewer), houseRules)

	editor := OneShotOpts{Agent: "fixer", Tools: []string{"Read", "Edit"}}
	assert.Contains(t, oneShotSystemPrompt(editor), writingRules)
	assert.Contains(t, oneShotSystemPrompt(editor), houseRules)

	assert.Contains(t, systemPrompt("agent text"), writingRules)
	assert.Contains(t, writingRules, "A response ends the active turn")
	// The rules governed order and not size, so a yes/no question came back
	// with headings, a table and two topics nobody asked about.
	assert.Contains(t, writingRules, "at the size it was asked")
	assert.Contains(t, writingRules, "A reply is 150 words or fewer")
	assert.Contains(t, writingRules, "Ordinary English only")
	assert.Contains(t, writingRules, "Never agree, praise or dramatise before answering")
	assert.Contains(t, writingRules, "A step says what to do, nothing else")
	assert.Contains(t, writingRules, "Cut every word that does not change the meaning")
	// The old closing line let any reply call itself the exception.
	assert.NotContains(t, writingRules, "when the detail *is* the answer")
	assert.Contains(t, writingRules, "No adverbs of degree, no hedges")
	assert.Contains(t, writingRules, "Nothing unasked")
}

func TestTheSearchRulesReachEveryRunAndNameTheTool(t *testing.T) {
	pin(t, "")
	pinRole(t, "")

	reviewer := OneShotOpts{Agent: "staff-reviewer", Tools: []string{"Read", "Grep", "Glob"}}
	assert.Contains(t, oneShotSystemPrompt(reviewer), searchRules)

	editor := OneShotOpts{Agent: "fixer", Tools: []string{"Read", "Edit"}}
	assert.Contains(t, oneShotSystemPrompt(editor), searchRules)

	assert.Contains(t, after(claudeArgs(Opts{}), "--append-system-prompt"), searchRules)
	// A rule that loses to the bypass-permissions reminder is no rule at all.
	assert.Contains(t, searchRules, "this\n  wins")
}

// A reviewer runs the build and the tests too, so it meets the same stale
// toolchain an editor does.
func TestTheToolRulesReachEveryRunAndNameMise(t *testing.T) {
	pin(t, "")
	pinRole(t, "")

	reviewer := OneShotOpts{Agent: "staff-reviewer", Tools: []string{"Read", "Grep", "Glob", "Bash"}}
	assert.Contains(t, oneShotSystemPrompt(reviewer), toolRules)

	editor := OneShotOpts{Agent: "fixer", Tools: []string{"Read", "Edit"}}
	assert.Contains(t, oneShotSystemPrompt(editor), toolRules)

	assert.Contains(t, after(claudeArgs(Opts{}), "--append-system-prompt"), toolRules)
	assert.Contains(t, toolRules, "mise use -g")
	// Looking before declaring a blocker is the whole of the rule.
	assert.Contains(t, toolRules, "which <tool>")
}

// The prose said three while the test enforced two, and agents wrote to the
// prose. Spelt from the constant now, so this only has to check it arrived.
func TestTheCommentRuleQuotesTheCapTheTestEnforces(t *testing.T) {
	assert.Contains(t, houseRules, "2 lines is the limit")
	assert.Contains(t, houseRules, "the 2-line limit")
	assert.NotContains(t, houseRules, "Three lines")
}

func TestTheSessionRulesReachTheMainChatAndNoOneShot(t *testing.T) {
	pin(t, "")
	pinRole(t, "")

	main := after(claudeArgs(Opts{}), "--append-system-prompt")
	assert.Contains(t, main, sessionRules)
	assert.Contains(t, sessionRules, "Do NOT run git commit")
	assert.Contains(t, main, MainSystemPrompt(""), "codex composes the same prompt")

	editor := OneShotOpts{Agent: "fixer", Tools: []string{"Read", "Edit"}}
	assert.NotContains(t, oneShotSystemPrompt(editor), sessionRules)
}

func TestABareRewriteOneShotIsLeftAlone(t *testing.T) {
	pin(t, "")

	got := oneShotSystemPrompt(OneShotOpts{SystemPrompt: "rewrite this comment"})

	assert.Equal(t, "rewrite this comment", got)
}

func TestAReadOnlyRunStillGetsTheStandingInstruction(t *testing.T) {
	pin(t, "only ever drive the local dev environment")

	got := oneShotSystemPrompt(OneShotOpts{Agent: "architect", Tools: []string{"Read"}})

	assert.Contains(t, got, "only ever drive the local dev environment")
	assert.Contains(t, got, userInstruction)
}

func TestARunThatCanEditIsToldAboutTheWiki(t *testing.T) {
	assert.Contains(t, systemPrompt(""), "wiki_submit")
	assert.NotContains(t, readOnlyPromptWithoutRole(""), "wiki_submit", "a reviewer publishes nothing")
}

func TestAHostRunMayPublishToTheWikiUnasked(t *testing.T) {
	args := strings.Join(claudeArgs(Opts{MCPConfig: "/x/wiki.json"}), " ")
	assert.Contains(t, args, "--mcp-config /x/wiki.json --allowedTools "+WikiTool)
	assert.NotContains(t, strings.Join(claudeArgs(Opts{}), " "), "--allowedTools")
}

// TestTheCliClaimsItsOwnIdentifier guards the Local Network grant, which is
// keyed to this string. Renaming it starts from no permission.
func TestTheCliClaimsItsOwnIdentifier(t *testing.T) {
	sign, err := os.ReadFile(filepath.Join("../..", "tools", "sign.sh"))
	require.NoError(t, err)

	found := regexp.MustCompile(`(?m)^bundle_id="([^"]+)"`).FindAllSubmatch(sign, -1)
	require.Len(t, found, 1, "tools/sign.sh sets bundle_id once")
	assert.Equal(t, "uk.co.owenrumney.ade-cli", string(found[0][1]))
}
