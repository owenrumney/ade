#!/usr/bin/env bash
# Builds the window into something launchable: ADE Window.app on a Mac,
# a directory anywhere else. Signed with an identity of its own, because
# the cli's is pinned to the cli and two programs may not share one.
set -euo pipefail
cd "$(dirname "$0")"

out="${ADE_APP_OUT:-$PWD/out}"

# The panels are the extension's own pages, served from vscode/ at run
# time. They are not inside the window, so they are staged beside it and
# main.ts reads them from there when packaged.
stage="$PWD/out/stage"
rm -rf "$stage"
mkdir -p "$stage/ade/vscode"
[ -d ../vscode/dist ] || { echo "vscode/dist is missing: run make app-build" >&2; exit 1; }
cp -R ../vscode/dist "$stage/ade/vscode/dist"
cp -R ../vscode/media "$stage/ade/vscode/media"
packager="./node_modules/.bin/electron-packager"
[ -x "$packager" ] || packager="./node_modules/@electron/packager/bin/electron-packager.js"
[ -e "$packager" ] || {
  echo "the packager is missing: run make app-deps" >&2
  exit 1
}

# Only what the window runs: the bundles, and the page that loads them.
# node_modules is 113 MB of monaco source that esbuild already inlined,
# and main.js requires nothing outside electron and node itself.
node "$packager" . "ADE Window" \
  --out="$out" \
  --overwrite \
  --app-bundle-id=uk.co.owenrumney.ade-window \
  --app-version="$(node -p 'require("./package.json").version')" \
  --icon=icon.icns \
  --ignore='/node_modules($|/)' \
  --ignore='^/(src|out)($|/)' \
  --ignore='^/renderer/.*\.(ts|map)$' \
  --ignore='\.(sh|md|icns)$' \
  --ignore='^/smoke\.js$' \
  --ignore='^/dist/shots($|/)' \
  --extra-resource="$stage/ade" \
  --prune=true

app=$(find "$out" -maxdepth 2 -name '*.app' -print -quit)
if [ -n "$app" ]; then
  ADE_BUNDLE_ID=uk.co.owenrumney.ade-window ../tools/sign.sh "$app"
  echo "built $app"
  exit 0
fi
echo "built $(find "$out" -maxdepth 1 -mindepth 1 -type d -print -quit)"
