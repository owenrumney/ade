#!/usr/bin/env bash
# Copy the role and agent prompts in from the private repository. They name
# people and employers, so they are not in this one.
set -euo pipefail

cd "$(dirname "$0")/.."
from="${ADE_PRIVATE:-../ade-private}"

[ -d "$from" ] || {
  echo "no $from: clone git@github.com:owenrumney/ade-private.git beside this" >&2
  echo "checkout, or point ADE_PRIVATE at it." >&2
  exit 1
}

copied=0
for kind in roles agents; do
  src="$from/internal/$kind/defaults"
  [ -d "$src" ] || continue
  for file in "$src"/*.md; do
    [ -e "$file" ] || continue
    cp "$file" "internal/$kind/defaults/"
    copied=$((copied + 1))
  done
done

[ "$copied" -gt 0 ] || { echo "no .md prompts under $from" >&2; exit 1; }
echo "copied $copied prompts from $from; they are gitignored here"
