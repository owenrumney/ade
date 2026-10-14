# Shared by the editor scripts. Sourced, under bash.
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
editor="$root/editor"
codium="$editor/vscodium"
vscode="$codium/vscode"
. "$editor/env.sh"
# The build wants the node in vscode's .nvmrc, not whatever brew has.
export PATH="$editor/node24/node_modules/.bin:$PATH"
