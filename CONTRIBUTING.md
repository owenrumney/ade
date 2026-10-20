# Contributing to ade

> **Working on ade with an agent?** Point it at [`CLAUDE.md`](CLAUDE.md) as well.
> The two overlap on purpose — no rule should live in only one of them.

## Getting set up

| You need | Why |
| --- | --- |
| Go 1.26.9 or later | the binary. `go.mod` names the version |
| Node and npm | `web/` and `vscode/` are npm projects |
| `git` | ade drives it constantly |
| `gh` | pull requests, checks, review comments |
| Docker, or Apple's `container` | the agent sandbox. Optional; `--sandbox=false` skips it |
| An agent CLI on `PATH` | `claude` or `codex`. ade starts one; it does not bundle one |

```bash
make build     # npm install && npm run build in web/, then go build into ./bin
make install   # the same, into your GOPATH
make quick-start  # sandbox and worker images, the binary, then the VS Code extension
```

The browser page is a Vite + Preact + TypeScript app under `web/`, built into
`internal/web/dist` and embedded in the binary with `go:embed`. **That directory
is not committed**, so a bare `go build ./cmd/ade` on a fresh checkout fails on
the embed until the frontend has been built once. `make build` does both in
order. For the same reason `go install github.com/owenrumney/ade/cmd/ade@latest`
does not work — install from a checkout.

The extension uses proposed VS Code APIs: the Comments pane's thread menu, the
Draft state on a comment, and a button in the commit message box. A stock VS
Code grants them only to an extension named in `~/.vscode/argv.json`. Run
**Preferences: Configure Runtime Arguments**, add this line, and restart:

```json
"enable-proposed-api": ["owenrumney.ade"]
```

Without it the extension works, and those three are missing. `vsce package`
accepts the proposals; only `vsce publish` refuses them.

A choice or a prompt the extension puts to you opens in ade's omnibox, the
large quick input of ade's own editor. The extension gets there through
`vscode/src/pick.ts`, whose `pick`, `input`, `quickPick` and `inputBox` ask the
editor for the big box before they show, and are the stock calls on a stock VS
Code. Use them rather than `vscode.window.showQuickPick` and its three
siblings: `pick.test.ts` fails on a stock one, because a follow-up in the small
widget breaks the one-box feel the omnibox is for.

On first run ade writes `~/.ade/settings.json` with `~/code` as the one directory
it scans for checkouts, and creates `~/.ade` for its own state. Nothing is
written into your repositories: comment threads, design documents and the server
record all live inside the git dir.

## Running it

```bash
make dev     # ade --dev: Go on one port, Vite on another, hot reload
make test    # go test ./... and the frontend's own tests
make lint    # golangci-lint run
make fmt     # gofmt -s -w .
```

Tests and builds are noisy. Trim the output:

```bash
go test ./... 2>&1 | tail -20
npx vitest run --reporter=dot
npm run build 2>&1 | tail -5
```

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

`make dev` runs with `--sandbox=false`. The agent is editing ade's own checkout,
which the container would mount read-write anyway, so the boundary buys nothing
and costs a round trip per turn.

Inside the sandbox the agent has your `~/.claude` agents, commands, skills and
enabled plugins, read-only, and Codex has the same skills linked into its own
home. A skill you install is there whichever agent runs, and wherever it runs.

**On macOS, run `make sign-setup` once.** Every other build is signed ad-hoc,
and an ad-hoc signature is a new identity to macOS each time it changes. TCC
keys permissions to that identity, so a rebuilt ade silently loses the ones it
was granted — first and worst, Local Network. Without it, ssh to a machine on
your LAN fails with `No route to host` while the toggle in System Settings still
reads on, and the tailnet and the router both keep working, which makes it look
like anything but a permission.

`make sign-setup` puts a self-signed code-signing certificate in your login
keychain. No admin password and no trust settings: codesign signs with an
untrusted certificate quite happily, and macOS keys a permission to the
signature, not to whether a root vouches for it. It does ask for your login
keychain password, which is what lets it give codesign standing access to the
new key; without that the keychain stops every build to ask. Answering nothing
is fine — the first dialog's **Always Allow** settles it just as well. Rerun
`make sign-setup` any time to set it for a certificate you already have. After it,
`make install` and `make ui` sign what they build under that identity, and a
grant survives a rebuild. Grant ade Local Network once in System Settings →
Privacy & Security and it stays.

