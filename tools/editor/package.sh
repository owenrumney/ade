#!/usr/bin/env bash
# Package the editor the way VSCodium's build.sh does on macOS, minus the rust
# cli. Leaves editor/vscodium/VSCode-darwin-arm64/ADE.app.
set -euo pipefail
. "$(dirname "$0")/common.sh"

"$root/tools/editor/sync.sh"
cd "$vscode"
export VSCODE_PUBLISH_COUNTER=1
npm run gulp vscode-min-prepack
rm -f .build/extensions/ms-vscode.js-debug/src/win32-app-container-tokens.*.node
npm run copy-policy-dto --prefix build
node build/lib/policies/policyGenerator.ts build/lib/policies/policyData.jsonc darwin
npm run gulp "vscode-darwin-${VSCODE_ARCH}-min-packing"
find "../VSCode-darwin-${VSCODE_ARCH}" -print0 | xargs -0 touch -c
echo "packaged: $codium/VSCode-darwin-${VSCODE_ARCH}/${APP_NAME}.app"

# Source maps are 30% of the app and nothing on a daily driver reads them.
find "$codium/VSCode-darwin-${VSCODE_ARCH}/${APP_NAME}.app/Contents/Resources/app/out" -name "*.map" -delete

# Signed with the same local certificate as the cli, so macOS keeps the app's
# permissions across rebuilds. Ad-hoc otherwise, with a note.
app="$codium/VSCode-darwin-${VSCODE_ARCH}/${APP_NAME}.app"
hash=$(security find-identity -p codesigning 2>/dev/null | awk -v name='"ADE Dev"' '$0 ~ name {print $2}' | tail -1)
if [ -n "$hash" ]; then
  codesign --force --deep --sign "$hash" "$app"
  codesign --verify --deep --strict "$app" && echo "signed $app as ADE Dev"
else
  echo "no ADE Dev certificate: $app is ad-hoc signed; run make sign-setup" >&2
fi
