.PHONY: private full app-install build install version test fmt vet tidy lint release-snapshot clean extension extension-sign sign-setup quick-start

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

# The app spike: a window of ade's own. See app/README.md.
ESBUILD := ./node_modules/.bin/esbuild

# The bundles are plain web code, so this is npm and nothing else. The
# scripts are not run: nothing here has one worth running.
app-deps:
	cd app && { [ -d node_modules/monaco-editor ] || \
		npm install --no-audit --no-fund --ignore-scripts; }

# The window's own bundles, the panels among them: a missing one is a
# blank pane with a 404 nobody sees.
# PAGES are the panels the window frames: one bundle each, named as the
# host asks for them in app/renderer/panels.ts.
PAGES := settings:settings newsession:newsession

app-build:
	cd app && mkdir -p dist \
		&& for page in $(PAGES); do \
			$(ESBUILD) "pages/$${page%%:*}/main.ts" --bundle \
				--outfile="dist/$${page##*:}.js" --format=iife --platform=browser \
				--log-level=warning || exit 1; \
		done \
		&& cp ../internal/render/assets/mermaid.min.js dist/mermaid.min.js \
		&& $(ESBUILD) renderer/app.ts --bundle --outfile=dist/renderer.js \
			--format=iife --platform=browser \
			--loader:.ttf=dataurl --external:vs/* \
		&& $(ESBUILD) renderer/editorentry.ts --bundle --outfile=dist/editor.js \
			--format=iife --global-name=adeEditor --platform=browser \
			--loader:.ttf=dataurl --external:vs/*

app-test:
	cd app && ./node_modules/.bin/tsc -p . \
		&& ./node_modules/.bin/vitest run --root . --reporter=dot

# Serves the window without opening one, and checks the page and its
# bundle come back. No display needed.
app-smoke: app-deps app-build
	tools/appsmoke.sh

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
	go run ./cmd/adewindow

test:
	go test ./...
	$(MAKE) app-test

# The binary and the app, in order: a rebuild after a pull.
full:
	$(MAKE) install
	$(MAKE) app-install

# Everything a new machine needs: the binary and the window.
quick-start: install app-install

fmt:
	gofmt -s -w .

vet:
	go vet ./...

tidy:
	go mod tidy

lint:
	golangci-lint run

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

