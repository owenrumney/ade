#!/usr/bin/env bash
# Apply VSCodium's patches and ours, write product.json, and npm ci. Leaves the
# result as one baseline commit so `git diff` in the tree is an ade patch.
set -euo pipefail
. "$(dirname "$0")/common.sh"
editor_lock

cd "$vscode"
git reset -q --hard "$MS_COMMIT"
git clean -fdq -e node_modules -e .build -e out -e extensions/ade

rm -f "$codium/patches/user/"*.patch
cp "$editor/patches/"*.patch "$codium/patches/user/"

cd "$codium"
# VSCodium's helpers read args they are not given, so its script runs without -u.
set +u; . ./prepare_vscode.sh; set -u

cd "$vscode"
cp "$editor/resources/darwin/code.icns" resources/darwin/code.icns
jq -s '.[0] * .[1]' product.json "$editor/product.json" > product.json.ade
mv product.json.ade product.json

grep -qx 'extensions/ade/' .git/info/exclude || echo 'extensions/ade/' >> .git/info/exclude
git add -A
git -c user.email=ade@local -c user.name=ade commit --no-verify -qm "ADE BASELINE: vscodium patches, ade patches, product overlay"
editor_record
echo "prepared: $(git rev-parse --short HEAD) in $codium"
