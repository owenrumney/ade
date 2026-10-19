.PHONY: private full editor-fetch editor-prepare editor-dev editor-sync editor-package editor-reh editor-install vscode vscode-test vscode-package vscode-remote build ui install version test fmt vet tidy lint release-snapshot clean sandbox-image worker-image worker-ship extension extension-sign sign-setup quick-start

# What `ade --version` reports. A tag when there is one, the commit otherwise,
# and -dirty for a working tree that is not the commit it claims to be.
#
# ade over ssh insists both machines run the same version before it forwards
# anything, so an unstamped binary would make that check pass against anything.
# Where `go install` writes: GOBIN when the environment sets one, GOPATH/bin
# otherwise. sign.sh skips a path that does not exist, so a guess is silent.
GOBIN ?= $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o ./bin/ade ./cmd/ade

# Every ade. The quit is for an ADE.app left installed from before the shell
# was removed: while it is up it starts the launcher again, on the old binary.
killall:
	@osascript -e 'quit app "ADE"' 2>/dev/null || true
	killall ade || true

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/ade
	@./tools/sign.sh "$(GOBIN)/ade"

# One admin prompt, once per machine. Without it every build is ad-hoc signed
# and macOS drops ade's Local Network grant, which reads as an ssh routing fault.
sign-setup:
	@./tools/sign.sh setup

# The role and agent prompts, which live in ade-private. Gitignored here.
private:
	@./tools/private.sh

# The binary and the extension together, which is what a change to either
# needs: the editor is the only front end, so neither is useful alone.
ui: killall
	go install -ldflags "$(LDFLAGS)" ./cmd/ade
	@./tools/sign.sh "$(GOBIN)/ade"
	@cd vscode && if [ -d node_modules ]; then npm run build; \
	else echo "vscode: no node_modules — run make vscode once"; fi

version:
	@echo $(VERSION)

# Work on ade itself. ade starts Vite, proxies its own API through it and opens
# that URL: a UI change is a save with HMR, and a Go change rebuilds and
# restarts on the same port.
#
# No sandbox here. The agent is editing ade's own checkout, which the container
# mounts read-write anyway, so the boundary buys nothing — and it costs a docker
# round trip on every turn plus a container whose fixed name clashes when a run
# is orphaned. Sessions you did not start yourself still sandbox by default.
# VIEWS names the views to shoot: all of them is more than one reply can show.
shots:
	./tools/shots.sh $(VIEWS)

test:
	go test ./...
	$(MAKE) vscode-test

# The binary and the app, in order: a rebuild after a pull.
full:
	$(MAKE) install
	$(MAKE) editor-package
	$(MAKE) editor-install

# Everything a new machine needs: both images, the binary, and the editor.
quick-start: sandbox-image worker-image install editor-fetch editor-prepare editor-package editor-install

fmt:
	gofmt -s -w .

vet:
	go vet ./...

tidy:
	go mod tidy

lint:
	golangci-lint run

# The container the agent runs in when a session is sandboxed. Apple's
# container when its services are up, Docker otherwise — the same choice ade
# makes at run time.
# The same name the binary pulls, so a local build is what runs here and
# nothing is pulled over it.
SANDBOX_IMAGE ?= ghcr.io/owenrumney/ade-agent:latest
sandbox-image:
	@if command -v container >/dev/null && container system status >/dev/null 2>&1; then \
	container build -t $(SANDBOX_IMAGE) sandbox; \
	else \
	docker build -t $(SANDBOX_IMAGE) sandbox; \
	fi

WORKER_IMAGE ?= ghcr.io/owenrumney/ade-worker:latest
worker-image:
	docker build --build-arg VERSION=$(VERSION) -f worker/Dockerfile -t $(WORKER_IMAGE) .
	docker build -f provisioner/Dockerfile -t ghcr.io/owenrumney/ade-provisioner:latest .

# Carry a worker built from this checkout to a host, for a fix CI has not
# published. See .git/ade/docs/shipping-a-worker.md.
worker-ship:
	tools/worker-ship.sh $(if $(HOST),$(HOST),$(error set HOST=<ssh host>))

# The Firefox add-on. build produces an unsigned package, which only a Firefox
# with xpinstall.signatures.required turned off will install; sign asks Mozilla
# for a signed one, which any Firefox installs and which is not listed on AMO.
# Signing wants WEB_EXT_API_KEY and WEB_EXT_API_SECRET in the environment.
extension:
	cd extension && npm install && npx web-ext lint && npx web-ext build

# The VS Code extension. A client of the HTTP API in this same tree, which is
# why it lives here: a route and its caller change in one commit.

# --ignore-scripts: vsce brings keytar, whose install step
# fetches a prebuilt binary from a release url the lockfile cannot vouch for.
vscode:
	cd vscode && npm install --no-audit --no-fund --ignore-scripts && npm run check && npm run build

vscode-test:
	cd vscode && npm install --no-audit --no-fund --ignore-scripts && npm run check && npm test

# vsce is a devDependency, so this packages with the version the lockfile
# resolved rather than whatever is latest on the day it runs.
vscode-package: vscode
	cd vscode && npx vsce package --out ade.vsix

# Installs the extension into another machine's editor server, for a Remote-SSH
# window: there the extension runs on the remote host, not in ADE.app.
vscode-remote: vscode-package
	@test -n "$(HOST)" || { echo "usage: make vscode-remote HOST=<ssh host>"; exit 1; }
	@./tools/vscode-remote.sh "$(HOST)" vscode/ade.vsix

extension-sign:
	cd extension && npm install && npx web-ext sign

release-snapshot:
	goreleaser release --clean --snapshot --skip=publish

clean:
	rm -rf ./bin ./dist ./extension/web-ext-artifacts

# The editor fork: VSCodium's build scripts over a pinned vscode, with ade's
# patches in editor/patches and the extension built in. See
# .git/ade/docs/editor-fork.md. Fetch once, prepare after a patch or vscode
# bump (it runs npm ci, about ten minutes), dev to compile and run from source.
editor-fetch:
	tools/editor/fetch.sh

editor-prepare:
	tools/editor/prepare.sh

editor-dev:
	tools/editor/dev.sh $(if $(COMPILE),-c) $(ARGS)

editor-sync:
	tools/editor/sync.sh

editor-package:
	tools/editor/package.sh

# The editor server a window on another machine runs. Linux only, and the
# server must be built from the same commit as the client, which is why CI
# builds it from this tree: .github/workflows/editor-reh.yml.
editor-reh:
	tools/editor/reh.sh $(ARCH)

# The packaged app into /Applications and its cli on PATH. A nightly is
# make editor-package editor-install.
editor-install:
	tools/editor/install.sh
