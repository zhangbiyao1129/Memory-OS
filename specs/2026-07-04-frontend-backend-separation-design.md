# Memory OS 前后端分区规范化与部署规范化设计规格

- 日期: 2026-07-04
- 状态: 已修订,待实现
- 范围: 前端目录规范化 + Docker/Makefile 路径规范化 + T480 安全部署规范化

## 1. 结论

本轮规范化不移动 Go 后端源码目录。

保留:

- `cmd/`
- `internal/`
- `migrations/`
- `go.mod`
- `go.sum`

原因:

- 当前 Go import 使用 `memory-os/internal/xxx`。
- 如果把 `internal/` 移到 `backend/internal/`,import path 必须变成 `memory-os/backend/internal/xxx`。
- Go 的 `internal` 包规则会让“移动目录但 import 不变”这件事直接构建失败。

本轮只做低风险、可验证、可回滚的规范化:

1. `web/` 改名为 `frontend/`。
2. 前端 Dockerfile 和 nginx 配置移动到 `deploy/frontend/`。
3. 后端 Dockerfile 移动到 `deploy/backend/`,但仍从根目录复制 `cmd/`、`internal/`、`migrations/`。
4. Makefile 保留现有 target 名,只修正路径和生产环境加载入口。
5. T480 先使用新目录演练 git clone 部署,不直接覆盖当前 `/opt/memory-os`。
6. 旧备份、旧 cron、旧 volume 不在本轮直接删除,进入单独清理清单。

## 2. 背景与目标

当前 Memory OS 仓库是单 Go module + Nuxt 子目录结构。后端 Go 代码位于根目录 `cmd/`、`internal/`、`migrations/`,前端位于 `web/`,部署文件位于 `deploy/`。

T480 上的 `/opt/memory-os` 目前以同步快照方式维护,还存在生产环境变量加载、备份脚本、历史产物和 cron 入口需要统一的问题。

目标:

1. **前端规范化**:把 Nuxt 管理台从 `web/` 统一命名为 `frontend/`。
2. **部署规范化**:按前端/后端拆分 Dockerfile 和 nginx 配置,但不改变运行端口、volume 名和服务名。
3. **生产入口规范化**:生产环境变量只通过 `.env.production` + `scripts/load-prod-env.sh` 进入 Makefile 和 compose。
4. **T480 规范化**:从快照部署逐步迁移到 git clone 部署,但先在新目录演练,验证成功后再切换。
5. **后端重构延后**:Go 后端目录拆分不和本轮前端/部署规范化混在一起。

## 3. 硬约束

- 不移动 `cmd/`、`internal/`、`migrations/`。
- 不修改 Go import path。
- 不修改 Go module 名 `memory-os`。
- 不改数据库 schema。
- 不改 Docker volume 名。
- 不改对外端口。
- 不删除线上数据。
- 不删除旧备份、旧 cron、旧 volume,除非进入单独清理计划并再次确认。
- 不把 `.env.example` 或 placeholder secret 用于生产启动。
- 不允许 T480 在 Go 1.25 不可用时直接执行宿主机 `go test`、`go build` 或 `go run`。
- 所有生产 compose 命令必须固定 `COMPOSE_PROJECT_NAME=deploy`,不能依赖当前目录名推导 project name。
- 不自动 push、commit、deploy。执行这些动作前必须再次确认。

## 4. 目标仓库布局

