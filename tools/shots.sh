#!/usr/bin/env bash
# Capture the launcher against a fixture home, so UI work can be looked at
# rather than described. Writes PNGs to /tmp/ade-shots.
#
# Usage: tools/shots.sh [view...] — with no names, every view. Name the ones
# the change is about: a reply carries four pictures, so a full run buries them.
set -euo pipefail

# A leftover fixture server holds the port the next run needs.
pkill -f '/tmp/ade-shots/ade' 2>/dev/null || true
pkill -f '/tmp/ade-shots/code' 2>/dev/null || true

out=/tmp/ade-shots
fix=/tmp/ade-shots/home
rm -rf "$out"
# The binary is built here and then run, so the directory has to be ours. Plain
# mkdir is the atomic claim: it fails on anything at that path, symlink included.
mkdir "$out" || { echo "$out already exists and is not ours"; exit 1; }
mkdir -p "$fix/.ade" "$out/repos/ade"

# stamp <gnu offset> <bsd offset>: BSD date has no -d and GNU date has no -v.
stamp() { date -u -d "$1" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -v"$2" +%Y-%m-%dT%H:%M:%SZ; }

# quiet <what> <cmd...>: output only when it failed. Under set -e a swallowed
# build exits with nothing, which is what made a missing shot unexplainable.
quiet() {
  local what=$1; shift
  local log; log=$(mktemp)
  if ! "$@" >"$log" 2>&1; then
    echo "$what failed:" >&2
    # tsc, vite and npm print the causal error first and a summary last, so the
    # head is the part worth keeping.
    head -30 "$log" >&2
    if [ "$(wc -l <"$log")" -gt 30 ]; then echo "  […]" >&2; tail -5 "$log" >&2; fi
    rm -f "$log"
    return 1
  fi
  rm -f "$log"
}

git -C "$out/repos/ade" init -q
git -C "$out/repos/ade" config user.email shots@example.com
git -C "$out/repos/ade" config user.name shots
# An origin, so the project store has the slug it would really have. Without
# one the identity falls back to the git dir path and the fixture would have to
# restate how a path becomes a slug.
git -C "$out/repos/ade" remote add origin git@github.com:owenrumney/ade.git
# Line two is edited rather than added, so the diff has a pair of lines to
# word-mark and a shot can say whether the marking landed.
printf 'fixture\nedited in the working copy\n' > "$out/repos/ade/README.md"
git -C "$out/repos/ade" add -A
git -C "$out/repos/ade" commit -qm "fixture"

# Something on each side of the index, so the Diff tab has files to stage and
# unstage rather than an empty state.
printf 'fixture\nedited in the working tree\n' > "$out/repos/ade/README.md"
printf 'package main\n\nfunc main() {}\n' > "$out/repos/ade/main.go"
git -C "$out/repos/ade" add main.go

# Design documents, so the Design tab opens on its picker rather than an empty
# state. None is named after the branch, which is what leaves the picker up.
docs="$out/repos/ade/.git/ade/docs"
mkdir -p "$docs"
for name in session-registry project-memory cold-sandbox review-filters; do
  printf '# %s\n\nA fixture design document.\n' "$name" > "$docs/$name.md"
done

# One document with something to render, for the frame that draws them.
cat > "$docs/cold-sandbox.md" <<'MD'
# Cold sandbox

A container started **before** the turn needs it, so the first message does
not wait on `docker pull`. The cost is a slot held open.

## The shape

```mermaid
flowchart LR
    A[a turn starts] --> B{is a box warm?}
    B -->|yes| C[use it]
    B -->|no| D[start one, and wait]
    C --> E[the turn runs]
    D --> E
```

## What it costs

| | warm | cold |
| --- | --- | --- |
| First message | instant | eight seconds |
| Slots held | one per checkout | none |

![the mark a warm box wears](cold-sandbox.svg)

- A slot is a container, not a process.
- Four is the ceiling, and it is not configurable.

```lua
for _, r in ipairs(results) do
  total = total + r.value
end
```
MD
cp vscode/media/ade.svg "$docs/cold-sandbox.svg"

# A subfolder, so the Design tree is photographed nesting one rather than
# listing one level and hiding what is under it.
mkdir -p "$docs/enhancements"
for name in 01-context-ledger 02-rejected-hunks 03-prove-it; do
  printf '# %s\n\nA fixture design document.\n' "$name" > "$docs/enhancements/$name.md"
