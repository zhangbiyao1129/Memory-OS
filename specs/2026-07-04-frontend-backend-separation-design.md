# Memory OS 前后端分区规范化与部署规范化 设计规格

- 日期: 2026-07-04
- 状态: 待实现
- 范围: 本地仓库结构规范化 + T480 部署规范化

## 1. 背景与目标

当前 Memory OS 仓库是单 Go module + Nuxt 子目录的混合结构,目录平铺、命名不统一、前后端配置脚本混杂。T480 上的 `/opt/memory-os` 是 rsync 同步的快照(非 git 仓库),生产密钥靠手动 export 注入,脚本散落根目录,存在新旧两套备份 cron。

目标:
1. **本地仓库**:前后端分区规范化,每个分区自包含源码/Dockerfile/配置/文档入口,根目录只放真正跨前后端的统筹件。
2. **T480 部署**:改为 git clone 部署,密钥用 `.env.production` 文件注入,清理散落文件和旧备份,标准化部署流程。

不动业务逻辑,不动 Go import 路径,不动数据卷。

## 2. 约束

- `go.mod` 留根,module 名 `memory-os` 不变 → 所有 Go `import "memory-os/internal/xxx"` 完全不变。
- 不改业务代码,只动目录结构、路径引用、Makefile、Dockerfile、配置。
- Docker compose 服务定义基本不动,只改 Dockerfile 路径和 build context。
- volume 名 `deploy_*` 暂不改(改要重建数据卷,风险高)。
- 所有操作可回滚:已对 T480 做全量备份(`/root/memory-os-rollback-20260704-093720/`),代码+PostgreSQL+Qdrant 三路验证可恢复。

## 3. 本地仓库目标布局

```
Memory OS/
├── go.mod / go.sum              # 留根,module memory-os 不变
├── Makefile                     # 根 Makefile,只做统筹调用
├── .gitignore / .dockerignore / .env.example
├── README.md                    # 项目总入口
├── specs/                       # 设计规格(进 git)
│
├── backend/                     # 后端自包含
│   ├── Makefile                 # 后端专属: test/build/smoke/seed-dev/lint
│   ├── README.md                # 后端说明
│   ├── cmd/                     # 10 个 Go 入口
│   ├── internal/                # 37 个 Go 包
│   └── migrations/              # SQL
│
├── frontend/                    # 前端自包含(原 web/ 改名)
│   ├── Makefile                 # 前端专属: build/dev/generate
│   ├── README.md                # 前端说明
│   ├── .env.example             # 前端环境变量样例(NUXT_PUBLIC_API_BASE 等)
│   ├── app.vue / nuxt.config.ts / package.json
│   ├── pages/ components/ composables/ stores/ assets/
│
├── deploy/                      # 部署统筹件
│   ├── docker-compose.yml       # 留根(统筹前后端)
│   ├── docker-compose.t480.yml
│   ├── docker-compose.restore-rehearsal.yml
│   ├── backend/                 # 后端 Dockerfile
│   │   ├── Dockerfile.api
│   │   ├── Dockerfile.mcp
│   │   ├── Dockerfile.worker
│   │   ├── Dockerfile.llm-mock
│   │   └── memory-llm-mock.py
│   └── frontend/                # 前端 Dockerfile
│       ├── Dockerfile.web       # 原 deploy/Dockerfile.web
│       └── nginx.conf           # 原 deploy/nginx.conf
│
├── scripts/                     # 运维脚本
│   ├── backend/                 # backup/restore/secret-scan/audit-report/preflight/post-deploy-verify/restore-rehearsal*/verify/secret-injection-audit
│   ├── frontend/                # validate-openapi-runtime
│   └── (通用留根)               # docker-cleanup*/load-build-info/load-prod-env/install-*-cron/final-delivery-report
│
└── docs/                        # 留根(.gitignore 已排除,不进 git)
```

## 4. Makefile 三层拆分

### 根 Makefile(统筹)

保留现有 target 名不变(兼容使用习惯),内部改为调用子 Makefile 或直接调 compose:

