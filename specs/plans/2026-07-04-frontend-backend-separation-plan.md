# Memory OS 前后端分区规范化 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 把 `web/` 改名为 `frontend/`，Dockerfile 按前后端拆到 `deploy/{backend,frontend}/`，Makefile 修正路径并固定 `COMPOSE_PROJECT_NAME=deploy`，preflight 新增 placeholder secret 检查。

**架构：** 不移动 Go 后端目录（`cmd/`、`internal/`、`migrations/` 留根，避免 Go `internal` 规则导致 import 失效）。只动前端目录名、Dockerfile 位置、Makefile 路径引用、配置文件。所有改动可回滚——已对 T480 全量备份（`/root/memory-os-rollback-20260704-093720/`）。

**技术栈：** Go 1.25 / Hertz / Nuxt 3 / Docker Compose / Make

**参考规格：** `specs/2026-07-04-frontend-backend-separation-design.md`

**验证环境：** 本地 macOS 跑 `go test`/`go build`/`docker-compose config`；前端 `nuxt build` 和完整容器验证在 T480 跑（项目规则：本地不跑容器）。本地 Go 1.25.0 已确认。

---

## 文件结构

**移动（git mv 保留历史）：**
- `web/` → `frontend/`（整个目录）
- `deploy/Dockerfile.api` → `deploy/backend/Dockerfile.api`
- `deploy/Dockerfile.worker` → `deploy/backend/Dockerfile.worker`
- `deploy/Dockerfile.mcp` → `deploy/backend/Dockerfile.mcp`
- `deploy/Dockerfile.llm-mock` → `deploy/backend/Dockerfile.llm-mock`
- `deploy/memory-llm-mock.py` → `deploy/backend/memory-llm-mock.py`
- `deploy/Dockerfile.web` → `deploy/frontend/Dockerfile.web`
- `deploy/nginx.conf` → `deploy/frontend/nginx.conf`

**修改：**
- `Makefile` — `web` → `frontend`，prod-up/backup/restore 加 `export COMPOSE_PROJECT_NAME=deploy`，Go 1.25 检测
- `deploy/docker-compose.yml` — 5 处 `dockerfile:` 路径
- `deploy/docker-compose.t480.yml` — 若有 dockerfile 引用
- `deploy/docker-compose.restore-rehearsal.yml` — 若有 dockerfile 引用
- `deploy/backend/Dockerfile.api/worker/mcp` — `COPY . .` 改为精确 COPY（避免拷 frontend）
- `deploy/backend/Dockerfile.llm-mock` — `COPY deploy/memory-llm-mock.py` → `COPY deploy/backend/memory-llm-mock.py`
- `deploy/frontend/Dockerfile.web` — `web/` → `frontend/`，nginx.conf 路径
- `internal/webdeploy/*_test.go` — `web/` → `frontend/`，`../../web/` → `../../frontend/`
- `.gitignore` — `web/dist` → `frontend/dist`
- `.dockerignore` — `web/...` → `frontend/...`
- `frontend/package.json` — name `memory-os-web` → `memory-os-frontend`
- `scripts/preflight.sh` — 新增 placeholder secret 检查

**新建：**
- `frontend/README.md` — 前端说明
- `frontend/.env.example` — 前端环境变量样例

---

## 阶段 1：前端目录规范化

### 任务 1：重命名 web/ 为 frontend/

**文件：**
- 移动：`web/` → `frontend/`

- [ ] **步骤 1：git mv 重命名**

```bash
cd "/Users/kanyun/Memory OS"
git mv web frontend
```

- [ ] **步骤 2：确认移动成功**

运行：`ls frontend/ && ls web/ 2>/dev/null`
预期：`frontend/` 列出 app.vue、pages/ 等；`web/` 不存在

- [ ] **步骤 3：确认 git 跟踪状态**

运行：`git status --short | head -5`
预期：显示 `renamed: web/... -> frontend/...`

---

### 任务 2：更新 frontend/package.json 的 name

**文件：**
- 修改：`frontend/package.json`

- [ ] **步骤 1：修改 name 字段**

把 `"name": "memory-os-web"` 改为 `"name": "memory-os-frontend"`。

```json
{
  "name": "memory-os-frontend",
  "private": true,
  "type": "module",
  ...
}
```

- [ ] **步骤 2：确认修改**

运行：`grep '"name"' frontend/package.json`
预期：`"name": "memory-os-frontend"`

---

### 任务 3：新建 frontend/.env.example 和 frontend/README.md

**文件：**
- 创建：`frontend/.env.example`
- 创建：`frontend/README.md`

- [ ] **步骤 1：创建 frontend/.env.example**

```bash
cat > frontend/.env.example << 'EOF'
# 前端环境变量样例
# Nuxt 公共 API 地址，构建时注入
NUXT_PUBLIC_API_BASE=http://localhost:18081
EOF
```

