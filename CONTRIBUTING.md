# Contributing to ade

> **Working on ade with an agent?** Point it at [`CLAUDE.md`](CLAUDE.md) as well.
> The two overlap on purpose — no rule should live in only one of them.

## Getting set up

| You need | Why |
| --- | --- |
| Go 1.26.9 or later | the binary. `go.mod` names the version |
| Node and npm | `web/` and `app/` are npm projects |
| `git` | ade drives it constantly |
| `gh` | pull requests, checks, review comments |
| An agent CLI on `PATH` | `claude` or `codex`. ade starts one; it does not bundle one |

```bash
make build     # npm install && npm run build in web/, then go build into ./bin
make install   # the same, into your GOPATH
make quick-start  # the binary, then the window
```

The browser page is a Vite + Preact + TypeScript app under `web/`, built into
`internal/web/dist` and embedded in the binary with `go:embed`. **That directory
is not committed**, so a bare `go build ./cmd/ade` on a fresh checkout fails on
the embed until the frontend has been built once. `make build` does both in
order. For the same reason `go install github.com/owenrumney/ade/cmd/ade@latest`
does not work — install from a checkout.

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
anywhere that shells out to Go. Check `which go` before editing `go.mod` or
calling the check unverifiable.

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
`make install` signs what it builds under that identity, and a
grant survives a rebuild. Grant ade Local Network once in System Settings →
Privacy & Security and it stays.

**Which row that is depends on who started ade.** macOS judges a local network
connection by the process it holds *responsible*, not the one that dials. Start
ade from a terminal and that is the terminal, or ade under its own name. Let
the window start it and that is ADE.app.

The binary signs as `uk.co.owenrumney.ade-cli`. It may not be renamed: the
permission is keyed to the exact string, so a rename throws it away and leaves
the user toggling a row that names nothing. `TestTheCliClaimsItsOwnIdentifier`
pins it.

**When a dial to a LAN host fails, measure in this order.** Do not skip step 1.

1. From a terminal outside ade, with an Apple-signed tool so nothing new gets
   judged: `nc -vz -G 3 <lan host> 22`. Not a binary you just built — an
   unsigned one gets its own Settings row and its own refusal. If `nc` fails,
   the Mac cannot reach the host and nothing here is the cause.
2. The same dial from a Shell tab inside the window. This is the verdict.
3. Only if step 2 says `No route to host`: turn on the *window's* row in System
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
| `app/` | ade's own window: the renderer and the pages it frames, served by `cmd/adewindow` |
| `extension/` | the Firefox add-on that puts *Open in ADE* on a GitHub PR |
| `tools/` | screenshots, icons, and other build-time scripts |

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

Enforced by `internal/ratchet` — `TestNoCommentInTheWorkingTreeRunsPastTheCap` and
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
the pages it dressed; the design is `.git/ade/docs/shell-and-web-removal.md`.

The window's own chrome is `app/renderer/*.css`, and its palette is
`theme.css`: `--ink`, `--bg`, `--line`, `--accent` and the rest. A page under
`app/pages` is an iframe and cannot reach that stylesheet, so
`app/renderer/paneltheme.ts` carries the same names and puts them on the
panel's own document. Take a colour from the theme rather than naming one,
because a hard-coded colour is right in one theme and wrong in every other.
`panel.test.ts` fails a stylesheet that reads a name neither file sets.

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
make app-shots VIEWS="app-session app-diff"
```

Run it after any change under `app/`. It drives a headless chromium over a
fixture home and writes PNGs. A picture of the change is worth more than a
paragraph claiming it works.

**Name the views.** A bare run writes every view to `app/dist/shots` and sends
none; only named views reach the outbox, and a reply carries four. The views
are listed in `tools/appshots.mjs`, and `VIEWS=nope` prints them.

## Commits and pull requests

- **Conventional commits**, matching the existing log: `feat(launcher): …`,
  `fix(web): …`, `chore(deps): …`.
- **One logical change per pull request.** Mechanical renames go in their own,
  so review is a single read.
- Say how you verified it. For a change under `app/`, that means screenshots.

The pull request template asks for exactly this.

## What needs a maintainer

Two things a contributor cannot do, and should not need to:

- **Signing the Firefox extension.** `make extension-sign` needs
  `WEB_EXT_API_KEY` and `WEB_EXT_API_SECRET`. Verify an extension change with
  `make extension` — lint plus an unsigned build — and leave signing to a
  release.
- **Releases.** Tagging, and whatever signing a platform demands. See
  [docs/RELEASING.md](docs/RELEASING.md).

## Security

Do not open a public issue for a vulnerability. [SECURITY.md](SECURITY.md) says
where to send one, what is in scope, and what the trust boundary actually is.

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md), version
2.1, adopted as-is. Report a problem to owen@owenrumney.co.uk.

## Licence

Apache-2.0. Your contribution arrives under the same terms, by §5 of the
licence — there is no separate CLA to sign.

## The front end

ade's own window is the whole front end: `app/`, a renderer served over
loopback by the Go host in `cmd/adewindow`, which is an `NSWindow` holding a
`WKWebView`. `make app` runs it from source, `make app-test` type checks and
tests it, `make app-install` puts `ADE.app` in /Applications.

Electron went on 2026-11-06, because Chromium stalls this platform's renderer
for most of a second at a time and WebKit does not: `.git/ade/docs/webview-host.md`
holds the measurements. The VSCodium fork and the VS Code extension went
before it, by `.git/ade/docs/editor-removal.md`.

- `app/renderer` is the window itself.
- `app/pages` are the panels it frames, one iframe each, talking to a host in
  `app/renderer/panels.ts` over `postMessage`. They were the extension's
  webviews and keep its contract: `acquireVsCodeApi()`, a `ready` message, a
  `state` back.
- `app/media` is their CSS, and `PAGES` in the Makefile is the bundle list.

## The prompts that are not here

`internal/agents/defaults` holds a `README` and no
`.md`. The prompts name people, an employer and internal conventions, so they
live in the private `ade-private` repository beside this checkout. `make
private` copies them in, and `.gitignore` stops them coming back.

Without them ade builds and every test passes: `install` installs nothing, and
the machine's own `~/.claude/agents` are read as always.
`TestTheShippedAgentsParseAndOnlyTheWritersWrite` skips itself, so the check
that the shipped agents declare the right tools only runs once you have copied
them in. Edit a prompt in `ade-private`, not in the working copy here: the copy
is gitignored and the next `make private` overwrites it.