```text
Memory OS/
├── go.mod / go.sum
├── Makefile
├── .gitignore / .dockerignore / .env.example
├── README.md
├── specs/
│
├── cmd/                         # 保留根目录:Go 入口
├── internal/                    # 保留根目录:Go 内部包
├── migrations/                  # 保留根目录:SQL + embed
│
├── frontend/                    # 原 web/
│   ├── README.md
│   ├── .env.example
│   ├── app.vue
│   ├── nuxt.config.ts
│   ├── package.json
│   ├── pages/
│   ├── components/
│   ├── composables/
│   ├── stores/
│   └── assets/
│
├── deploy/
│   ├── docker-compose.yml
│   ├── docker-compose.t480.yml
│   ├── docker-compose.restore-rehearsal.yml
│   ├── backend/
│   │   ├── Dockerfile.api
│   │   ├── Dockerfile.worker
│   │   ├── Dockerfile.mcp
│   │   ├── Dockerfile.llm-mock
│   │   └── memory-llm-mock.py
│   └── frontend/
│       ├── Dockerfile.web
│       └── nginx.conf
│
├── scripts/                     # 本轮先不整体搬迁,避免脚本相对路径批量失效
├── docs/                        # 当前按项目规则不进 git
├── artifacts/                   # 本地/服务器验收产物,不进 git
└── backups/                     # 本地/服务器备份产物,不进 git
```

## 5. Makefile 规范

根 Makefile 继续作为统一入口。保留现有 target 名,避免破坏使用习惯。

推荐目标:

```makefile
test:
	@if command -v go >/dev/null 2>&1 && go version | grep -q 'go1\.25'; then \
		GOPROXY=$(GOPROXY) go test ./...; \
	else \
		docker run --rm -e GOPROXY=$(GOPROXY) -e NO_PROXY=$(NO_PROXY) -e no_proxy=$(NO_PROXY) -v "$$(pwd)":/src -w /src golang:1.25-bookworm go test ./...; \
	fi

build-api:
	@if command -v go >/dev/null 2>&1 && go version | grep -q 'go1\.25'; then \
		GOPROXY=$(GOPROXY) go build -o bin/memory-api ./cmd/memory-api; \
	else \
		docker run --rm -e GOPROXY=$(GOPROXY) -e NO_PROXY=$(NO_PROXY) -e no_proxy=$(NO_PROXY) -v "$$(pwd)":/src -w /src golang:1.25-bookworm go build -o bin/memory-api ./cmd/memory-api; \
	fi

build-worker:
	@if command -v go >/dev/null 2>&1 && go version | grep -q 'go1\.25'; then \
		GOPROXY=$(GOPROXY) go build -o bin/memory-worker ./cmd/memory-worker; \
	else \
		docker run --rm -e GOPROXY=$(GOPROXY) -e NO_PROXY=$(NO_PROXY) -e no_proxy=$(NO_PROXY) -v "$$(pwd)":/src -w /src golang:1.25-bookworm go build -o bin/memory-worker ./cmd/memory-worker; \
	fi

build-mcp:
	@if command -v go >/dev/null 2>&1 && go version | grep -q 'go1\.25'; then \
		GOPROXY=$(GOPROXY) go build -o bin/memory-mcp ./cmd/memory-mcp; \
	else \
		docker run --rm -e GOPROXY=$(GOPROXY) -e NO_PROXY=$(NO_PROXY) -e no_proxy=$(NO_PROXY) -v "$$(pwd)":/src -w /src golang:1.25-bookworm go build -o bin/memory-mcp ./cmd/memory-mcp; \
	fi

build-web:
	npm --prefix frontend install
	npm --prefix frontend run build

smoke:
	@if command -v go >/dev/null 2>&1 && go version | grep -q 'go1\.25'; then \
		GOPROXY=$(GOPROXY) go run ./cmd/memory-smoke; \
	else \
		docker run --rm -e GOPROXY=$(GOPROXY) -e NO_PROXY=$(NO_PROXY) -e no_proxy=$(NO_PROXY) -v "$$(pwd)":/src -w /src golang:1.25-bookworm go run ./cmd/memory-smoke; \
	fi

prod-up:
	. scripts/load-prod-env.sh && \
	. scripts/load-build-info.sh && \
	ALLOW_EXISTING_DEPLOYMENT=1 scripts/preflight.sh && \
	export COMPOSE_PROJECT_NAME=deploy && \
	APP_ENV=production ENABLE_DEV_ENDPOINTS=false \
	docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-api memory-worker memory-mcp memory-web

backup:
	. scripts/load-prod-env.sh && export COMPOSE_PROJECT_NAME=deploy && scripts/backup.sh

restore:
	. scripts/load-prod-env.sh && export COMPOSE_PROJECT_NAME=deploy && scripts/restore.sh
```

