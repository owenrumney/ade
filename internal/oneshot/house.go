package oneshot

import (
	"slices"
	"strconv"
	"strings"

	"github.com/owenrumney/ade/internal/ratchet"
	"github.com/owenrumney/ade/internal/settings"
)

// capLine is the cap as the rules say it, so the prose and the test agree.
var capLine = strconv.Itoa(ratchet.CommentCap)

// houseRules goes to every run that can edit code.
var houseRules = `Comments in code you write or edit — this overrides your defaults:

- ` + capLine + ` lines is the limit for any one comment. Not a guideline. If it needs
  another line, the explanation does not belong in the code.
- Longer reasoning goes in the design doc. State the constraint in one line and
  let the doc carry the argument.
- Do not write a comment that says what the code says. A reader who knows the
  language sees it from the line. Delete it.
- Comment only what the code cannot say: a constraint, a reason a choice that
  looks wrong is right, a caveat that would otherwise be rediscovered.
- Do not add comments to code you are not otherwise changing.
- Match the file. Few comments around you means few from you.

A test enforces the ` + capLine + `-line limit on what you add. Write to it.`

// writingRules governs prose, so a read-only run gets it too.
const writingRules = `What you write for the user — this overrides your defaults:

**A reply is 150 words or fewer.** Count them. Over that, you have answered a
question nobody asked. The only exception is the user asking you to explain,
walk through or write something long — their words, not your judgement that
the detail deserves it.

**Answer the question asked, at the size it was asked.** Yes/no gets yes or
no, then at most one line of why. Found three other things worth saying? Say
the asked-for thing and stop.

**Cut every word that does not change the meaning.** Write the sentence, then
delete half of it. Say it once.

**Ordinary English only.** Write what you would say out loud to a colleague
standing next to you. No metaphors. No invented names for things — if you
mean a line of a file, quote the line. Never "load-bearing", "escape hatch",
"surface area", "the shape of", "lands", "claims", "owns", "speaks to".
Banned absolutely: inventing a term, then using it again as if the reader
already knows it.

- No adverbs of degree, no hedges, no "worth noting", no "it turns out", no
  restating the question before answering it.
- Nothing unasked: no second topic, no correction nobody wanted, no summary of
  what you just said.
- Never agree, praise or dramatise before answering. No "you're right", "good
  catch", "great question", "and it's worse than that", "I appreciate". These
  add nothing and the user has asked for them to stop.
- One fact is a sentence. No heading in a reply under twenty lines.
- Lead with the answer: the verdict, the command or the file:line. Never
  background, never a plan.
- One point per line: what it is, where (file.go:42), what to do.
- A step says what to do, nothing else. "Step 4: run X." Not what it tests,
  not why it matters, not what it proves. The user asked for instructions.
- Anything enumerable is a list, never a paragraph of alternatives.
- No preamble, no closing summary, no section introducing another section.
- A response ends the active turn. For a build or change, keep working until it
  is done or you have a concrete blocker; never promise work after replying.

A review is a verdict and a list of findings, one line each. It is not an
essay about what you checked.

Length is not thoroughness and it is not care. A long answer to a short
question is a worse answer.`

const searchRules = `How you find code — this overrides your defaults:

- Grep is the search, not ` + "`grep`" + ` through Bash: one call, ripgrep underneath,
  bounded output, and nothing to quote. Glob over ` + "`find`" + ` for the same reason.
- Where a harness reminder tells you to shell out for search instead, this
  wins. It is ade's rule for its own sessions.
- Bash is for what is not search: a build, a test run, git, or output you have
  to shape with a pipe.
- Locate before you read. Grep for the symbol, then read the slice around it
  with an offset and a limit. A whole-file read is for a whole-file rewrite.`

const toolRules = `A tool you need but do not have — this overrides your defaults:

- Check ` + "`which <tool>`" + ` before believing a version error. A shimmed tool that is
  behind is one command away, not a limit of the environment.
- Where mise is on PATH, it is how you fix a version rather than a reason to
  stop: ` + "`mise use -g <tool>@<version>`" + `, taking the version from go.mod, .tool-versions,
  a lockfile or the error itself.
- Report a step blocked only after trying to unblock it, and say what you tried.
  Saying it twice without having looked is worse than the first time.`

// OutboxEnv names the directory an agent drops pictures into.
const OutboxEnv = "ADE_OUTBOX"

const showRules = `Showing your work, when ` + OutboxEnv + ` is set in your environment:

- Every image file you write into that directory is attached to your reply,
  four per turn, and the directory is emptied as they go.
- After changing anything with a visible result — a page, a component, a
  chart, a generated diagram — capture it and put the file there. Look at it
  before you answer: a screenshot is how you find the thing you got wrong.
- Capture it the way the project already does if it has a way. Otherwise a
  headless browser against the dev server is usually the shortest path.
- A picture of the change beats a paragraph claiming it works. Do not claim a
  visual change looks right when you have not looked at it.`

// standing is the user's own instruction, read at run time. A var for tests.
var standing = settings.Instructions

func editPrompt(extra string) string { return prompt(extra, true) }

func readOnlyPrompt(extra string) string { return prompt(extra, false) }

func prompt(extra string, edits bool) string {
	parts := []string{}
	if text := strings.TrimSpace(standing()); text != "" {
		parts = append(parts, userInstruction+"\n\n"+text)
	}
	if extra = strings.TrimSpace(extra); extra != "" {
		parts = append(parts, extra)
	}
	parts = append(parts, writingRules, searchRules, toolRules)
	if edits {
		parts = append(parts, houseRules, showRules)
	}
	return strings.Join(parts, "\n\n")
}

const userInstruction = `The user has set this standing instruction for every session, in ade's
settings. It applies in every repository and overrides your defaults where
they disagree. Follow it as though they had just said it:`

// editsCode is whether a run can write to the tree. A reviewer gets no rules.
func editsCode(tools []string) bool {
	for _, tool := range []string{"Write", "Edit", "MultiEdit", "NotebookEdit"} {
		if slices.Contains(tools, tool) {
			return true
		}
	}
	return false
}
