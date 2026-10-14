#!/usr/bin/env bash
# Copy the built ade extension in as a builtin. Only what runs: a tsconfig in
# the tree would make vscode's gulp try to compile it.
set -euo pipefail
. "$(dirname "$0")/common.sh"

(cd "$root/vscode" && npm install --no-audit --no-fund --ignore-scripts >/dev/null && npm run build >/dev/null)
rsync -a --delete --include='package.json' --include='LICENSE' \
  --include='dist/***' --include='media/***' --include='schema/***' \
  --exclude='*' "$root/vscode/" "$vscode/extensions/ade/"
echo "synced extensions/ade"
