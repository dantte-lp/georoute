# georoute — build and packaging
SHELL := /bin/bash

BIN          := georoute
VERSION      := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS      := -s -w -X main.version=$(VERSION)
GOFLAGS      := -trimpath -ldflags '$(LDFLAGS)'
INSTALL_BIN  := /usr/local/bin/$(BIN)
SYSTEMD_DIR  := /etc/systemd/system
PODMAN       ?= podman
COMPOSE_FILE ?= compose.dev.yaml
DEV_IMAGE    ?= localhost/georoute-dev:go1.26
CONTAINERS_CONF ?= $(CURDIR)/containers.dev.conf

.PHONY: help build install install-systemd uninstall \
        fmt lint vet vuln test test-race coverage \
        clean ci \
        podman-build podman-test podman-ci podman-shell

help: ## show this help
	@awk 'BEGIN{FS=":.*##"; printf "Targets:\n"} /^[a-zA-Z_-]+:.*##/ {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## compile the binary
	go build $(GOFLAGS) -o $(BIN) .

install: build ## install binary to /usr/local/bin
	install -m 0755 $(BIN) $(INSTALL_BIN)

install-systemd: ## install service + timer
	install -m 0644 deploy/systemd/$(BIN).service $(SYSTEMD_DIR)/$(BIN).service
	install -m 0644 deploy/systemd/$(BIN).timer   $(SYSTEMD_DIR)/$(BIN).timer
	systemctl daemon-reload
	systemctl enable --now $(BIN).timer
	@echo 'Run "systemctl status $(BIN).timer" to verify.'

uninstall: ## remove binary + units
	-systemctl disable --now $(BIN).timer
	-rm -f $(SYSTEMD_DIR)/$(BIN).service $(SYSTEMD_DIR)/$(BIN).timer
	-rm -f $(INSTALL_BIN)
	systemctl daemon-reload

fmt: ## run gofmt and goimports
	gofmt -s -w .
	@command -v goimports >/dev/null && goimports -w . || echo '(goimports not installed, skipping)'

lint: ## run golangci-lint v2 (strict)
	golangci-lint run --max-issues-per-linter=0 --max-same-issues=0

vet: ## run go vet
	go vet ./...

vuln: ## check reachable vulnerabilities with the official Go scanner
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

test: ## run unit tests
	go test ./...

test-race: ## run unit tests with -race
	go test -race -shuffle=on -covermode=atomic ./...

coverage: ## generate coverage profile
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	@echo 'HTML report: go tool cover -html=coverage.out'

clean: ## remove build artifacts
	rm -f $(BIN) coverage.out coverage.html *.prof

ci: fmt vet lint vuln test-race ## the exact chain CI runs

podman-build: ## build reproducible Podman development image
	CONTAINERS_CONF=$(CONTAINERS_CONF) $(PODMAN) build -f Containerfile.dev -t $(DEV_IMAGE) .

podman-test: podman-build ## run unit tests inside the Podman development image
	CONTAINERS_CONF=$(CONTAINERS_CONF) $(PODMAN) run --rm \
		-v $(CURDIR):/workspace:Z \
		-v georoute-go-build-cache:/cache/go-build:Z \
		-v georoute-go-mod-cache:/cache/go-mod:Z \
		-w /workspace \
		-e CGO_ENABLED=0 \
		-e GOCACHE=/cache/go-build \
		-e GOMODCACHE=/cache/go-mod \
		-e GOTOOLCHAIN=local \
		$(DEV_IMAGE) make test

podman-ci: podman-build ## run fmt, vet, lint and tests inside the Podman development image
	CONTAINERS_CONF=$(CONTAINERS_CONF) $(PODMAN) run --rm \
		-v $(CURDIR):/workspace:Z \
		-v georoute-go-build-cache:/cache/go-build:Z \
		-v georoute-go-mod-cache:/cache/go-mod:Z \
		-w /workspace \
		-e CGO_ENABLED=1 \
		-e GOCACHE=/cache/go-build \
		-e GOMODCACHE=/cache/go-mod \
		-e GOTOOLCHAIN=local \
		$(DEV_IMAGE) make ci

podman-shell: podman-build ## open an interactive shell in the Podman development image
	CONTAINERS_CONF=$(CONTAINERS_CONF) $(PODMAN) run --rm -it \
		-v $(CURDIR):/workspace:Z \
		-v georoute-go-build-cache:/cache/go-build:Z \
		-v georoute-go-mod-cache:/cache/go-mod:Z \
		-w /workspace \
		-e CGO_ENABLED=0 \
		-e GOCACHE=/cache/go-build \
		-e GOMODCACHE=/cache/go-mod \
		-e GOTOOLCHAIN=local \
		$(DEV_IMAGE) bash
