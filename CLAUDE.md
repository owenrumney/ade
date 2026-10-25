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

**Never run `make install`, `make ui` or `make full` from a session.** Each
depends on `killall`, which quits ADE.app and kills every `ade` — including
the launcher and the server running the session you are in. Install the same
binary without that: `go install -ldflags "-X main.version=$(git describe
--tags --always --dirty)" ./cmd/ade && ./tools/sign.sh "$(go env GOPATH)/bin/ade"`.
Replacing the file does not disturb the running process; the next start picks
it up.

**`make install` is the Go binary and nothing else.** Not the pages. Change
anything under `vscode/src` or `app/renderer` and telling someone to `make
install` ships them none of it — the symptom is a backend that plainly works
and a UI that cannot draw it.

**Building is not installing.** `make app-build` only writes `app/dist`;
`/Applications/ADE.app` runs the copy inside it, which `make app-install`
replaces. The extension's own bundles are `make ui`, and a Remote-SSH host
takes them through `make vscode-remote HOST=<host>`.

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

`make app-shots` is the same for the app's window, and it will not send one
you did not name: `make app-shots VIEWS="app-chat app-code"`. Named views are
the only ones shot and the only ones copied to the outbox; a bare run writes
every view to `app/dist/shots` and sends none. `make app-shots VIEWS=nope`
lists the names.

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

## The front end

ade's own window is the front end: `app/`, an Electron shell over a renderer
that talks to the launcher and the session servers over HTTP. `make app` runs
it from source, `make app-install` puts it in /Applications. The VSCodium fork
is gone; `.git/ade/docs/editor-removal.md` is the plan it went by.

The extension under `vscode/` still builds for a stock VS Code, but the
proposed APIs the fork granted it are not granted there, so some menus do not
draw. The pages it holds are what the window loads, which is why the directory
is still here — see stage 3 of the same document.

- Anything the extension puts to the person goes through `vscode/src/pick.ts`:
  `pick()`, `input()`, `quickPick()` or `inputBox()`, never the stock four,
  and `pick.test.ts` fails the build on a stock one.