- [ ] **步骤 2：创建 frontend/README.md**

````markdown
# Memory OS 前端管理台

Nuxt 3 静态构建，托管在 Nginx。

## 开发

```bash
npm install
npm run dev
```

## 构建

```bash
npm install
npm run build      # 或 npm run generate 生成静态站点
```

## 环境变量

- `NUXT_PUBLIC_API_BASE`：后端 API 地址，构建时注入（见 `nuxt.config.ts` 的 `runtimeConfig.public.apiBase`）

## 目录

- `pages/`：路由页面
- `components/`：Vue 组件
- `composables/`：组合式函数
- `stores/`：Pinia 状态
- `assets/`：静态资源
````

- [ ] **步骤 3：确认文件存在**

运行：`ls frontend/.env.example frontend/README.md`
预期：两个文件都存在

---

### 任务 4：更新 .gitignore 和 .dockerignore 的前端路径

**文件：**
- 修改：`.gitignore`
- 修改：`.dockerignore`

- [ ] **步骤 1：修改 .gitignore**

把 `web/dist/` 改为 `frontend/dist/`。其余 `node_modules/`、`.nuxt/`、`.output/`、`dist/` 是根级通用规则，不动。

当前行：
```
web/dist/
```
改为：
```
frontend/dist/
```

- [ ] **步骤 2：修改 .dockerignore**

把所有 `web/...` 改为 `frontend/...`。

当前：
```
web/node_modules/
web/.nuxt/
web/.output/
web/dist/
```
改为：
```
frontend/node_modules/
frontend/.nuxt/
frontend/.output/
frontend/dist/
```

- [ ] **步骤 3：确认无残留 web/ 引用**

运行：`grep -n "web/" .gitignore .dockerignore`
预期：无输出（或只匹配到非路径的注释）

---

### 任务 5：更新 internal/webdeploy 测试里的前端路径引用

**文件：**
- 修改：`internal/webdeploy/web_dockerfile_test.go`（及该目录下其他 `*_test.go`）

> 注意：测试文件在 `internal/webdeploy/` 下（留根，不移动），相对路径 `../../web/` 指向仓库根的 `web/`。改名后只需把 `web/` 替换为 `frontend/`，层级不变（因为测试文件位置没动）。

- [ ] **步骤 1：批量替换测试文件里的 web/ 路径**

```bash
cd "/Users/kanyun/Memory OS"
sed -i '' 's|/src/web/|/src/frontend/|g; s|"web/.nuxt/"|"frontend/.nuxt/"|g; s|"web/.output/"|"frontend/.output/"|g; s|"web/"|"frontend/"|g; s|\.\./\.\./web/|../../frontend/|g' internal/webdeploy/*_test.go
```

- [ ] **步骤 2：确认替换无残留**

运行：`grep -n "web/" internal/webdeploy/*_test.go`
预期：无输出（所有 `web/` 路径已改为 `frontend/`）

- [ ] **步骤 3：确认测试仍能编译**

运行：`go vet ./internal/webdeploy/...`
预期：无错误

- [ ] **步骤 4：运行 webdeploy 测试**

运行：`go test ./internal/webdeploy/... -count=1`
预期：PASS。如果 FAIL，检查是否有遗漏的 `web/` 路径或测试断言里的字符串没改（断言文本对应 Dockerfile.web 的内容，Dockerfile.web 还没改，此时测试会因 Dockerfile 内容仍是 `web/` 而失败——这是预期的，任务 9 改完 Dockerfile.web 后再跑会通过）。

> 说明：此任务后测试可能 FAIL（因为 Dockerfile.web 还没改）。这是 TDD 的「确认覆盖缺口」步骤。任务 9 改完 Dockerfile.web 后测试会通过。如果不想中间状态失败，可把任务 5 移到任务 9 之后。本计划保持顺序，接受中间 FAIL。

---

### 任务 6：更新根 Makefile 的前端路径和 Go 1.25 检测

**文件：**
- 修改：`Makefile`

- [ ] **步骤 1：修改 build-web target**

把 `cd web` 改为 `cd frontend`，`$(pwd)/web` 改为 `$(pwd)/frontend`。

当前：
```makefile
build-web:
	@if command -v npm >/dev/null 2>&1; then \
		cd web && npm install && npm run build; \
	else \
		docker run --rm -e NO_PROXY=$(NO_PROXY) -e no_proxy=$(NO_PROXY) -v "$$(pwd)/web":/src -w /src node:22-bookworm bash -lc 'npm install && npm run build'; \
	fi
```

改为：
```makefile
build-web:
	@if command -v npm >/dev/null 2>&1; then \
		cd frontend && npm install && npm run build; \
	else \
		docker run --rm -e NO_PROXY=$(NO_PROXY) -e no_proxy=$(NO_PROXY) -v "$$(pwd)/frontend":/src -w /src node:22-bookworm bash -lc 'npm install && npm run build'; \
	fi
```

