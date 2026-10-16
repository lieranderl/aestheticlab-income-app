SHELL := /bin/bash

GO ?= go
BUN ?= bun
AIR ?= air
DEV_START_MONTH ?= 2026-10
DEV_PORT ?= 8080
GOLANGCI_LINT ?= $(GO) tool golangci-lint
ACTIONLINT ?= $(GO) run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
# Apple's container CLI on macOS, then Docker (CI runners, which also have
# Podman), then Podman (Linux without Docker). Override with CONTAINER_TOOL=….
CONTAINER_TOOL ?= $(shell command -v container 2>/dev/null || command -v docker 2>/dev/null || command -v podman 2>/dev/null || echo docker)
FIRESTORE_EMULATOR_IMAGE ?= gcr.io/google.com/cloudsdktool/google-cloud-cli:587.0.0-emulators@sha256:8d8573471c489a409f03891d2c24d177fa531276184f7002b9a2d853d1b3ed54
# Settings for local runs (see .env.web.example); never committed.
WEB_ENV = set -a; if [ -f .env.web ]; then . ./.env.web; fi; set +a;
GENERATED = static/app.css internal/views/icons_gen.go

# The local Firestore emulator. `make dev` always uses it, whatever .env.web
# says, so a local run can never write to production.
EMULATOR_HOST = localhost:8086
EMULATOR_PROJECT = demo-kassa
EMULATOR_DATABASE = aestheticlab-income
EMULATOR_CONTAINER = kassa-firestore
EMULATOR_ENV = FIRESTORE_EMULATOR_HOST=$(EMULATOR_HOST) INCOME_FIRESTORE_PROJECT=$(EMULATOR_PROJECT) INCOME_FIRESTORE_DATABASE=$(EMULATOR_DATABASE)
# A fixed key for local runs only, so Air's restarts keep you signed in.
LOCAL_SESSION_KEY = kassa-local-development-only-session-key

.PHONY: all help hooks assets templ templ-check assets-check lint test security coverage build ci \
	dev dev-memory dev-october web web-dev seed-local firestore firestore-wait firestore-reset firestore-stop test-store mobile \
	container-ready image docker-build clean

.PHONY: agent-test agent-loop

all: ci

help:
	@echo "Aesthetic Lab Kassa"
	@echo ""
	@echo "Run locally:"
	@echo "  make dev-october      Ready October app on memory; sign in as anna with the printed temporary password"
	@echo "                        (data and login reset on every restart)"
	@echo "  make dev              Persistent emulator + first data + hot reload (October by default)"
	@echo "                        (first run prints anna's temporary password; data lasts while the emulator runs)"
	@echo "  make dev-memory       Memory app; defaults to October on http://localhost:8080"
	@echo "  Override month/port:  make dev-memory DEV_START_MONTH=2026-08 DEV_PORT=8090"
	@echo "  make web-dev / web    App with the settings in .env.web (with / without hot reload)"
	@echo ""
	@echo "Local Firestore emulator ($(EMULATOR_HOST), project $(EMULATOR_PROJECT)):"
	@echo "  make firestore        Start it (keeps a running one and its data)"
	@echo "  make seed-local       Write the first data once (anna + categories + kassa accounts)"
	@echo "  make firestore-reset  Delete all emulator data and seed again (new temporary password)"
	@echo "  make firestore-stop   Stop it (its data is lost)"
	@echo ""
	@echo "Checks:"
	@echo "  make test             Frontend helpers + race-enabled Go tests (memory store)"
	@echo "  make test-store       Store tests on the emulator too"
	@echo "  make mobile           Playwright phone checks (iPhone SE/15, Pixel 7; light+dark) on cmd/e2e"
	@echo "  make lint             Generated-file checks and static analysis"
	@echo "  make security         govulncheck and gosec"
	@echo "  make ci               Everything CI runs"
	@echo "  make agent-test       Local agent queue/process regression tests (no model calls)"
	@echo ""
	@echo "Other:"
	@echo "  make image            Production image (Apple container, Podman or Docker)"
	@echo "  make hooks            Use the repo git hooks (blocks direct pushes to main)"
	@echo "  make agent-loop ARGS=list   Inspect the local Jev/Claude task queue"
	@echo "  make clean            Remove build output"

# Point git at the repo's hooks (.githooks/pre-push blocks direct pushes to
# main). Every local build target depends on it, so a fresh clone is guarded
# from its first `make`; skipped outside a git checkout (Docker build).
hooks:
	@if git rev-parse --git-dir >/dev/null 2>&1 && [ "$$(git config core.hooksPath)" != ".githooks" ]; then \
		git config core.hooksPath .githooks && echo "git hooks: using .githooks (direct pushes to main are blocked)"; \
	fi

node_modules: package.json bun.lock
	$(BUN) install --frozen-lockfile
	@touch node_modules

assets: hooks node_modules
	$(BUN) run assets

templ: hooks
	$(GO) tool templ generate

templ-check:
	$(GO) tool templ generate -check

assets-check: assets
	git diff --exit-code -- $(GENERATED)

lint: templ-check assets-check
	$(ACTIONLINT)
	$(GO) vet ./...
	$(GO) tool staticcheck ./...
	$(GOLANGCI_LINT) run ./...
	@test -z "$$(gofmt -l cmd internal static)" || (echo "Unformatted Go files:" && gofmt -l cmd internal static && exit 1)

