#!/usr/bin/env bash
# Puts the packaged window in /Applications, replacing any ADE.app there.
set -euo pipefail
cd "$(dirname "$0")"

app=$(find "${ADE_APP_OUT:-$PWD/out}" -maxdepth 2 -name 'ADE.app' -print -quit)
[ -n "$app" ] || { echo "no ADE.app: run make app-package first" >&2; exit 1; }
osascript -e 'quit app "ADE"' >/dev/null 2>&1 || true
rsync -a --delete "$app/" /Applications/ADE.app/
echo "installed /Applications/ADE.app"

# The editor this replaces linked its cli onto PATH from inside the bundle.
link=/usr/local/bin/ade-editor
if [ -L "$link" ] && [ ! -e "$link" ]; then
  rm -f "$link" && echo "removed $link, which pointed into the old editor"
fi
