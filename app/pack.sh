#!/usr/bin/env bash
# Builds the window into ADE.app: the Go host, the bundles it serves, and
# an Info.plist. Signed with an identity of its own, because the cli's is
# pinned to the cli and two programs may not share one.
#
# The bundle id is the one Electron's packager used. macOS keys every
# permission to that string, so renaming it starts from no permission.
set -euo pipefail
cd "$(dirname "$0")"

out="${ADE_APP_OUT:-$PWD/out}"
app="$out/ADE.app"
id=uk.co.owenrumney.ade-window
version=$(node -p 'require("./package.json").version' 2>/dev/null || echo 0.0.0)

[ -d dist ] || { echo "app/dist is missing: run make app-build" >&2; exit 1; }

rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources/ade/app"

# The host finds these at ../Resources/ade/app, which is what assets()
# in cmd/adewindow looks for first.
cp -R dist "$app/Contents/Resources/ade/app/dist"
cp -R media "$app/Contents/Resources/ade/app/media"
mkdir -p "$app/Contents/Resources/ade/app/renderer"
cp renderer/*.html renderer/*.css "$app/Contents/Resources/ade/app/renderer/"
cp icon.icns "$app/Contents/Resources/ade.icns"

# Built here rather than installed, so packaging never picks up a stale
# binary from someone's GOPATH.
CGO_ENABLED=1 go build -trimpath \
  -ldflags "-X main.version=$version" \
  -o "$app/Contents/MacOS/ADE" ../cmd/adewindow

cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>ADE</string>
  <key>CFBundleDisplayName</key><string>ADE</string>
  <key>CFBundleExecutable</key><string>ADE</string>
  <key>CFBundleIdentifier</key><string>$id</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleIconFile</key><string>ade</string>
  <key>CFBundleShortVersionString</key><string>$version</string>
  <key>CFBundleVersion</key><string>$version</string>
  <key>LSMinimumSystemVersion</key><string>12.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSSupportsAutomaticGraphicsSwitching</key><true/>
</dict>
</plist>
PLIST

ADE_BUNDLE_ID="$id" ../tools/sign.sh "$app"
echo "built $app"