注意:

- 后端测试必须继续使用 `go test ./...`。
- 不使用 `go test ./backend/...`,因为本轮没有 `backend/` Go 目录。
- 根 Makefile 是唯一强制入口;本轮不新增 `backend/Makefile`,也不强制新增 `frontend/Makefile`,避免前后端入口不对称。
- `test` 必须先确认宿主机 Go 是 1.25;如果不是,必须用 `golang:1.25-bookworm` 容器执行,避免 T480 当前 Go 1.24.1 触发失败。
- 所有生产 target 和脚本内 compose 调用必须显式 `COMPOSE_PROJECT_NAME=deploy`,避免换目录后 compose project name 改变导致 volume 名变化。

## 6. 路径修改清单

### 6.1 前端路径

| 文件 | 修改 |
|------|------|
| `web/` | 重命名为 `frontend/` |
| `frontend/package.json` | `name` 从 `memory-os-web` 改为 `memory-os-frontend` |
| 根 `Makefile` | `cd web`、`$(pwd)/web` 改为 `frontend` |
| `.gitignore` | `web/dist` 改为 `frontend/dist`;同时保留根级 `node_modules/`、`.nuxt/`、`.output/` 规则 |
| `.dockerignore` | `web/node_modules`、`web/.nuxt`、`web/.output` 改为 `frontend/...` |
| 脚本中的前端引用 | `$REPO_ROOT/web` 改为 `$REPO_ROOT/frontend` |
| Go 测试中的前端路径 | 只修正引用 `web/` 的测试断言和相对路径,不移动测试文件 |

### 6.2 Docker 路径

| 文件 | 修改 |
|------|------|
| `deploy/Dockerfile.api` | 移到 `deploy/backend/Dockerfile.api` |
| `deploy/Dockerfile.worker` | 移到 `deploy/backend/Dockerfile.worker` |
| `deploy/Dockerfile.mcp` | 移到 `deploy/backend/Dockerfile.mcp` |
| `deploy/Dockerfile.llm-mock` | 移到 `deploy/backend/Dockerfile.llm-mock` |
| `deploy/memory-llm-mock.py` | 移到 `deploy/backend/memory-llm-mock.py` |
| `deploy/Dockerfile.web` | 移到 `deploy/frontend/Dockerfile.web` |
| `deploy/nginx.conf` | 移到 `deploy/frontend/nginx.conf` |
| `deploy/docker-compose*.yml` | 只更新 `dockerfile:` 路径;`context: ..` 保持不变 |

### 6.3 后端 Dockerfile COPY 策略

后端 Dockerfile 的 build context 仍是仓库根。

后端镜像必须复制:

```dockerfile
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
```

构建命令保持:

```dockerfile
RUN go build -o /out/memory-api ./cmd/memory-api
```

不得改成:

```dockerfile
COPY backend/ ./backend/
RUN go build ./backend/cmd/memory-api
```

### 6.4 前端 Dockerfile COPY 策略

前端 Dockerfile 的 build context 仍是仓库根。

```dockerfile
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm install
COPY frontend/ ./
RUN npm run generate

COPY deploy/frontend/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/frontend/.output/public/ /usr/share/nginx/html/
```

## 7. T480 部署规范化

### 7.1 安全迁移原则

T480 迁移不直接覆盖当前 `/opt/memory-os`。

使用新目录演练:

```text
/opt/memory-os          # 当前生产目录,先保留
/opt/memory-os-next     # git clone 演练目录
```

只有满足以下条件后,才允许切换:

- 当前生产目录已完成一次新备份。
- `/opt/memory-os-next` 已完成 compose config 校验。
- `/opt/memory-os-next` 已完成镜像构建。
- `/opt/memory-os-next` 已能读取 `.env.production`。
- `/opt/memory-os-next` 的 `COMPOSE_PROJECT_NAME=deploy` 已确认。
- `/opt/memory-os-next` 已确认 Go 1.25 可用;若宿主机只有 Go 1.24.1 或 `go` 不在 SSH PATH,必须走 Docker Go 1.25 fallback。
- `/opt/memory-os-next` 的服务健康检查通过。
- 用户再次确认允许切换生产目录。

