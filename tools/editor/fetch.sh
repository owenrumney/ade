#!/usr/bin/env bash
# Fetch what the editor build needs: VSCodium's scripts at a pinned commit,
# vscode at the commit VSCodium pins, and node 24 for the build itself.
set -euo pipefail
. "$(dirname "$0")/common.sh"

if [ ! -d "$codium/.git" ]; then
  git clone -q https://github.com/VSCodium/vscodium "$codium"
fi
git -C "$codium" fetch -q --depth 1 origin "$VSCODIUM_COMMIT"
git -C "$codium" checkout -q "$VSCODIUM_COMMIT"
# A go.mod of its own keeps vscode's Go fixtures out of ade's `go test ./...`.
printf 'module ignored\n' > "$codium/go.mod"

if [ ! -x "$editor/node24/node_modules/.bin/node" ]; then
  npm install --prefix "$editor/node24" --no-audit --no-fund --silent node@24.18.0
fi

if [ ! -d "$vscode/.git" ]; then
  (cd "$codium" && . ./get_repo.sh) || true
fi
# A global git-lfs filter with no git-lfs installed kills the checkout; vscode
# only tracks the Copilot extension's sqlite files with it, and that gets removed.
git -C "$vscode" config filter.lfs.process ""
git -C "$vscode" config filter.lfs.smudge cat
git -C "$vscode" config filter.lfs.clean cat
git -C "$vscode" config filter.lfs.required false
git -C "$vscode" checkout -q -f "$MS_COMMIT"
echo "vscode at $(git -C "$vscode" rev-parse --short HEAD) ($MS_TAG)"