```makefile
test:       ; $(MAKE) -C backend test
build-api:  ; $(MAKE) -C backend build
build-web:  ; $(MAKE) -C frontend build
smoke:      ; $(MAKE) -C backend smoke
dev-up:     ; docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build
dev-down:   ; docker-compose -f deploy/docker-compose.yml down
prod-up:    ; 同 dev-up(生产 profile)
backup:     ; . scripts/load-prod-env.sh && scripts/backend/backup.sh
restore:    ; . scripts/load-prod-env.sh && scripts/backend/restore.sh
# 其余统筹 target(docker-cleanup/audit-report/verify 等)
```

### `backend/Makefile`

```makefile
test:    ; cd .. && go test ./backend/...
build:   ; cd .. && go build -o bin/memory-api ./backend/cmd/memory-api  # 各 cmd
smoke:   ; cd .. && go run ./backend/cmd/memory-smoke
lint:    ; cd .. && go vet ./backend/...
seed-dev: ; cd .. && go run ./backend/cmd/memory-bootstrap
```

> Go module 在根目录,backend/Makefile 里的 go 命令需 `cd ..` 到根目录执行,或用 `GOFLAGS` 指定。具体实现时确认。

### `frontend/Makefile`

```makefile
build:    ; npm install && npm run build
dev:      ; npm run dev
generate: ; npm run generate
```

## 5. 路径引用同步修改清单

| 文件 | 改动 |
|------|------|
| `deploy/docker-compose*.yml` | `Dockerfile.api` → `backend/Dockerfile.api`(build context 仍为根 `.`),`Dockerfile.web` → `frontend/Dockerfile.web` |
| `deploy/backend/Dockerfile.*` | 确认 `COPY cmd/ internal/ migrations/` 路径(build context 为根时改为 `COPY backend/cmd/` 等,或 context 改为 `backend/`) |
| `deploy/frontend/Dockerfile.web` | `COPY web/` → build context 为根时 `COPY frontend/`,`COPY --from=build /src/web/.output` → `/src/frontend/.output` |
| `Makefile`(根+子) | 全部 `web/` → `frontend/`,`cd web` → `cd frontend` |
| `internal/webdeploy/*_test.go` | `web/` → `frontend/`,`../../web/` → `../../frontend/`(注意:这些测试现在在 `backend/internal/webdeploy/` 下,相对路径要重新算) |
| `.dockerignore` | `web/node_modules` 等 → `frontend/node_modules` 等 |
| `.gitignore` | `web/dist` 等 → `frontend/dist` 等 |
| `frontend/package.json` | name `memory-os-web` → `memory-os-frontend` |
| `scripts/*` 内路径引用 | 跟着目录变(`$REPO_ROOT/web` → `$REPO_ROOT/frontend`,`scripts/backup.sh` → `scripts/backend/backup.sh` 等) |

## 6. Go 代码改动范围

- **零业务代码改动**。
- `import "memory-os/internal/xxx"` 全部不变(module 名不变)。
- 唯一需改的 Go 文件:`backend/internal/webdeploy/*_test.go` 里的字符串断言和相对路径(因为目录名 `web` → `frontend`)。

## 7. T480 部署规范化

### 7.1 阶段顺序

1. 本地完成分区规范化(第 3-6 节)。
2. 本地验证通过后 push 到 GitHub。
3. T480 上把 `/opt/memory-os` 改为 git clone(保留本地状态文件)。
4. 创建 `.env.production`,注入生产密钥。
5. 清理散落文件和旧备份。
6. 验证容器健康。

### 7.2 T480 目标目录结构

```
/opt/memory-os/                # git clone 的仓库
├── (仓库内容: backend/ frontend/ deploy/ scripts/ ...)
├── .env.production            # 生产密钥,权限 600,不进 git
├── backups/                   # 备份产物,不进 git
├── artifacts/                 # 审计/报告产物,不进 git
└── .gocache/                  # Go 构建缓存,不进 git
```

### 7.3 密钥注入

