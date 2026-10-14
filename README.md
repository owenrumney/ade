# ADE

[![test](https://github.com/owenrumney/ade/actions/workflows/test.yml/badge.svg?event=pull_request)](https://github.com/owenrumney/ade/actions/workflows/test.yml)
[![scan](https://github.com/owenrumney/ade/actions/workflows/scan.yml/badge.svg?event=pull_request)](https://github.com/owenrumney/ade/actions/workflows/scan.yml)
[![licence: Apache-2.0](https://img.shields.io/badge/licence-Apache--2.0-blue.svg)](LICENSE)

Design a change, have an agent build it, and review what it did — in your browser,
on your Claude subscription.

**A**gentic **D**evelopment **E**nvironment: the design document is the brief the
agent builds from, and the thing you come back to when the diff stops making sense.

```
ade create --linear FIX-623   # a design document, and a worktree to build it in
ade                           # review the branch
ade done                      # finished: stop the server, remove the worktree
```

```bash
ade                    # merge base of the default branch → working tree
ade --committed        # ignore uncommitted work; base → HEAD
ade main..feature      # any revspec
ade --base develop     # override the base branch
ade --pr 6             # a GitHub pull request, with its review comments
```

ade prints a `http://127.0.0.1:PORT/?t=...` url and opens it. Pass `--no-open`
to just print it.

## The chat

The page opens on the **Chat** tab: a conversation with the agent, in the style
of a messaging app. Ask for something and it reads the repository, answers, or
makes the change; what it changes lands in the working tree, where the Diff tab
shows it. Everything the agent says and does is in the transcript as it
happens — its prose as replies, each tool call as a folded row you can open for
the arguments and the result, and a quiet note for what a run did or why it
stopped. The transcript is the session's history: what Review, Resolve and the
rest did is read there, between the messages they came between.

A message typed while the agent is working is **injected** into the run, so it
hears you mid-turn rather than after; tick *queue until this run finishes* to
have it wait instead. A cold run — Review, Commit message, Create PR — cannot
take one, so a message sent during those waits regardless.

A waiting message says so, and offers **mark urgent** to move it to the front of
the queue, **remove** to drop it, and **run now** when nothing is running. Urgent
does not interrupt: the queue only moves when the run that is going ends.

**⌘N** (or Ctrl+N; the browser keeps Cmd+N for itself in some builds) starts a
new session from a prompt, on a model you choose. **⌘P** opens the command palette, which holds the main actions.

Every message of yours has **Undo**. ade snapshots the working tree before the
message runs, and Undo puts the files back to that: it lists what it will restore,
delete and recreate, and waits for you to confirm. It undoes every later turn too,
and your own edits since. The conversation is left as it is, and the agent is told
on your next message. **Redo** reverses it. The last 20 snapshots are kept, under
`refs/ade/turns/`; ignored files are not in them, and a commit made since refuses it.

After a turn that changed files, ade runs **this repository's own checks** and says
so: `checks passed (3)`, or `vet failed` with the output folded under it. They run
in order and stop at the first failure, and the failure is handed to the agent with
your next message, so nothing is pasted back by hand. Nothing is gated — the turn is
marked, not blocked — and **Stop** ends a check run as well as the turn.

A check can also carry a `narrow` form, such as `go test {{.Packages}}` beside
`go test ./...`. It runs first, on what the turn changed, and the full list starts
only when it passes. `{{.Packages}}` is the Go packages that changed and
`{{.Files}}` the changed files under the check's `dir`; set both in
`.git/ade/checks.json`. A narrow pass is reported as one and never as `checks passed`.

The list is a name and a command each, kept in ade's own state rather than in the
repository: a committed one would be a command from a stranger, run the moment a
turn ends. ade offers a detected list the first time, which is never a test command;
the status bar item edits it, or runs them without waiting for a turn.

Highlighting text in a design document or a source file and pressing *Ask about
this* attaches it to your next message. That is how the doc and the code get
quoted; there is no side conversation scoped to either.

The conversation is written through to `<git dir>/ade/chat.db` — a bbolt store,
one conversation per Claude session — so a restart, a tab switch or a wander
off to the diff costs nothing. Half-typed messages survive the tab switch too.

## What it shows

A side-by-side diff with inline comment threads. Hover a changed line's gutter,
click `+`, and leave a comment. Threads support replies and can be resolved and
reopened, like a GitHub review thread.

By default the diff includes **uncommitted edits and untracked files**, so a new
file you have not `git add`ed still gets reviewed. `--committed` narrows it to
what is actually on the branch.

Code is syntax highlighted by language, picked from the file name. The colours are
deliberately muted so the add/delete tint stays the thing you read first; a file
with no known lexer renders as plain text.

Highlighting happens server-side with chroma, and the browser builds each line
from token runs using `textContent` — repository content is never parsed as
HTML. Each hunk is lexed a side at a time, so a string or comment spanning
several lines is still understood.

## Design, then build, then review

A design document is optional: it is what you reach for when the work is complex
enough to want a brief. `ade create <name>` starts a markdown design document,
checks the repository out beside itself on a branch named after it —
`~/code/owenrumney/ade` becomes `~/code/owenrumney/ade-<name>` on branch
`<name>` — and opens on the document, so the work is isolated from the start.
Run it again for the same name and it opens what is already there — the
document, the worktree, and the server if one is running — so coming back to a
ticket lands you where you left it rather than refusing.

`ade create --linear FIX-123` does the same for a ticket: the document, and so
the branch, is named after it and drafted from it.

`ade investigate --linear FIX-123` is the other half: a ticket you need to
understand rather than build. It opens a session in the checkout you are in —
no document, no worktree, no branch — with a skill from `~/.claude/skills` as
the agent's instructions (`prod-triage` unless `--skill` names another), and the
ticket as the brief. The agent reads it, then goes after the evidence with
whatever it has: logs, the database, the code in front of it.

It gets a conversation of its own and runs on the host, not in a container: the
evidence is behind this machine's MCP servers, and a sandboxed agent reaches
none of them.

Design documents live under the repository's **common** git dir, which every worktree
shares. That is what makes the above work: the document is written in the main
checkout and still found from the worktree. Comment threads are per-branch and stay
in each checkout's own git dir.

A document opens by itself when one belongs to the branch you are on. ade looks
in `.scratch` and inside the git dir for one named after the branch's ticket —
`feat/dev-82-add-lsp` finds `DEV-82.md` — or named for the branch itself. No match
means the Design tab lists what exists and offers to start one, rather than
opening whichever document you touched last. Talk the change through in the
chat — the open document is named to the agent on every turn, so a decision made
in conversation gets written down there. Highlight a paragraph and press *Ask
about this* to attach it to your next message.

The document is watched while ade runs, so when the agent rewrites it the page
follows within a second or so — no reloading at the right moment. The one exception
is while you have the editor open: reloading would throw away what you are typing,
so it says the agent has rewritten it underneath you instead.

Saving the first draft starts the conversation on its own — the agent reads what
you wrote and comes back with what it thinks the change is and what is still
unclear. Saving a template with nothing under the headings does not, so an empty
scaffold never spends a run.

When the design is settled, press **Go**. It asks where to build:

- **Here** — in the checkout ade was launched from, on the session it has been
  designing with.
- **In a new worktree** — checks the repo out again at `../<repo>-<slug>` on a new
  branch, and **ade follows it there**. The Diff tab, Review, Resolve and Commit
  all act on the worktree from then on. The agent starts fresh in it: Claude Code
  keys transcripts by working directory, so the design conversation cannot follow —
  which is what the document is for. Getting back to the original checkout means
  restarting ade there.
 The agent implements what the document
describes, the view switches to **Diff**, and files appear as it writes them. From
there it is the review flow you already had: comments, Review, Resolve, commit, PR.

The design conversation, the build and Resolve all run on **one Claude session**, so
the agent resolving your review comments knows what you designed and why. Review
stays a cold agent with no session, so it cannot rubber-stamp what that session just
wrote.

Documents live in `.scratch` when the repo has one, otherwise beside the thread store
inside the git dir, so they are never committed. `--docs` overrides it.

Rendering is `internal/render` — GFM, Mermaid and syntax highlighting, with its
stylesheet scoped to the document pane so it cannot restyle the diff. It began as
[mdview](https://github.com/owenrumney/mdview)'s `render` package and was absorbed
here so ADE builds from one source; mdview still ships separately, on its own copy,
for previewing and exporting markdown.

## PRs and Review

The **PRs** tab lists the repository's open pull requests, so reviewing someone
else's work is a click rather than a checkout. Pick one and the diff, the comment
threads and the checks all switch to it; **Back to my branch** returns.

The **Review** tab describes whatever is under review — the pull request you picked,
or your own branch's — with its description and checks. It appears only when there
is one to describe.

Someone else's branch is not checked out, so their pull request arrives as a
read-only snapshot from GitHub. Review, comments and drafted replies all work;
**Resolve is refused**, because editing would land on whatever you happen to have
checked out rather than on their code.

Each pull request keeps its own comment store, so its threads do not leak into your
branch's and neither set is orphaned by switching.

It shows:
its description, and its CI checks with failures first and a link straight to each
failing run.

**Resolve with agent** hands the failing checks over. The agent is told to read the
actual run log rather than guess from the check's name, to fix causes in the working
tree without committing, and to say so plainly when a check fails for a reason that
has nothing to do with the branch.

Checks are re-read every 30 seconds, since each pass is a network round trip through
`gh`. **Refresh** does it now.

## What the diff is showing

The diff pane switches between three views:

| | shows |
| --- | --- |
| **Branch** | everything on this branch, committed or not |
| **Committed** | only what is committed |
| **Uncommitted** | only what is still pending commit |

Uncommitted is the one for deciding what goes in the next commit. Comment threads are
kept across a switch: anything anchored outside the narrower view is marked orphaned
and comes back when you switch out again.

`--committed` picks the view to start on.

## Reviewer comments

Once the branch has a pull request, the comments people leave on it are pulled into
the diff as ordinary threads, marked as coming from the pull request. Checks refresh
every 30 seconds; comments every two minutes, or on **Refresh**.

Each one gets two actions:

- **Resolve with agent** — as ever, it changes the code.
- **Draft reply** — the agent writes what you might say back, as a local comment with
  a copy button. It is told to read the code first, to disagree with a reason where
  the reviewer is wrong, and to ask rather than guess.

**ade never posts to GitHub.** A drafted reply is yours to paste or ignore. This is
enforced rather than promised: every `gh` invocation in `internal/ghpr` is checked
against a read-only allowlist before it runs, and a test covers the write shapes —
`pr comment`, `pr review`, `api --method POST` and the rest — so it cannot be added
by accident.

An imported comment is a copy taken at the time. If the reviewer edits or deletes
theirs, the local one stays as it was.

## Review and describe

**Review & describe** does both halves of finishing a change in one run: the cold
reviewer goes over the branch and files its findings as comments, then a second
pass writes a pull request description knowing what it found.

The findings are context, not content — they are already filed as comments, so the
description uses them only to decide what a reader should look at closely. If the
repository has a pull request template, the description fits it.

The result lands in an editable modal with a copy button. **ade does not open or
edit the pull request**; pasting it in is yours.

## Where comments live

`<git-dir>/ade/<branch>.json`.

That is inside the git directory, so there is nothing to gitignore and nothing to
accidentally commit or stage. Worktrees get their own store, because
`--absolute-git-dir` differs per worktree and each one is a different review.

## Comments follow the code

ade re-runs the diff every 1.5s, so edits, commits and branch switches all show
up without a reload. Threads are anchored by content, not line number: each one
remembers a hash of its line plus the neighbouring lines in the same hunk.

When code moves, threads move with it. When the anchored line is rewritten or
deleted, the thread is **orphaned** rather than deleted — it moves to a sidebar
panel with the code it used to point at, so nothing is silently lost.

A thread is not re-anchored to identical content more than 200 lines away, since
that is more likely a coincidental match on a line like `return nil` than a real
move.

## Pull requests

```bash
ade --pr 6
```

Fetches the pull request through `gh` (so it reuses your existing auth) and
imports its inline review comments as threads, replies included. Copilot's
comments and your answers to them arrive as real threads you can Resolve.

Which diff you get depends on whether the PR is checked out:

| Local HEAD | Mode | Diff source | Resolve |
| --- | --- | --- | --- |
| the PR's head commit | `pr` | local git, against the PR's base | available |
| anything else | `pr snapshot` | fetched from GitHub | **blocked** |

With the branch checked out, the diff comes from local git against the PR's base
ref. It matches the PR to begin with, and picks up Resolve's edits as they land.

Without it, the diff is a fetched snapshot and **Resolve is refused** — the agent
edits the working tree, so on the wrong branch it would edit the wrong code. The
header says so and tells you to `gh pr checkout`. The snapshot never changes, so
ade also stops polling rather than re-fetching from the API.

Imported threads are keyed by their GitHub comment id, so restarting ade does
not duplicate them. A comment left against an earlier push is prefixed
`(on an earlier push)` and anchored to where it used to point, or filed as
orphaned if that line has gone.

Pushing comments back up to GitHub is still out of scope.

## Open in ADE, from Firefox

A Firefox add-on that puts an **Open in ADE** button in a GitHub pull request's
header. Pressing it runs `ade --pr N` in the checkout that pull request belongs
to, which is the same thing as typing it — the server comes up and the browser
opens on it.

A page cannot run a command, so the button talks to ade through Firefox's
[native messaging](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/Native_messaging):
a manifest in Firefox's own directory names a host it is allowed to run, and
`ade browser host` is what speaks that protocol on stdin and stdout.

```bash
ade browser install     # write the manifest, and the shim it points at
ade browser uninstall   # take it away again
```

Then install the add-on. Firefox only keeps an add-on across a restart if
Mozilla has signed it, so which of these you want depends on whether you would
rather sign it or turn that off:

```bash
make extension        # lint, and package it unsigned
make extension-sign   # ask Mozilla for a signed one
```

`make extension-sign` uploads to addons.mozilla.org on the **unlisted** channel
and gets a signed `.xpi` back. Nothing is listed, published or reviewed: an
unlisted add-on is not on AMO's site and is not searchable, the upload is only
how Mozilla signs it. It needs an [AMO API
credential](https://addons.mozilla.org/developers/addon/api/key/) in
`WEB_EXT_API_KEY` and `WEB_EXT_API_SECRET`. Install the `.xpi` it writes to
`extension/web-ext-artifacts` by opening it in Firefox, and it stays installed.
AMO refuses a version it has signed before, so bump `version` in
`extension/manifest.json` before signing again.

`make extension` packages the same thing unsigned, which Firefox Developer
Edition, Nightly and ESR will install once `xpinstall.signatures.required` is
`false` in `about:config`. Release Firefox will not, at any setting.

For working on the add-on itself, `about:debugging` → *This Firefox* → *Load
Temporary Add-on* → `extension/manifest.json` still loads it without either,
until Firefox restarts.

Which checkout it opens is worked out from the page's url. ade reads the remotes
of every checkout under its scan directories and takes the one whose `origin`
is that repository — its main checkout, not a worktree, since that is where a
`--pr` checkout gets made. If there is no clone of it, the button says so.

The button goes in the pull request header, next to its number. GitHub's page
is Primer React, whose class names carry a build hash, so the add-on matches on
the `data-component` names and on the stable part of the class instead. If a
GitHub change ever leaves it nowhere to go, it says so once in the page console
rather than just not appearing.

Firefox starts the host from the desktop session, whose `PATH` is the bare
system one — no `gh`, no `claude`, neither of which ade can do without. So the
run it starts gets the `PATH` your login shell would give it, read once and
fenced in the output so that anything your profile prints is not mistaken for
it. A failure says why on the button, and in full in the page console.

The host is deliberately small: it takes an owner, a repository and a number,
and the only thing it can do with them is run ade. It does not take a path, a
branch or a command from the page.

## Review

**Review** runs a *fresh* agent with no session history, so it cannot rubber-stamp
code your session just wrote. Each finding is filed as an agent-authored comment
thread on the line it applies to, which you can then reply to or Resolve.

The reviewer gets the diff **embedded in its prompt**, already annotated with
new-file line numbers, rather than running git itself. That means it reviews
exactly what the browser shows — in `--committed` mode it cannot wander off into
your uncommitted work — and its reported line numbers land on the right lines.

It runs with only `Read`, `Grep` and `Glob`, so it can read surrounding code for
context but cannot modify anything or run a shell.

A finding whose line is not in the diff is nudged onto the nearest diff line
within 12 lines; beyond that it is filed as **orphaned** rather than dropped, so a
real finding is never lost to a bad line number.

### Findings arrive as they are found

The reviewer is asked to emit each finding in its own fenced `ade-finding`
block the moment it confirms one, so threads appear in the diff while the run is
still going instead of all at once at the end.

This is best-effort: `/code-review` finds, then verifies, then reports, so how
much actually streams depends on how the run unfolds. When nothing streams you
get the old behaviour — every finding filed at the end — never fewer findings.

The final JSON block stays authoritative. Anything in it that was not streamed is
filed, and anything streamed that the reviewer dropped from it is withdrawn. If
the final block is missing or unreadable, the streamed findings stand rather than
being thrown away.

Cancelling a run mid-review keeps the findings filed so far.

### Where the findings come from

ade invokes the `/code-review` skill and asks it to end with a fenced JSON
block. It does not rely on the `ReportFindings` tool: that skill reviews inside a
subagent, so the tool call never reaches the parent process — only the final text
does.

### Classification

Every filed finding carries the reviewer's own classification — `security`,
`correctness`, `test-coverage`, `simplification` and whatever else it reaches for.
The word sits on the comment in a colour that says how much it matters: red for
what breaks, amber for what costs, grey for what tidies. A comment nobody
classified — every comment you write yourself — stays the plain accent.

The sidebar lists the classifications in play, with a count each. Turning one off
takes it out of the diff, the file counts, the ‹ › walk and **Resolve all**
together, so what you can see and what the agent is sent are the same set. **Show
all** puts them back. Nothing is deleted by filtering.

### Starting again

**Reset comments** deletes every comment on the branch, so a re-review starts from
nothing rather than reading its own last pass back. What came from a pull request,
or has already been posted to one, stays: neither is ade's to throw away.

## Permissions

The agents run with `--dangerously-skip-permissions`, because there is no terminal
attached to answer a permission prompt on — a gated run would stall rather than ask.
This means an agent can run shell commands unattended in your repository. Pass
`--skip-permissions=false` to gate them instead, accepting that some runs will hang.

Review is unaffected either way: it is restricted to `Read`, `Grep` and `Glob`, and
that allowlist holds regardless of permission mode.

## Resolve

**Resolve** hands your open comments to an agent, which edits the working tree in
place. The agent itself never commits, stages or branches.

Per thread, **Resolve** sends just that comment; **Mark done** closes a thread by
hand without involving the agent. **Resolve all** at the top sends every open
thread in one turn.

The agent reports a verdict per comment, and only `fixed` closes a thread:

| Verdict | Effect |
| --- | --- |
| `fixed` | the note is added as a reply and the thread closes |
| `declined` | the agent disagreed; the note is added and the thread **stays open** |
| `unclear` | the agent wants clarification; the thread **stays open** |

So a disagreement is a real answer you can reply to, then Resolve again. If the
agent reports no verdict block at all, its whole reply is recorded and every
thread is left open — a run that edited files is never silently marked done.

The agent runs with `--permission-mode acceptEdits`, so it writes files
unattended while shell commands stay gated. Only one run happens at a time; a
second request gets a 409.

### Which session

By default the agent resumes **the session this checkout was last in**, so
restarting ade — or opening a recent directory again — lands back in the same
conversation rather than in front of a stranger. The session id is written to
`<git dir>/ade/agent.json` as it happens: it comes off the agent's own event
stream, since a fresh session only names itself once it is running. The
conversation itself is in `<git dir>/ade/chat.db`, keyed by that id, and follows
the id when a resume forks to a new one.

It is keyed by git dir, not by branch. Claude Code keys transcripts by working
directory, so a checkout is in one conversation whatever branch it is on, and a
worktree — having its own git dir — has its own. Moving into a worktree takes on
that worktree's memory, which is a fresh agent for one just created.

A remembered session whose transcript has since been deleted is dropped rather
than resumed, since `--resume` on a missing id fails every run.

`--session <id>` names a specific one, and `--session none` starts fresh — the
way out of a session that has gone bad without deleting a file by hand.

Resuming starts a *separate headless process* replaying that transcript. Beware
resuming a session that is both live and very long: it replays the whole thing.

## Commit message

**Commit message** asks a cold agent to run the `/ctb` skill over your **staged**
changes, then shows the result in an editable modal with a copy button.

**Commit** in the modal commits the message you see. It shells out to plain
`git commit`, so your own git config applies — identity, hooks, and commit
signing included. Verified: a commit made this way carries a `gpgsig` SSH
signature when `commit.gpgsign` is set.

It commits **only the index** — no `-a` — so unstaged edits and untracked files
are left exactly where they are. **Copy message** is still there if you would
rather commit by hand.

Anything not staged is listed at the top of the modal with a warning, so you
cannot commit half a change without seeing it. Tick the files you want — or use
**Select all** — and hit **Stage selected**; that is the only staging ade ever
does, and only from an explicit click.

**Commit** in the topbar is available whenever anything is pending, staged or not,
since the modal is where you stage. With an empty index it opens straight to that
list and waits: **Write message** turns on once something is staged, so no agent
run is spent describing nothing.

**Push** publishes the branch, setting the upstream on a first push. The modal
stays open after a commit so you can push from the same place — it never closes
itself.

- It describes **only what is staged**, so unstaged edits and untracked files are
  ignored. If nothing is staged you get told to `git add` first, before any agent
  run is spent.
- The modal lists the staged files it was based on, so you can check the message
  covers what you think it does.
- Reopening the modal does not re-run the agent. **Regenerate** does that
  explicitly.

The skill's own output is prose, so ade asks it to repeat the finished message
in a fenced `ade-commit` block. Without that block it will only accept a line
that looks like a conventional-commit subject — otherwise you would end up with
the agent's preamble as your commit title. If nothing parses, the modal shows the
raw reply rather than guessing.

## Create PR

**Create PR** asks a cold agent to run the `/pr-create` skill, which writes a
plain-English description from your commits, pushes the branch, and opens the
pull request **as a draft**. ade parses the resulting URL and links it in the
header.

If the repository has a pull request template, **ade reads it and passes it in**
rather than leaving the agent to go and find it, with instructions to keep it
verbatim below the prose and never tick a box on your behalf. After the PR is
opened ade reads the submitted body back, and says so in the agent log if part
of the template did not survive.

The button is disabled when the branch already has a pull request — the Review tab
is where that one lives.

Because this pushes and publishes, it is behind a confirmation dialog. If a PR
already exists for the branch the skill reports that one instead of opening a
second.

### Risk scores

Some pull request templates leave a line for a risk score and expect the author
to write over it. ade looks for that shape:

```
- [ ] TBC: `/review` risk score
```

When it finds one — an unticked box, the word `TBC`, and `risk score` on the same
line — it passes the agent the 1-to-10 scale to use, so a score written here means
the same as one written by `/review` later. **This is the only box ade will ever
tick**, and only because it is the one it filled in. A template without that line
gets no scoring guidance at all, and the placeholder being written over is not
counted as the template having been dropped.

## The team bus

[agentbus](https://github.com/infracost/agentbus) is how agents reach people.
Somebody else's agent stops mid-turn with a question only a human can settle,
and the question is offered to every harness on the team's tailnet. ade is one
of those harnesses.

Each ade joins as its own instance, named for the checkout it serves, and keeps
that name across restarts so a question that arrived while it was down finds it
again. It says what it is doing — `infracost/infra on fix-702`, `an agent is
working`, `waiting on an answer` — so `agentbus who` tells you who is there.

Presence is **active** while you are at the window, **idle** when you are not,
and **dnd** while your own agent is waiting on an answer. `dnd` queues what the
team sends you until it lifts, so it is spent on the one moment a second
question would land badly, not on a long build — a build running is when you
are most free to answer. The editor reports its window focus for this; a
checkout with only the page open falls back to whether a tab is attached.

Two things arrive:

- **a question.** It shows up in the top bar with a count. Opening one gives
  you the whole question, the options their agent offered, and a box for words
  of your own. The first answer wins: if somebody else gets there first, the
  question leaves your list and says so.
- **a review request.** Taking it claims the pull request — the other
  harnesses are told, so two people do not review the same thing — and checks
  it out in a worktree with its own ade on the Review tab. Approve or send it
  back there and the verdict goes back to whoever asked, with your words.

The **Team** view in the editor also lists who is on the bus, under *Online*:
each person's harness, whether they are here, away or answering their own
agent, what it says it is doing, and how many agents are running under it. It
is kept up by the presence the server pushes, so it costs nothing to watch.

In the editor, the requests live under **Team** in the ADE sidebar, with a badge
on the container. A question with a few short options is a quick pick; a long
one opens a panel, because a question you have to guess at from an elided row is
one answered badly. A toast nudges once and then gets out of the way — the row
stays until it is answered.

Answers come the other way too. When something asked from here is answered, the
bus **pushes** it to this ade — nothing polls for it — and it appears in the
same list as a line you click away once you have read it. An answer an agent
gave on its human's behalf says so.

Nothing here is automatic. ade never answers for you, and never posts a review
verdict you did not press a button for.

There is no configuration to do: ade finds the bus through `tailscale status`,
or `$AGENTBUS_URL`. A machine with neither stays off it quietly. In
`~/.ade/settings.json`:

```json
{
  "agentbus": {
    "off": false,
    "repos": ["infracost/*"],
    "no_ask": false,
    "no_review": false
  }
}
```

`repos` limits which repositories' reviews are offered here; `no_ask` and
`no_review` drop one kind without dropping the other. With both set, this ade
only sends.

## Status

All eight phases are done.

| Phase | Scope |
| --- | --- |
| P0 ✅ | side-by-side diff, inline comment threads, anchoring |
| P1 ✅ | Resolve: `claude --resume` edits the working tree from your comments |
| P2 ✅ | Review: a cold agent runs `/code-review` and files findings as threads |
| P3 ✅ | Commit message: runs `/ctb` over the staged diff, shows an editable modal |
| P4 ✅ | `--pr N`: pull a GitHub PR's diff and existing review comments via `gh` |
| P5 ✅ | Streaming review findings, staging and push in the modal, Create PR |
| P6 ✅ | Design tab: write the design, build it, review it, all in one session |
| P7 ✅ | Build in a worktree, and a Review tab for the branch's PR and CI checks |

Nothing is staged, committed, pushed or published without an explicit click, and
ade never amends or rewrites history.

## Switching to other work

**switch…** beside the name in the corner, and `ade launch` from anywhere, open
the session picker: repositories on the left, and for the one you pick, its
sessions across every checkout and worktree of it on the right — read by
`internal/transcripts`, which scans what claude, codex, pi and watchtower each
keep on disk. It came from [sm](https://github.com/owenrumney/sm), which still runs
as its own session-manager TUI. Pick a session and ADE starts, or reuses, the
server for that checkout and hands it the session; a claude session is resumed
where it left off. Each checkout also offers to open with a fresh agent.

A session can be **archived** from either place, which hides it until you ask for
it back. The flag lives in `~/.ade/archived.json`, since sessions themselves are
read fresh from transcripts and have nowhere to keep one.

**tidy up** in the Sessions header is for the ones you are done with. It puts a
tick box on every row; pick them and **delete** takes them out of the list for
good, on this machine, in every ade and in the launcher. There is a ✕ on each row
for deleting just that one, and the session you are looking at cannot be picked.

Deleting is ade forgetting, not ade erasing: the id goes into `~/.ade/hidden.json`
and is never listed again, and **the conversation file the agent wrote is left
exactly where it is**. Nothing in `~/.claude` is touched. So this clears a flooded
launcher; it does not reclaim disk.

### Work with nowhere to put it

Some work has no project yet. **Scratch** in the launcher, and `ade scratch
"what it is about"` from anywhere, make a checkout under `~/.ade/scratch` and
open a session in it: no repository picked, nothing borrowed from one.

It is a real git repository, made to be moved out of later. An empty commit on
`main`, and the session on a branch above it, so the diff has a base and the
work comes out as a range of commits — `git format-patch main`, or a
cherry-pick, into wherever it turns out to belong. They stay on the launcher's
list, badged **scratch**, until you do.

The launcher is a second page of the same Preact build as the app, so the two
share the picker and the look.

Nothing becomes machine-wide. Every server is still one checkout's, recorded in its
own git dir, following its own branch, exiting on its own. This saves you a
terminal, not an architecture.

A transcript outlives the directory it was recorded in, so sessions pointing at a
renamed repo or a removed worktree are left out rather than offered.

## One server per checkout

`ade` in a repository that already has one running prints that server's link and
exits, so you get the same port, the same token and the same agent rather than a
rival on another port.

```
ade              # backgrounds itself and prints the URL
ade              # prints the same URL again and exits
ade --foreground # keeps it in this terminal instead
ade cancel       # cancels the running turn; the server and the conversation stay
ade stop         # shuts it down
```

`ade cancel` is the Stop button for when the editor will not answer. It drops the
queued messages and stops the side runs too. `ade cancel --all` does it for every
session on this machine, and `ade cancel --host <name>` for every session on a
connected machine; both ask the launcher, so it must be running.

An agent is also held back before it gets that far. Its `go` commands run half as
many binaries at once as the machine has cores (`GOFLAGS=-p`, and a `-p` of your own
wins), and a sandbox container gets the same number as `--cpus`. An agent on the host
with more than 32 processes running has its turn cancelled, and the chat says why.

The **door** in the top right corner is `ade stop` without a terminal: it stops the
server serving the page you are on, and offers to delete the session on the way out
so it stops filling the launcher. The checkout, the worktree and everything in them
are left alone; only the server goes. The page notices on its next poll and says no
ade is listening.

The running server is recorded at `<gitDir>/ade/server.json`. **That file contains
the session token**, since the whole point is printing a link that works: it is
written `0600` inside the git dir, which is never committed, and the token dies with
the process. A server killed hard leaves the record behind; the next run notices
nothing is answering and takes over.

A backgrounded server exits after 30 minutes with no browser attached, unless an agent
run is in flight — `--idle 0` keeps it up, `--idle 5m` is impatient.

It also follows the branch: check out something else and the diff, the comment
store, the design document and the pull request all move with you within a couple of
seconds. A worktree gets its own git dir, so it gets its own server; `ade stop`
inside one will not find a server started in the main checkout.

## Finishing, and finding things

```
ade done         # in a worktree: stop its server and remove the checkout
ade prune        # remove every finished worktree of this repo (-n to look first)
ade ps           # servers running for this repo and its worktrees
ade doc ls       # design documents, * marks the one for this branch
ade doc rm <name>
```

A new session can start **in a new worktree** (⌘N → *In a new worktree*), so a
second piece of work runs beside the first with its own agent rather than under
it. Worktrees pile up that way, so `prune` — and ⌘P → *Prune worktrees* on the
page — removes the finished ones: kept are any a ade is serving, any with
uncommitted files, and any whose branch has commits no other branch holds.
`--force` takes the last kind too; the branch keeps the commits. Uncommitted
work is never removed by prune, and branches are never deleted.

`done` keeps the branch and the design document — the branch is your local record of
the work, the design is why the change looks the way it does. It refuses while
anything is uncommitted or unpushed, since that work exists nowhere else; `--force`
throws it away anyway.

## Supported setups

What is **tested**, on every pull request, is Linux. What is **used daily** is Linux and
macOS. Everything else is expectation rather than evidence, and an issue saying
otherwise is welcome.

| | State |
| --- | --- |
| Linux | tested in CI, developed on |
| macOS (Apple silicon) | used daily, not in CI |
| macOS (Intel) | built for, not run |
| Windows | **not attempted.** The sandbox, the browser host's `PATH` handling and the Keychain branch all assume macOS or Linux |
| Docker | the sandbox runtime on Linux |
| Apple's `container` | preferred on macOS when its services are up; Docker otherwise |
| Claude Code | the agent ade was built around |
| Codex | supported, less exercised |
| Node and npm | **required to build.** `web/` is a Vite app embedded in the binary |
| `gh` | required for anything touching a pull request |
| `git` | required, and recent enough for worktrees and `--absolute-git-dir` |

Running without a sandbox (`--sandbox=false`) needs no container runtime at all.

## What ade does with your Claude login

ade holds no credential of its own and never asks you for an API key. It reads
the login Claude Code already made, from wherever that CLI keeps it:

| Host | Where the login is read from |
| --- | --- |
| macOS | Keychain, service `Claude Code-credentials`; `~/.claude/.credentials.json` if that fails |
| Everything else | `~/.claude/.credentials.json` |

The Keychain is tried first on a Mac only because the CLI refreshes the token
there, so the file beside it goes stale. Linux is a first-class path, not a
fallback — ade is developed on it.

What is read is an **OAuth access token, never an API key**: ade runs on your
Claude subscription, and an API key would be a different bill. A sandboxed
session does not get your whole `~/.claude` — ade keeps its own copy of the login
at `~/.ade/claude/.credentials.json`, `0600`, and mounts that one file into the
container. Nothing is sent anywhere except to Anthropic, by the agent, exactly as
the CLI would.

**The `/usage` figures are unofficial.** The rate-limit numbers in the corner come
from the same undocumented endpoint Claude Code itself asks. It may change or
disappear without notice; ade reads the shapes defensively and shows nothing
rather than failing. If you hit a parse error, that is why — it was expected.

## Security

ade runs an agent with file-system and shell access on your machine, and serves
a page that drives it. The trust boundary is two things together: **a listener
bound to `127.0.0.1`**, and **the guard token in the URL**. Neither alone is
enough, and there is no flag that binds anything wider.

- **What ade trusts:** the local user. Anyone who can already run processes as
  you can read the token out of the git dir, so a local attacker with your uid
  has already won.
- **What ade does not trust:** anything off loopback. `sameHost` rejects a
  request that did not address the server by a name it bound, which is the
  DNS-rebinding guard; the terminal websocket checks `Origin`; the token handout
  refuses anything not on the loopback listener; attachments cannot execute as a
  page on ade's origin.
- **What the sandbox does and does not contain:** it keeps the agent's writes to
  the mounted checkout and its shell to the container. It does **not** isolate
  the network — the agent has to reach the model, so it can reach everything
  else — and it does not make the checkout read-only, because editing it is the
  point.
- **Docker from the sandbox:** Docker-backed sandboxes resolve the active local
  Docker context, including OrbStack and Docker Desktop, mount that daemon's
  Unix socket at `/var/run/docker.sock`, and include the Docker CLI with Compose.
  Agents may create and remove
  only the containers, networks and volumes they create for local development,
  such as temporary Postgres databases, seeded UI fixtures and screenshot
  services. This is an operational boundary, not daemon-enforced isolation: a
  process with the socket can control every resource in that Docker daemon and
  should be trusted like a host process. ADE never runs Docker-in-Docker.

Text other people wrote — review comments, issue bodies, old transcripts — is
fenced before the agent sees it, and there are tests that pin that. Prompt
injection is defended, not solved.

[SECURITY.md](SECURITY.md) has the detail, what is in scope, and how to report
something privately.

## Building

```bash
make install     # npm ci && npm run build in web/, then go install ./cmd/ade
make test        # go test ./... and the frontend's own tests
```

Everything ADE needs is in this repository. It used to require agentbridge, mdview
and sm checked out as siblings, with `replace` directives pointing at `../` — which
meant it only built on a machine laid out exactly like the one it was written on.
Those packages now live under `internal/`, and a test in `internal/web` fails if a
`replace` or a relative path ever reappears in `go.mod`.

The browser page is a Vite + Preact + TypeScript app under `web/`, built into
`internal/web/dist` and embedded in the binary. That directory is not committed,
so `go build ./cmd/ade` on a fresh checkout fails on the embed until the frontend
has been built once; `make build` does both in order. For the same reason
`go install github.com/owenrumney/ade/cmd/ade@latest` does not work — install
from a checkout.

Needs Node and npm for the frontend. Nothing else does.

### Dictation

The mic in the chat runs a local `whisper-cli` over the recording. Both
pieces are fetched once by hand; ade never downloads them.

```bash
brew install whisper.cpp
mkdir -p ~/Library/Caches/ade/whisper && curl -L \
  -o ~/Library/Caches/ade/whisper/ggml-base.en.bin \
  https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin
```

On Linux the model lives under `~/.cache/ade/whisper` instead.
`ADE_WHISPER_MODEL` points at a different model file. With either piece
missing, the mic's error says which.

### Working on ade itself

```bash
make dev
```

`ade --dev` starts the Go server on a random port, starts Vite on another, and
prints Vite's URL with the session token on it. Vite proxies `/api`,
`/design-assets` and the mermaid bundle back to Go and serves everything else
itself, so a frontend change is a save with hot reload. A Go change is a save
too: ade rebuilds, replaces itself on the same port, and the page you have open
reconnects — Vite is carried across the restart rather than restarted with it.

## Contributing

Patches welcome. [CONTRIBUTING.md](CONTRIBUTING.md) covers getting set up, the
layout, and the house rules that fail the build — the comment cap and the style
tokens surprise people, so they are their own section. There is a
[code of conduct](CODE_OF_CONDUCT.md).

[docs/RELEASING.md](docs/RELEASING.md) says what ships today and what is still
undecided.

## Licence

[Apache-2.0](LICENSE). Your contribution arrives under the same terms by §5 of
the licence, so there is no separate CLA. There are no per-file SPDX headers and
that is deliberate — see CONTRIBUTING.
