#!/bin/sh
set -eu

IFS= read -r GH_TOKEN || true
export GH_TOKEN
if [ -z "$GH_TOKEN" ]; then
  echo "ade-provision: no GitHub token, so nothing can be cloned" >&2
  exit 2
fi
gh auth setup-git
exec gh repo clone "$1" "$2"