### 7.2 `.env.production`

生产密钥统一存放:

```text
/opt/memory-os/.env.production
```

要求:

- 权限为 `600`。
- 不允许是 symlink。
- 不进 git。
- 只通过 `scripts/load-prod-env.sh` 加载。
- `prod-up`、`backup`、`restore`、`post-deploy-verify` 必须先 source `scripts/load-prod-env.sh`。
- compose 不使用 `env_file`;密钥通过 `load-prod-env.sh` source 到当前 shell,再由同一个 shell 中的 compose `${VAR}` interpolation 注入。
- 任何 `docker-compose config/up/ps/exec` 命令如果依赖生产变量,必须写在 `. scripts/load-prod-env.sh && ...` 之后,不能拆成两个 shell。
- 生产启动必须拒绝 `replace-me`、`example`、`dev-only`、`mock` 这类占位值。
- 当前 `scripts/preflight.sh` 尚未实现 placeholder secret 检查;阶段 3 必须新增该检查,不能把它当成已有能力。

禁止:

- 用 `.env.example` 启动生产。
- 把真实密钥写入文档、日志、测试快照、memory、Archive、Qdrant payload 或回复。
- 用软链绕过 `.env.production` 权限检查。

### 7.3 迁移步骤

1. 在当前生产目录执行备份:

```bash
cd /opt/memory-os
make backup
```

2. 创建新 git clone 目录:

```bash
cd /opt
: "${REPO_URL:?REPO_URL must be set to the confirmed Git remote URL}"
git clone "$REPO_URL" memory-os-next
```

3. 复制生产环境文件:

```bash
cp /opt/memory-os/.env.production /opt/memory-os-next/.env.production
chmod 600 /opt/memory-os-next/.env.production
```

4. 在新目录做静态校验:

```bash
cd /opt/memory-os-next
export MEMORY_OS_ENV_FILE=.env.production
. scripts/load-prod-env.sh
COMPOSE_PROJECT_NAME=deploy docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml config >/tmp/memory-os-compose-config.txt
```

5. 确认 Go 1.25 工具链:

```bash
cd /opt/memory-os-next
if command -v go >/dev/null 2>&1 && go version | grep -q 'go1\.25'; then
  go version
else
  docker run --rm -v "$PWD":/src -w /src golang:1.25-bookworm go version
fi
```

6. 构建并验证:

```bash
cd /opt/memory-os-next
MEMORY_OS_ENV_FILE=.env.production make test
MEMORY_OS_ENV_FILE=.env.production make build-web
MEMORY_OS_ENV_FILE=.env.production make prod-up
MEMORY_OS_ENV_FILE=.env.production make post-deploy-verify
```

说明:

- `/opt/memory-os-next` 是新 clone,首次 `make test` 和 `make build-web` 会重新下载 Go module 与 npm 依赖,耗时会比当前生产目录更长。
- `make prod-up` 必须在 Makefile 内部固定 `COMPOSE_PROJECT_NAME=deploy`;调用方不需要额外传入,避免不同执行者漏传。
- `make backup`、`make restore`、`make post-deploy-verify` 以及脚本内部 compose 调用也必须继承 `COMPOSE_PROJECT_NAME=deploy`。

7. 切换前必须再次确认:

```text
确认项:
- 是否允许短暂停止当前生产容器
- 是否允许把 /opt/memory-os 切换为 git clone 目录
- 是否保留旧目录为 /opt/memory-os-previous-<timestamp>
```

### 7.4 清理策略

本轮只移动明显的非运行产物到 `artifacts/`,不删除历史备份和旧 volume。

允许移动:

- `final-delivery-report.sh`
- `completion-checklist-audit.md`
- `permission-isolation-bundle.md`
- `security-evidence-bundle.md`

移动目标:

```text
/opt/memory-os/artifacts/
```

