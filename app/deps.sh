#!/usr/bin/env bash
# Makes sure Electron is here, runnable, and built for the machine running
# it. Two things go wrong and neither says so on its own:
#
#   - the npm shim exists whether or not the 100 MB binary downloaded,
#     which is what `ignore-scripts` leaves behind;
#   - an ade session in a Linux container shares this checkout with the
#     Mac it is on, so an install done in one leaves a binary the other
#     cannot exec. That is `spawn ENOEXEC`, from a stack trace in npm.
set -euo pipefail
cd "$(dirname "$0")"

# binary is the path electron reports, or nothing. Its index.js throws
# "failed to install correctly" when the postinstall did not finish, so
# nothing here is the ordinary way a half-install shows up.
binary() {
  node -e 'try{process.stdout.write(require("electron"))}catch{}' 2>/dev/null
}

# built_for is the platform the downloaded binary is for, read from the
# file itself rather than from whatever wrote it.
built_for() {
  local at magic
  at="$1"
  [ -e "$at" ] || return 1
  magic=$(head -c 4 "$at" | od -An -tx1 | tr -d ' \n')
  case "$magic" in
    7f454c46) echo linux ;;
    cffaedfe|cefaedfe|cafebabe) echo darwin ;;
    4d5a*) echo win32 ;;
    *) echo unknown ;;
  esac
}

ours() {
  case "$(uname -s)" in
    Darwin) echo darwin ;;
    Linux) echo linux ;;
    *) echo unknown ;;
  esac
}

# The editor is pure JavaScript, so it installs the same way everywhere
# and none of the Electron care below applies to it. Its scripts are not
# run: it has none, and running them here would run Electron's.
if [ ! -d node_modules/monaco-editor ]; then
  echo "installing the editor (monaco, about 80 MB unpacked, once)…"
  npm install --no-audit --no-fund --ignore-scripts
fi

at=$(binary)
want=$(ours)

# An ade session is Linux over the Mac's own files, so installing here
# leaves that Mac an Electron it cannot exec. Refuse by default: the
# person at the keyboard should not have to re-download 100 MB because
# an agent ran a build. ADE_CROSS_INSTALL=1 to mean it.
if [ "$want" = linux ] && [ -d /Users ] && [ "${ADE_CROSS_INSTALL:-}" != 1 ]; then
  if [ -n "$at" ] && [ -e "$at" ] && [ "$(built_for "$at")" = darwin ]; then
    echo "a Mac's Electron is installed here and this is a Linux session; leaving it alone" >&2
    exit 0
  fi
  cat >&2 <<'WHY'
This is a Linux session over a Mac's checkout, so installing Electron
here would leave the Mac a binary it cannot exec.

Use `make app-shots` to look at the window, which needs no Electron. To
install anyway, knowing the next `make app` on the Mac re-downloads:

    ADE_CROSS_INSTALL=1 make app-deps

WHY
  exit 1
fi

# The packager came after Electron: a good Electron says nothing about it, or its version.
if [ -n "$at" ] && [ -e "$at" ]; then
  got=$(built_for "$at" || echo missing)
  if [ "$got" != "$want" ] && [ "$got" != unknown ]; then
    echo "the Electron here was built for $got and this is $want; installing again…" >&2
    rm -rf node_modules package-lock.json
  elif npm ls @electron/packager >/dev/null 2>&1; then
    exit 0
  fi
fi

# Electron's installer exits 0 and silently downloads nothing when this
# is set, so the postinstall looks like it worked. Unset it here rather
# than asking: whatever it is for, it is not for this.
if [ -n "${ELECTRON_SKIP_BINARY_DOWNLOAD:-}" ] ||
   [ -n "${npm_config_electron_skip_binary_download:-}" ]; then
  echo "ELECTRON_SKIP_BINARY_DOWNLOAD is set, which is why nothing downloads; ignoring it here"
  unset ELECTRON_SKIP_BINARY_DOWNLOAD
  unset npm_config_electron_skip_binary_download
  export npm_config_electron_skip_binary_download=
fi

if [ -z "$at" ] || [ ! -e "$at" ]; then
  echo "installing Electron into app/node_modules (about 100 MB, once)…"
fi
npm install --no-audit --no-fund

# good reports whether a binary this machine can run is now here.
good() {
  local at
  at=$(binary)
  [ -n "$at" ] && [ -e "$at" ] && [ "$(built_for "$at")" = "$want" ]
}

good && exit 0

# The binary arrives through electron's postinstall, and npm has two ways
# of not running one. Newer npm holds every install script behind an
# approval list; older npm has ignore-scripts. Approving is a no-op on an
# npm with no such command, so it is tried rather than detected: `npm
# help` exits 0 for a command it does not have, so there is nothing
# honest to detect with.
npm install-scripts approve electron >/dev/null 2>&1 || true

# What electron's own error asks for, and the only reliable fix: npm
# will not re-run a postinstall for a package it thinks is already
# there, so the package goes and comes back.
echo "electron is half-installed; fetching its binary…"
rm -rf node_modules/electron
npm install --no-audit --no-fund --foreground-scripts

good && exit 0

# A rebuild as the last go, for an npm that kept the package anyway.
npm rebuild electron --foreground-scripts || true

good && exit 0

# Still nothing, and every attempt said "Cache hit" in under two
# seconds: the cached zip is the problem, not the download. An install
# cut off partway leaves a truncated one, and extracting it yields some
# of the archive and no error. force_no_cache makes it fetch again.
echo "the cached download looks truncated; fetching it again…"
rm -rf node_modules/electron
force_no_cache=true npm install --no-audit --no-fund --foreground-scripts

good && exit 0

# Last resort: electron's own installer, run directly so whatever it says
# is on screen. Through npm its output is swallowed and a silent exit 0
# is indistinguishable from a download.
if [ -f node_modules/electron/install.js ]; then
  echo "running electron's installer directly:"
  (cd node_modules/electron && node install.js) || true
fi

good && exit 0

cat >&2 <<'WHY'

Electron is installed but its binary is not, and nothing above fetched
one. Run `make app-doctor`, which checks each cause in turn.

The causes, in the order they are worth ruling out:

  1. A truncated zip in @electron/get's cache. Every attempt says
     "Cache hit" and finishes in seconds, and the extract yields part
     of the archive without an error. Empty the cache by hand:

         rm -rf ~/Library/Caches/electron     # macOS
         rm -rf ~/.cache/electron             # Linux

  2. ELECTRON_SKIP_BINARY_DOWNLOAD set anywhere. Its installer then
     exits 0 having downloaded nothing, so the postinstall looks fine.
     Check your shell, ~/.npmrc and .npmrc here.

  3. Newer npm holding install scripts for approval. `allowScripts` in
     app/package.json answers that, for the exact version installed.

  4. ignore-scripts, the older blanket switch:  npm config get ignore-scripts

WHY
exit 1
