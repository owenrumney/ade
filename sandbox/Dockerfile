# The box carries no toolchain. Builds and tests go to the host through
# ade's checks MCP server, which runs the repository's own checks on the
# versions the checkout pins. See .git/ade/docs/rethink-workflow-and-uk.md.
FROM node:22-bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl git ripgrep jq gnupg openssh-client \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    -o /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
    > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*
RUN npm install -g @anthropic-ai/claude-code @earendil-works/pi-coding-agent @openai/codex
# A toolchain reached for out of habit fails in one line rather than sending
# the agent off to install one. The checks tool is the way.
COPY toolstub.sh /usr/local/bin/ade-toolstub
RUN chmod 0755 /usr/local/bin/ade-toolstub \
    && for t in go gofmt cargo rustc python python3 pip pip3 mvn gradle dotnet; do \
         ln -sf /usr/local/bin/ade-toolstub "/usr/local/bin/$t"; \
       done \
    && mkdir -p /cache/npm
ENV npm_config_cache=/cache/npm
ENTRYPOINT []
