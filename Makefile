SHELL := /bin/bash
COMPOSE := docker-compose
COMPOSE_FILE := deploy/docker-compose.yml
COMPOSE_T480_FILE := deploy/docker-compose.t480.yml
GOPROXY ?= https://goproxy.cn,direct
NO_PROXY ?= localhost,127.0.0.1,postgres,redis,qdrant,memory-api,memory-web,memory-mcp,memory-llm-mock

.PHONY: test build-web smoke dev-up prod-up prod-up-services post-deploy-verify verify-light dev-down preflight secret-scan secret-injection-audit backup restore backup-restore-dry-run restore-rehearsal-preflight restore-rehearsal-dry-run docker-cleanup-plan docker-cleanup-images install-docker-cleanup-cron install-backup-cron verify audit-report final-delivery-report lint seed-dev t480-sync t480-build-check t480-deploy t480-deploy-api-fast t480-deploy-web-fast t480-deploy-api-web-fast t480-verify-light

test:
	@if command -v go >/dev/null 2>&1 && go version | grep -q 'go1\.25'; then \
		GOPROXY=$(GOPROXY) go test ./...; \
	else \
		docker run --rm -e GOPROXY=$(GOPROXY) -e NO_PROXY=$(NO_PROXY) -e no_proxy=$(NO_PROXY) -v "$$(pwd)":/src -w /src golang:1.25-bookworm go test ./...; \
	fi

build-web:
	@if command -v npm >/dev/null 2>&1; then \
		cd frontend && npm install && npm run build; \
	else \
		docker run --rm -e NO_PROXY=$(NO_PROXY) -e no_proxy=$(NO_PROXY) -v "$$(pwd)/frontend":/src -w /src node:22-bookworm bash -lc 'npm install && npm run build'; \
	fi

smoke:
	@if command -v go >/dev/null 2>&1 && go version | grep -q 'go1\.25'; then \
		SMOKE_ENABLE_DEV_ENDPOINTS=$${SMOKE_ENABLE_DEV_ENDPOINTS:-false} GOPROXY=$(GOPROXY) go run ./cmd/memory-smoke; \
	else \
		docker run --rm --network $${SMOKE_DOCKER_NETWORK:-host} \
			-e SMOKE_API_URL=$${SMOKE_API_URL:-} \
			-e SMOKE_QDRANT_URL=$${SMOKE_QDRANT_URL:-} \
			-e SMOKE_WEB_URL=$${SMOKE_WEB_URL:-} \
			-e SMOKE_MCP_URL=$${SMOKE_MCP_URL:-} \
			-e SMOKE_LLM_BASE_URL=$${SMOKE_LLM_BASE_URL:-} \
			-e SMOKE_LLM_API_KEY=$${SMOKE_LLM_API_KEY:-} \
			-e SMOKE_TIMEOUT=$${SMOKE_TIMEOUT:-} \
			-e SMOKE_ENABLE_DEV_ENDPOINTS=$${SMOKE_ENABLE_DEV_ENDPOINTS:-false} \
			-e SMOKE_ENABLE_TENANT_GOVERNANCE=$${SMOKE_ENABLE_TENANT_GOVERNANCE:-} \
			-e SMOKE_REQUIRE_CONFIGURED_RETRIEVAL=$${SMOKE_REQUIRE_CONFIGURED_RETRIEVAL:-false} \
			-e SMOKE_ENABLE_PIPELINE_E2E=$${SMOKE_ENABLE_PIPELINE_E2E:-false} \
			-e SMOKE_ADAPTER_TOKEN=$${SMOKE_ADAPTER_TOKEN:-} \
			-e SMOKE_SEARCH_PAT=$${SMOKE_SEARCH_PAT:-} \
			-e SMOKE_PIPELINE_E2E_MARKER=$${SMOKE_PIPELINE_E2E_MARKER:-} \
			-e SMOKE_PIPELINE_E2E_TIMEOUT=$${SMOKE_PIPELINE_E2E_TIMEOUT:-} \
			-e SMOKE_POSTGRES_DSN=$${SMOKE_POSTGRES_DSN:-} \
			-e SMOKE_SEARCH_USER_ID=$${SMOKE_SEARCH_USER_ID:-} \
			-e SMOKE_SEARCH_ORG_ID=$${SMOKE_SEARCH_ORG_ID:-} \
			-e SMOKE_SEARCH_PROJECT_ID=$${SMOKE_SEARCH_PROJECT_ID:-} \
			-e SMOKE_SEARCH_AGENT_ID=$${SMOKE_SEARCH_AGENT_ID:-} \
			-e SMOKE_SEARCH_PERMISSION_LABEL=$${SMOKE_SEARCH_PERMISSION_LABEL:-} \
			-e GOPROXY=$(GOPROXY) -e NO_PROXY=$(NO_PROXY) -e no_proxy=$(NO_PROXY) \
			-v "$$(pwd)":/src -w /src golang:1.25-bookworm go run ./cmd/memory-smoke; \
	fi

