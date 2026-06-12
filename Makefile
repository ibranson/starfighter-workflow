# starfighter-workflow — headless arcade repair-workflow daemon (sfworkflowd)
#
# Common targets:
#   make build                     local build for the host OS (quick syntax check)
#   make pi                        cross-compile linux/arm64 binary for Raspberry Pi 5
#   make web                       build the SvelteKit SPA into internal/web/dist
#   make all                       web + pi
#   make run                       run locally against ./dev-data on :9090
#   make provision REMOTE=<host>   first-time setup on a Pi (installs the service)
#   make deploy    REMOTE=<host>   build + ship + restart
#   make logs      REMOTE=<host>   follow the daemon's journal
#   make restart   REMOTE=<host>   restart the daemon (no rebuild)
#   make clean                     remove build artifacts

BINARY      := sfworkflowd
PKG         := ./cmd/sfworkflowd
PI_OUT      := bin/$(BINARY)
HOST_OUT    := bin/$(BINARY)-host
WEB_DIR     := web
WEB_DIST    := internal/web/dist

GO          ?= go
NPM         ?= npm
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X starfighter-workflow/internal/version.Current=$(VERSION)

REMOTE      ?=

.PHONY: all build pi web run clean provision deploy logs restart tidy

all: web pi

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(HOST_OUT) $(PKG)

pi:
	@mkdir -p bin
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o $(PI_OUT) $(PKG)

web:
	cd $(WEB_DIR) && $(NPM) install && $(NPM) run build
	@mkdir -p $(WEB_DIST)
	@rm -rf $(WEB_DIST)/*
	@cp -r $(WEB_DIR)/build/* $(WEB_DIST)/

# Run the daemon locally with a throwaway config + data dir on :9090.
run: build
	@mkdir -p dev-data
	@printf '{ "data_dir": "dev-data/state", "display_name": "Dev Bench", "http": { "addr": ":9090" } }\n' > dev-data/config.json
	$(HOST_OUT) -config dev-data/config.json

tidy:
	$(GO) mod tidy

clean:
	rm -rf bin $(WEB_DIR)/build $(WEB_DIR)/.svelte-kit dev-data
	@git checkout -- $(WEB_DIST)/index.html 2>/dev/null || true

provision:
	@if [ -z "$(REMOTE)" ]; then echo "usage: make provision REMOTE=<host>"; exit 1; fi
	pwsh -NoProfile -File scripts/provision.ps1 -RemoteHost $(REMOTE)

deploy:
	@if [ -z "$(REMOTE)" ]; then echo "usage: make deploy REMOTE=<host>"; exit 1; fi
	pwsh -NoProfile -File scripts/deploy.ps1 -RemoteHost $(REMOTE)

logs:
	@if [ -z "$(REMOTE)" ]; then echo "usage: make logs REMOTE=<host>"; exit 1; fi
	pwsh -NoProfile -File scripts/logs.ps1 -RemoteHost $(REMOTE)

restart:
	@if [ -z "$(REMOTE)" ]; then echo "usage: make restart REMOTE=<host>"; exit 1; fi
	pwsh -NoProfile -File scripts/restart.ps1 -RemoteHost $(REMOTE)
