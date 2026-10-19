#!/usr/bin/env bash
# Build the remote extension host — the editor server a window on another
# machine runs — the way VSCodium's build.sh does, and tar it under the name
# product.json's serverDownloadUrlTemplate asks for.
#
# Linux only, and the arch has to be the runner's: the native modules under
# remote/ are built here, so a Mac cannot cross-build either of them. See
# .git/ade/docs/remote-sessions-vscodium.md.
#
# Usage: tools/editor/reh.sh [arm64|x64]
set -euo pipefail

if [ "$(uname -s)" != "Linux" ]; then
  echo "reh.sh builds a linux server, so it runs on linux: use make editor-reh in CI" >&2
  exit 1
fi

# Before env.sh, which defaults these to the Mac build's and leaves what is
# already set alone.
export OS_NAME="linux"
export VSCODE_ARCH="${1:-$(uname -m | sed -e s/aarch64/arm64/ -e s/x86_64/x64/)}"
export CI_BUILD="yes"
export SHOULD_BUILD_REH="yes"
export SHOULD_BUILD_REH_WEB="no"

. "$(dirname "$0")/common.sh"

# ade's own extension goes in as a builtin, so the server carries it.
"$root/tools/editor/sync.sh"

cd "$vscode"
export VSCODE_PUBLISH_COUNTER=1
# The -ci variant only packages: it expects an out-build a desktop build left
# behind. The plain task compiles first, then runs the same packaging.
npm run gulp "vscode-reh-linux-${VSCODE_ARCH}-min"

# The same name and shape prepare_assets.sh gives it: no wrapping directory,
# because that is what the resolver's install unpacks.
app_lc=$(echo "$APP_NAME" | tr '[:upper:]' '[:lower:]')
tarball="$codium/assets/${app_lc}-reh-linux-${VSCODE_ARCH}-${RELEASE_VERSION}.tar.gz"
mkdir -p "$codium/assets"
(cd "$codium/vscode-reh-linux-${VSCODE_ARCH}" && tar czf "$tarball" .)
echo "built: $tarball"
