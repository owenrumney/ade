# Shared by the editor scripts. Sourced, under bash.
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
editor="$root/editor"
# The build tree is gigabytes and its npm ci is ten minutes, so every worktree
# shares the main checkout's. $editor stays this worktree's own patches.
base=$(git -C "$root" rev-parse --git-common-dir 2>/dev/null || echo "")
case "$base" in
  "") base="$root" ;;
  /*) base=$(cd "$base/.." && pwd) ;;
  *) base=$(cd "$root/$base/.." && pwd) ;;
esac
shared="$base/editor"
codium="$shared/vscodium"
vscode="$codium/vscode"
. "$editor/env.sh"
# The build wants the node in vscode's .nvmrc, not whatever brew has.
export PATH="$shared/node24/node_modules/.bin:$PATH"

# editor_stamp is what this worktree would prepare the shared tree into: its
# patches, its pins and its product overlay.
editor_stamp() {
  (cd "$editor" && find patches resources env.sh product.json -type f 2>/dev/null |
    LC_ALL=C sort | xargs shasum) | shasum | cut -d' ' -f1
}

# editor_record says whose patches the shared tree now holds.
editor_record() {
  printf '%s %s\n' "$(editor_stamp)" "$root" > "$codium/.ade-prepared"
}

# editor_check refuses to build somebody else's patches as if they were yours.
editor_check() {
  if [ ! -d "$vscode/.git" ]; then
    echo "there is no editor build tree at $codium." >&2
    echo "run: make editor-fetch editor-prepare" >&2
    exit 1
  fi
  local got="" where=""
  if [ -r "$codium/.ade-prepared" ]; then
    read -r got where < "$codium/.ade-prepared"
  fi
  if [ "$got" = "$(editor_stamp)" ]; then
    return 0
  fi
  if [ -z "$got" ]; then
    echo "the shared editor tree at $codium has not been prepared." >&2
  elif [ "$where" = "$root" ]; then
    echo "the shared editor tree at $codium holds older patches of this checkout." >&2
  else
    echo "the shared editor tree at $codium holds $where's patches." >&2
  fi
  echo "run: make editor-prepare" >&2
  exit 1
}

# editor_lock holds the shared tree for one fetch or prepare at a time: two
# worktrees reaching for it at once would leave neither's patches in it.
editor_lock() {
  mkdir -p "$shared"
  local lock="$shared/.ade-lock" held
  until mkdir "$lock" 2>/dev/null; do
    held=$(cat "$lock/owner" 2>/dev/null || echo "another checkout")
    echo "waiting for the shared editor tree, held by $held…" >&2
    sleep 5
  done
  printf '%s\n' "$root" > "$lock/owner"
  trap 'rm -rf "'"$lock"'"' EXIT
}