旧 cron、旧脚本、旧 volume 清理必须单独生成清单,清单包含:

- 路径或 volume 名。
- 当前是否仍被进程或 cron 引用。
- 最近修改时间。
- 删除后影响。
- 回滚方式。
- 用户确认记录。

## 8. 验证计划

### 8.1 本地验证

修改完成后运行:

```bash
go test ./...
go vet ./...
go build ./...
npm --prefix frontend install
npm --prefix frontend run build
POSTGRES_PASSWORD=local-check \
LLM_API_KEY=local-check \
SECRET_VAULT_KEY_ID=local-check \
SECRET_VAULT_KEY_B64=dGVzdC1rZXktMTIzNDU2Nw== \
COMPOSE_PROJECT_NAME=deploy \
docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml config >/tmp/memory-os-compose-config.txt
```

说明:

- 如果本地或服务器宿主机 Go 不是 1.25,`go test`、`go vet`、`go build` 必须改用 Docker `golang:1.25-bookworm` 执行。
- 本地 compose config 只做静态解析,不启动生产。
- 本地 `npm --prefix frontend run build` 是轻量前端构建验证,不启动 Memory OS 容器,不违反“本地不运行 Memory OS 容器”的项目规则。
- 本地 fake 值只允许用于 `config` 静态校验,不得用于 `prod-up`。

### 8.2 T480 验证

在服务器执行:

```bash
cd /opt/memory-os-next
if command -v go >/dev/null 2>&1 && go version | grep -q 'go1\.25'; then go version; else docker run --rm -v "$PWD":/src -w /src golang:1.25-bookworm go version; fi
export MEMORY_OS_ENV_FILE=.env.production
. scripts/load-prod-env.sh
COMPOSE_PROJECT_NAME=deploy docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml config >/tmp/memory-os-compose-config.txt
MEMORY_OS_ENV_FILE=.env.production make test
MEMORY_OS_ENV_FILE=.env.production make build-web
MEMORY_OS_ENV_FILE=.env.production make prod-up
COMPOSE_PROJECT_NAME=deploy docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps
curl -fsS http://127.0.0.1:18081/healthz
curl -fsS http://127.0.0.1:18081/openapi.json
curl -fsS http://127.0.0.1:18080/
MEMORY_OS_ENV_FILE=.env.production make backup
MEMORY_OS_ENV_FILE=.env.production make post-deploy-verify
```

验收条件:

- 所有命令成功。
- `docker-compose ps` 显示核心服务 healthy 或 running。
- `docker-compose ps` 必须在 `COMPOSE_PROJECT_NAME=deploy` 下执行。
- Go 1.25 可用,或 Docker `golang:1.25-bookworm` fallback 可用。
- Web 登录页可访问。
- OpenAPI JSON 可访问。
- 备份产物生成。
- 没有真实 secret 出现在日志、文档、测试快照或命令输出中。

## 9. 风险与缓解

| 风险 | 级别 | 缓解 |
|------|------|------|
| 误移动 Go `internal/` 导致 import 失效 | P0 | 本轮明确禁止移动 `cmd/`、`internal/`、`migrations/` |
| T480 Go 版本低于 1.25 或 SSH PATH 没有 `go` | P0 | Makefile 必须检测 Go 1.25;不满足时使用 Docker `golang:1.25-bookworm` fallback |
| 换目录后 compose project name 改变,导致 volume 名变化 | P0 | 所有生产 compose 命令和脚本设置 `COMPOSE_PROJECT_NAME=deploy`;验证 `docker-compose ps` 也必须带同一 project name |
| `.env.production` 没被加载,生产变量缺失 | P1 | 所有生产 Makefile target 先 source `scripts/load-prod-env.sh`;compose 命令必须与 source 在同一个 shell 链路执行 |
| placeholder secret 被用于生产 | P1 | 阶段 3 新增 `preflight` 检查,拒绝 `replace-me`、`dev-only`、`mock`、`example` |
| 前端路径改漏导致 build 失败 | P1 | `npm --prefix frontend run build` 必须通过 |
| Dockerfile COPY 路径改漏导致镜像构建失败 | P1 | `docker-compose config` + `make prod-up` 构建验证 |
| 旧备份 cron 删除后备份中断 | P1 | 本轮不删除旧 cron;新备份验证后单独清理 |
| `scripts/` 批量移动导致相对路径失效 | P2 | 本轮不整体移动 scripts,只修正引用 `web/` 的路径 |