- [ ] **步骤 2：给 test target 加 Go 1.25 检测**

当前 `test` 只检测 `command -v go`，不检测版本。T480 是 Go 1.24.1，会因版本不符失败。改为检测 `go1.25`，不满足 fallback 到 Docker。

当前：
```makefile
test:
	@if command -v go >/dev/null 2>&1; then \
		GOPROXY=$(GOPROXY) go test ./...; \
	else \
		docker run --rm -e GOPROXY=$(GOPROXY) -e NO_PROXY=$(NO_PROXY) -e no_proxy=$(NO_PROXY) -v "$$(pwd)":/src -w /src golang:1.25-bookworm go test ./...; \
	fi
```

改为：
```makefile
test:
	@if command -v go >/dev/null 2>&1 && go version | grep -q 'go1\.25'; then \
		GOPROXY=$(GOPROXY) go test ./...; \
	else \
		docker run --rm -e GOPROXY=$(GOPROXY) -e NO_PROXY=$(NO_PROXY) -e no_proxy=$(NO_PROXY) -v "$$(pwd)":/src -w /src golang:1.25-bookworm go test ./...; \
	fi
```

- [ ] **步骤 3：同样给 smoke target 加 Go 1.25 检测**

把 `smoke` target 里的 `if command -v go >/dev/null 2>&1` 改为 `if command -v go >/dev/null 2>&1 && go version | grep -q 'go1\.25'`。

- [ ] **步骤 4：给 prod-up 加 export COMPOSE_PROJECT_NAME=deploy**

在 `prod-up` target 的 `APP_ENV=production` 前加 `export COMPOSE_PROJECT_NAME=deploy && \`。

当前：
```makefile
prod-up:
	. scripts/load-prod-env.sh && \
	. scripts/load-build-info.sh && \
	ALLOW_EXISTING_DEPLOYMENT=1 scripts/preflight.sh && \
	APP_ENV=production ENABLE_DEV_ENDPOINTS=false \
	$(COMPOSE) -f $(COMPOSE_FILE) -f $(COMPOSE_T480_FILE) up -d --build memory-api memory-worker memory-mcp memory-web && \
	DRY_RUN=0 DOCKER_IMAGE_CLEANUP_MODE=dangling CONFIRM_DOCKER_IMAGE_CLEANUP=I_UNDERSTAND_IMAGE_DELETE bash scripts/docker-cleanup-images.sh
```

改为：
```makefile
prod-up:
	. scripts/load-prod-env.sh && \
	. scripts/load-build-info.sh && \
	ALLOW_EXISTING_DEPLOYMENT=1 scripts/preflight.sh && \
	export COMPOSE_PROJECT_NAME=deploy && \
	APP_ENV=production ENABLE_DEV_ENDPOINTS=false \
	$(COMPOSE) -f $(COMPOSE_FILE) -f $(COMPOSE_T480_FILE) up -d --build memory-api memory-worker memory-mcp memory-web && \
	DRY_RUN=0 DOCKER_IMAGE_CLEANUP_MODE=dangling CONFIRM_DOCKER_IMAGE_CLEANUP=I_UNDERSTAND_IMAGE_DELETE bash scripts/docker-cleanup-images.sh
```

- [ ] **步骤 5：给 backup 和 restore 加 export COMPOSE_PROJECT_NAME=deploy**

当前：
```makefile
backup:
	. scripts/load-prod-env.sh && scripts/backup.sh

restore:
	. scripts/load-prod-env.sh && scripts/restore.sh
```

改为：
```makefile
backup:
	. scripts/load-prod-env.sh && export COMPOSE_PROJECT_NAME=deploy && scripts/backup.sh

restore:
	. scripts/load-prod-env.sh && export COMPOSE_PROJECT_NAME=deploy && scripts/restore.sh
```

- [ ] **步骤 6：确认 Makefile 无残留 web 引用（除 NO_PROXY 里的 memory-web 服务名）**

运行：`grep -n "web" Makefile | grep -v "memory-web\|memory-web"`
预期：无 `cd web` 或 `$(pwd)/web` 残留（`memory-web` 是 compose 服务名，保留）

---

### 任务 7：阶段 1 验证和 commit

**文件：** 无（验证 + commit）

- [ ] **步骤 1：本地 go test 确认 webdeploy 之外的测试通过**

运行：`go test ./internal/... -count=1 2>&1 | tail -20`
预期：除 `internal/webdeploy`（因 Dockerfile.web 还没改，预期 FAIL）外，其他包 PASS

- [ ] **步骤 2：commit 阶段 1**

```bash
cd "/Users/kanyun/Memory OS"
git add -A
git commit -m "refactor(frontend): web/ 改名为 frontend/ 并更新路径引用

