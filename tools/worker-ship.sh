#!/usr/bin/env bash
# Build the worker image from this checkout and carry it to a host, for a fix
# that has not been published yet. Nothing pushes ade-worker outside CI, and a
# host only ever pulls, so a local build reaches one by hand or not at all.
#
# Usage: tools/worker-ship.sh <ssh host>
set -euo pipefail

host=${1:?usage: worker-ship.sh <ssh host>}
tag=${ADE_WORKER_IMAGE:-ade-worker:shipped}
# Dirty, when it is: the build takes the working tree while rev-parse names the
# commit, and a version that hides the difference cost this repository an evening.
version=$(git describe --always --dirty --match 'v[0-9]*' 2>/dev/null || git rev-parse --short HEAD)

# The host's architecture, not this machine's: a Mac builds arm64 by default
# and a linux/amd64 worker will not start from it.
arch=$(ssh "$host" uname -m)
case "$arch" in
  x86_64) platform=linux/amd64 ;;
  aarch64 | arm64) platform=linux/arm64 ;;
  *) echo "no worker build for $arch on $host" >&2; exit 1 ;;
esac

echo "building $tag for $platform from $version"
docker build --platform "$platform" --build-arg "VERSION=$version" \
  -f worker/Dockerfile -t "$tag" .

echo "carrying it to $host"
docker save "$tag" | ssh "$host" 'docker load'

# The launcher adopts a running worker, so the old one has to go for the new
# image to be used. It is recreated on the next connect.
ssh "$host" "docker rm -f ade-worker >/dev/null 2>&1 || true"

cat <<DONE

shipped $version to $host as $tag

Start ade with the image named, or it pulls the published one over this:

    ADE_WORKER_IMAGE=$tag ade

DONE