test: hooks node_modules
	$(BUN) test
	$(GO) test -race -count=1 ./...

security:
	$(GO) tool govulncheck ./...
	$(GO) tool gosec -quiet ./...

coverage:
	$(GO) test -coverprofile=coverage.out ./...

build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w" -o bin/web ./cmd/web

agent-test:
	python3 -m unittest discover -s scripts -p 'test_agent_loop.py' -v

agent-loop:
	python3 scripts/agent_loop.py $(ARGS)

ci: lint test security build agent-test

# The everyday local run: emulator, first data, hot reload.
dev: assets seed-local
	@echo "App on http://localhost:$(DEV_PORT) (emulator data, start month $(DEV_START_MONTH)). Sign in as anna."
	@$(WEB_ENV) $(EMULATOR_ENV) INCOME_SESSION_KEY=$(LOCAL_SESSION_KEY) PORT=$(DEV_PORT) INCOME_BASE_URL=http://localhost:$(DEV_PORT) INCOME_START_MONTH=$(DEV_START_MONTH) $(AIR)

# In-memory data, no hot reload: each restart starts over and logs a new
# one-off password for anna.
dev-memory: templ assets
	env -u INCOME_FIRESTORE_PROJECT -u INCOME_FIRESTORE_DATABASE -u FIRESTORE_EMULATOR_HOST -u INCOME_SESSION_KEY \
		PORT=$(DEV_PORT) INCOME_BASE_URL=http://localhost:$(DEV_PORT) INCOME_START_MONTH=$(DEV_START_MONTH) $(GO) run ./cmd/web

# A ready October session without persistent emulator data.
dev-october: DEV_START_MONTH = 2026-10
dev-october: dev-memory

web: templ assets
	$(WEB_ENV) $(GO) run ./cmd/web

web-dev: assets
	$(WEB_ENV) $(AIR)

# Writes the first data once; when it already exists, says so and carries on.
seed-local: firestore-wait
	@mkdir -p bin
	@$(GO) build -o bin/seed ./cmd/seed
	@$(EMULATOR_ENV) ./bin/seed -project $(EMULATOR_PROJECT) -database $(EMULATOR_DATABASE); rc=$$?; \
	if [ $$rc = 3 ]; then echo "Emulator already has data: sign in with your password (make firestore-reset starts over)."; \
	elif [ $$rc != 0 ]; then exit $$rc; fi

# Apple's container CLI needs its background service; say how to start it.
container-ready:
	@if [ "$(notdir $(CONTAINER_TOOL))" = container ] && ! container system status >/dev/null 2>&1; then \
		echo "Apple container service is not running: start it with 'container system start'"; exit 1; \
	fi

# Starts the emulator unless its container exists. A running emulator is
# never replaced: that would lose its data.
firestore: container-ready
	@if $(CONTAINER_TOOL) inspect $(EMULATOR_CONTAINER) >/dev/null 2>&1; then \
		echo "Firestore emulator already running ($(EMULATOR_CONTAINER))"; \
	else \
		$(CONTAINER_TOOL) run -d --rm --name $(EMULATOR_CONTAINER) -p 8086:8086 $(FIRESTORE_EMULATOR_IMAGE) \
			gcloud emulators firestore start --host-port=0.0.0.0:8086 >/dev/null && \
		echo "Firestore emulator started on $(EMULATOR_HOST)"; \
	fi

# Waits until the emulator answers (it takes a few seconds to start).
firestore-wait: firestore
	@for i in $$(seq 1 60); do curl -sf $(EMULATOR_HOST) >/dev/null && exit 0; sleep 1; done; \
		echo "The Firestore emulator did not answer on $(EMULATOR_HOST)"; exit 1

# Deletes every emulator document (the emulator keeps running), then seeds.
firestore-reset: firestore-wait
	curl -sf -X DELETE "http://$(EMULATOR_HOST)/emulator/v1/projects/$(EMULATOR_PROJECT)/databases/$(EMULATOR_DATABASE)/documents" >/dev/null
	@echo "Emulator data deleted."
	@$(MAKE) --no-print-directory seed-local

# Stops the emulator and waits until its container is gone, so the next
# `make firestore` can start a new one.
firestore-stop:
	@if $(CONTAINER_TOOL) inspect $(EMULATOR_CONTAINER) >/dev/null 2>&1; then \
		$(CONTAINER_TOOL) stop $(EMULATOR_CONTAINER) >/dev/null; \
		for i in $$(seq 1 30); do $(CONTAINER_TOOL) inspect $(EMULATOR_CONTAINER) >/dev/null 2>&1 || break; sleep 1; done; \
		echo "Firestore emulator stopped (its data is gone)"; \
	else echo "Firestore emulator is not running"; fi

test-store: firestore-wait
	FIRESTORE_EMULATOR_HOST=$(EMULATOR_HOST) $(GO) test -count=1 -race ./internal/store/... ./cmd/...

# Phone checks against cmd/e2e (memory store, seeded staff); needs the
# browsers once: bunx playwright install chromium webkit
mobile: templ assets
	$(BUN) run test:mobile

image: container-ready
	$(CONTAINER_TOOL) build -t aestheticlab-income:local .

docker-build: image

clean:
	rm -rf bin tmp build-errors.log coverage.out
