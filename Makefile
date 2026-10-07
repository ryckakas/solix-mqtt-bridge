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
ACTIONLINT_VERSION := v1.7.12
GORELEASER_IMAGE := goreleaser/goreleaser:v2.18.2
DEV_COMPOSE := docker compose -f deploy/dev/docker-compose.yml
SESSION_COMPOSE := docker compose -f deploy/session/docker-compose.yml
IMAGE ?= solix-mqtt-bridge:dev

.PHONY: help fmt lint complexity deadcode readonly tidy-check zizmor actionlint typos test race integration cover vuln build image dev-up dev-down dev-logs session-up session-down session-logs release-check check clean

help:
	@echo "solix-mqtt-bridge make targets:"
	@echo "  fmt         - golangci-lint fmt (gofumpt + goimports, auto-fix)"
	@echo "  lint        - golangci-lint run"
	@echo "  complexity  - bonsai-lint cognitive complexity gate"
	@echo "  deadcode    - whole-program unreachable-function gate (incl. tests + integration tag)"
	@echo "  readonly    - no Modbus write call outside internal/simulator"
	@echo "  tidy-check  - go.mod/go.sum match what go mod tidy produces (changes nothing)"
	@echo "  zizmor      - audit .github/ workflows and dependabot.yml (offline)"
	@echo "  actionlint  - lint .github/workflows with actionlint $(ACTIONLINT_VERSION) (needs shellcheck)"
	@echo "  typos       - spell-check the repository"
	@echo "  test        - go test ./..."
	@echo "  race        - go test -race ./..."
	@echo "  integration - race tests incl. -tags=integration (needs Docker)"
	@echo "  cover       - race tests + coverage report (coverage.out)"
	@echo "  vuln        - govulncheck $(GOVULNCHECK_VERSION)"
	@echo "  build       - build ./cmd/solix-mqtt-bridge into ./bin/"
	@echo "  image       - build the container image for the local platform"
	@echo "  dev-up      - start the local stand-in stack (simulator, mosquitto, fake openWB, bridge)"
	@echo "  dev-down    - stop the local stand-in stack"
	@echo "  dev-logs    - follow the stand-in stack's logs"
	@echo "  session-up  - read-only session against the REAL Solarbank (needs SOLARBANK_ADDR and the owner's permission)"
	@echo "  session-down - stop the session"
	@echo "  session-logs - follow the session's bridge logs"
	@echo "  release-check - validate .goreleaser.yaml (via the goreleaser container)"
	@echo "  check       - everything CI runs; green here should mean green in CI"
	@echo "  clean       - remove build/coverage artifacts"

.DEFAULT_GOAL := help

# Usage: $(call check_tool,<binary>,<install hint>)
define check_tool
@command -v $(1) >/dev/null 2>&1 || { echo "solix-mqtt-bridge: '$(1)' is required but not installed."; echo "  install: $(2)"; exit 1; }
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
	if [ -n "$$out" ]; then echo "$$out"; echo "solix-mqtt-bridge: unreachable functions found"; exit 1; fi

# The bridge is read-only towards the Solarbank; only the simulator (a test stand-in) may name a write call.
MODBUS_WRITE_CALLS := \.(Write(Single|Multiple)?(Coil|Register)s?|WriteUint(16|32|64)s?|WriteFloat(32|64)s?|WriteBytes|WriteRawBytes|MaskWriteRegister|ReadWriteMultipleRegisters|WriteFileRecord)\(
readonly:
	@if git grep --untracked -nE '$(MODBUS_WRITE_CALLS)' -- '*.go' ':!internal/simulator/'; then \
		echo "solix-mqtt-bridge: Modbus write call outside internal/simulator"; exit 1; fi

tidy-check:
	go mod tidy -diff

zizmor:
	$(call check_tool,zizmor,brew install zizmor or https://docs.zizmor.sh/installation/)
	zizmor --offline --collect=all --strict-collection .

# actionlint silently skips its shell-script rules when shellcheck is missing, so require it.
actionlint:
	$(call check_tool,shellcheck,brew install shellcheck or https://github.com/koalaman/shellcheck#installing)
	go run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

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
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/solix-mqtt-bridge ./cmd/solix-mqtt-bridge

image:
	docker build --build-arg GO_VERSION=$(GO_VERSION) --build-arg VERSION=$(VERSION) -t $(IMAGE) .

dev-up:
	GO_VERSION=$(GO_VERSION) $(DEV_COMPOSE) up --build --detach

dev-down:
	GO_VERSION=$(GO_VERSION) $(DEV_COMPOSE) down

dev-logs:
	GO_VERSION=$(GO_VERSION) $(DEV_COMPOSE) logs --follow

session-up:
	@test -n "$(SOLARBANK_ADDR)" || { echo "solix-mqtt-bridge: set SOLARBANK_ADDR=<host>:502 (the REAL device; read-only)"; exit 1; }
	@mkdir -p tmp/session
	GO_VERSION=$(GO_VERSION) SOLARBANK_ADDR=$(SOLARBANK_ADDR) $(SESSION_COMPOSE) up --build --detach

session-down:
	GO_VERSION=$(GO_VERSION) SOLARBANK_ADDR=unused $(SESSION_COMPOSE) down

session-logs:
	GO_VERSION=$(GO_VERSION) SOLARBANK_ADDR=unused $(SESSION_COMPOSE) logs --follow bridge

release-check:
	docker run --rm -v "$(CURDIR):/src" -w /src $(GORELEASER_IMAGE) check

check: lint complexity deadcode readonly tidy-check zizmor actionlint typos integration vuln
	go vet ./...

clean:
	rm -rf bin dist coverage.out