- git mv web frontend
- frontend/package.json name 改为 memory-os-frontend
- 新增 frontend/README.md 和 frontend/.env.example
- .gitignore/.dockerignore 前端路径 web/ -> frontend/
- internal/webdeploy 测试路径 web/ -> frontend/
- Makefile build-web 改 frontend, test/smoke 加 Go 1.25 检测
- Makefile prod-up/backup/restore 固定 COMPOSE_PROJECT_NAME=deploy

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## 阶段 2：Docker 布局规范化

### 任务 8：移动 Dockerfile 到 deploy/{backend,frontend}/

**文件：**
- 移动：8 个文件（见下）

- [ ] **步骤 1：创建目录并 git mv**

```bash
cd "/Users/kanyun/Memory OS"
mkdir -p deploy/backend deploy/frontend
git mv deploy/Dockerfile.api deploy/backend/Dockerfile.api
git mv deploy/Dockerfile.worker deploy/backend/Dockerfile.worker
git mv deploy/Dockerfile.mcp deploy/backend/Dockerfile.mcp
git mv deploy/Dockerfile.llm-mock deploy/backend/Dockerfile.llm-mock
git mv deploy/memory-llm-mock.py deploy/backend/memory-llm-mock.py
git mv deploy/Dockerfile.web deploy/frontend/Dockerfile.web
git mv deploy/nginx.conf deploy/frontend/nginx.conf
```

- [ ] **步骤 2：确认移动**

运行：`ls deploy/backend/ deploy/frontend/`
预期：
```
deploy/backend/: Dockerfile.api Dockerfile.llm-mock Dockerfile.mcp Dockerfile.worker memory-llm-mock.py
deploy/frontend/: Dockerfile.web nginx.conf
```

---

### 任务 9：更新 deploy/frontend/Dockerfile.web 的路径

**文件：**
- 修改：`deploy/frontend/Dockerfile.web`

- [ ] **步骤 1：修改 COPY 和 WORKDIR 路径**

当前：
```dockerfile
FROM node:22-bookworm AS build
WORKDIR /src/web
ARG NUXT_PUBLIC_API_BASE=http://localhost:18081
ENV NUXT_PUBLIC_API_BASE=${NUXT_PUBLIC_API_BASE}
COPY web/package*.json ./
RUN npm install
COPY web/ ./
RUN npm run generate

FROM nginx:1.27-alpine
COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
RUN rm -rf /usr/share/nginx/html/*
COPY --from=build /src/web/.output/public/ /usr/share/nginx/html/
EXPOSE 18080
```

改为：
```dockerfile
FROM node:22-bookworm AS build
WORKDIR /src/frontend
ARG NUXT_PUBLIC_API_BASE=http://localhost:18081
ENV NUXT_PUBLIC_API_BASE=${NUXT_PUBLIC_API_BASE}
COPY frontend/package*.json ./
RUN npm install
COPY frontend/ ./
RUN npm run generate

FROM nginx:1.27-alpine
COPY deploy/frontend/nginx.conf /etc/nginx/conf.d/default.conf
RUN rm -rf /usr/share/nginx/html/*
COPY --from=build /src/frontend/.output/public/ /usr/share/nginx/html/
EXPOSE 18080
```

- [ ] **步骤 2：确认无残留 web/ 引用**

运行：`grep -n "web" deploy/frontend/Dockerfile.web`
预期：无输出

---

### 任务 10：更新 deploy/backend/Dockerfile.llm-mock 的 COPY 路径

**文件：**
- 修改：`deploy/backend/Dockerfile.llm-mock`

- [ ] **步骤 1：修改 COPY 路径**

当前：
```dockerfile
FROM python:3.12-alpine

WORKDIR /app
COPY deploy/memory-llm-mock.py /app/memory-llm-mock.py
ENV PYTHONUNBUFFERED=1
ENTRYPOINT ["python3", "/app/memory-llm-mock.py"]
```

改为：
```dockerfile
FROM python:3.12-alpine

WORKDIR /app
COPY deploy/backend/memory-llm-mock.py /app/memory-llm-mock.py
ENV PYTHONUNBUFFERED=1
ENTRYPOINT ["python3", "/app/memory-llm-mock.py"]
```

- [ ] **步骤 2：确认**

运行：`grep "memory-llm-mock.py" deploy/backend/Dockerfile.llm-mock`
预期：`COPY deploy/backend/memory-llm-mock.py /app/memory-llm-mock.py`

---

### 任务 11：更新 deploy/backend/Dockerfile.{api,worker,mcp} 的 COPY 策略

**文件：**
- 修改：`deploy/backend/Dockerfile.api`
- 修改：`deploy/backend/Dockerfile.worker`
- 修改：`deploy/backend/Dockerfile.mcp`

