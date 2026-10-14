#!/usr/bin/env bash
# Install the built extension into the VS Code server on another machine, so a
# Remote-SSH window there has ade in it. Nothing publishes the extension, so a
# marketplace install is not available and the .vsix is carried over by hand.
#
# Usage: tools/vscode-remote.sh <ssh host> <vsix>
set -euo pipefail

host=${1:?usage: vscode-remote.sh <ssh host> <vsix>}
vsix=${2:?usage: vscode-remote.sh <ssh host> <vsix>}
[ -f "$vsix" ] || { echo "no $vsix: run make vscode-package first" >&2; exit 1; }

# The newest server, by the directory's own time: a host that has connected to
# two VS Code versions keeps both, and the window will be the later one.
remote_cli=$(ssh "$host" 'ls -dt ~/.vscode-server/cli/servers/Stable-*/server/bin/code-server 2>/dev/null | head -1')
if [ -z "$remote_cli" ]; then
  echo "$host has no VS Code server. Open a Remote-SSH window there once, which installs it." >&2
  exit 1
fi

at=$(ssh "$host" 'mktemp -d')
trap 'ssh "$host" "rm -rf $at" >/dev/null 2>&1 || true' EXIT
scp -q "$vsix" "$host:$at/ade.vsix"

# --force replaces the same version, which a .vsix built from a checkout always
# is: the manifest version only moves on a release.
ssh "$host" "'$remote_cli' --install-extension '$at/ade.vsix' --force"
echo "installed into $host. A Remote-SSH window there picks it up on its next open;"
echo "one already open needs Developer: Reload Window."
