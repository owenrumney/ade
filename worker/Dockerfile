FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o /ade ./cmd/ade

FROM debian:bookworm-slim
# gh and Docker come from their own apt repositories, as the agent image does:
# bookworm's gh is 2.23 and predates flags ghpr calls.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl git gnupg \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    -o /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
    > /etc/apt/sources.list.d/github-cli.list \
    && install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc \
    && chmod a+r /etc/apt/keyrings/docker.asc \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian $(. /etc/os-release && echo \"$VERSION_CODENAME\") stable" \
    > /etc/apt/sources.list.d/docker.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends gh docker-ce-cli docker-compose-plugin \
    vim less openssh-client git-lfs \
    && rm -rf /var/lib/apt/lists/*
# Not bookworm's neovim: that is 0.7, and a laptop's configuration is written
# against a current one. ade sends ~/.config/nvim over, so the versions must meet.
ARG NVIM_VERSION=v0.12.5
RUN set -eux; \
    case "$(dpkg --print-architecture)" in \
      arm64) arch=arm64 ;; \
      amd64) arch=x86_64 ;; \
      *) echo "no neovim build for $(dpkg --print-architecture)" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/nvim.tar.gz \
      "https://github.com/neovim/neovim/releases/download/${NVIM_VERSION}/nvim-linux-${arch}.tar.gz"; \
    tar xzf /tmp/nvim.tar.gz -C /usr/local --strip-components=1; \
    rm /tmp/nvim.tar.gz; \
    nvim --version | head -1
COPY --from=build /ade /usr/local/bin/ade
ENTRYPOINT []
