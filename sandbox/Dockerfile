# The image the agent runs in when a session is sandboxed: Claude Code, Pi,
# Codex, git and the toolchains the work here uses. Built by `make sandbox-image`
# for whichever runtime is on the machine; the same image serves Docker and
# Apple's container.
FROM golang:1.26-bookworm

ARG NODE_MAJOR=22
# gh and Docker come from their own apt repositories. Only Docker's client and
# Compose plugin are installed; the daemon remains on the host.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl git ripgrep jq make gnupg \
    && curl -fsSL https://deb.nodesource.com/setup_${NODE_MAJOR}.x | bash - \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    -o /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
    > /etc/apt/sources.list.d/github-cli.list \
    && install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc \
    && chmod a+r /etc/apt/keyrings/docker.asc \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
    > /etc/apt/sources.list.d/docker.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends nodejs gh docker-ce-cli docker-compose-plugin \
    openssh-client \
    && rm -rf /var/lib/apt/lists/*

RUN npm install -g @anthropic-ai/claude-code @earendil-works/pi-coding-agent @openai/codex

# mise, for a repository that pins its own toolchain in .mise.toml: the
# shims come first on PATH and install what the repository names on first
# use; a repository that names nothing gets the image's go and node. Shims,
# not `mise activate`, since /bin/sh here is dash and the activation is
# bash. Everything under /cache is a host directory (~/.ade/cache) mounted
# at run time, so installs and build caches outlive the container.
RUN curl -fsSL https://mise.run | sh \
    && mv /root/.local/bin/mise /usr/local/bin/mise \
    && mkdir -p /cache/mise /cache/go-build /cache/go-mod /cache/npm
ENV MISE_DATA_DIR=/cache/mise \
    MISE_STATE_DIR=/cache/mise-state \
    MISE_CACHE_DIR=/cache/mise-cache \
    MISE_YES=1 \
    GOCACHE=/cache/go-build \
    GOMODCACHE=/cache/go-mod \
    npm_config_cache=/cache/npm

# The checkout and $HOME/.claude are mounted at their host paths; $HOME is
# set at run time to match. Nothing is baked in that names a user.
ENV PATH=/cache/mise/shims:/root/go/bin:/usr/local/go/bin:$PATH
ENTRYPOINT []
