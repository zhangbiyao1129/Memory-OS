SHELL := /bin/bash
COMPOSE := docker-compose
COMPOSE_FILE := deploy/docker-compose.yml
COMPOSE_T480_FILE := deploy/docker-compose.t480.yml
GOPROXY ?= https://goproxy.cn,direct

.PHONY: test build-web smoke dev-up dev-down preflight secret-scan backup restore install-backup-cron verify audit-report lint seed-dev

test:
	@if command -v go >/dev/null 2>&1; then \
		GOPROXY=$(GOPROXY) go test ./...; \
	else \
		docker run --rm -e GOPROXY=$(GOPROXY) -v "$$(pwd)":/src -w /src golang:1.25-bookworm go test ./...; \
	fi

build-web:
	@if command -v npm >/dev/null 2>&1; then \
		cd web && npm install && npm run build; \
	else \
		docker run --rm -v "$$(pwd)/web":/src -w /src node:22-bookworm bash -lc 'npm install && npm run build'; \
	fi

smoke:
	@if command -v go >/dev/null 2>&1; then \
		GOPROXY=$(GOPROXY) go run ./cmd/memory-smoke; \
	else \
		docker run --rm --network host -e GOPROXY=$(GOPROXY) -v "$$(pwd)":/src -w /src golang:1.25-bookworm go run ./cmd/memory-smoke; \
	fi

dev-up:
	$(COMPOSE) -f $(COMPOSE_FILE) -f $(COMPOSE_T480_FILE) up -d --build

dev-down:
	$(COMPOSE) -f $(COMPOSE_FILE) -f $(COMPOSE_T480_FILE) down

preflight:
	scripts/preflight.sh

secret-scan:
	scripts/secret-scan.sh

backup:
	scripts/backup.sh

restore:
	scripts/restore.sh

install-backup-cron:
	scripts/install-backup-cron.sh

verify:
	scripts/verify.sh

audit-report:
	scripts/audit-report.sh

lint:
	gofmt -w cmd internal
	GOPROXY=$(GOPROXY) go test ./...

seed-dev:
	@echo "seed-dev is not implemented in phase 1"
	@exit 1