> 这三个 Dockerfile 当前用 `COPY . .` 把整个仓库拷进构建镜像（含 frontend/、docs/ 等），浪费空间且慢。改为精确 COPY。build context 仍是仓库根（compose 里 `context: ..` 不变）。

- [ ] **步骤 1：修改 Dockerfile.api 的 COPY 行**

当前：
```dockerfile
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -ldflags "..." -o /out/memory-api ./cmd/memory-api
```

改为：
```dockerfile
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
RUN go build -ldflags "-X memory-os/internal/buildinfo.Version=${BUILD_VERSION} -X memory-os/internal/buildinfo.Commit=${BUILD_COMMIT} -X memory-os/internal/buildinfo.BuildTime=${BUILD_TIME} -X memory-os/internal/buildinfo.Dirty=${BUILD_DIRTY}" -o /out/memory-api ./cmd/memory-api
```

> 注意：`go build ./cmd/memory-api` 路径不变（cmd/ 还在根，build context 是根）。

- [ ] **步骤 2：Dockerfile.worker 同样改 COPY 行**

把 `COPY . .` 改为：
```dockerfile
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
```
`go build ... ./cmd/memory-worker` 路径不变。

- [ ] **步骤 3：Dockerfile.mcp 同样改 COPY 行**

把 `COPY . .` 改为：
```dockerfile
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
```
`go build ... ./cmd/memory-mcp` 路径不变。

- [ ] **步骤 4：确认三个 Dockerfile 的 COPY 行**

运行：`grep -n "COPY" deploy/backend/Dockerfile.api deploy/backend/Dockerfile.worker deploy/backend/Dockerfile.mcp`
预期：每个文件都是 `COPY go.mod go.sum` + `COPY cmd/` + `COPY internal/` + `COPY migrations/`，无 `COPY . .`

---

### 任务 12：更新 docker-compose.yml 的 dockerfile 路径

**文件：**
- 修改：`deploy/docker-compose.yml`
- 修改：`deploy/docker-compose.t480.yml`（若有 dockerfile 引用）
- 修改：`deploy/docker-compose.restore-rehearsal.yml`（若有 dockerfile 引用）

- [ ] **步骤 1：更新 docker-compose.yml 的 5 处 dockerfile 路径**

当前：
```yaml
      dockerfile: deploy/Dockerfile.llm-mock
      ...
      dockerfile: deploy/Dockerfile.api
      ...
      dockerfile: deploy/Dockerfile.worker
      ...
      dockerfile: deploy/Dockerfile.mcp
      ...
      dockerfile: deploy/Dockerfile.web
```

改为：
```yaml
      dockerfile: deploy/backend/Dockerfile.llm-mock
      ...
      dockerfile: deploy/backend/Dockerfile.api
      ...
      dockerfile: deploy/backend/Dockerfile.worker
      ...
      dockerfile: deploy/backend/Dockerfile.mcp
      ...
      dockerfile: deploy/frontend/Dockerfile.web
```

`context: ..` 保持不变。

- [ ] **步骤 2：检查 t480 和 restore-rehearsal compose 是否有 dockerfile 引用**

运行：`grep -n "dockerfile:" deploy/docker-compose.t480.yml deploy/docker-compose.restore-rehearsal.yml`
预期：若有 `deploy/Dockerfile.*`，同步改为 `deploy/backend/...` 或 `deploy/frontend/...`；若无输出，跳过

- [ ] **步骤 3：确认无残留 deploy/Dockerfile 路径**

运行：`grep -n "deploy/Dockerfile" deploy/docker-compose*.yml`
预期：无输出（所有都改为 `deploy/backend/Dockerfile.*` 或 `deploy/frontend/Dockerfile.*`）

---

### 任务 13：阶段 2 验证和 commit

**文件：** 无（验证 + commit）

- [ ] **步骤 1：本地 docker-compose config 静态校验**

运行：
```bash
cd "/Users/kanyun/Memory OS"
POSTGRES_PASSWORD=local-check \
LLM_API_KEY=local-check \
SECRET_VAULT_KEY_ID=local-check \
SECRET_VAULT_KEY_B64=dGVzdC1rZXktMTIzNDU2Nw== \
docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml config >/tmp/memory-os-compose-config.txt
```
预期：命令成功，输出 compose 配置到 `/tmp/memory-os-compose-config.txt`，无报错。检查输出里 `dockerfile:` 路径都是 `deploy/backend/...` 或 `deploy/frontend/...`。

- [ ] **步骤 2：本地 webdeploy 测试现在应该通过**

运行：`go test ./internal/webdeploy/... -count=1`
预期：PASS（Dockerfile.web 已改，测试断言匹配）

- [ ] **步骤 3：本地 go build 确认编译通过**

运行：`go build ./...`
预期：无错误

