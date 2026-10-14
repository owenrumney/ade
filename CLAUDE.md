# ade

Notes for an agent working in this repository. A human contributor wants
[CONTRIBUTING.md](CONTRIBUTING.md), which carries the same rules with the
reasoning a newcomer needs; neither file should be the only place a rule lives.

## Reading code

`internal/web/web.go` (1.2k lines) is the big one. Never read it whole to make a
small edit.

- Locate first: `grep -n 'thing' file`
- Read the slice: `sed -n '400,440p' file`
- Whole-file reads only when rewriting the whole file.

## Running things

Tests and builds are noisy. Trim the output:

- `go test ./... 2>&1 | tail -20`
- `npx vitest run --reporter=dot`
- `npm run build 2>&1 | tail -5`

On a Mac, `go test` links each test binary into `GOTMPDIR` and runs it from
there. Out of the default `/var/folders` Gatekeeper kills it — `signal: killed`,
and an `"x.test" Not Opened` dialog in the user's face. `internal/gotmp` points
`GOTMPDIR` at `~/Library/Caches/ade/gobuild` for every process ade starts, agent
and terminal alike, so this should not come up.

If it does anyway, the mitigation missed and the binary is being refused rather
than misplaced: say so, leave the point unverified, and do not work around it.
Never `codesign` a test binary and never build one into `/tmp` to dodge this.

**A toolchain error is usually one command, not a wall.** `go` here is a mise
shim, so `go.mod` can require a newer Go than the shim selects, and
`GOTOOLCHAIN=local` stops Go fetching its own:

```
go: go.mod requires go >= 1.26.9 (running go 1.26.6; GOTOOLCHAIN=local)
```

`mise use -g go@1.26.9` fixes it, usually without a download. It surfaces
anywhere that shells out to Go — `make shots` dies on its `seedchat` step with
the same message and no picture. Check `which go` before editing `go.mod` or
calling the check unverifiable.

**Never `go test ./...` unattended, and `internal/web` only deliberately.** Both
that package and `internal/launcher` reach a real `claude -p` when a runner is
not injected, and `go test` runs packages in parallel. `internal/agentshim` puts
a refusal on PATH for those two suites, which is a floor and not a licence: one
package at a time, in the foreground. `internal/web` takes two minutes.

On macOS a build is signed by `tools/sign.sh` from `make install` and `make ui`,
under a certificate `make sign-setup` creates once. Ad-hoc
signatures make every rebuild a new identity, so a permission granted to one
build does not survive the next.

**`make install` is the Go binary and nothing else.** Not the editor extension.
Change anything under `vscode/src` and telling someone to `make install` ships
them none of it — the symptom is a backend that plainly works and a UI that
cannot draw it. `make ui` is the one that builds both.

**Building the extension is not installing it.** `make ui` only writes
`vscode/dist`; ADE.app runs the copy built into it. A change under `vscode/src`
reaches it through `make editor-package && make editor-install`, or `make
editor-dev` from source. A Remote-SSH window runs the extension on the remote
host: `make vscode-remote HOST=<host>`.

**Local Network is granted to the editor, not the binary.** macOS judges a
connection by the process it holds responsible, which for an `ade` the
extension starts is `Code.app`, and for one a terminal starts is the terminal.
ade has no Settings row of its own in the editor's tree and never will, and
nothing this repository ships can ask on the editor's behalf: a dial from the
Go binary is judged exactly as the ssh it stands in for.

The symptom is ssh to a LAN host failing with `No route to host` from the
Machines tree while Tailscale, the router and the internet all work. Measure
it in this order, and do not skip step 1:

1. From a terminal outside ade, with an Apple-signed tool so nothing new gets
   judged: `nc -vz -G 3 <lan host> 22`. Not a binary you just built: an
   unsigned one gets its own Settings row and its own refusal. That mistake
   cost this repository an evening. If `nc` fails, the Mac cannot reach the
   host and nothing here will change it.
2. The same dial from inside the editor-spawned tree, or `ade.diagnose`
   (**ade: Check the network**), which dials every bookmarked machine from the
   launcher. This is the verdict.
3. Only if step 2 says `No route to host`: the *editor's* row under Settings →
   Privacy & Security → Local Network.

**Only `No route to host` is a refusal.** Do not read `ifconfig`, `arp` or
`networksetup` as any part of this. A masked `02:00:00:00:00:00` MAC and an
empty `arp -a` were both measured in a tree whose LAN dials worked.

The binary signs as `uk.co.owenrumney.ade-cli` and may not be renamed: the
grant is keyed to the exact string, so a rename starts from no permission.
`TestTheCliClaimsItsOwnIdentifier` pins it.

Two more traps. `security find-identity -v` lists only certificates chaining to
a trusted root, so a self-signed one never appears and `0 valid identities
found` does not mean `make sign-setup` was skipped: read the signature off the
binary with `codesign -dv`. And never send the user to Settings twice. If they
say the row is on, it is on, and the answer is elsewhere.

Ruled out by measurement, so do not raise them again without new evidence: a
network filter such as Little Snitch, because the internet and gateway keep
working and a filter would take those too; routing, for the same reason;
embedding an `NSLocalNetworkUsageDescription` plist in the cli, which changes
nothing; duplicate Settings rows; and a relaunch to pick up a fresh grant.

## Screenshots

`make shots` is how a UI change gets looked at rather than described. It drives a
headless chromium over a fixture home and writes PNGs to `.git/ade/outbox/`,
which ade attaches to the reply. The views it captures are listed in
`tools/shots/shots.mjs`.

