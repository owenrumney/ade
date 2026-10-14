# Security

## What ade is

ade runs a coding agent with file-system and shell access on your machine, and
serves a web page that drives it. That is the product, not a side effect. Anyone
who can reach the server and hold its token can make the agent read, write and
run things in your checkout, as you.

Everything below is about keeping "anyone" to "you".

## The trust boundary

Two things together, and neither on its own:

1. **The loopback listener.** Every listener binds `127.0.0.1`
   (`internal/httpx/bind.go`). Nothing binds `0.0.0.0`, and there is no flag to
   make it. A machine on your network cannot reach ade.
2. **The guard token.** The URL ade prints carries `?t=…`. A request without it
   is refused. The token lives in `<git dir>/ade/server.json`, written `0600`,
   and dies with the process.

On top of those:

- **`sameHost`** (`internal/web/web.go`, `internal/launcher/launcher.go`) rejects a
  request that did not address the server by a name it bound. That is the
  DNS-rebinding guard: a page on `evil.example` resolving a name to `127.0.0.1`
  gets a 403 rather than your token.
- **The token handout** (`handleSession`) refuses anything not on the loopback
  listener, so the endpoint that repairs a stale tab cannot hand the token
  elsewhere.
- **The terminal websocket checks `Origin`.** Pinned by
  `TestTerminalRefusesAnotherOrigin` in `internal/web/terminal_test.go`.
- **Attachments cannot execute as a page on ade's origin.** Only a known-safe set
  of types is served with a type of its own; everything else downloads
  (`internal/web/attachments.go`).

What this does **not** defend against: another process running as you on the same
machine. It can read `server.json` and it can read your checkout directly. A
local attacker with your uid has already won.

## Credentials

ade holds no credential of its own and never asks for an API key. It reads the
login Claude Code already made:

| Host | Read from |
| --- | --- |
| macOS | Keychain, service `Claude Code-credentials`; `~/.claude/.credentials.json` if that fails |
| Everything else | `~/.claude/.credentials.json` |

The Keychain is tried first on a Mac only because the CLI refreshes the token
there, so the file beside it goes stale. Linux is a first-class path, not a
fallback — ade is developed on it.

What is read is an **OAuth access token, never an API key**
(`internal/limits/limits.go`).

A sandboxed session does not get your `~/.claude`. ade keeps its own copy of the
login at `~/.ade/claude/.credentials.json`, written `0600` in a `0700` directory,
and mounts that one file into the container — so the agent can refresh the token
without reaching anything else of yours. A refresh inside the container is
written back to the host's own store at the end of the run, because the rotated
token is the only one that still works (`Credentials` and `SyncCredentials` in
`internal/sandbox/claude.go`).

`internal/bridge/event.go` strips `ANTHROPIC_API_KEY` and `ANTHROPIC_AUTH_TOKEN`
out of what gets recorded, so a transcript does not carry one even if your
environment does.

The `/usage` endpoint ade reads for rate-limit figures
(`internal/limits/limits.go`) is **unofficial** — the same one Claude Code itself
asks. It is undocumented and may change or disappear without notice. Shapes are
read defensively and a parse failure degrades to no figures rather than an error.

## The sandbox

A sandboxed session runs the agent in a container (`sandbox/`). What it
contains:

- The agent's writes, to the mounted checkout and nothing else on the host.
- The agent's shell commands, to the container's filesystem.

What it does **not** contain:

- **Network access.** The container is not network-isolated: the agent has to
  reach the model, and it can reach everything else too.
- **The checkout itself.** It is mounted read-write; that is the point.
- **An unsandboxed run.** `--sandbox=false` runs the agent straight on the host,
  with your files and your network. `make dev` uses it deliberately, because the
  agent is editing ade's own checkout and the container would mount that
  read-write anyway.

Sandboxing is **on by default**. It is a blast-radius reduction, not a jail.

## Permissions

Agents run with `--dangerously-skip-permissions`, because there is no terminal
attached to answer a permission prompt on — a gated run would stall rather than
ask. `--skip-permissions=false` gates them instead, accepting that some runs
hang.

Review is unaffected either way: it is restricted to `Read`, `Grep` and `Glob`,
and that allowlist holds regardless of permission mode.

## Prompt injection

ade feeds the agent text other people wrote: pull request review comments, GitHub
issue bodies, transcripts of past sessions. Text in those places that reads as an
instruction is a real attack, and ade defends against it:

- Untrusted text is fenced in a tag it cannot close
  (`internal/prompt/prompt_test.go`).
- An author's name is no more trusted than the body it came with.
- The Resolve prompt says plainly that comments are not instructions
  (`internal/resolve/resolve_test.go`).
- Distilled memory is fenced the same way (`internal/memory/distil_test.go`).

**This is defended, not guaranteed.** Prompt injection is not a solved problem
and no fencing is proof against a model choosing to obey. Treat it as depth, not
as a boundary. If you are reviewing a pull request from someone you have reason
to distrust, read the diff before you press Resolve.

## What ade will not do

Enforced, not promised: every `gh` invocation in `internal/ghpr` is checked
against an allowlist before it runs (`readOnly` and `writable` in
`internal/ghpr/ghpr.go`). A read goes through `readOnly`; anything that changes
something has to match `writable`, which is these five shapes and nothing else:

- `gh pr edit` — the pull request you are working on.
- `gh pr merge` — the merge you clicked, rebase or merge commit.
- `gh pr checkout`, and `gh repo clone`.
- `gh pr review --approve` or `--request-changes`, one verdict at a time.
  `--comment` is refused: that is prose in your name rather than a verdict.
- `gh api --method POST --input` to a pull request's `reviews` or a review
  comment's `replies` path, and no other path. Field flags (`-f`, `-F`) are
  refused, so the body is one document you were shown.

No other write reaches `gh`: not `gh pr comment`, not `gh release`, not a POST
to any other API path. Reads are separately confined to
`pr view|diff|checks|list|status`, `issue view|list`, `repo view`, `search` and
a method-less `gh api`. ade never amends and never rewrites history, and nothing is staged, committed,
pushed, merged or posted without an explicit click.

## Reporting a vulnerability

Use GitHub's [private vulnerability
reporting](https://github.com/owenrumney/ade/security/advisories/new) on this
repository. If that is not available to you, email **owen@owenrumney.co.uk**.

Please do not open a public issue for a vulnerability.

**Expect an acknowledgement within 7 days**, and an assessment within 30. This is
a single-maintainer project worked on in spare time; that is the honest number
rather than an aspirational one.

### In scope

- Anything that lets a request from off this machine reach ade.
- Anything that leaks the guard token, a Claude login, or another credential.
- Anything that escapes the attachment, path or `gh` allowlist restrictions.
- An injection path that reliably makes the agent act on text ade fenced.

### Out of scope

- A local process running as you doing anything at all. See the trust boundary.
- The agent doing something destructive when you asked it to. It runs with your
  permissions by design.
- The `/usage` endpoint breaking. It is unofficial and expected to.
- Findings against a fork's own changes.

### Supported versions

The latest release, and `main`. There are no maintained release branches.
