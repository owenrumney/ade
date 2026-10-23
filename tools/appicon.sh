#!/usr/bin/env bash
# Rebuilds app/icon.icns from editor/icon/ade.svg. Run by hand after the
# artwork changes: the .icns is committed so a build needs no imagemagick.
set -euo pipefail
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/ade.iconset"
for sz in 16 32 64 128 256 512; do
  magick -background none editor/icon/ade.svg -resize "${sz}x${sz}" "$work/ade.iconset/icon_${sz}x${sz}.png"
  magick -background none editor/icon/ade.svg -resize "$((sz * 2))x$((sz * 2))" "$work/ade.iconset/icon_${sz}x${sz}@2x.png"
done
iconutil -c icns "$work/ade.iconset" -o app/icon.icns
echo "wrote app/icon.icns"
