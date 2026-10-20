#!/usr/bin/env bash
# Compile and run the editor from source, with its own data dirs. Pass -c to
# recompile the client after a source change. Any other args go to the editor.
set -euo pipefail
. "$(dirname "$0")/common.sh"

editor_check

compile=no
if [ "${1:-}" = "-c" ]; then compile=yes; shift; fi
cd "$vscode"
# A client-only compile leaves out/main.js with no extensions behind it.
if [ ! -f out/main.js ] || [ ! -f extensions/git/out/main.js ]; then
  npm run gulp compile-client compile-extensions compile-extension-media
elif [ "$compile" = yes ]; then
  npm run gulp compile-client
fi
"$root/tools/editor/sync.sh"

data="$HOME/Library/Caches/ade/editor-dev"
mkdir -p "$data/data" "$data/ext"
# The socket in the data dir has a 103 char ceiling, so the dir stays short.
# HOME is left alone: a fake one makes macOS put up a keychain dialog.
exec ./scripts/code.sh --user-data-dir="$data/data" --extensions-dir="$data/ext" \
  ${CDP:+--remote-debugging-port=$CDP} \
  --disable-updates --skip-welcome --skip-release-notes --disable-workspace-trust "$@"
