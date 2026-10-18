---
name: editor-bump
description: Move the editor to VSCodium's current stable pin and re-seat ade's patches
schedule: adhoc
continuity: fresh
allow: writes
---

ade is its own editor: VSCodium's build scripts over a pinned vscode, with
ade's patches in `editor/patches/` applied on top of VSCodium's. `CLAUDE.md`
("The editor") and `.git/ade/docs/editor-fork.md` say how it is built and
why. Move it to VSCodium's current stable release.

1. Find the pin. `git -C editor/vscodium/vscodium pull -q` (run
   `make editor-fetch` first if that directory is missing), then read
   `editor/vscodium/vscodium/upstream/stable.json`: `tag` and `commit`. If
   `MS_TAG` in `editor/env.sh` already matches, report that and stop.
2. Update `editor/env.sh`: `MS_TAG`, `MS_COMMIT`, `RELEASE_VERSION` (the tag
   with the dot after the minor removed, VSCodium's own form), and
   `VSCODIUM_COMMIT` to what `git -C editor/vscodium/vscodium rev-parse HEAD`
   says now.
3. `make editor-prepare 2>&1 | tail -40`. Ten minutes. It resets the vscode
   tree to the pin, applies VSCodium's patches and then ours from
   `editor/patches/`, and commits the result as `ADE BASELINE`. A hunk that
   does not apply names its patch. A reject in one of VSCodium's own patches
   is theirs to fix: fall back to the newest tag whose patches all apply, and
   say so in the report.
4. For each of ours that rejects: the baseline commit holds everything that
   did apply, so in `editor/vscodium/vscode` read the new source around the
   hunk, make the same change again by hand, and write the patch back with
   `git diff -- <the files it touches> > ../../patches/NN-name.patch`. Keep
   the patch's intent, not its lines. Then `make editor-prepare` again until
   it is clean.
5. `make editor-sync`, then `cd vscode && npx vitest run --reporter=dot` and
   `go test ./internal/bridge`, then `make editor-package 2>&1 | tail -5`.
   The package must sign; a build that compiles but does not sign is not done.
6. Commit on this branch as you go: the pin, then each patch you re-seated.
7. Publish the server. A window on another machine runs an editor server
   built from the same commit as the client, so a pin that moves without one
   leaves every host refusing the next window with `Client refused: version
   mismatch`. Once the pin is on `main`, `.github/workflows/editor-reh.yml`
   runs itself; from this branch, start it by hand with
   `gh workflow run editor-reh.yml -f release_version=<the new
   RELEASE_VERSION>` and say in the report that it is 25 to 40 minutes per
   arch and that no remote window will open until it is green.

The report: the old and new pins, every patch you touched and what moved under
it, the editor server run and its state, anything you fell back on, and what
failed. You cannot look at the editor, so end with what to look at after
`make editor-dev`: the title bar bell and session pill, the omnibox on
⌘⇧Space, a comment thread's head, and the floating concierge.