Run it after any change under `vscode/src`. A picture of the change is worth
more than a paragraph claiming it works.

The outbox itself is not specific to this repository: `$ADE_OUTBOX` names it in
every session, and the system prompt tells every editing agent to use it.

Name the views: `make shots VIEWS=launcher-worktrees`. A reply carries four
pictures, and the outbox picks them in name order, so a bare `make shots` always
sends the four alphabetically first and never the one the change is about.

A reprovisioned container has neither chromium nor `tools/shots/node_modules`;
the script installs both. Every build and install step reports its own failure,
so when a shot does not appear, the reason is in the output.

The VS Code extension's chat is shot too — `editor-chat` and `editor-chat-wide`.
`tools/shots/editor.mjs` rebuilds that webview over a page already on ade's
origin, which is what makes its fetches work: the server only sends CORS headers
to a `vscode-webview://` origin, so anything else is blocked.

The editor's own UI is shot as the `code-*` views: `code-sidebar`,
`code-memory`, `code-drift` and so on. `tools/shots.sh` serves the real
workbench with `code serve-web`, installs the extension into it, and the same
chromium drives it, so a tree or a row is photographed and not redrawn. Run
one after any change under `vscode/src` that draws something.

It runs under the fixture home on purpose. The extension starts `ade` in the
folder it opens and talks to whichever launcher its home names, so the same
thing under the real home starts a real session. The first run downloads the
VS Code server and accepts its licence terms; `code` has to be on PATH.

`internal/web/preview.go` is a different feature — the post-turn preview capture
of a local dev server. It is off under a sandbox by design. It is not this.

## The prompts that are not here

`internal/roles/defaults` and `internal/agents/defaults` hold a `README` and no
`.md`. The prompts name people, an employer and internal conventions, so they
live in the private `ade-private` repository beside this checkout. `make
private` copies them in, and `.gitignore` stops them coming back.

Without them ade builds and every test passes: `install` installs nothing, and
the machine's own `~/.ade/roles` and `~/.claude/agents` are read as always.
`TestTheShippedAgentsParseAndOnlyTheWritersWrite` skips itself, so the check
that the shipped agents declare the right tools only runs once you have copied
them in. Edit a prompt in `ade-private`, not in the working copy here: the copy
is gitignored and the next `make private` overwrites it.

## agentbus

`internal/agentbus` speaks the agentbus wire protocol by hand, over
`github.com/coder/websocket`, which ade already has. There **is** a Go client
for it, `github.com/infracost/agentbus/client`, and it is private: putting it in
`go.mod` breaks the build for anyone without access, CI included. The harness
guide allows speaking the protocol instead.

Do not swap the mirror for the real client. A frame that changes is changed
here; a frame ade does not send is not copied here.

The design doc is `.git/ade/docs/agentbus-harness.md`, including why a review
claimed in one checkout cannot report its verdict from the worktree ade opens
for it.

## Comments

Two lines is the hard limit for any one comment, and comments may be at most
12% of the lines a change adds. Both are enforced by `internal/bridge`
(`TestNoCommentInTheWorkingTreeRunsPastTheCap`,
`TestCommentsAreASmallPartOfWhatTheWorkingTreeAdds`) against the working tree,
so a change that argues its case in the margins fails the build.

The repository averages 13% and predates the rule. Do not take it as the
standard. Reasoning goes in the design doc under `.git/ade/docs/`; the code
gets the one line that says what could not be read off the line below it.

## Style

There is no stylesheet of ade's own any more. `internal/ui/tokens.css` went with
the pages it dressed; what the editor draws wears `--vscode-*` variables, in
`vscode/media/chat.css`. Take a colour from the theme rather than naming one.

## The editor

ade is its own editor: VSCodium's scripts over a pinned vscode, ade's patches
in `editor/patches`, the extension built in. The plan and the reasoning are in
`.git/ade/docs/editor-fork.md`; the ignored build tree is `editor/vscodium`.

- `make editor-fetch` once; `make editor-prepare` after a patch or pin change
  (ten minutes, it is `npm ci`); `make editor-dev` runs from source,
  `COMPILE=1` after a source change; `make editor-package` builds `ADE.app`.
- The vscode tree holds one `ADE BASELINE` commit. Edit source, then
  `git diff > editor/patches/NN-name.patch`. Patches apply in name order after
  VSCodium's, so a later one is diffed against the earlier ones applied.
- The scripts are bash. Run them, do not source them in zsh: an empty glob
  aborts the run halfway through the patches.
- Never launch the dev editor with HOME pointed elsewhere; macOS raises a
  keychain dialog on the user's screen. Isolate with `--user-data-dir`.
- Pictures: `CDP=9333 tools/editor/dev.sh <folder>` opens the debugging port
  and `tools/editor/drive.mjs` sends keys, clicks and captures the window.
  The terminal is not allowed to send keystrokes.
- A proposed API the extension uses goes in `enabledApiProposals` and in
  `editor/product.json`; the editor grants it from there, and `menu.test.ts`
  checks the two agree. Stock VS Code needs `argv.json` instead.
- Anything put to the person goes through `vscode/src/pick.ts`: `pick()`,
  `input()`, `quickPick()` or `inputBox()`, never the stock four. The
  omnibox is the picker for everything, and `pick.test.ts` fails the build
  on a stock one.
- The monthly bump to VSCodium's pin is the job `.ade/jobs/editor-bump.md`,
  run by hand from the worktree it should branch from: `ade jobs run
  editor-bump` there, or "Run it now" from the editor's job pick. It makes
  a worktree and a session and leaves the branch for you to look at.