dev-up:
	POSTGRES_PASSWORD=$${POSTGRES_PASSWORD:-replace-me-local-only} \
	LLM_BASE_URL=$${LLM_BASE_URL:-http://example.local:8000} \
	LLM_API_KEY=$${LLM_API_KEY:-replace-me-dev-only} \
	APP_ENV=development ENABLE_DEV_ENDPOINTS=true $(COMPOSE) -f $(COMPOSE_FILE) -f $(COMPOSE_T480_FILE) up -d --build

prod-up-mock:
	if [[ -f .env.production || -f .env ]] || docker inspect deploy-memory-api-1 >/dev/null 2>&1; then \
		. scripts/load-prod-env.sh; \
	fi; \
	export POSTGRES_PASSWORD=$${POSTGRES_PASSWORD:-replace-me-mock-password}; \
	export LLM_BASE_URL=$${LLM_BASE_URL:-http://memory-llm-mock:11434}; \
	export LLM_API_KEY=$${LLM_API_KEY:-memory-llm-mock-key}; \
	. scripts/load-build-info.sh && \
	ALLOW_EXISTING_DEPLOYMENT=1 scripts/preflight.sh && \
	APP_ENV=production ENABLE_DEV_ENDPOINTS=false \
	$(COMPOSE) -f $(COMPOSE_FILE) -f $(COMPOSE_T480_FILE) up -d --build memory-api memory-worker memory-mcp memory-web memory-llm-mock

prod-up:
	. scripts/load-prod-env.sh && \
	. scripts/load-build-info.sh && \
	APP_ENV=production ALLOW_EXISTING_DEPLOYMENT=1 scripts/preflight.sh && \
	export COMPOSE_PROJECT_NAME=deploy && \
	APP_ENV=production ENABLE_DEV_ENDPOINTS=false \
	$(COMPOSE) -f $(COMPOSE_FILE) -f $(COMPOSE_T480_FILE) up -d --build memory-api memory-worker memory-mcp memory-web && \
	DRY_RUN=0 DOCKER_IMAGE_CLEANUP_MODE=dangling CONFIRM_DOCKER_IMAGE_CLEANUP=I_UNDERSTAND_IMAGE_DELETE bash scripts/docker-cleanup-images.sh

SERVICES ?= memory-api memory-worker memory-mcp memory-web
CLEANUP_IMAGES ?= 0
prod-up-services:
	. scripts/load-prod-env.sh && \
	. scripts/load-build-info.sh && \
	APP_ENV=production ALLOW_EXISTING_DEPLOYMENT=1 scripts/preflight.sh && \
	export COMPOSE_PROJECT_NAME=deploy && \
	APP_ENV=production ENABLE_DEV_ENDPOINTS=false \
	$(COMPOSE) -f $(COMPOSE_FILE) -f $(COMPOSE_T480_FILE) up -d --build $(SERVICES); \
	if [[ "$(CLEANUP_IMAGES)" == "1" ]]; then \
		DRY_RUN=0 DOCKER_IMAGE_CLEANUP_MODE=dangling CONFIRM_DOCKER_IMAGE_CLEANUP=I_UNDERSTAND_IMAGE_DELETE bash scripts/docker-cleanup-images.sh; \
	fi

post-deploy-verify:
	scripts/post-deploy-verify.sh

verify-light:
	scripts/verify-light.sh

dev-down:
	$(COMPOSE) -f $(COMPOSE_FILE) -f $(COMPOSE_T480_FILE) down

preflight:
	scripts/preflight.sh

secret-scan:
	scripts/secret-scan.sh

secret-injection-audit:
	scripts/secret-injection-audit.sh

backup:
	. scripts/load-prod-env.sh && export COMPOSE_PROJECT_NAME=deploy && scripts/backup.sh

restore:
	. scripts/load-prod-env.sh && export COMPOSE_PROJECT_NAME=deploy && scripts/restore.sh

backup-restore-dry-run:
	@set -euo pipefail; \
	run_id="$${RUN_ID:-dry-run-$$(date -u +%Y%m%dT%H%M%SZ)}"; \
	backup_root="$${BACKUP_ROOT:-$$(mktemp -d)}"; \
	RUN_ID="$$run_id" BACKUP_ROOT="$$backup_root" DRY_RUN=1 scripts/backup.sh; \
	backup_dir="$$backup_root/$$run_id"; \
	BACKUP_DIR=$$backup_dir DRY_RUN=1 scripts/restore.sh; \
	echo "backup-restore dry-run completed: $$backup_dir"

restore-rehearsal-dry-run:
	@set -euo pipefail; \
	if [[ -z "$${BACKUP_DIR:-}" ]]; then echo "BACKUP_DIR is required" >&2; exit 1; fi; \
	RESTORE_REHEARSAL_MODE=dry-run bash scripts/restore-rehearsal.sh

restore-rehearsal-preflight:
	@set -euo pipefail; \
	if [[ -z "$${BACKUP_DIR:-}" ]]; then echo "BACKUP_DIR is required" >&2; exit 1; fi; \
	bash scripts/restore-rehearsal-preflight.sh

docker-cleanup-plan:
	bash scripts/docker-cleanup-plan.sh

docker-cleanup-images:
	bash scripts/docker-cleanup-images.sh

install-docker-cleanup-cron:
	bash scripts/install-docker-cleanup-cron.sh

install-backup-cron:
	scripts/install-backup-cron.sh

verify:
	scripts/verify.sh

audit-report:
	scripts/audit-report.sh

final-delivery-report:
	scripts/final-delivery-report.sh

t480-sync:
	bash scripts/sync-t480.sh

t480-build-check: t480-sync
	ssh $${TARGET_HOST:-thinkpad} 'cd $${TARGET_DIR:-/opt/memory-os} && make test && make build-web'

t480-deploy: t480-sync
	ssh $${TARGET_HOST:-thinkpad} 'cd $${TARGET_DIR:-/opt/memory-os} && make prod-up && make post-deploy-verify'

t480-deploy-api-fast: t480-sync
	ssh $${TARGET_HOST:-thinkpad} 'cd $${TARGET_DIR:-/opt/memory-os} && SERVICES="memory-api" make prod-up-services && make verify-light'

t480-deploy-web-fast: t480-sync
	ssh $${TARGET_HOST:-thinkpad} 'cd $${TARGET_DIR:-/opt/memory-os} && SERVICES="memory-web" make prod-up-services && make verify-light'

t480-deploy-api-web-fast: t480-sync
	ssh $${TARGET_HOST:-thinkpad} 'cd $${TARGET_DIR:-/opt/memory-os} && SERVICES="memory-api memory-web" make prod-up-services && make verify-light'

t480-verify-light:
	ssh $${TARGET_HOST:-thinkpad} 'cd $${TARGET_DIR:-/opt/memory-os} && make verify-light'

lint:
	gofmt -w cmd internal
	GOPROXY=$(GOPROXY) go test ./...

seed-dev:
	@echo "seed-dev is not implemented in phase 1"
	@exit 1