- 创建 `/opt/memory-os/.env.production`(权限 600),内容:`LLM_API_KEY`、`POSTGRES_PASSWORD`、`SECRET_VAULT_KEY_B64`、`SECRET_VAULT_KEY_ID`、`LLM_BASE_URL` 等生产值。
- `docker-compose.yml` 已用 `${LLM_API_KEY:?required}` 强制校验(保留)。
- compose 启动时自动加载 `.env.production`(docker-compose 默认读 `.env`,需显式 `--env-file .env.production` 或软链)。
- `.env.production` 必须在 `.gitignore`(已有 `.env.*` 规则覆盖)。

### 7.4 部署流程标准化

根 Makefile 提供 deploy target:
```makefile
deploy-pull:    ; git pull
deploy-up:      ; $(MAKE) build-api && $(MAKE) build-web && docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build
deploy-down:    ; docker-compose -f deploy/docker-compose.yml down
deploy-restart: ; deploy-down && deploy-up
deploy-status:  ; docker-compose -f deploy/docker-compose.yml ps
```

### 7.5 散落文件清理(T480)

- 根目录散落的 `final-delivery-report.sh`、`completion-checklist-audit.md`、`permission-isolation-bundle.md`、`security-evidence-bundle.md` → 移到 `artifacts/` 或删除(这些是本地未跟踪的产物)。

### 7.6 备份规范化

- 删除旧的 `/root/memory-service/backup.sh` 和对应 cron(6 月旧脚本,和 `memory-service_pgdata` 旧 volume 一起清理)。
- 保留 `/opt/memory-os/scripts/backend/backup.sh`。
- cron 统一调 `cd /opt/memory-os && make backup`(已有,保留)。
- 备份输出到 `/opt/memory-os/backups/`。

### 7.7 不动的

- volume 名 `deploy_*` 不改(改要重建数据卷)。
- 无 systemd 托管(容器靠 docker `restart: unless-stopped`,机器重启靠 docker daemon 自启,不额外加 systemd)。

## 8. 验证计划

### 本地(分区规范化后)

- `go build ./...` + `go vet ./...`
- `go test ./backend/...`(尤其 `backend/internal/webdeploy` 测试)
- `docker-compose -f deploy/docker-compose.yml config` 静态校验
- 前端 `nuxt build` 在 T480 跑(本地不跑容器)

### T480(部署规范化后)

- `git pull` 成功,目录结构正确
- `.env.production` 权限 600,密钥值存在
- `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml config` 校验通过(密钥解析正常)
- `make deploy-up` 容器全部 healthy
- `curl http://127.0.0.1:18081/healthz` 返回 200
- `curl http://127.0.0.1:18081/openapi.json` 返回 JSON
- Web 访问 `http://127.0.0.1:18080` 登录页正常
- `make backup` 手动跑一次确认备份正常

## 9. 风险与缓解

| 风险 | 缓解 |
|------|------|
| Dockerfile build context 路径改错导致镜像构建失败 | 逐个 Dockerfile 确认 COPY 路径,compose config 静态校验,build 失败立即回滚 |
| `webdeploy` 测试断言文本改漏导致 go test 挂 | 改完单独跑 `go test ./backend/internal/webdeploy` |
| T480 git clone 时本地状态文件(backups/artifacts/.env)丢失 | 这些都在 .gitignore,git clone 不会动它们;但迁移前再确认一次 |
| `.env.production` 密钥值错误导致容器起不来 | 先用 `.env.example` 占位值跑通,再换真实密钥;密钥错误时容器会报明确错误 |
| 旧备份 cron 删除后备份中断 | 先确认新 cron 正常,再删旧 cron |

## 10. 范围检查

本规格聚焦「目录结构 + 部署流程」规范化,不涉及业务功能。可用一个实现计划覆盖,分两阶段执行:
- 阶段一:本地分区规范化(第 3-6 节)。
- 阶段二:T480 部署规范化(第 7 节)。

阶段一完成后 push、阶段二再开始,中间有验证关卡。