- [ ] **步骤 4：commit 阶段 2**

```bash
cd "/Users/kanyun/Memory OS"
git add -A
git commit -m "refactor(deploy): Dockerfile 按前后端拆到 deploy/{backend,frontend}/

- Dockerfile.api/worker/mcp/llm-mock + memory-llm-mock.py 移到 deploy/backend/
- Dockerfile.web + nginx.conf 移到 deploy/frontend/
- 后端 Dockerfile COPY . . 改为精确 COPY cmd/internal/migrations
- 前端 Dockerfile.web 路径 web/ -> frontend/, nginx.conf 路径同步
- docker-compose.yml 5 处 dockerfile 路径更新

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## 阶段 3：Makefile 和生产入口规范化

### 任务 14：preflight 新增 placeholder secret 检查

**文件：**
- 修改：`scripts/preflight.sh`

> 当前 preflight.sh（78 行）只检查端口、磁盘、docker/compose 命令，不检查 secret 值。新增：当 `APP_ENV=production` 或检测到生产 compose 命令时，拒绝 `replace-me`、`example`、`dev-only`、`mock` 这类占位值出现在 `POSTGRES_PASSWORD`、`LLM_API_KEY`、`SECRET_VAULT_KEY_ID`、`SECRET_VAULT_KEY_B64` 中。

- [ ] **步骤 1：在 preflight.sh 末尾（exit 0 之前）加 placeholder 检查函数**

在 `scripts/preflight.sh` 末尾、最终 `exit 0` 之前，插入：

```bash
# placeholder secret 检查：生产环境不允许占位值
check_placeholder_secrets() {
  local app_env="${APP_ENV:-}"
  if [[ "$app_env" != "production" ]]; then
    return 0
  fi
  local placeholder_patterns='replace-me|example|dev-only|mock'
  local secret_vars=("POSTGRES_PASSWORD" "LLM_API_KEY" "SECRET_VAULT_KEY_ID" "SECRET_VAULT_KEY_B64")
  for var in "${secret_vars[@]}"; do
    local val="${!var:-}"
    if [[ -z "$val" ]]; then
      echo "production secret check failed: $var is empty" >&2
      exit 1
    fi
    if [[ "$val" =~ ^($placeholder_patterns) ]]; then
      echo "production secret check failed: $var has placeholder value" >&2
      exit 1
    fi
  done
}
check_placeholder_secrets
```

- [ ] **步骤 2：确认语法正确**

运行：`bash -n scripts/preflight.sh`
预期：无输出（语法正确）

- [ ] **步骤 3：测试 placeholder 检查在非生产环境不触发**

运行：`APP_ENV=development bash scripts/preflight.sh 2>&1 | tail -5`
预期：正常执行（不因 secret 检查失败）

- [ ] **步骤 4：测试 placeholder 检查在生产环境拒绝占位值**

运行：
```bash
APP_ENV=production \
POSTGRES_PASSWORD=replace-me \
LLM_API_KEY=dev-only \
SECRET_VAULT_KEY_ID=mock \
SECRET_VAULT_KEY_B64=example \
bash scripts/preflight.sh 2>&1 | tail -5
```
预期：输出 `production secret check failed: ...` 并退出非 0

- [ ] **步骤 5：测试 placeholder 检查在生产环境接受真实值**

运行：
```bash
APP_ENV=production \
POSTGRES_PASSWORD=real-password \
LLM_API_KEY=real-key \
SECRET_VAULT_KEY_ID=real-id \
SECRET_VAULT_KEY_B64=dGVzdC1rZXktMTIzNDU2Nw== \
bash scripts/preflight.sh 2>&1 | tail -5
```
预期：不因 secret 检查失败（可能因其他端口检查失败，但不应是 secret 检查失败）

---

### 任务 15：阶段 3 验证和 commit

**文件：** 无（验证 + commit）

- [ ] **步骤 1：完整 go test**

运行：`go test ./... -count=1 2>&1 | tail -20`
预期：全部 PASS

- [ ] **步骤 2：go vet**

运行：`go vet ./...`
预期：无错误

- [ ] **步骤 3：gofmt 检查**

运行：`gofmt -l cmd internal`
预期：无输出（所有文件格式正确）

- [ ] **步骤 4：commit 阶段 3**

```bash
cd "/Users/kanyun/Memory OS"
git add -A
git commit -m "feat(preflight): 新增 placeholder secret 检查

生产环境(APP_ENV=production)启动时拒绝 replace-me/example/dev-only/mock
占位值出现在 POSTGRES_PASSWORD/LLM_API_KEY/SECRET_VAULT_KEY_ID/
SECRET_VAULT_KEY_B64 中。

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## 阶段 4：push 到 GitHub（本地阶段完成）

### 任务 16：push 本地改动到 GitHub

**文件：** 无（git 操作）