## 10. 后端目录拆分的独立方案

如果未来仍要把 Go 后端移动到 `backend/`,必须作为独立重构执行,不得和本轮混做。

可选路线:

### 路线 A: 保持根 Go module,只移动入口文档

- `cmd/`、`internal/`、`migrations/` 继续留根。
- 新增 `backend/README.md` 解释后端目录由根级 Go module 承载。
- 风险最低。

### 路线 B: Go 代码整体迁入 `backend/`

必须同步修改:

- 所有 `memory-os/internal/xxx` import 改为 `memory-os/backend/internal/xxx`。
- 所有 `go build ./cmd/...` 改为 `go build ./backend/cmd/...`。
- 所有 `go test ./...` 保留为全仓验证。
- 所有 Dockerfile COPY 策略重写。
- migrations embed 路径重新验证。
- OpenAPI、smoke、bootstrap、worker、MCP 全量验证。

路线 B 是真实重构,需要单独测试窗口和回滚计划。

## 11. 执行阶段

### 阶段 1: 前端目录规范化

- 重命名 `web/` 为 `frontend/`。
- 更新前端路径引用。
- 更新 `.gitignore` 和 `.dockerignore`。
- 新增 `frontend/README.md`。
- 验证 `npm --prefix frontend run build`。

### 阶段 2: Docker 布局规范化

- 移动 Dockerfile 到 `deploy/backend/` 和 `deploy/frontend/`。
- 更新 compose `dockerfile:` 路径。
- 后端 Dockerfile 继续复制根目录 `cmd/`、`internal/`、`migrations/`。
- 验证 `docker-compose config`。

### 阶段 3: Makefile 和生产入口规范化

- 根 Makefile 保留现有 target。
- `build-web` 改为直接执行 `npm --prefix frontend install` 和 `npm --prefix frontend run build`。
- `prod-up` 固定通过 `scripts/load-prod-env.sh` 加载生产变量。
- `prod-up`、`backup`、`restore`、`post-deploy-verify` 和相关脚本内 compose 调用全部设置或继承 `COMPOSE_PROJECT_NAME=deploy`。
- 所有生产 compose 命令必须与 `. scripts/load-prod-env.sh` 保持同一 shell 链路。
- `test`、`build-api`、`smoke` 等 Go target 必须检测 Go 1.25;宿主机不满足时用 Docker Go 1.25 fallback。
- `preflight` 新增 placeholder secret 拒绝规则,覆盖 `POSTGRES_PASSWORD`、`LLM_API_KEY`、`SECRET_VAULT_KEY_ID`、`SECRET_VAULT_KEY_B64` 等生产必填变量。

### 阶段 4: T480 git clone 演练

- 创建 `/opt/memory-os-next`。
- 复制 `.env.production`。
- 在新目录完成测试、构建、compose config、prod-up、post-deploy-verify。
- 通过后等待用户确认是否切换。

### 阶段 5: 清理计划

- 只生成清理清单。
- 不自动删除。
- 用户确认后再执行旧 cron、旧脚本、旧 volume 清理。

## 12. 完成定义

本规格完成时必须满足:

- `go test ./...` 通过。
- `go vet ./...` 通过。
- `go build ./...` 通过。
- `npm --prefix frontend run build` 通过。
- `docker-compose config` 通过。
- T480 `/opt/memory-os-next` 演练通过。
- `/healthz` 可访问。
- `/openapi.json` 可访问。
- Web 登录页可访问。
- 备份命令可运行并生成产物。
- 没有删除线上数据。
- 没有改变 volume 名。
- 没有把真实 secret 写入仓库、日志、memory、Archive、Qdrant payload 或交付报告。
