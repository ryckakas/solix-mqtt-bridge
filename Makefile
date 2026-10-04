SHELL := /bin/bash

# go.mod's toolchain line is only a minimum; run CI's exact version instead.
GOTOOLCHAIN ?= $(shell sed -n 's/^toolchain //p' go.mod)
export GOTOOLCHAIN
GO_VERSION := $(patsubst go%,%,$(GOTOOLCHAIN))

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

GOVULNCHECK_VERSION := v1.8.0
IMAGE ?= solix-openwb-bridge:dev

.PHONY: help fmt lint complexity deadcode zizmor typos test race integration cover vuln build image check clean

help:
	@echo "solix-openwb-bridge make targets:"
	@echo "  fmt         - golangci-lint fmt (gofumpt + goimports, auto-fix)"
	@echo "  lint        - golangci-lint run"
	@echo "  complexity  - bonsai-lint cognitive complexity gate"
	@echo "  deadcode    - whole-program unreachable-function gate (incl. tests + integration tag)"
	@echo "  zizmor      - audit .github/ workflows and dependabot.yml (offline)"
	@echo "  typos       - spell-check the repository"
	@echo "  test        - go test ./..."
	@echo "  race        - go test -race ./..."
	@echo "  integration - race tests incl. -tags=integration (needs Docker)"
	@echo "  cover       - race tests + coverage report (coverage.out)"
	@echo "  vuln        - govulncheck $(GOVULNCHECK_VERSION)"
	@echo "  build       - build ./cmd/solix-openwb-bridge into ./bin/"
	@echo "  image       - build the container image for the local platform"
	@echo "  check       - everything CI runs; green here should mean green in CI"
	@echo "  clean       - remove build/coverage artifacts"

.DEFAULT_GOAL := help

# Usage: $(call check_tool,<binary>,<install hint>)
define check_tool
@command -v $(1) >/dev/null 2>&1 || { echo "solix-openwb-bridge: '$(1)' is required but not installed."; echo "  install: $(2)"; exit 1; }
endef

fmt:
	$(call check_tool,golangci-lint,brew install golangci-lint or https://golangci-lint.run/welcome/install/)
	golangci-lint fmt

lint:
	$(call check_tool,golangci-lint,brew install golangci-lint or https://golangci-lint.run/welcome/install/)
	golangci-lint run

complexity:
	go tool bonsai-lint .

# deadcode exits 0 even with findings, so any output fails the target.
deadcode:
	@out="$$(go tool deadcode -test -tags=integration ./...)"; \
	if [ -n "$$out" ]; then echo "$$out"; echo "solix-openwb-bridge: unreachable functions found"; exit 1; fi

zizmor:
	$(call check_tool,zizmor,brew install zizmor or https://docs.zizmor.sh/installation/)
	zizmor --offline --collect=all --strict-collection .

typos:
	$(call check_tool,typos,brew install typos-cli or https://github.com/crate-ci/typos#install)
	typos

test:
	go test ./...

race:
	go test -race ./...

integration:
	go test -race -tags=integration ./...

cover:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/solix-openwb-bridge ./cmd/solix-openwb-bridge

image:
	docker build --build-arg GO_VERSION=$(GO_VERSION) --build-arg VERSION=$(VERSION) -t $(IMAGE) .

check: lint complexity deadcode zizmor typos integration vuln
	go vet ./...

clean:
	rm -rf bin dist coverage.out