- [ ] **步骤 1：确认本地测试全通过**

运行：`go test ./... -count=1 && go vet ./... && go build ./...`
预期：全部成功

- [ ] **步骤 2：确认 git 状态干净**

运行：`git status --short`
预期：无未提交改动

- [ ] **步骤 3：push 到 GitHub**

运行：`git push`
预期：push 成功

> 此后阶段 5-6 在 T480 服务器执行，涉及生产环境操作，每步需用户确认。

---

## 阶段 5：T480 git clone 演练（服务器操作,需用户确认）

> **此阶段在 T480 执行，涉及生产环境。每个步骤前需用户确认。不直接覆盖 `/opt/memory-os`，用 `/opt/memory-os-next` 演练。**

### 任务 17：T480 全量备份（演练前安全网）

**文件：** 无（服务器操作）

- [ ] **步骤 1：确认已有备份**

已在 2026-07-04 09:37 做过全量备份：`/root/memory-os-rollback-20260704-093720/`（代码+PostgreSQL+Qdrant，已验证可恢复）。

运行：`ssh thinkpad "ls /root/memory-os-rollback-20260704-093720/"`
预期：列出 code.tar.gz、postgres.dump、qdrant.snapshot、containers.txt、volumes.txt、ROLLBACK.md

- [ ] **步骤 2：再跑一次 make backup 确保最新**

运行：`ssh thinkpad "cd /opt/memory-os && make backup 2>&1 | tail -10"`
预期：备份成功，产物在 `/opt/memory-os/backups/`

> 此步骤需用户确认后执行。

---

### 任务 18：创建 /opt/memory-os-next 并 git clone

**文件：** 无（服务器操作）

- [ ] **步骤 1：确认 GitHub 仓库 URL**

运行：`cd "/Users/kanyun/Memory OS" && git remote get-url origin`
预期：`git@github.com:zhangbiyao1129/Memory-OS.git`

- [ ] **步骤 2：在 T480 clone 到 memory-os-next**

运行：
```bash
ssh thinkpad "cd /opt && git clone git@github.com:zhangbiyao1129/Memory-OS.git memory-os-next 2>&1 | tail -5"
```
预期：clone 成功

> 需用户确认。如果 T480 没配 GitHub SSH key，需改用 HTTPS + token，或先把代码 rsync 过去。

- [ ] **步骤 3：复制 .env.production**

运行：
```bash
ssh thinkpad "cp /opt/memory-os/.env.production /opt/memory-os-next/.env.production 2>/dev/null && chmod 600 /opt/memory-os-next/.env.production && ls -la /opt/memory-os-next/.env.production"
```
预期：文件权限 600

> 注意：当前 `/opt/memory-os/.env.production` 可能不存在（之前验证显示只有 `.env.example`）。如果不存在，需先从当前运行容器的环境变量里提取真实密钥创建。此步骤需用户确认如何获取密钥。

- [ ] **步骤 4：静态校验 compose**

运行：
```bash
ssh thinkpad "cd /opt/memory-os-next && export MEMORY_OS_ENV_FILE=.env.production && . scripts/load-prod-env.sh && COMPOSE_PROJECT_NAME=deploy docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml config >/tmp/next-compose-config.txt 2>&1 && echo CONFIG_OK || cat /tmp/next-compose-config.txt"
```
预期：输出 `CONFIG_OK`

---

### 任务 19：在新目录构建并验证

**文件：** 无（服务器操作）

- [ ] **步骤 1：确认 Go 1.25 工具链**

运行：`ssh thinkpad "cd /opt/memory-os-next && command -v go && go version"`
预期：T480 是 Go 1.24.1，不满足 1.25。Makefile 会 fallback 到 `golang:1.25-bookworm` Docker 镜像。

- [ ] **步骤 2：make test（用 Docker fallback）**

运行：`ssh thinkpad "cd /opt/memory-os-next && make test 2>&1 | tail -20"`
预期：测试通过（首次会拉 golang:1.25 镜像，较慢）

> 需用户确认。

- [ ] **步骤 3：make build-web**

运行：`ssh thinkpad "cd /opt/memory-os-next && make build-web 2>&1 | tail -20"`
预期：Nuxt 构建成功

- [ ] **步骤 4：make prod-up（启动新容器）**

> 此步骤会启动新容器，但用 `COMPOSE_PROJECT_NAME=deploy` 会和当前生产容器冲突（同名）。需先确认：是停掉当前生产容器再起，还是用不同 project name 演练。

运行：`ssh thinkpad "cd /opt/memory-os-next && MEMORY_OS_ENV_FILE=.env.production make prod-up 2>&1 | tail -20"`
预期：容器启动（如果 project name 冲突，需用户决定）

> **此步骤需用户明确确认**——涉及停止/重启生产容器。

- [ ] **步骤 5：健康检查**

