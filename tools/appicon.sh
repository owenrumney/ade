#!/usr/bin/env bash
# Rebuilds app/icon.icns from app/icon/ade.svg. Run by hand after the
# artwork changes: the .icns is committed so a build needs no browser.
set -euo pipefail
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/ade.iconset"

# A browser, not imagemagick: magick's svg delegate is rsvg-convert, and
# without it the gradient and every stroke render as a black square.
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
[ -n "${ADE_CHROME:-}" ] || {
  echo "no chromium found; set ADE_CHROME to a browser binary" >&2; exit 1; }
export ADE_CHROME
[ -d tools/shots/node_modules ] || (cd tools/shots && npm install --silent)

for sz in 16 32 64 128 256 512; do
  node tools/shots/icon.mjs app/icon/ade.svg "$work/ade.iconset/icon_${sz}x${sz}.png" "$sz"
  node tools/shots/icon.mjs app/icon/ade.svg "$work/ade.iconset/icon_${sz}x${sz}@2x.png" "$((sz * 2))"
done
iconutil -c icns "$work/ade.iconset" -o app/icon.icns
echo "wrote app/icon.icns"