done

# Past builds of a design this size, priced so the modal shows a window share
# and, against the 68% reading below, the amber cap.
for mins in 32 38 41 47 55; do
  printf '{"kind":"build","model":"claude-opus-5","repo":"%s","docWords":6,"checklistItems":1,"started":"2026-09-01T10:00:00Z","finished":"2026-09-01T10:%02d:00Z","usage":{"costUSD":0.00%d,"costKnown":true}}\n' \
    "$out/repos/ade" "$mins" "$((mins / 7))" >> "$fix/.ade/estimates.jsonl"
done

# A diagram, so a shot says whether mermaid drew. The syntax is the modern
# subset: & chaining, a quoted edge label, <br/> in a node.
cat >> "$docs/session-registry.md" <<'MD'

```mermaid
flowchart LR
    ctx["diffContext<br/>provider, repoURL"] --> sw{provider}
    sw -->|github| gh["vcsurl.OwnerRepo"]
    sw -->|gitlab| gl["vcsurl.GitLabProject"]
    gl -->|"+ ServerURL"| gln["gitlab.New"]
    gh --> ghn["github.New"]
    opts["commentOptions<br/>Tag, TLSConfig"] --> ghn & gln
```
MD

# A named agent's reply in the conversation. Only a real run produces one and
# the fixture has no agent, so it is written in directly — before the server,
# which holds the store's lock for as long as it is up.
# A table, because that is what the editor's chat draws worst: markdown is
# rendered here as the server would, so the shot is of the real html.
quiet "seedchat table" go run ./tools/seedchat "$out/repos/ade/.git" \
  11111111-2222-3333-4444-555555555555 staff-reviewer \
  "Every modal dialog in the extension.