**Which row that is depends on who started ade.** macOS judges a local network
connection by the process it holds *responsible*, not the one that dials. Start
ade from a terminal and that is the terminal, or ade under its own name. Let
the VS Code extension start it and that is the editor: ade has no row of its
own there and never will. The editor usually holds the grant already, because
Remote-SSH and port forwarding ask for it.

The binary signs as `uk.co.owenrumney.ade-cli`. It may not be renamed: the
permission is keyed to the exact string, so a rename throws it away and leaves
the user toggling a row that names nothing. `TestTheCliClaimsItsOwnIdentifier`
pins it.

**When ssh to a LAN host fails, measure in this order.** Do not skip step 1.

1. From a terminal outside ade, with an Apple-signed tool so nothing new gets
   judged: `nc -vz -G 3 <lan host> 22`. Not a binary you just built — an
   unsigned one gets its own Settings row and its own refusal. If `nc` fails,
   the Mac cannot reach the host and nothing here is the cause.
2. The same dial from a terminal inside the editor, or **ade: Check the
   network** from the ade menu, which dials every bookmarked machine from the
   launcher itself. This is the verdict.
3. Only if step 2 says `No route to host`: turn on the *editor's* row in System
   Settings → Privacy & Security → Local Network.

Only `No route to host` is a refusal. `ifconfig` showing every MAC as
`02:00:00:00:00:00` and an empty `arp -a` are not: both were measured in a
process tree whose dials to the LAN worked.

Skipping it costs nothing until you use a remote host: the signing steps print
a reminder and carry on.

## Layout

| | |
| --- | --- |
| `cmd/ade` | the CLI: flags, subcommands, and starting the server |
| `internal/` | everything else. 58 packages; see below for the large one |
| `web/` | the browser page — Vite, Preact, TypeScript |
| `vscode/` | the VS Code extension: native trees, the chat, and frames around the pages |
| `extension/` | the Firefox add-on that puts *Open in ADE* on a GitHub PR |
| `sandbox/` | the Dockerfile for the container the agent runs in |
| `tools/` | screenshots, icons, and other build-time scripts |
| `.ade/jobs/` | scheduled-job definitions ade itself reads. `design-drift.md` is one |

`internal/web/web.go` is the large one — about 1,200 lines. Locate before you
read: `grep -n 'thing' internal/web/web.go`, then `sed -n '400,440p'`. Read it
whole only if you are rewriting it whole.

### `internal/agentbus` is a mirror, not a dependency

