#!/usr/bin/env bash
# Serves the window without opening one and checks the page and its bundle
# come back. The host picks its own port, so the url is read from its log.
set -euo pipefail
cd "$(dirname "$0")/.."

log=$(mktemp)
trap 'rm -f "$log"; [ -n "${host:-}" ] && kill "$host" 2>/dev/null || true' EXIT

ADE_WINDOW_SERVE=1 go run ./cmd/adewindow >"$log" 2>&1 &
host=$!

for _ in $(seq 1 60); do
  at=$(grep -o 'http://127.0.0.1:[0-9]*' "$log" | head -1 || true)
  [ -n "$at" ] && break
  sleep 0.5
done
[ -n "${at:-}" ] || { echo "the host never said where it was listening:" >&2; cat "$log" >&2; exit 1; }

for path in / /dist/renderer.js /theme.css /host/keys; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "$at$path")
  [ "$code" = 200 ] || { echo "$path answered $code" >&2; exit 1; }
done
# The page has to actually carry the bundle, not merely exist.
curl -s "$at/" | grep -q 'renderer.js' || { echo "the page does not load the bundle" >&2; exit 1; }
echo "the window serves: page, bundle, styles and keys all answered"
