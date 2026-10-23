#!/usr/bin/env bash
# Usage: tools/sign.sh setup | tools/sign.sh <path>... — a stable signature, so
# macOS keeps a grant; ad-hoc makes every rebuild a new identity to TCC.
set -euo pipefail

cd "$(dirname "$0")/.."

identity="${ADE_SIGN_IDENTITY:-ADE Dev}"
# The Local Network grant is keyed to this exact string: renaming it starts
# from no permission. TestTheCliClaimsItsOwnIdentifier pins it.
#
# The window is a second program and takes its own, or two things claim one
# identity and macOS cannot tell their permissions apart.
bundle_id="${ADE_BUNDLE_ID:-uk.co.owenrumney.ade-cli}"

[ "$(uname -s)" = "Darwin" ] || exit 0

# The SHA-1 of the newest matching identity, empty when there is none. Not -v:
# that lists only identities chaining to a trusted root, which a self-signed
# one never does, and codesign signs with it regardless.
identity_hash() {
  security find-identity -p codesigning 2>/dev/null |
    awk -v name="\"$identity\"" '$0 ~ name {print $2}' | tail -1
}

have_identity() {
  [ -n "$(identity_hash)" ]
}

# Without a partition list codesign stops for a keychain prompt on every build,
# and the keychain will not set one without its own password.
allow_codesign() {
  local keychain="$HOME/Library/Keychains/login.keychain-db" login=""
  if [ -t 0 ]; then
    printf 'login keychain password, so codesign stops asking on every build: '
    read -rs login
    echo
  fi
  if security set-key-partition-list -S apple-tool:,apple:,codesign: -s \
    -k "$login" "$keychain" >/dev/null 2>&1; then
    echo "codesign may use $identity without asking"
    return
  fi
  echo "could not set the partition list: codesign will ask for the key on" >&2
  echo "every build until you answer Always Allow once." >&2
}

# A named step, not something a build does to you: it writes to your keychain.
setup() {
  if have_identity; then
    echo "$identity is already in the login keychain"
    allow_codesign
    exit 0
  fi
  # An interrupted run leaves an identity behind, and a duplicate name makes
  # codesign refuse as ambiguous. delete-identity takes the key with it.
  for stale in $(security find-identity -p codesigning 2>/dev/null |
    awk -v name="\"$identity\"" '$0 ~ name {print $2}' | sort -u); do
    security delete-identity -Z "$stale" >/dev/null 2>&1 || true
  done
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
    -keyout "$tmp/key.pem" -out "$tmp/cert.pem" -subj "/CN=$identity" \
    -addext "basicConstraints=critical,CA:false" \
    -addext "keyUsage=critical,digitalSignature" \
    -addext "extendedKeyUsage=critical,codeSigning" >/dev/null 2>&1
  # Keychain reads neither an empty password nor OpenSSL 3's default cipher:
  # both fail as "MAC verification failed". -legacy is absent from LibreSSL,
  # which only writes the old format anyway.
  pass=$(openssl rand -hex 16)
  openssl pkcs12 -export -legacy -out "$tmp/id.p12" -inkey "$tmp/key.pem" \
    -in "$tmp/cert.pem" -passout "pass:$pass" >/dev/null 2>&1 ||
    openssl pkcs12 -export -out "$tmp/id.p12" -inkey "$tmp/key.pem" \
      -in "$tmp/cert.pem" -passout "pass:$pass" >/dev/null 2>&1
  keychain="$HOME/Library/Keychains/login.keychain-db"
  security import "$tmp/id.p12" -k "$keychain" -P "$pass" -T /usr/bin/codesign >/dev/null
  allow_codesign
  have_identity || { echo "the certificate imported but codesign cannot see it" >&2; exit 1; }
  echo "$identity is ready; rerun make install, then grant ade Local Network once"
}

if [ "${1:-}" = "setup" ]; then
  setup
  exit 0
fi

[ $# -gt 0 ] || { echo "usage: tools/sign.sh setup | tools/sign.sh <path>..." >&2; exit 1; }

if ! have_identity; then
  echo "no $identity certificate: leaving these ad-hoc signed, so macOS will" >&2
  echo "forget their permissions on the next build. Fix it with: make sign-setup" >&2
  exit 0
fi

# By hash, not by name: two certificates sharing a name are ambiguous to
# codesign, and a half-finished setup is how a machine gets two.
hash=$(identity_hash)

for target in "$@"; do
  [ -e "$target" ] || continue
  # Nested code first under its own identifiers, then the bundle under ours.
  if [ -d "$target" ]; then codesign --force --deep --sign "$hash" "$target" 2>/dev/null; fi
  codesign --force --sign "$hash" --identifier "$bundle_id" "$target" 2>/dev/null
  codesign --verify --deep --strict "$target" 2>/dev/null ||
    { echo "signed $target but it does not verify" >&2; exit 1; }
  echo "signed $target as $identity"
done