运行：
```bash
ssh thinkpad "curl -fsS http://127.0.0.1:18081/healthz && echo HEALTHZ_OK"
ssh thinkpad "curl -fsS http://127.0.0.1:18081/openapi.json | head -5"
ssh thinkpad "curl -fsS http://127.0.0.1:18080/ | head -5"
```
预期：healthz 返回 OK，openapi.json 返回 JSON，web 返回 HTML

- [ ] **步骤 6：make post-deploy-verify**

运行：`ssh thinkpad "cd /opt/memory-os-next && MEMORY_OS_ENV_FILE=.env.production make post-deploy-verify 2>&1 | tail -20"`
预期：验证通过

- [ ] **步骤 7：make backup 确认新目录备份正常**

运行：`ssh thinkpad "cd /opt/memory-os-next && MEMORY_OS_ENV_FILE=.env.production make backup 2>&1 | tail -10"`
预期：备份成功

---

### 任务 20：等待用户确认切换生产目录

**文件：** 无（用户决策）

- [ ] **步骤 1：向用户报告演练结果**

汇总任务 18-19 的结果，列出新旧目录对比，请用户确认是否：
- 把 `/opt/memory-os` 改名为 `/opt/memory-os-previous-<timestamp>`
- 把 `/opt/memory-os-next` 改名为 `/opt/memory-os`
- 更新 cron 指向新目录

- [ ] **步骤 2：用户确认后才执行切换**

不自动切换。等用户明确回复。

---

## 阶段 6：清理清单（不自动执行）

### 任务 21：生成清理清单

**文件：**
- 创建：`artifacts/cleanup-checklist.md`（不进 git）

- [ ] **步骤 1：扫描 T480 上的旧文件、cron、volume**

运行：
```bash
ssh thinkpad "echo '=== 旧备份脚本 ===' && ls -la /root/memory-service/ 2>/dev/null && echo '=== cron ===' && crontab -l 2>/dev/null | grep -iE 'memory|backup' && echo '=== 旧 volume ===' && docker volume ls | grep -iE 'memory-service' && echo '=== 散落文件 ===' && ls /opt/memory-os/*.md /opt/memory-os/*.sh 2>/dev/null | grep -v -E 'README'"
```

- [ ] **步骤 2：生成清理清单文档**

把扫描结果写入 `artifacts/cleanup-checklist.md`，每项包含：路径/volume 名、是否被引用、最近修改时间、删除影响、回滚方式。

- [ ] **步骤 3：请用户审查清单后再决定是否执行清理**

不自动删除任何东西。

---

## 完成定义

本计划完成时必须满足：
- [ ] `go test ./...` 通过
- [ ] `go vet ./...` 通过
- [ ] `go build ./...` 通过
- [ ] `docker-compose config` 通过（本地静态校验）
- [ ] `internal/webdeploy` 测试通过
- [ ] 改动已 push 到 GitHub
- [ ] T480 `/opt/memory-os-next` 演练通过（阶段 5，需用户确认）
- [ ] `/healthz`、`/openapi.json`、Web 登录页可访问
- [ ] 没有删除线上数据
- [ ] 没有改变 volume 名（`COMPOSE_PROJECT_NAME=deploy` 固定）
- [ ] 没有把真实 secret 写入仓库、日志、memory 或回复

---

## 自检

### 规格覆盖度

逐项对照 spec 章节：
- spec §3 硬约束（不动 Go 目录）→ 任务 11 明确 `COPY cmd/internal/migrations`，不移动 ✅
- spec §4 目标布局 → 任务 1-12 覆盖 ✅
- spec §5 Makefile（Go 1.25 检测、COMPOSE_PROJECT_NAME）→ 任务 6 ✅
- spec §6.1 前端路径 → 任务 1-6 ✅
- spec §6.2 Docker 路径 → 任务 8、12 ✅
- spec §6.3 后端 Dockerfile COPY → 任务 11 ✅
- spec §6.4 前端 Dockerfile COPY → 任务 9 ✅
- spec §7 T480 部署 → 任务 17-21 ✅
- spec §8 验证计划 → 任务 7、13、15、19 ✅
- spec §9 风险 → 任务 6（Go 版本）、任务 14（placeholder）、任务 11（COPY 精确）✅
- spec §11 阶段 1-5 → 本计划阶段 1-6 对应 ✅

### 占位符扫描

- 无 TODO/待定/后续实现
- 每个代码步骤都有完整代码块
- 每个命令都有预期输出

### 类型一致性

- 路径引用前后一致：`frontend/`（不是 `web/`）、`deploy/backend/`、`deploy/frontend/`
- Makefile target 名保留不变（`build-web`、`prod-up`、`backup`、`restore`）
- `COMPOSE_PROJECT_NAME=deploy` 在所有生产 target 一致
