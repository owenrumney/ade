# What the VSCodium scripts read. Sourced by tools/editor/*.sh under bash.
export VSCODIUM_COMMIT="5b01560f8419b3cbbe4b1aa0ce07183ef8c45b0f"
export APP_NAME="ADE"
export ASSETS_REPOSITORY="owenrumney/ade"
export BINARY_NAME="ade-editor"
export GH_REPO_PATH="owenrumney/ade"
export ORG_NAME="owenrumney"
# The Mac build's own, unless the caller already set them: the editor server
# is the same tree built for a linux runner. See tools/editor/reh.sh.
export CI_BUILD="${CI_BUILD:-no}"
export OS_NAME="${OS_NAME:-osx}"
export VSCODE_ARCH="${VSCODE_ARCH:-arm64}"
export VSCODE_QUALITY="stable"
export VSCODE_SKIP_NODE_VERSION_CHECK="yes"
export MAX_OLD_SPACE_SIZE="8192"
export NODE_OPTIONS="--max-old-space-size=8192"
export ELECTRON_SKIP_BINARY_DOWNLOAD=1
export PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
export MS_TAG="1.135.0"
export MS_COMMIT="08d4889f9ec4a1685d257b9b95de036c8e1ce1e5"
export RELEASE_VERSION="1.135.06783"
export BUILD_SOURCEVERSION="08d4889f9ec4a1685d257b9b95de036c8e1ce1e5"
