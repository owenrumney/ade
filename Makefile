.PHONY: private full app-install build install version test fmt vet tidy lint release-snapshot clean sandbox-image worker-image worker-ship extension extension-sign sign-setup quick-start

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
# The app spike: a window of ade's own. See app/README.md.
ESBUILD := ./node_modules/.bin/esbuild

# Electron is a hundred megabytes, so it is installed on demand rather than
# by every target that only needs the bundles. The check is for the binary
# itself: a shim with no download behind it is the usual way this fails,
# and `electron: command not found` is not a useful thing to be told.
app-deps:
	cd app && ./deps.sh

# Every precondition make app needs, checked one at a time. Run this
# first when the window will not start.
app-doctor:
	cd app && ./doctor.sh

# The window's own bundles, the panels among them: a missing one is a
# blank pane with a 404 nobody sees.
# PAGES are the panels the window frames: one bundle each, named as the
# host asks for them in app/renderer/panels.ts.
PAGES := chat:chat settings:settings commitpanel:commit worktreepanel:worktrees \
	repo:repo purgepanel:purge designprunepanel:designprune quizpanel:quiz \
	designedit:designedit newsession:newsession sandboxpanel:sandbox audit:audit \
	machines:machines

app-build:
	cd app && mkdir -p dist \
		&& for page in $(PAGES); do \
			$(ESBUILD) "pages/$${page%%:*}/main.ts" --bundle \
				--outfile="dist/$${page##*:}.js" --format=iife --platform=browser \
				--log-level=warning || exit 1; \
		done \
		&& cp ../internal/render/assets/mermaid.min.js dist/mermaid.min.js \
		&& $(ESBUILD) src/main.ts --bundle --outfile=dist/main.js \
			--platform=node --external:electron --format=cjs \
		&& $(ESBUILD) src/preload.ts --bundle --outfile=dist/preload.js \
			--platform=node --external:electron --format=cjs \
		&& $(ESBUILD) src/webview.ts --bundle --outfile=dist/webview.js \
			--platform=node --external:electron --format=cjs \
		&& $(ESBUILD) src/keymap.ts --bundle --outfile=dist/keymap.js \
			--platform=node --format=cjs \
		&& $(ESBUILD) renderer/app.ts --bundle --outfile=dist/renderer.js \
			--format=iife --platform=browser \
			--loader:.ttf=dataurl --external:vs/* \
		&& $(ESBUILD) renderer/editorentry.ts --bundle --outfile=dist/editor.js \
			--format=iife --global-name=adeEditor --platform=browser \
			--loader:.ttf=dataurl --external:vs/*

app-test:
	cd app && ./node_modules/.bin/tsc -p . \
		&& ./node_modules/.bin/vitest run --root . --reporter=dot

# Starts the real window headless and checks it drew. Needs a display, or
# xvfb-run on a machine without one.
app-smoke: app-deps app-build
	cd app && ./node_modules/.bin/electron smoke.js --no-sandbox --disable-gpu 2>&1 \
		| grep -vE 'libva|GPU|dbus|Fontconfig|gbm|EGL|bus\.cc|Floss|vulkan|interface_endpoint'

# A browser harness for pictures only: the keyboard is the shell's, so this
# is never a way to run ade.
app-shots: app-build
	node tools/appshots.mjs $(VIEWS)

# The window as something launchable: ADE.app on a Mac. Its own
# bundle id, because the cli's is pinned to the cli.
app-package: app-deps app-build
	cd app && ./pack.sh

app-install: app-package
	cd app && ./install.sh

app: app-deps app-build
	cd app && ./node_modules/.bin/electron .

test:
	go test ./...
	$(MAKE) app-test

# The binary and the app, in order: a rebuild after a pull.
full:
	$(MAKE) install
	$(MAKE) app-install

# Everything a new machine needs: both images, the binary, and the window.
quick-start: sandbox-image worker-image install app-install

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

extension-sign:
	cd extension && npm install && npx web-ext sign

release-snapshot:
	goreleaser release --clean --snapshot --skip=publish

clean:
	rm -rf ./bin ./dist ./extension/web-ext-artifacts

