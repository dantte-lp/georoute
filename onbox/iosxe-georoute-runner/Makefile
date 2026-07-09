SHELL := /bin/sh

APP_ID ?= georoute-runner
IMAGE ?= localhost/iosxe-georoute-runner:0.1.0
PACKAGE ?= dist/$(APP_ID).tar
PODMAN ?= podman

.PHONY: help lock check test build package clean

help:
	@printf '%s\n' 'Targets: lock check test build package clean'

lock:
	uv lock --check

check:
	uv sync --locked --dev
	uv run ruff format --check
	uv run ruff check
	uv run ty check

test:
	uv run pytest

build:
	$(PODMAN) build -f Containerfile -t $(IMAGE) .

package: build
	mkdir -p dist
	$(PODMAN) save -o $(PACKAGE).tmp $(IMAGE)
	mv $(PACKAGE).tmp $(PACKAGE)
	sha256sum $(PACKAGE) > $(PACKAGE).sha256

clean:
	rm -rf dist
