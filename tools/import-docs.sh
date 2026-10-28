#!/usr/bin/env bash
# Move every checkout's .git/ade/docs into the designs store. Run it once per
# machine: ade reads designs from the store, and a second copy only drifts.
set -uo pipefail

DRY=0
CLEAR=0
ROOT="$HOME/code"
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY=1 ;;
    --clear) CLEAR=1 ;;
    -*) echo "usage: $0 [--dry-run] [--clear] [root]" >&2; exit 2 ;;
    *) ROOT="$arg" ;;
  esac
done

STORE="$HOME/.ade/designs"
LIST="$(mktemp)"
LEFT="$(mktemp)"
trap 'rm -f "$LIST" "$LEFT" "$LEFT.raw"' EXIT

command -v ade >/dev/null || { echo "no ade on PATH" >&2; exit 1; }
echo "store: $STORE"
[ "$DRY" = 1 ] && echo "dry run: nothing is written"
echo

# Docs dirs, any under a tmp path last so the canonical copy claims the name.
find "$ROOT" -maxdepth 6 -type d -path '*/.git/ade/docs' 2>/dev/null | while read -r d; do
  ls "$d"/*.md >/dev/null 2>&1 || continue
  case "$d" in */tmp/*) echo "1 $d" ;; *) echo "0 $d" ;; esac
done | sort | cut -d' ' -f2- > "$LIST"

while read -r docs; do
  [ -n "$docs" ] || continue
  repo="${docs%/.git/ade/docs}"
  echo "== $repo ($(ls "$docs"/*.md 2>/dev/null | wc -l | tr -d ' ') docs)"
  if [ "$DRY" = 1 ]; then
    for f in "$docs"/*.md; do echo "   would import $(basename "$f")"; done
    continue
  fi
  (cd "$repo" && ade designs import) 2>&1 | tee -a "$LEFT.raw" | sed 's/^/   /'
done < "$LIST"
grep '^left ' "$LEFT.raw" 2>/dev/null | sort -u > "$LEFT"

echo
echo "== images"
# A design references its pictures by plain name, so they must sit beside it.
for design in "$STORE"/*/design.md; do
  [ -e "$design" ] || continue
  dir=$(dirname "$design")
  grep -oE '\]\([^)/]+\.(png|jpg|jpeg|gif|svg|webp)\)' "$design" 2>/dev/null |
    sed -E 's/^\]\(//; s/\)$//' | sort -u | while read -r img; do
    [ -e "$dir/$img" ] && continue
    found=""
    while read -r src; do
      [ -e "$src/$img" ] && { found="$src/$img"; break; }
    done < "$LIST"
    if [ -z "$found" ]; then
      echo "   missing  $img  ($(basename "$dir"))"
    elif [ "$DRY" = 1 ]; then
      echo "   would copy $img -> $(basename "$dir")"
    else
      cp "$found" "$dir/$img" && echo "   copied   $img -> $(basename "$dir")"
    fi
  done
done

if [ -s "$LEFT" ]; then
  echo
  echo "== left behind: the name is taken by a different document"
  sed 's/^/   /' "$LEFT"
  echo "   Rename the original and run again, or move it by hand."
fi

if [ "$CLEAR" = 1 ] && [ "$DRY" = 0 ]; then
  echo
  echo "== clearing originals"
  # Only an original whose body is already in the store, so a document the
  # import left behind survives.
  python3 - "$STORE" < "$LIST" <<'PY'
import glob, os, re, sys
store = sys.argv[1]
fm = re.compile(rb"\A---\r?\n.*?\r?\n---\r?\n?", re.S)
body = lambda p: fm.sub(b"", open(p, "rb").read()).strip()
bodies = {body(d) for d in glob.glob(os.path.join(store, "*", "design.md"))}
kept = {os.path.basename(p) for p in glob.glob(os.path.join(store, "*", "*"))}
md = img = 0
for docs in [l.strip() for l in sys.stdin if l.strip()]:
    for f in glob.glob(os.path.join(docs, "*")):
        if not os.path.isfile(f):
            continue
        name = os.path.basename(f)
        if f.endswith(".md") and body(f) in bodies:
            os.remove(f); md += 1
        elif not f.endswith(".md") and name in kept:
            os.remove(f); img += 1
    if os.path.isdir(docs) and not os.listdir(docs):
        os.rmdir(docs)
print(f"   deleted {md} documents and {img} images")
PY
else
  echo
  echo "Originals are untouched. Re-run with --clear once the store looks right."
fi

echo
echo "Then: ade designs sync"
