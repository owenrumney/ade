#!/usr/bin/env bash
# Everything `make app` needs, checked one at a time, so a failure is one
# round trip rather than three. Prints a verdict per line and exits
# non-zero if any of them is bad.
cd "$(dirname "$0")"

bad=0
say() { printf '%-28s %s\n' "$1" "$2"; }
no() { bad=1; say "$1" "NO   — $2"; }
ok() { say "$1" "ok   $2"; }

magic() {
  [ -e "$1" ] || return 1
  head -c 4 "$1" | od -An -tx1 | tr -d ' \n'
}

say "where" "$(pwd)"
say "platform" "$(uname -s) $(uname -m)"
say "you are" "$(id -un) ($(id -u))"

# A session runs as root over the same files, so anything it installed is
# root's and the person at the keyboard cannot replace it.
for d in node_modules node_modules/electron node_modules/electron/dist; do
  [ -d "$d" ] || continue
  owner=$(ls -ld "$d" | awk '{print $3}')
  if [ ! -w "$d" ]; then
    no "$d" "owned by $owner and not writable by you; sudo rm -rf app/node_modules"
  elif [ "$owner" != "$(id -un)" ] && [ "$(id -u)" != 0 ]; then
    no "$d" "owned by $owner, not you; an agent session installed it"
  fi
done
say "node" "$(node --version 2>&1)"
say "npm" "$(npm --version 2>&1)"

# The bundles the window loads.
for f in dist/main.js dist/preload.js dist/renderer.js; do
  if [ -s "$f" ]; then ok "$f" "$(wc -c <"$f" | tr -d ' ') bytes"
  else no "$f" "not built; run make app-build"; fi
done

# The page and its stylesheets.
for f in renderer/index.html renderer/theme.css package.json; do
  [ -s "$f" ] && ok "$f" "" || no "$f" "missing"
done

# Electron: the package, the binary, and whether this machine can run it.
at=$(node -e 'try{process.stdout.write(require("electron"))}catch{}' 2>/dev/null)
if [ -z "$at" ] && [ -d node_modules/electron/dist ]; then
  # Unzipped but never finished: extractFile writes path.txt last, so a
  # dist with no path.txt is an extract that was cut off, usually by two
  # installs racing on this directory.
  no "electron install" "half-extracted — dist/ exists, path.txt does not; make app-deps redoes it"
elif [ -z "$at" ]; then
  no "electron package" "not installed; run make app-deps"
elif [ ! -e "$at" ]; then
  no "electron binary" "the shim points at $at, which is not there"
else
  case "$(magic "$at")" in
    cffaedfe|cefaedfe|cafebabe) built=darwin ;;
    7f454c46) built=linux ;;
    4d5a*) built=win32 ;;
    *) built=unknown ;;
  esac
  case "$(uname -s)" in Darwin) want=darwin ;; Linux) want=linux ;; *) want=unknown ;; esac
  if [ "$built" = "$want" ]; then
    ok "electron binary" "$built, $(du -sh "$(dirname "$at")" 2>/dev/null | cut -f1)"
  else
    no "electron binary" "built for $built, this is $want; make app-deps reinstalls"
  fi
fi

# The quietest cause: electron's installer exits 0 having done nothing.
skip="${ELECTRON_SKIP_BINARY_DOWNLOAD:-${npm_config_electron_skip_binary_download:-}}"
if [ -n "$skip" ]; then
  no "skip-binary-download" "ELECTRON_SKIP_BINARY_DOWNLOAD=$skip — its installer exits 0 and fetches nothing"
else
  ok "skip-binary-download" "unset"
fi
for rc in "$HOME/.npmrc" .npmrc ../.npmrc; do
  [ -f "$rc" ] && grep -iq 'electron_skip_binary_download\|ignore-scripts' "$rc" &&
    no "$rc" "sets $(grep -i 'electron_skip_binary_download\|ignore-scripts' "$rc" | head -1)"
done

# npm's install-script gate, which is what stops the binary downloading.
if grep -q '"allowScripts"' package.json 2>/dev/null; then
  ok "install scripts" "allowScripts in package.json: $(grep -A2 '"allowScripts"' package.json | grep -o 'electron@[0-9.]*' | head -1)"
else
  say "install scripts" "note — no allowScripts; newer npm may hold electron's postinstall"
fi
[ "$(npm config get ignore-scripts 2>/dev/null)" = "true" ] &&
  no "ignore-scripts" "is true, so no postinstall runs"

# The version allowScripts names has to be the version installed, or the
# approval does not apply to it.
if [ -f node_modules/electron/package.json ]; then
  have=$(node -p 'require("./node_modules/electron/package.json").version' 2>/dev/null)
  named=$(grep -o 'electron@[0-9.]*' package.json | head -1 | cut -d@ -f2)
  if [ -n "$named" ] && [ "$named" != "$have" ]; then
    no "allowScripts version" "names $named, installed is $have"
  elif [ -n "$named" ]; then
    ok "allowScripts version" "$have"
  fi
fi

echo
if [ "$bad" = 0 ]; then
  echo "all good — if make app still fails, the error is from Electron itself:"
  echo "    cd app && ./node_modules/.bin/electron . 2>&1 | head -40"
else
  echo "fix the NO lines above, then make app"
fi
exit "$bad"
