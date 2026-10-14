#!/usr/bin/env bash
# Put the packaged, signed app where macOS looks for it, and its cli on PATH.
# Replaces an ADE.app already there: it is this machine's own build.
set -euo pipefail
. "$(dirname "$0")/common.sh"

app="$codium/VSCode-darwin-${VSCODE_ARCH}/${APP_NAME}.app"
[ -d "$app" ] || { echo "no $app: run make editor-package first" >&2; exit 1; }
osascript -e "quit app \"${APP_NAME}\"" >/dev/null 2>&1 || true
rsync -a --delete "$app/" "/Applications/${APP_NAME}.app/"
echo "installed /Applications/${APP_NAME}.app"

cli="/Applications/${APP_NAME}.app/Contents/Resources/app/bin/${BINARY_NAME}"
if [ -w /usr/local/bin ]; then
  ln -sf "$cli" "/usr/local/bin/${BINARY_NAME}"
  echo "${BINARY_NAME} is on PATH via /usr/local/bin"
else
  echo "put ${BINARY_NAME} on PATH yourself: ln -s \"$cli\" ~/.local/bin/${BINARY_NAME}"
fi
