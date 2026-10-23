#!/bin/sh
# Stands in for a toolchain this box does not carry. See the Dockerfile.
echo "ade: $(basename "$0") is not in this container." >&2
echo "Builds and tests run on the host: use the checks tool (run_*)." >&2
echo "No check for what you need? Ask for one to be added." >&2
exit 127
