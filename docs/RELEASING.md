# Releasing

One artifact ships: the Go binary. The Electron shell that used to sit beside
it is gone, and VS Code is the only front end. Undecided things are marked
**undecided** rather than papered over.

## The Go binary

### What ships

`.goreleaser.yml` builds `darwin/linux × amd64/arm64`, static (`CGO_ENABLED=0`),
version stamped into `main.version`. Each archive is a `tar.gz` carrying the
binary, `README.md`, `LICENSE` and `NOTICE`, with a `checksums.txt` beside them.

`internal/web/dist` is embedded with `go:embed` and is not committed, so the
frontend has to exist before any build starts. The `before` hook runs `npm ci`
and `npm run build` in `web/` — `npm ci`, not `make web`'s `npm install`, so a
release embeds the lockfile's tree. **The release machine needs Node and npm**,
not just Go.

**Nothing is signed.** `checksums.txt` ships beside the archives in the same
release, so it detects a corrupted download and not a tampered one: whoever can
replace an asset can replace the checksums too. Signing the archives (cosign
keyless, or minisign) is **undecided**; until it is settled, do not tell anyone
the checksums prove provenance.

### Cutting one

```bash
make release-snapshot   # local dry run: builds everything, publishes nothing
git tag v0.3.0
git push origin v0.3.0  # .github/workflows/release.yml does the rest
```

The release is created as a **draft**. Write the notes, then publish it by hand.
goreleaser generates a changelog from the commit log, dropping `docs:`, `test:`
and `chore(deps):`.

A tag matching `v*` is the only trigger. There is no manual dispatch, and the
release workflow does not wait on `test.yml` — **push the tag only from a commit
whose test run is green**.

### When a release run fails

The tag is already on the remote and may already have been fetched, and a
partial draft release may exist. Re-running the same tag fails on "release
already exists". Clean up by hand: delete the draft release, delete the tag
locally and on the remote, then tag the fixed commit with the **next** version
rather than reusing the failed one. Tags with a `release` concurrency group run
one at a time, so pushing two together serialises rather than races.

### Homebrew

**Decided: no tap.** A tap is a second repository to keep alive, and the audience
for ade is already comfortable with `go install`-shaped tools. The README says
so rather than leaving people looking for one.

If that changes, goreleaser's `brews:` block writes the formula; it needs a
`homebrew-tap` repository and a token that can push to it.

### `go install`

Does not work, and will not:

```
go install github.com/owenrumney/ade/cmd/ade@latest   # fails
```

`internal/web/dist` is not committed, so the `go:embed` fails on a fresh module
download. Install from a checkout with `make install`, or take a release archive.

## Windows — **not attempted**

ade has never been run on Windows. The sandbox, the `PATH` handling in the
browser host, and the Keychain branch all assume macOS or Linux. This is not a
"coming soon"; it is a "no".

## The window

`make app-install` builds `ADE.app` and puts it in /Applications. That is the
whole distribution story for the front end: nothing publishes it, and a release
archive does not carry it.

## The sandbox image

`.github/workflows/sandbox-image.yml` publishes `ghcr.io/owenrumney/ade-agent` on
every push to `main` that touches `sandbox/`, tagged `latest` and by commit sha.
The binary pulls `:latest` on first use. It is **not** tied to a release tag, so
an old ade can find a newer image.

## The Firefox extension

`make extension-sign` uploads to addons.mozilla.org on the **unlisted** channel
and gets a signed `.xpi` back. It needs `WEB_EXT_API_KEY` and
`WEB_EXT_API_SECRET`, so it is a maintainer step; a contributor verifies an
extension change with `make extension` instead.

AMO refuses a version it has signed before, so bump `version` in
`extension/manifest.json` before signing again. Nothing is listed or published —
the upload is only how Mozilla signs it.

## Before a release, and before publication

- `squealer --everything --config-file .squealer.yaml .` — CI does this on every
  push, but a release is worth one by hand.
- `go-licenses report ./...` — everything in the Go tree is currently MIT,
  Apache-2.0, BSD-3-Clause or ISC.
- `npx license-checker --summary` in `web/` and `app/` — no GPL or AGPL. `web/`
  pulls `lightningcss` (MPL-2.0) and `caniuse-lite` (CC-BY-4.0), both build-time
  only and neither shipped.

## Known gaps

- **gosec is not a gate.** `.github/workflows/scan.yml` runs it with `-no-fail`
  and uploads SARIF. 29 findings predate the workflow, mostly G702/G703/G704
  taint analysis over a tool whose job is to run commands and walk paths. They
  need triage before a new finding can be made to fail the build.
- **`unused` is disabled in `.golangci.yml`.** Seven parked symbols in
  `internal/web` — `pictureBlocks`, `prettyInput`, `ticketIDRE`, `callSubagent`,
  `startSide`, and the `rowMu`/`rowPath` fields. Use them or delete them, then
  turn the linter back on.