| # | Where | Command | Prompt | Buttons |
|---|---|---|---|---|
| 1 | \`extension.ts:221\` | \`ade.startIssue\` | Start FIX-623 here? | Start |
| 2 | \`extension.ts:304\` | \`ade.fixChecks\` | Investigate N failing checks? | Investigate |
| 3 | \`extension.ts:1116\` | \`ade.mergePR\` | Merge #N? | Merge |
| 4 | \`scm.ts:125\` | \`ade.push\` | the remote has commits this push would drop | Force push |"

# A fenced block, for the copy button in its corner.
quiet "seedchat fence" go run ./tools/seedchat "$out/repos/ade/.git" \
  11111111-2222-3333-4444-555555555555 claude \
  "Build the extension and install it into the editor:

\`\`\`sh
make ui
make editor-package editor-install
open http://127.0.0.1:5173/
\`\`\`"

# Long prose with the short form the cleanse would have put beside it, so the
# shot is of what the page draws first and the way back to the original.
quiet "seedchat" go run ./tools/seedchat \
  -short "Order the write-back by expiry, not by whether the access token is in date.

- Two copies of one login: the one refreshed last holds the only chain that still works.
- None of the three call sites in \`internal/authstore\` sorts before writing, so the stale copy wins when the newer one was refreshed on another machine.
- Fix: one sort in the writer, and a test that refreshes the second copy first." \
  "$out/repos/ade/.git" \
  11111111-2222-3333-4444-555555555555 architect \
  "The write-back needs an ordering rule. Two copies of one login, and whichever refreshed last holds the only chain that still works — so order them by expiry, not by whether the access token is in date. I went through the three call sites in internal/authstore and, as far as I can tell, none of them sorts before writing, which means the stale copy wins whenever the newer one happened to be refreshed on another machine. The fix is probably one sort in the writer, plus a test that refreshes the second copy first so the ordering is actually exercised." \
  "VITE v7.1.0  ready in 412 ms

  ➜  Local:   http://127.0.0.1:5173/
  ➜  Network: use --host to expose"

# Two worktrees, one of them on a branch long enough to size the node, so the
# worktree graph has something to draw rather than an empty state.
git -C "$out/repos/ade" worktree add -q -b ade-pr-1028 "$out/repos/ade-pr-1028" >/dev/null
git -C "$out/repos/ade" worktree add -q \
  -b fix-656-serve-enum-names-from-the-credential-provider \
  "$out/repos/ade-pr-1046" >/dev/null

# What a worker leaves behind: a clone under a random session id. The picker
# has to name it from origin, not after the directory.
clone="$out/repos/80a6ccb2de7fe172/dashboard"
git -C "$out" init -q "$clone"
git -C "$clone" config user.email shots@example.com
git -C "$clone" config user.name shots
git -C "$clone" remote add origin git@github.com:infracost/dashboard.git
echo fixture > "$clone/README.md"
git -C "$clone" add -A
git -C "$clone" commit -qm "fixture"

# A standing instruction, so the Instructions pane has one and every session
# started against this fixture demonstrably gets it.
cat > "$fix/.ade/settings.json" <<JSON
{"scan_dirs":["$out/repos"],"palette":"ghostty-deep","default_role":"oss-dev",
 "hosts":[{"name":"worker","label":"Build worker"}],
 "sandbox":{"paths":[],"env":{"CI":"1","GOFLAGS":"-mod=mod","NPM_TOKEN":"npm_realsecretvalue","DB_PASSWORD":"hunter2"}},
 "instructions":"Never push to a shared branch without asking first.\nPrefer the standard library over adding a dependency."}
JSON

# Two roles, so the roles pick is of a machine that has some.
mkdir -p "$fix/.ade/roles"
cat > "$fix/.ade/roles/oss-dev.md" <<'ROLE'
You are a pragmatic open-source maintainer working on Owen's own tools.

Prefer small, idiomatic changes that follow the repository's style.
ROLE
cat > "$fix/.ade/roles/reviewer.md" <<'ROLE'
You are reviewing, not writing. Say what is wrong and why; change nothing.
ROLE

# A transcript, so the session panels have rows rather than empty states. It
# has never been "seen", which is what puts it in Waiting on you.
proj="$fix/.claude/projects/-tmp-ade-shots-repos-ade"
mkdir -p "$proj"
spoke=$(stamp '-12 min' -12M)
cat > "$proj/11111111-2222-3333-4444-555555555555.jsonl" <<JSON
{"type":"user","timestamp":"$(stamp '-20 min' -20M)","cwd":"$out/repos/ade","gitBranch":"main","message":{"role":"user","content":"make the usage bars the same width"}}
{"type":"assistant","timestamp":"$spoke","requestId":"req-1","message":{"id":"m1","role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":100,"output_tokens":200,"cache_read_input_tokens":1000,"cache_creation_input_tokens":500}}}
{"type":"ai-title","aiTitle":"Equal-width usage bars"}
JSON

# A week's worth of requests, so the stats dials have a working day and a
# working week in them rather than a single wedge. 2026-09-14 is a Monday.
week="$fix/.claude/projects/-tmp-ade-shots-repos-week"
mkdir -p "$week"
{
  echo '{"type":"user","timestamp":"2026-09-14T08:00:00Z","cwd":"'"$out"'/repos/ade","gitBranch":"main","message":{"role":"user","content":"a week of work"}}'
  n=0
  # <hour>:<weight> and <day>:<weight>; a slot gets the two multiplied, so the
  # dial reads as the working day and the pie as the working week.
  clock="8:2 9:4 10:4 11:3 12:2 13:3 14:4 15:4 16:3 17:2 18:1 20:1"
  for day_weight in 14:3 15:4 16:5 17:4 18:3 19:1 20:1; do
    day=${day_weight%%:*}; days=${day_weight##*:}
    for hour_weight in $clock; do
      hour=${hour_weight%%:*}; hours=${hour_weight##*:}
      for pass in $(seq 1 $((days * hours))); do
        n=$((n + 1))
        printf '{"type":"assistant","timestamp":"2026-09-%02dT%02d:%02d:00Z","requestId":"w-%d","message":{"id":"wm-%d","role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":120,"output_tokens":240,"cache_read_input_tokens":9000,"cache_creation_input_tokens":600}}}\n' \
          "$day" "$hour" "$((pass * 2))" "$n" "$n"
      done
    done
  done
  echo '{"type":"ai-title","aiTitle":"A week of work"}'
} > "$week/22222222-3333-4444-5555-666666666666.jsonl"

# A plan reading, so the usage panel has bars rather than an empty state.
cat > "$fix/.ade/claude-limits.json" <<JSON
{"provider":"claude","windows":[
  {"name":"5h","percent":68,"resetsAt":"$(stamp '+47 min' +47M)"},
  {"name":"7d","percent":24,"resetsAt":"$(stamp '+3 day' +3d)"}],
 "checkedAt":"$(date -u +%Y-%m-%dT%H:%M:%SZ)"}
JSON

# A few todos, so the Concierge list has rows: one late, one due, one started.
cat > "$fix/.ade/todos.json" <<JSON
[
 {"id":"td1","text":"Rename the worker image before the next release",
  "due":"$(stamp '-2 day' -2d)","createdAt":"$(stamp '-9 day' -9d)"},
 {"id":"td2","text":"Ask about the agentbus review token",
  "due":"$(stamp '+1 day' +1d)","from":"agent","session":"s4",
  "createdAt":"$(stamp '-1 day' -1d)"},
 {"id":"td3","text":"Write up how the launcher proxies a worker",
  "started":"s7","createdAt":"$(stamp '-3 day' -3d)"},
 {"id":"td4","text":"Try the bell on a narrow window",
  "createdAt":"$(stamp '-2 hour' -2H)"}
]
JSON

# A project memory, so the Project modal has instructions and entries rather
# than three empty states. The slug is the origin above, normalised.
proj_store="$fix/.ade/projects/github.com-owenrumney-ade"
mkdir -p "$proj_store"
printf '{"slug":"github.com-owenrumney-ade","remote":"github.com/owenrumney/ade","remembered":true}\n' \
  > "$proj_store/project.json"
cat > "$proj_store/instructions.md" <<'MD'
Only ever drive the local dev environment. The seeded login is
dev@example.com / hunter2, and nothing here talks to staging.
MD
cat > "$proj_store/memory.json" <<JSON
{"entries":[
  {"id":"e1","heading":"Local dev environment","body":"Runs on localhost:5173 with seeded data. There is no staging login.","state":"accepted","session":"1111","added":"$(stamp '-30 day' -30d)","lastSeen":"$(stamp '-2 day' -2d)"},
  {"id":"e2","heading":"Frontend tests are noisy","body":"Run one Go package at a time, in the foreground. internal/web reaches a real agent without a shim.","state":"accepted","session":"2222","added":"$(stamp '-200 day' -200d)","lastSeen":"$(stamp '-190 day' -190d)"},
  {"id":"e3","heading":"Test command","body":"Run make test.","state":"superseded","session":"3333","added":"$(stamp '-300 day' -300d)"}
]}
JSON
cat > "$proj_store/proposed.json" <<JSON
[
  {"id":"p1","heading":"Style tokens","body":"Colour and spacing live in internal/ui/tokens.css. A raw hex anywhere else fails the build.","state":"accepted","session":"4444","added":"$(date -u +%Y-%m-%dT%H:%M:%SZ)"},
  {"id":"p2","heading":"Test command","body":"Run go test ./... — make test was removed.","state":"accepted","session":"4444","replacesHeading":"Test command","added":"$(date -u +%Y-%m-%dT%H:%M:%SZ)"}
]
JSON

# Two jobs, so the Jobs pane and the Job reports card have rows rather than
# empty states: one that reported and one that has stopped working.
mkdir -p "$fix/.ade/jobs" "$fix/.ade/job-state" "$proj_store/jobs"
cat > "$fix/.ade/jobs/morning.md" <<'MD'
---
name: morning
description: what happened overnight on the branches I have open
schedule: "0 8 * * 1-5"
project: github.com/owenrumney/ade
continuity: summary
allow: reads
---

/catchup
MD
cat > "$fix/.ade/jobs/drift-watch.md" <<'MD'
---
name: drift-watch
description: what has moved under my branch since I based it
schedule: "@every 6h"
---

Report what has landed on the default branch since this branch was based, and
which of it will conflict.
MD
cat > "$proj_store/jobs/morning.json" <<JSON
{"name":"morning","session":"11111111-2222-3333-4444-555555555555",
 "handover":"Reported on PR 41 and the flaky terminal test on 9 Sep.",
 "lastFired":"$(stamp '-3 hour' -3H)","nextDue":"$(stamp '+21 hour' +21H)",
 "outcomes":[
  {"firedAt":"$(stamp '-3 hour' -3H)","millis":48000,"costUsd":0.07,
   "report":"PR 41 has two failing checks and a review comment from @vic.","dir":"$out/repos/ade",
   "session":"11111111-2222-3333-4444-555555555555"},
  {"firedAt":"$(stamp '-1 day' -1d)","millis":39000,"costUsd":0.05,"seen":true,
   "report":"Nothing overnight but a dependabot bump."}]}
JSON
cat > "$fix/.ade/job-state/drift-watch.json" <<JSON
{"name":"drift-watch","lastFired":"$(stamp '-2 hour' -2H)","nextDue":"$(stamp '+4 hour' +4H)",
 "outcomes":[
  {"firedAt":"$(stamp '-2 hour' -2H)","millis":4000,"error":"github.com/owenrumney/other is not checked out on this machine"},
  {"firedAt":"$(stamp '-8 hour' -8H)","millis":3000,"error":"github.com/owenrumney/other is not checked out on this machine"}]}
JSON

# Review comments on the fixture diff, so the Diff tab's severity and category
# filters have chips rather than nothing to choose between.
#
# ctxhash reproduces comments.hashAt: the three lines around the anchored one,
# NUL separated, first 8 bytes of the sha. Get it wrong and the shot shows
# orphaned comments, which is the wrong picture rather than a silent one.
sha() { sha256sum 2>/dev/null || shasum -a 256; }
ctxhash() { printf '%s\0%s\0%s\0' "$1" "$2" "$3" | sha | cut -c1-16; }
seeded="$out/repos/ade/.git/ade/$(git -C "$out/repos/ade" symbolic-ref --short HEAD).json"
mkdir -p "$(dirname "$seeded")"
now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
cat > "$seeded" <<JSON
[
 {"id":"t1","file":"main.go","side":"right","line":3,
  "anchor":{"contextHash":"$(ctxhash '' 'func main() {}' '')","content":"func main() {}"},
  "category":"correctness","severity":"critical","createdAt":"$now",
  "comments":[{"id":"c1","author":"staff-reviewer","at":"$now",
   "body":"The entry point does no work and returns zero.\n\nFails when: anything calls the binary expecting it to serve."}]},
 {"id":"t2","file":"main.go","side":"right","line":2,
  "anchor":{"contextHash":"$(ctxhash 'package main' '' 'func main() {}')","content":""},
  "category":"security","severity":"high","createdAt":"$now",
  "comments":[{"id":"c2","author":"security","at":"$now",
   "body":"The package declares no build constraints, so it links into every target."}]},
 {"id":"t3","file":"main.go","side":"right","line":1,
  "anchor":{"contextHash":"$(ctxhash '' 'package main' '')","content":"package main"},
  "category":"design","severity":"medium","createdAt":"$now",
  "comments":[{"id":"c3","author":"architect","at":"$now",
   "body":"main belongs under cmd/, not at the root of the module."}]},
 {"id":"t5","file":"main.go","side":"right","line":3,
  "anchor":{"contextHash":"0000000000000000","content":"func main() { old }"},
  "category":"correctness","severity":"high","createdAt":"$now",
  "comments":[{"id":"c5","author":"staff-reviewer","at":"$now",
   "body":"The line this was pinned to has been rewritten since."}]},
 {"id":"t6","file":"main.go","side":"right","line":1,
  "anchor":{"contextHash":"$(ctxhash '' 'package main' '')","content":"package main"},
  "category":"correctness","severity":"medium","createdAt":"$now","externalId":"gh-99",
  "comments":[{"id":"c6","author":"copilot","at":"$now",
   "body":"Consider naming this package cmd so the binary is discoverable."}]},
 {"id":"t4","file":"README.md","side":"right","line":2,
  "anchor":{"contextHash":"$(ctxhash 'fixture' 'edited in the working tree' '')","content":"edited in the working tree"},
  "category":"test-coverage","severity":"low","createdAt":"$now",
  "comments":[{"id":"c4","author":"tester","at":"$now",
   "body":"No test covers the README being read at startup."}]}
]
JSON

# Another repository the sandbox mounts read-write, with work in it, so the
# Diff tab's root switcher has somewhere to go and something to report.
git -C "$out" init -q ci
git -C "$out/ci" config user.email shots@example.com
git -C "$out/ci" config user.name shots
printf 'steps: []\n' > "$out/ci/release.yml"
git -C "$out/ci" add -A
git -C "$out/ci" commit -qm "fixture"
printf 'steps:\n  - build\n  - publish\n' > "$out/ci/release.yml"
mkdir -p "$out/ci/.git/ade"
cat > "$out/ci/.git/ade/$(git -C "$out/ci" symbolic-ref --short HEAD).json" <<JSON
[{"id":"r1","scope":"repo","createdAt":"$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "comments":[{"id":"rc1","author":"you","at":"$(date -u +%Y-%m-%dT%H:%M:%SZ)",
   "body":"The publish step runs before the tests."}]}]
JSON
cat > "$out/repos/ade/.git/ade/sandbox.json" <<JSON
{"paths":[{"path":"$out/ci","rw":true}],"env":{}}
JSON

# Attachments in the fixture checkout, so the disk panel has a tree to offer
# emptying and the unrecoverable confirmation has a file count to name.
att="$out/repos/ade/.git/ade/attachments"
mkdir -p "$att"
printf 'a spec\n' > "$att/0001-rate-limit-spec.pdf"
head -c 240000 /dev/zero | tr '\0' 'x' > "$att/0002-run.log"
printf 'name,count\na,1\n' > "$att/0003-counts.csv"

# Files for the composer shot: one picture, one not. A 1x1 PNG is enough —
# the thumbnail is the point, not what is in it.
shot="$out/fixtures"
mkdir -p "$shot"
cat > "$shot/png.py" <<'PNGGEN'
import struct, sys, zlib

# A 64x48 block of one colour. Built rather than embedded: a base64 blob with
# a bad CRC decodes to a file that is a broken picture in the shot.
w, h = 64, 48
rgb = [int(v, 16) for v in sys.argv[2:5]]
raw = b"".join(b"\x00" + bytes(rgb * w) for _ in range(h))

def chunk(kind, body):
    return (struct.pack(">I", len(body)) + kind + body
            + struct.pack(">I", zlib.crc32(kind + body) & 0xFFFFFFFF))

png = (b"\x89PNG\r\n\x1a\n"
       + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0))
       + chunk(b"IDAT", zlib.compress(raw))
       + chunk(b"IEND", b""))
open(sys.argv[1], "wb").write(png)
PNGGEN
python3 "$shot/png.py" "$shot/screenshot.png" 0x4c 0x56 0x6a
# Two more for the lightbox, in different colours: an arrow that changed
# nothing would photograph the same as one that did.
python3 "$shot/png.py" "$shot/before.png" 0x8a 0x3f 0x3f
python3 "$shot/png.py" "$shot/after.png" 0x3f 0x6b 0x8a
printf 'GET /webhook 429 rate limited\n' > "$shot/run.log"

# The Files tab's preview, one file per kind it renders. The picture is named
# after the document so the two sort together, which is what the list is for.
cp "$shot/screenshot.png" "$docs/session-registry.png"
printf '\n![the session rail](session-registry.png)\n' >> "$docs/session-registry.md"
printf 'ADE_PORT=7777\nADE_TOKEN=fixture\n' > "$att/0004-.env"
printf 'server:\n  port: 7777\n  tls: false\n' > "$att/0005-config.yaml"

# A browser, from wherever this is running: the container installs one, a mac
# has no apt-get and uses the Chrome it already has.
if [ -z "${ADE_CHROME:-}" ]; then
  for c in chromium chromium-browser google-chrome google-chrome-stable; do
    if p=$(command -v "$c" 2>/dev/null); then ADE_CHROME=$p; break; fi
  done
fi
if [ -z "${ADE_CHROME:-}" ]; then
  for p in "/Applications/Chromium.app/Contents/MacOS/Chromium" \
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"; do
    if [ -x "$p" ]; then ADE_CHROME=$p; break; fi
  done
fi
# A fresh image has no package index either: install alone fails, update first.
if [ -z "${ADE_CHROME:-}" ] && command -v apt-get >/dev/null 2>&1; then
  quiet "refreshing the package index" apt-get update || true
  quiet "installing chromium" apt-get install -y chromium || true
  ADE_CHROME=$(command -v chromium 2>/dev/null || true)
fi
[ -n "${ADE_CHROME:-}" ] || {
  echo "no chromium found; set ADE_CHROME to a browser binary"; exit 1; }
export ADE_CHROME

go build -o "$out/ade" ./cmd/ade

# The editor's chat is its own bundle, not the one embedded above, so a change
# to vscode/src is invisible in its shot unless it is built here too.
(cd vscode && quiet "installing the extension deps" npm install --no-audit --no-fund &&
  quiet "the extension build" npm run build)

# The shipped agents, so the diff toolbar has a review picker and an ELI5.
HOME="$fix" "$out/ade" agents install >/dev/null

# waitForURL <log> <what>: ade prints the address to open once it is listening.
waitForURL() {
  for _ in $(seq 1 80); do
    found=$(grep -o 'http://127.0.0.1:[0-9]*/?t=[a-f0-9]*' "$1" | head -1 || true)
    [ -n "$found" ] && { echo "$found"; return 0; }
    sleep 0.25
  done
  echo "$2 never printed a url" >&2
  tail -5 "$1" >&2
  return 1
}

# Not ade's own port: the launcher you have open is holding that one, and a
# shot should never need you to close it.
HOME="$fix" ADE_EDITOR=none "$out/ade" launch --no-open --foreground --idle 5m --port 29334 > "$out/launcher.log" 2>&1 &
launcher=$!
# A ade serving the fixture checkout. Everything behind the top bar — the
# Project modal, the exit dialog — is here rather than on the launcher. It
# takes its checkout from the working directory, hence the subshell.
(cd "$out/repos/ade" && HOME="$fix" ADE_EDITOR=none "$out/ade" --no-open --idle 5m \
  --session 11111111-2222-3333-4444-555555555555) > "$out/server.log" 2>&1 &
served=$!
trap 'kill $launcher $served 2>/dev/null || true; pkill -f "$out/ade" 2>/dev/null || true' EXIT

url=$(waitForURL "$out/launcher.log" "the launcher")
server_url=$(waitForURL "$out/server.log" "the server") || server_url=""

# Warm the reads the page makes, so the capture is of a settled page rather
# than of "Measuring…". The disk walk is the slow one and is cached after.
base="${url%%/?t=*}"
token="${url##*t=}"
for path in limits limits/window disk waiting dash; do
  curl -s -o /dev/null --max-time 60 "$base/api/$path?t=$token" || true
done

# A card for the desk to be caught proposing. Drawn through the tool the
# concierge would call, so the shot is of the real path and not a mock.
curl -s -o /dev/null --max-time 20 -X POST "$base/mcp/concierge?t=$token" \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"start",
       "arguments":{"repo":"'"$out/repos/ade"'","name":"ade",
       "text":"Rename the worker image before the next release, and update the deploy notes with the new name."}}}' || true

# Two sent messages, one picture each. The lightbox walks the conversation
# rather than one message, and that is only visible with pictures spread over
# more than one. The runs they start have no sandbox here and fail; the
# attachments are recorded either way, which is what is being photographed.
if [ -n "$server_url" ]; then
  sbase="${server_url%%/?t=*}"
  stoken="${server_url##*t=}"
  for pair in "before.png:the launcher before" "after.png:and after"; do
    file="${pair%%:*}"
    said="${pair#*:}"
    python3 - "$shot/$file" "$said" > "$out/msg.json" <<'BODY'
import base64, json, sys
data = base64.b64encode(open(sys.argv[1], "rb").read()).decode()
name = sys.argv[1].split("/")[-1]
json.dump({"message": sys.argv[2],
           "files": [{"name": name, "mediaType": "image/png", "data": data}]},
          sys.stdout)
BODY
    curl -s -o /dev/null --max-time 30 -X POST -H 'content-type: application/json' \
      --data-binary "@$out/msg.json" "$sbase/api/chat?t=$stoken" || true
  done
  # Written after the messages, so undoing one has a file to take back out.
  printf 'made by the turn\n' > "$out/repos/ade/made-by-the-turn.txt"
fi

# The editor itself: VS Code's own server, with the extension installed, for
# the same browser to photograph. Only when a code-* view is wanted, or all are.
wants_code=false
[ $# -eq 0 ] && wants_code=true
for view in "$@"; do case "$view" in code-*) wants_code=true ;; esac; done

code_url=""
if $wants_code && ! command -v code >/dev/null 2>&1; then
  echo "no code cli on PATH: skipping the code-* views" >&2
elif $wants_code; then
  code_port=29335
  # Outside the fixture, so the server it downloads is fetched once per
  # VS Code version rather than once per run.
  case "$(uname -s)" in
    Darwin) code_cli="$HOME/Library/Caches/ade/code-cli" ;;
    *) code_cli="${XDG_CACHE_HOME:-$HOME/.cache}/ade/code-cli" ;;
  esac
  mkdir -p "$code_cli" "$out/code/data/Machine"
  cat > "$out/code/data/Machine/settings.json" <<'JSON'
{
  "workbench.colorTheme": "Default Dark Modern",
  "workbench.secondarySideBar.defaultVisibility": "hidden",
  "chat.disableAIFeatures": true,
  "telemetry.telemetryLevel": "off", "extensions.ignoreRecommendations": true
}
JSON
  (cd vscode && { [ -d node_modules ] || quiet "installing the extension's tools" npm install --ignore-scripts --no-audit --no-fund; } \
    && quiet "building the extension" npm run build \
    && quiet "packaging the extension" npx vsce package --out "$out/ade.vsix")

  code_up() {
    for _ in $(seq 1 240); do
      [ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://127.0.0.1:$code_port/" || true)" = "200" ] && return 0
      sleep 0.5
    done
    return 1
  }

  # True when it wrote something, which is what makes the restart worth it.
  code_proposals() {
    node -e '
      const fs = require("fs"), at = process.argv[1];
      const p = JSON.parse(fs.readFileSync(at, "utf8"));
      const want = JSON.parse(fs.readFileSync(process.argv[2], "utf8")).enabledApiProposals ?? [];
      const held = p.extensionEnabledApiProposals ?? (p.extensionEnabledApiProposals = {});
      if (JSON.stringify(held["owenrumney.ade"]) === JSON.stringify(want)) process.exit(1);
      held["owenrumney.ade"] = want;
      fs.writeFileSync(at, JSON.stringify(p, null, 2));
    ' "$1" vscode/package.json
  }

  # HOME is the fixture's: the extension talks to whichever launcher its home
  # names and starts ade in the folder it opens, and neither may be the real one.
  HOME="$fix" ADE_EDITOR=none PATH="$out:$PATH" code serve-web --without-connection-token \
    --accept-server-license-terms --port "$code_port" \
    --server-data-dir "$out/code" --cli-data-dir "$code_cli" \
    --default-folder "$out/repos/ade" > "$out/code.log" 2>&1 &
  workbench=$!
  disown "$workbench"
  trap 'kill $launcher $served $workbench 2>/dev/null || true; pkill -f "$out/ade" 2>/dev/null || true; pkill -f "$out/code" 2>/dev/null || true' EXIT

  # The first answer is what makes it fetch its server, which is the binary
  # that can install an extension: serve-web itself has no flag for it.
  code_up || true
  code_server=$(ls -t "$code_cli"/serve-web/*/bin/code-server 2>/dev/null | head -1 || true)
  if [ -z "$code_server" ]; then
    echo "the VS Code server never came up: skipping the code-* views" >&2
    tail -5 "$out/code.log" >&2
  else
    # serve-web takes no --enable-proposed-api; product.json is the other way
    # in. The server reads it once at startup, so a patch means a restart.
    if code_proposals "$(dirname "$(dirname "$code_server")")/product.json"; then
      kill "$workbench" 2>/dev/null || true
      HOME="$fix" ADE_EDITOR=none PATH="$out:$PATH" code serve-web --without-connection-token \
        --accept-server-license-terms --port "$code_port" \
        --server-data-dir "$out/code" --cli-data-dir "$code_cli" \
        --default-folder "$out/repos/ade" >> "$out/code.log" 2>&1 &
      workbench=$!
      disown "$workbench"
      code_up || echo "the restarted VS Code server never came back" >&2
    fi
    HOME="$fix" quiet "installing the extension into the served editor" "$code_server" \
      --install-extension "$out/ade.vsix" --extensions-dir "$out/code/extensions" \
      --server-data-dir "$out/code"
    code_url="http://127.0.0.1:$code_port"
  fi
fi

# A driven browser, not a --screenshot flag: the flag can only photograph the
# first paint, and the views worth seeing are behind a click.
(cd tools/shots && quiet "installing the shot driver" npm install --no-audit --no-fund)
ADE_CODE_URL="$code_url" node tools/shots/shots.mjs "$url" "$out" "$server_url" "$shot" "$@"

# The outbox is how a picture reaches the chat: reading it only shows it to
# the agent. ade attaches whatever is here to the turn's reply.
git_dir=$(git rev-parse --git-dir)
# The running binary may predate the ADE rename and still watch .git/sow, so
# the directory that is already there wins over the one we would name.
state="$git_dir/ade"
[ -d "$state" ] || { [ -d "$git_dir/sow" ] && state="$git_dir/sow"; }
mkdir -p "$state/outbox"
# Emptied first: a view that failed this run would otherwise leave the last
# run's picture behind, which reads as a shot of the change rather than a gap.
rm -f "$state/outbox"/*.png
cp "$out"/*.png "$state/outbox/" 2>/dev/null || true
ls "$state/outbox/"
