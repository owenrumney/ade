# app — ADE.app

ade's own window: an Electron shell over a renderer that talks to the
launcher and the session servers over HTTP, with no extension host in
between. It replaces the VSCodium fork as `/Applications/ADE.app`. The
reasoning is in `.git/ade/docs/rethink-workflow-and-uk.md`.

`ade .` opens it when it is installed. `"front": "editor"` in
`~/.ade/settings.json`, or `ADE_FRONT=editor`, opens the editor instead.

## Why Electron

The keyboard. The shell sees every keystroke through
`before-input-event`, matches it against one table, and hands the window a
named command; nothing else gets a say. A browser cannot give up ⌘K, and
`globalShortcut` would take it from every other application.

## Running it

```
make app            # the window from source; installs Electron the first time
make app-test       # type check and tests
make app-package    # app/out/…/ADE.app, signed
make app-install    # app-package, then into /Applications. Quits ADE first
make app-smoke      # starts the real window headless and checks it drew
make app-shots      # pictures, into the outbox
```

`app-smoke` is the one that proves the bundles load together under a
real Electron, and that ⌘K travels from the key through the table to the
window. On a machine with no display, put `xvfb-run -a` in front of it.

**`node_modules` here is platform-specific and this checkout is shared.**
An ade session runs in a Linux container over the same files as the Mac
it is on, so an `npm install` done in one leaves an Electron the other
cannot exec — `spawn ENOEXEC`, reported as a stack trace from npm.
`deps.sh` reads the binary's own magic bytes and reinstalls when it was
built for the other platform. Nothing to do by hand, but the first
`make app` after a session worked here will spend a minute on it.

Electron is the only dependency, so that is the only thing to reinstall.
The bundles are built with the editor extension's esbuild
(`vscode/node_modules`), which ships every platform's binary and picks at
run time, so it does not have the problem.

**Its binary arrives through a postinstall**, and npm has two ways of not
running one: newer npm holds install scripts behind an approval list
(`allowScripts` in `package.json` is the standing answer), older npm has
`ignore-scripts`. When the postinstall does not finish, `require("electron")`
throws *failed to install correctly* and the package sits there looking
installed, so npm will not run the script again for it. `deps.sh`
removes `node_modules/electron` and installs that one package back,
which is what Electron's own error asks for.

**The quietest cause is `ELECTRON_SKIP_BINARY_DOWNLOAD`.** Set anywhere
— a shell, `~/.npmrc`, CI config — `install.js` exits 0 at its fourteenth
line having downloaded nothing, so the postinstall reports success and
leaves no binary. `deps.sh` unsets it for its own install.

**`make app-doctor` when the window will not start.** It checks the
bundles, the page, the Electron binary and what platform it was built
for, that skip variable and any npmrc setting it, the install-script
gate, and whether the version `allowScripts` names is the version
installed. One line each, and it names the command to run next.

`app-shots` drives the page in a browser so it can be photographed on a
machine with no Electron. It is a harness, not a way to run ade: under it
the keyboard is the browser's.

## What is here

| | |
| --- | --- |
| `src/keymap.ts` | the whole keyboard, as a table. `~/.ade/keys.json` overrides it |
| `src/main.ts` | the window, the key handler, the menu, every reach to ade |
| `src/localnetwork.ts` | the LAN dial at start that makes macOS ask for Local Network |
| `src/preload.ts` | the only bridge the renderer gets |
| `renderer/app.ts` | the window: activity bar, sidebar, editor area, status bar |
| `renderer/real.ts` | what the launcher and sessions answer, as places and rows |
| `renderer/panels.ts` | the extension's panels, hosted on `ade-webview://` |
| `renderer/prpage.ts` | what a pull request's page offers to do |
| `renderer/editor.ts` | monaco, loaded the first time a file is opened |
| `renderer/omnibox.ts` | the large centred box, and what it searches |
| `renderer/theme.css` | ade's own colour names, no longer `--vscode-*` |

## Rebinding

`~/.ade/keys.json`, laid over the defaults:

```json
[
  { "keys": "mod+shift+k", "command": "omnibox" },
  { "keys": "mod+k", "command": "" }
]
```

`mod` is ⌘ on a Mac and Ctrl elsewhere. An empty command unbinds.

## Editing

A file opened from Code is editable, and ⌘S writes it back through the
session (`PUT /api/source`). Only a file already in the checkout, never
under `.git`, and the save is refused if the file changed on disk since it
was opened. Reopen it to take the new copy.