ade joins [agentbus](https://github.com/infracost/agentbus) by speaking its wire
protocol directly, over `github.com/coder/websocket`. There is a published Go
client, and it is the natural thing to reach for — but the module is private.
Adding it to `go.mod` breaks `go build` for every contributor outside Infracost,
including CI. The guide allows for speaking the protocol instead, and that is
what `internal/agentbus` does.

So: do not replace it with `github.com/infracost/agentbus/client`. If a frame
changes, change the mirror. If ade needs a frame it does not have, copy that one
across; the package is not a fork and does not track what ade never sends.

## The rules that fail the build

These surprise people, so they are their own section.

### Comments

- **Two lines maximum** for any one comment.
- **At most 12%** of the lines a change adds.

Enforced by `internal/bridge` — `TestNoCommentInTheWorkingTreeRunsPastTheCap` and
`TestCommentsAreASmallPartOfWhatTheWorkingTreeAdds` — against the **working
tree**, so a change that argues its case in the margins fails the build before it
reaches CI. **CI cannot check this**: the checkout there is clean, so `git diff
HEAD` is empty and both tests pass vacuously. Run `make test` locally, or the cap
is enforced in review instead.

The repository averages 13%, and predates the rule. Do not read that as the
standard. Reasoning belongs in a design document; the code gets the one line that
says what cannot be read off the line below it.

**No per-file SPDX headers.** Apache-2.0's appendix suggests them. They are noise
under a comment cap, and the two rules would fight. Their absence is deliberate,
not an oversight.

### Style

There is no stylesheet of ade's own any more. `internal/ui/tokens.css` went with
the pages it dressed when the editor became the only front end; the design is
`.git/ade/docs/shell-and-web-removal.md`.

What the editor draws wears the theme's own variables, in `vscode/media/chat.css`.
Take a colour from `--vscode-*` rather than naming one: a hard-coded colour is
right in one theme and wrong in every other.

### Running the tests

**One package at a time, in the foreground.** `internal/web` and
`internal/launcher` both reach a real `claude -p` when a runner is not injected,
and `go test` runs packages in parallel — `go test ./...` unattended once put 111
live agents on a laptop and it had to be power-cycled.

`internal/agentshim` puts a refusing `claude` and `codex` on PATH for those two
suites, so an unstubbed path now fails loudly instead of forking. That is a floor,
not a licence: `internal/web` still takes two minutes, so run it deliberately.

## Screenshots

```bash
make shots VIEWS=launcher-worktrees
```

Run it after any change under `vscode/src`. It drives a headless chromium over a
fixture home and writes PNGs. A picture of the change is worth more than a
paragraph claiming it works.

**Name the views.** A reply carries four pictures and they are picked in name
order, so a bare `make shots` always sends the four alphabetically first and
never the one your change is about. The views are listed in
`tools/shots/shots.mjs`.

**A change under `vscode/src` is looked at the same way.** The `code-*` views
photograph the extension inside the real VS Code workbench:

```bash
make shots VIEWS="code-sidebar code-memory"
```

It needs `code` on PATH. The first run downloads the VS Code server and
accepts its licence terms on your behalf, then keeps it under
`~/Library/Caches/ade/code-cli` (or `~/.cache/ade/code-cli`). Everything runs
under the fixture home, so it never touches your own sessions.

## Commits and pull requests

- **Conventional commits**, matching the existing log: `feat(launcher): …`,
  `fix(web): …`, `chore(deps): …`.
- **One logical change per pull request.** Mechanical renames go in their own,
  so review is a single read.
- Say how you verified it. For a `vscode/src` change, that means screenshots.

The pull request template asks for exactly this.

## What needs a maintainer

Three things a contributor cannot do, and should not need to:

- **Signing the Firefox extension.** `make extension-sign` needs
  `WEB_EXT_API_KEY` and `WEB_EXT_API_SECRET`. Verify an extension change with
  `make extension` — lint plus an unsigned build — and leave signing to a
  release.
- **Releases.** Tagging, and whatever signing a platform demands. See
  [docs/RELEASING.md](docs/RELEASING.md).
- **The sandbox image.** `.github/workflows/sandbox-image.yml` publishes it to
  GHCR on pushes to `main`. `make sandbox-image` builds the same Dockerfile
  locally.

## Security

Do not open a public issue for a vulnerability. [SECURITY.md](SECURITY.md) says
where to send one, what is in scope, and what the trust boundary actually is.

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md), version
2.1, adopted as-is. Report a problem to owen@owenrumney.co.uk.

## Licence

Apache-2.0. Your contribution arrives under the same terms, by §5 of the
licence — there is no separate CLA to sign.

## The editor

ade ships as its own editor, built from vscode with VSCodium's scripts and
ade's own patches under `editor/patches`. `make editor-fetch` pulls the
sources once per machine, `make editor-prepare` applies the patches and
installs (about ten minutes), `make editor-dev` runs it from source, and `make
editor-package` builds `ADE.app`. The build tree is shared by every worktree,
beside the main checkout, and holds one branch's patches at a time: a build
refuses and asks for `make editor-prepare` when the patches in it are not
yours. The design doc, `.git/ade/docs/editor-fork.md`, says why and
what each patch is for. The extension under `vscode/` is built in, so a change
there reaches the editor through `make editor-dev`, and the installed `ADE.app`
through `make editor-package editor-install`.

The monthly move to VSCodium's current pin is the job `.ade/jobs/editor-bump.md`,
not a make target: a rejected patch takes judgement. Run it from the worktree
it should branch from, with `ade jobs run editor-bump` there or "Run it now" in
the editor's job pick. It makes a worktree and a session beside that checkout
and leaves the branch for you to look at; the report says where to look.

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
