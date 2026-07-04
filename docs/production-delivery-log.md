# Memory OS v0.4 生产级交付记录

## 2026-07-02 Phase 0：基线审计

完成事项：

- 读取项目规则、执行总控计划和完成度自检清单。
- 审计 API 路由、仓库实现、migrations、worker、前端页面。
- 使用浏览器控制工具打开首页、组织、项目、归档、热记忆、Secret、Token、Qdrant、检索测试页。
- 补齐 README 入口文档。
- 生成 `docs/production-baseline-report.md`。

验证命令：

- `make test`：通过。
- `make build-web`：通过。
- `make smoke`：通过。
- `docker-compose -f deploy/docker-compose.yml ps`：核心容器均为 Up。
- `curl http://127.0.0.1:18081/healthz`：HTTP 200。
- `curl http://127.0.0.1:18081/openapi.json`：HTTP 200。

主要发现：

- 当前不能声明生产级完成。
- 生产 API 仍部分使用内存仓库。
- 管理台多页面仍是静态假数据或本地状态。
- OpenAPI 与目标生产 API 差距明显。
- 当前部署仍启用 development dev smoke endpoints。

## 2026-07-02 Phase 1.1：CORS / Preflight 修复

完成事项：

- 为浏览器 API 预检补充失败测试。
- 在 HTTP router 层添加最小 CORS middleware。
- 为所有带 `Origin` 的 API 响应附加 CORS 响应头。
- 为 `OPTIONS /*path` 返回 HTTP 204。
- 重建并重启服务器 `memory-api` 容器。

验证命令：

- 本地 `go test ./internal/http -run 'TestCORS'`：先红后绿。
- 本地 `go test ./...`：通过。
- 服务器 `make test`：通过。
- 服务器 `make smoke`：通过。
- 服务器 `curl -i -X OPTIONS http://127.0.0.1:18081/memory/search ...`：HTTP 204，CORS 响应头存在。
- 浏览器检索页点击 `运行检索`：不再报 `Failed to fetch`，进入后端 `503 Service Unavailable`。

剩余问题：

- `503 Service Unavailable` 表示生产 Retrieval 仍未配置完成。
- Auth/Tenant/RAG/TurnEvent 仍需从内存仓库切换到生产持久化实现。
- 管理台仍需从静态页面改为真实 API 驱动。

## 2026-07-02 Phase 1.2：Auth PostgreSQL Repository

完成事项：

- 新增 Auth PostgreSQL repository。
- 覆盖 password credential、PAT、Adapter Token、PAT revoke。
- API 生产启动路径中 AuthService 从 memory repository 切换为 PostgreSQL repository。
- 保留 development + dev endpoints 模式不注入 Auth/Tenant/Retrieval，避免 smoke 开发端点被生产鉴权阻断。

验证命令：

- 本地 `go test ./internal/auth -run 'TestPGRepository'`：通过。
- 本地 `go test ./...`：通过。
- 服务器 `make test`：通过。
- 服务器真实 Postgres contract：

```bash
docker run --rm --network deploy_default \
  -e POSTGRES_TEST_DSN="postgres://memory_os:***@postgres:5432/memory_os?sslmode=disable" \
  -v "$PWD":/src -w /src golang:1.25-bookworm \
  go test ./internal/auth -run "TestPGRepositoryPasswordAndTokens" -count=1 -v
```

结果：`TestPGRepositoryPasswordAndTokens` PASS。

- 服务器重建并重启 `memory-api`：成功。
- 服务器 `make smoke`：通过，输出 `smoke ok`。
- 服务器 `/healthz`：HTTP 200，`db/qdrant/redis` 均为 `ok`。

处理过的报错：

- 第一次真实 PG contract 失败：`relation "users" does not exist`。
- 根因：当前 Postgres volume 未自动应用 migrations。
- 处理：手动用 psql 应用 `/docker-entrypoint-initdb.d/*.sql` 中现有 migration；确认 37 张表存在后重试，contract 通过。

剩余问题：

- Tenant 仍使用 memory repository。
- Secret 仍使用 memory repository。
- Archive metadata、Audit、Retrieval access log 仍未切 PG。
- 当前 migration 应用依赖手动 psql，后续需要正式 migration runner，不能依赖 Docker 首次初始化。

## 2026-07-02 Phase 1.3：Tenant PostgreSQL Repository

完成事项：

- 新增 Tenant PostgreSQL repository。
- 覆盖 users、orgs、projects、roles、memberships。
- 保持 `PermissionContext` 现有 service 语义，支持项目成员权限上下文。
- API 生产启动路径中 TenantService 从 memory repository 切换为 PostgreSQL repository。
- 修复 `memory-smoke` 冷缓存不稳定问题：默认 smoke timeout 从 5 秒提升为 60 秒，并支持 `SMOKE_TIMEOUT` 覆盖。

验证命令：

- 本地 `go test ./internal/tenant -run 'TestPGRepository'`：通过。
- 本地 `go test ./cmd/memory-smoke -run 'TestSmokeTimeout|TestMemorySearchSmoke'`：通过。
- 本地 `go test ./...`：通过。
- 服务器 `make test`：通过。
- 服务器真实 Postgres contract：

```bash
docker run --rm --network deploy_default \
  -e POSTGRES_TEST_DSN="postgres://memory_os:***@postgres:5432/memory_os?sslmode=disable" \
  -v "$PWD":/src -w /src golang:1.25-bookworm \
  go test ./internal/tenant -run "TestPGRepository" -count=1 -v
```

结果：`TestPGRepositoryRequiresPool`、`TestPGRepositoryCreatesTenantGraph`、`TestPGRepositoryRejectsCrossProjectMembershipLookup` 全部 PASS。

- 服务器重建并重启 `memory-api`：成功。
- 服务器 `make smoke`：通过，输出 `smoke ok`。
- 服务器 `/healthz`：HTTP 200，`db/qdrant/redis` 均为 `ok`。

处理过的报错：

- `make smoke` 在冷缓存时失败：`adapter dry-run smoke: signal: killed`。
- 根因：`memory-smoke` 5 秒总超时会杀掉内部 `go run ./cmd/memory-adapter` 的依赖下载/编译。
- 处理：新增 `SMOKE_TIMEOUT` 配置，默认 60 秒；重跑服务器 `make test && make smoke` 通过。

剩余问题：

- Secret 仍使用 memory repository。
- Archive metadata、Audit、Retrieval access log 仍未切 PG。
- 当前 Retrieval 仍可能返回 `retrieval_not_configured`，Archive RAG 仍是 memory store。
- 管理台仍未接真实 Tenant/Auth API。

## 2026-07-02 Phase 1.4：Secret PostgreSQL Repository

完成事项：

- 新增 Secret PostgreSQL repository。
- 覆盖 Secret metadata、current version、禁用状态。
- 通过 `Vault` 生命周期 contract 验证创建、加密保存、解密使用、禁用后拒绝注入。
- 保持 Secret 明文只进入测试内的 Vault 创建/解密流程；repository 仅保存密文、nonce、key_id 和 metadata。

验证命令：

- 本地 `go test ./internal/secret -run 'TestPGRepository'`：通过。
- 本地 `go test ./...`：通过。
- 服务器 `make test`：通过。
- 服务器真实 Postgres contract：

```bash
docker run --rm --network deploy_default \
  -e POSTGRES_TEST_DSN="postgres://memory_os:***@postgres:5432/memory_os?sslmode=disable" \
  -v "$PWD":/src -w /src golang:1.25-bookworm \
  go test ./internal/secret -run "TestPGRepository" -count=1 -v
```

结果：`TestPGRepositoryRequiresPool`、`TestPGRepositoryVaultLifecycle` 全部 PASS。

处理过的报错：

- 第一次真实 PG contract 命令 exit 1 且无输出。
- 根因：服务器 `/opt/memory-os` 没有 `.env`，命令中的 `[ -f .env ] && ... && docker run ...` 在 Docker 启动前短路。
- 处理：使用 compose 当前默认的本地测试密码展开 DSN 后重跑，contract 通过。

剩余问题：

- 当前还没有 Secret 管理 API / RouterOptions 注入点，本切片只交付 repository contract，未硬接未暴露服务。
- Archive metadata、Audit、Retrieval access log 仍未切 PG。
- 当前 migration 应用依赖手动 psql，后续需要正式 migration runner。

## 2026-07-02 Phase 1.5：Archive Metadata PostgreSQL Repository

完成事项：

- 新增 Archive PostgreSQL repository。
- 覆盖 archive metadata、versions、source events、edit audit、index generations。
- 新增 `archive_request_idempotency` 向前 migration，用唯一 `request_id` 支撑 create/edit 幂等。
- `SaveCreate` 使用事务写入 archive、version、events、index generation 和 idempotency。
- `SaveEdit` 使用事务更新 archive metadata，新增 version、edit audit、index generation 和 idempotency。

新增 migration：

- `migrations/000011_archive_idempotency.sql`
- 已在服务器 Postgres 手动应用，验证 `archive_request_idempotency` 表存在。

验证命令：

- 本地 `go test ./internal/archive -run 'TestPGRepository'`：通过。
- 本地 `go test ./migrations -run 'TestArchive'`：通过。
- 本地 `go test ./...`：通过。
- 服务器 `make test`：通过。
- 服务器真实 Postgres contract：

```bash
docker run --rm --network deploy_default \
  -e POSTGRES_TEST_DSN="postgres://memory_os:***@postgres:5432/memory_os?sslmode=disable" \
  -v "$PWD":/src -w /src golang:1.25-bookworm \
  go test ./internal/archive -run "TestPGRepository" -count=1 -v
```

结果：`TestPGRepositoryRequiresPool`、`TestPGRepositorySaveCreatePersistsArchiveGraphAndDedupesRequestID`、`TestPGRepositorySaveEditCreatesVersionAuditAndIndexGeneration` 全部 PASS。

剩余问题：

- Archive service / worker 生产启动路径仍未切换到 PG repository。
- 当前 Archive 管理 API 和 Nuxt 页面仍未完成真实 CRUD 验收。
- migration 仍缺正式 runner，新增 migration 需要手动应用到当前服务器。
- Archive Markdown 文件持久化、PG metadata 和后续 RAG index worker 还未形成完整生产链路。

## 2026-07-02 Phase 1.6：Audit PostgreSQL Repository

完成事项：

- 新增 Audit PostgreSQL repository。
- 覆盖审计日志保存到 `audit_logs`。
- 支持 actor/org/project UUID 外键字段；空 scope 会转为 SQL NULL，避免空字符串触发 UUID 转换错误。
- metadata 以 JSONB 写入 PostgreSQL。

验证命令：

- 本地 `go test ./internal/audit -run 'TestPGRepository'`：通过。
- 本地 `go test ./internal/audit`：通过。
- 本地 `go test ./...`：通过。
- 服务器 `make test`：通过。
- 服务器真实 Postgres contract：

```bash
docker run --rm --network deploy_default \
  -e POSTGRES_TEST_DSN="postgres://memory_os:***@postgres:5432/memory_os?sslmode=disable" \
  -v "$PWD":/src -w /src golang:1.25-bookworm \
  go test ./internal/audit -run "TestPGRepository" -count=1 -v
```

结果：`TestPGRepositoryRequiresPool`、`TestPGRepositorySavePersistsAuditLog` 全部 PASS。

剩余问题：

- AuditService 尚未接入生产 API 的统一注入路径。
- `audit_logs.request_id` 当前 schema 没有唯一约束；如果后续需要严格幂等审计，应新增向前 migration 或按业务区分审计事件语义。
- Retrieval access log 仍未切 PG。

## 2026-07-02 Phase 1.7：Retrieval Access Log PostgreSQL Repository

完成事项：

- 新增 Retrieval PostgreSQL access log。
- `LogRequest` 写入 `retrieval_requests`，只保存 `query_hash`，不保存原始 query。
- `LogResult` 写入 `retrieval_results` 和 `memory_access_logs`。
- 新增 `retrieval_results_request_rank_unique` 向前 migration，用 `(request_id, rank)` 保证结果幂等。
- 重复 `LogRequest` 使用 `ON CONFLICT` 合并 `rerank_degraded`，重复 `LogResult` 不生成重复 result/access log。

新增 migration：

- `migrations/000012_retrieval_result_idempotency.sql`
- 已在服务器 Postgres 手动应用，验证 `retrieval_results_request_rank_unique` 索引存在。

验证命令：

- 本地 `go test ./internal/retrieval -run 'TestPGAccessLog'`：通过。
- 本地 `go test ./migrations -run 'TestRetrieval'`：通过。
- 本地 `go test ./...`：通过。
- 服务器 `make test`：通过。
- 服务器真实 Postgres contract：

```bash
docker run --rm --network deploy_default \
  -e POSTGRES_TEST_DSN="postgres://memory_os:***@postgres:5432/memory_os?sslmode=disable" \
  -v "$PWD":/src -w /src golang:1.25-bookworm \
  go test ./internal/retrieval -run "TestPGAccessLog" -count=1 -v
```

结果：`TestPGAccessLogRequiresPool`、`TestPGAccessLogPersistsRequestAndDedupesRequestID`、`TestPGAccessLogPersistsResultAndAccessLogIdempotently` 全部 PASS。

剩余问题：

- API 生产启动路径仍使用 `retrieval.NewMemoryAccessLog()`，尚未切换到 PG access log。
- Retrieval 主流程仍未完成 Archive RAG 生产索引和真实 Qdrant 检索闭环。
- migration 仍缺正式 runner，新增 migration 需要手动应用到当前服务器。

## 2026-07-02 Phase 1.8：Retrieval Access Log 生产注入与生产 Smoke 修复

完成事项：

- API 生产启动路径中 Retrieval access log 从 `NewMemoryAccessLog()` 切换为 `NewPGAccessLog(pool)`。
- 保持 `development + ENABLE_DEV_ENDPOINTS=true` 的 dev smoke 分支不注入生产 Auth/Tenant/Retrieval，避免开发烟测被生产鉴权阻断。
- `make smoke` 默认不再强制 `SMOKE_ENABLE_DEV_ENDPOINTS=true`，与当前生产配置 `ENABLE_DEV_ENDPOINTS=false` 对齐。
- `memory-smoke` 在无 `SMOKE_ADAPTER_TOKEN` 时验证 `/memory/turn-event` 返回 `adapter_token_required`，作为生产安全边界检查。
- `memory-smoke` 在未种子化真实租户数据时接受 `/memory/search` 返回 `memory_search_forbidden`，作为生产租户隔离检查。

验证命令：

- 本地 `go test ./cmd/memory-api`：通过。
- 本地 `go test ./cmd/memory-smoke -run 'TestMemorySearchSmoke|TestTurnEventSmoke|TestMakeSmoke|TestSmokeTimeout'`：通过。
- 本地 `go test ./...`：通过。
- 服务器 `make test`：通过。
- 服务器重建并重启 `memory-api`：成功。
- 服务器 `make smoke`：通过，输出 `smoke ok`。
- 服务器 `/healthz`：HTTP 200，`db/qdrant/redis` 均为 `ok`。

处理过的报错：

- 重建 API 后首次 `make smoke` 失败：`phase2 smoke status 404`。
- 根因：当前 API 是生产配置，dev smoke endpoints 已关闭，但 Makefile 强制启用 dev smoke 检查。
- 处理：Makefile 默认生产 smoke；需要 dev endpoint 时显式 `SMOKE_ENABLE_DEV_ENDPOINTS=true make smoke`。
- 第二次 `make smoke` 失败：`/memory/turn-event` 返回 `401 adapter_token_required`。
- 根因：生产 AuthService 已启用，smoke 未提供 Adapter Token。
- 处理：无 token 时验证 401 安全边界；有 `SMOKE_ADAPTER_TOKEN` 时仍可执行写入幂等链路。
- 第三次 `make smoke` 失败：`/memory/search` 返回 `403 memory_search_forbidden`。
- 根因：生产 TenantService 已启用，smoke 用户没有真实 membership。
- 处理：未种子化租户数据时验证 403 隔离边界。

剩余问题：

- `make smoke` 当前在生产无种子数据时只能证明安全边界、健康检查、Qdrant smoke、adapter/importer/web 基础链路；还不能证明完整 TurnEvent 写入和 Retrieval 查询成功。
- 需要后续增加安全的生产测试 fixture / seed 流程，生成临时用户、项目和 Adapter Token 后跑真实写入与检索，再清理或软删除。
- Archive RAG 生产索引、Hot Memory 生产派生、管理台真实 CRUD 仍未完成。

## 2026-07-02 Phase 1.9：PostgreSQL Migration Runner

完成事项：

- 新增嵌入式 PostgreSQL migration runner。
- 将 `migrations/*.sql` embed 到发布二进制，避免最终 Docker 镜像依赖源码目录或 `/docker-entrypoint-initdb.d`。
- runner 按 migration 文件名前缀排序执行，使用 `schema_migrations` 记录版本并幂等跳过已应用 migration。
- runner 拒绝明显破坏性语句：`DROP TABLE`、`TRUNCATE`、`DELETE FROM`、`ALTER TABLE ... DROP`。
- API 启动路径在 PostgreSQL pool 创建后自动运行 embedded migrations；migration 失败会阻止 API 继续启动。

验证命令：

- 本地 `go test ./internal/db -run 'TestMigration|TestRunMigrations|TestEmbedded'`：通过。
- 本地 `go test ./cmd/memory-api ./migrations`：通过。
- 本地 `go test ./...`：通过。
- 服务器 `make test`：通过。
- 服务器真实 Postgres contract：

```bash
docker run --rm --network deploy_default \
  -e POSTGRES_TEST_DSN="postgres://memory_os:***@postgres:5432/memory_os?sslmode=disable" \
  -v "$PWD":/src -w /src golang:1.25-bookworm \
  go test ./internal/db -run "TestRunEmbeddedMigrationsContract" -count=1 -v
```

结果：`TestRunEmbeddedMigrationsContract` PASS。

- 服务器重建并重启 `memory-api`：成功。
- 服务器 `make smoke`：通过，输出 `smoke ok`。
- 服务器 `/healthz`：HTTP 200，`db/qdrant/redis` 均为 `ok`。
- 服务器 `schema_migrations` 当前版本：`1, 2, 3, 4, 5, 6, 7, 10, 11, 12`。

剩余问题：

- migration runner 目前只接入 `memory-api` 启动路径，`memory-worker` 尚未接入。
- runner 只有 forward-only 防护，没有实现回滚；危险 migration 仍应走人工备份和审批。
- 现有数据库此前手动应用过 migrations，本次 runner 已补齐版本记录，但后续仍需要规范化部署流程，避免手动 psql 漏记。

## 2026-07-02 Phase 1.10：Worker Migration Runner 接入

完成事项：

- `memory-worker` 启动路径在存在 `POSTGRES_DSN` 时创建 PostgreSQL pool。
- `memory-worker` 启动时运行 embedded migration runner，避免后台服务依赖手动 psql 或 Docker 首次初始化。
- 保留无 `POSTGRES_DSN` 的测试/本地轻量 runner 构建路径。
- 为 worker 启动路径增加测试 hook，验证有 DSN 时会创建 pool 并运行 migrations。

验证命令：

- 本地 `go test ./cmd/memory-worker`：通过。
- 本地 `go test ./...`：通过。
- 服务器重建并重启 `memory-worker`：成功。
- 服务器 `make test`：通过。
- 服务器 `make smoke`：通过，输出 `smoke ok`。
- 服务器 `docker-compose -f deploy/docker-compose.yml ps memory-worker`：`deploy-memory-worker-1` 为 `Up`。
- 服务器 `docker logs --tail=40 deploy-memory-worker-1`：出现 `memory-worker starting`。

剩余问题：

- `memory-worker` 目前仍只是 jobs runner 骨架，尚未接入真实 Archive 生成、RAG indexing、Hot Memory 派生队列。
- worker 日志 environment 当前仍显示 `development`，后续需要改为读取 `cfg.AppEnv`。
- 生产队列、幂等 job lease、失败重试和崩溃恢复仍需继续实现。

## 2026-07-02 Phase 1.11：Worker 日志环境生产化

完成事项：

- `memory-worker` zap logger 环境从写死 `development` 改为读取 `cfg.AppEnv`。
- 增加 `workerLoggerOptions` 单元测试，确保生产配置下日志字段为 `production`。

验证命令：

- 本地 `go test ./cmd/memory-worker`：通过。
- 本地 `go test ./...`：通过。
- 服务器重建并重启 `memory-worker`：成功。
- 服务器 `make test`：通过。
- 服务器 `make smoke`：通过，输出 `smoke ok`。
- 服务器 `docker logs --tail=20 deploy-memory-worker-1`：`memory-worker starting` 日志包含 `"env":"production"`。

剩余问题：

- `memory-worker` 仍未接入真实 Archive/RAG/Hot Memory job 消费。
- worker 仍缺生产队列、租约、重试和失败恢复。

## 2026-07-02 Phase 1.12：Archive 目录生产配置

完成事项：

- 新增 `Config.ArchiveDir`。
- 新增默认值 `ARCHIVE_DIR=/data/memory-os`，作为 Markdown Archive 权威正文根目录。
- `.env.example` 增加 `ARCHIVE_DIR`。
- Docker Compose 为 `memory-api` 与 `memory-worker` 注入 `ARCHIVE_DIR`。

验证命令：

- 本地 `go test ./internal/config`：通过。
- 本地 `go test ./cmd/memory-worker ./cmd/memory-api`：通过。
- 本地 `go test ./...`：通过。
- 服务器 `make test`：通过。
- 服务器 `docker-compose -f deploy/docker-compose.yml config`：可看到 `ARCHIVE_DIR: /data/memory-os`。

剩余问题：

- 当前只是配置基础，Archive service / worker 尚未使用 `cfg.ArchiveDir` 接入 PG repository。
- Compose 尚未为 `/data/memory-os` 显式挂载持久卷；后续接入 Archive 正文写入前必须补齐持久化 volume。

## 2026-07-02 Phase 1.13：Archive 正文持久卷

完成事项：

- Docker Compose 新增 `archive_data` volume。
- `memory-api` 挂载 `archive_data:/data/memory-os`。
- `memory-worker` 挂载 `archive_data:/data/memory-os`。
- 增加 compose 静态测试，防止后续移除 Archive 正文持久化挂载。

验证命令：

- 本地 `go test ./internal/webdeploy -run 'TestComposePersistsArchiveDirectory|TestComposePassesExternalAPIBaseForT480WebBuild'`：通过。
- 本地 `go test ./cmd/memory-worker ./cmd/memory-api ./internal/config`：通过。
- 本地 `go test ./...`：通过。
- 服务器 `make test`：通过。
- 服务器 `docker-compose -f deploy/docker-compose.yml config`：可看到 `archive_data` 挂载到 `/data/memory-os`，并声明 `deploy_archive_data` volume。

剩余问题：

- 这是 Archive 正文持久化的基础设施，Archive service / worker 仍未切换到 `cfg.ArchiveDir + archive.PGRepository`。
- 当前运行中的容器尚未因为此配置变更而重建；在 Archive 正式接入正文写入前需要重建相关服务。

## 2026-07-02 Phase 1.14：ArchiveWorker PG 构造接入

完成事项：

- `memory-worker` 在有 `POSTGRES_DSN` 时构造 `archive.NewService(archive.NewPGRepository(pool), cfg.ArchiveDir)`。
- `memory-worker` 构造 `jobs.ArchiveWorker` 并放入 `jobs.Runner`。
- `jobs.Runner` 新增 ArchiveWorker 配置位和 cleanup hook，确保 Postgres pool 不会在 `buildWorker` 返回前关闭，而是在 Runner 退出时清理。
- `memory-api` 和 `memory-worker` 已重建，运行容器均挂载 `deploy_archive_data:/data/memory-os`。

验证命令：

- 本地 `go test ./internal/jobs`：通过。
- 本地 `go test ./cmd/memory-worker`：通过。
- 本地 `go test ./...`：通过。
- 服务器重建并重启 `memory-api memory-worker`：成功。
- 服务器 `make test`：通过。
- 服务器 `make smoke`：通过，输出 `smoke ok`。
- 服务器 `docker inspect deploy-memory-api-1`：挂载 `deploy_archive_data` 到 `/data/memory-os`。
- 服务器 `docker inspect deploy-memory-worker-1`：挂载 `deploy_archive_data` 到 `/data/memory-os`。
- 服务器 `docker logs --tail=20 deploy-memory-worker-1`：`memory-worker starting`，`env=production`。

剩余问题：

- ArchiveWorker 已构造，但 Runner 还没有真实队列消费 loop，因此不会自动处理 ArchiveJob。
- TurnEvent -> Archive job 的生产链路仍未接通。
- Archive 创建后的 RAG indexing job 仍未自动触发。

## 2026-07-02 Phase 1.15：Archive job loop 接口与行为测试

完成事项：

- `jobs.Runner` 新增 `ArchiveQueue` 接口，定义 `Lease`、`Complete`、`Fail` 三个队列边界。
- `jobs.Runner` 在同时配置 `ArchiveWorker` 和 `ArchiveQueue` 时启动 Archive job 消费 loop。
- job 处理成功后调用 `Complete`，处理失败后调用 `Fail`，无 job 时按 `PollInterval` 等待并响应 context cancel。
- 未配置 `ArchiveQueue` 时保持原有 runner 行为，只等待 context 退出，避免影响当前生产启动路径。
- `ArchiveWorker` 增加测试注入点，用于验证 runner loop 行为，不改变正常 service 调用路径。

验证命令：

- 本地红测 `go test ./internal/jobs`：实现前按预期失败，缺少 `ArchiveQueue`、`PollInterval` 和测试 worker handler。
- 本地 `gofmt -w internal/jobs/runner.go internal/jobs/runner_test.go internal/jobs/archive_worker.go`：通过。
- 本地 `go test ./internal/jobs`：通过。
- 本地 `go test ./cmd/memory-worker`：通过。
- 本地 `go test ./internal/jobs ./internal/archive`：通过。
- 本地 `go test ./...`：通过。

剩余问题：

- 当前只是 runner 的 queue 接口与 loop，不是 durable queue backend。
- 仍缺 PostgreSQL 或 Redis backed ArchiveJob lease、retry、visibility timeout、崩溃恢复和幂等状态表。
- TurnEvent -> ArchiveJob 的生产链路仍未接通，worker 暂时不会从真实事件自动生成 Archive。
- Archive 完成后的 RAG indexing job 仍未自动触发。

## 2026-07-02 Phase 1.16：PostgreSQL ArchiveJob 持久队列

完成事项：

- 新增 forward-only migration `000013_archive_jobs.sql`。
- 新增 `archive_jobs` 表，保存 `request_id`、`archive_id`、租户 scope、`event_ids`、状态、attempts、lease、last_error、completed_at。
- 新增 `PGArchiveQueue`，实现 `Enqueue`、`Lease`、`Complete`、`Fail`。
- `Enqueue` 使用 `request_id` 唯一约束保证幂等。
- `Lease` 使用事务和 `FOR UPDATE SKIP LOCKED`，支持多 worker 下避免重复领取同一 job。
- `Lease` 从 `turn_events + turn_event_payloads` 还原安全 TurnEvent payload，不复制未脱敏原始事件。
- `Fail` 在未超过 `max_attempts` 时重新置为 `pending`，达到上限后置为 `failed`。
- `memory-worker` 在有 `POSTGRES_DSN` 时注入 `PGArchiveQueue`，Runner 不再停留在“有 ArchiveWorker 但无持久队列”的状态。

验证命令：

- 本地红测 `go test ./migrations ./internal/jobs`：实现前按预期失败，缺少 `000013_archive_jobs.sql` 和 `PGArchiveQueue`。
- 本地 `gofmt -w internal/jobs/pg_archive_queue.go internal/jobs/pg_archive_queue_test.go internal/jobs/runner.go cmd/memory-worker/main.go cmd/memory-worker/main_test.go migrations/migrations_test.go`：通过。
- 本地 `go test ./migrations ./internal/jobs ./cmd/memory-worker`：通过。
- 本地 `go test ./...`：通过。
- 服务器 `make test`：通过。
- 服务器真实 PostgreSQL migration contract：`go test ./internal/db -run TestRunEmbeddedMigrationsContract -count=1 -v` 通过。
- 服务器真实 PostgreSQL queue contract：`go test ./internal/jobs -run TestPGArchiveQueue -count=1 -v` 通过。
- 服务器 `make smoke`：通过，输出 `smoke ok`。
- 服务器 PostgreSQL `schema_migrations`：已确认存在版本 `13`。

剩余问题：

- 运行中的 `memory-worker` 容器尚未重建为包含 `PGArchiveQueue` 注入的新镜像；重启/部署属于难回滚操作，需要用户确认。
- TurnEvent -> ArchiveJob 的自动生产链路仍未接通，当前需要后续由事件写入服务或归档调度器 enqueue。
- Archive job 完成后仍未自动生成 RAG indexing job。
- `PGArchiveQueue` 当前是 PostgreSQL backed queue，尚未加入 Redis 限流/分布式锁；Phase 1 先以 PostgreSQL 权威状态满足崩溃恢复基础。

## 2026-07-02 Phase 1.17：TurnEvent 持久化与 ArchiveJob 自动入队

完成事项：

- 新增 `eventlog.PGRepository`，生产 `/memory/turn-event` 不再只能使用内存 EventLog repository。
- `eventlog.Repository.Save` 改为接收 `SanitizedEvent`，把 safe payload、hash、bytes、truncated、warnings 一起持久化。
- `eventlog.Service.Ingest` 在内部返回已脱敏的 TurnEvent，JSON 响应不暴露该内部字段。
- `TurnEventHandler` 支持可选 ArchiveQueue hook，成功 ingest 且非重复请求时自动 enqueue ArchiveJob。
- ArchiveJob 使用稳定 `request_id=archive_<turn_event_request_id>` 和 `archive_id=archive_<event_id>`，重复 TurnEvent 请求不会重复 enqueue。
- HTTP 入队使用 sanitized event，测试覆盖含假 Secret 的 payload 不会把明文传入 ArchiveJob。
- `memory-api` 生产 routerOptions 注入 `eventlog.NewPGRepository(pool)` 和 `jobs.NewPGArchiveQueue(pool, WorkerID: memory-api)`。
- 开发 smoke 模式仍保持无认证内存路径，不影响现有 smoke。

验证命令：

- 本地红测 `go test ./internal/eventlog ./internal/http`：实现前按预期失败，缺少 `NewPGRepository`、`EventLogService` 和 `ArchiveQueue` 注入字段。
- 本地 `gofmt -w internal/eventlog/*.go internal/http/router.go internal/http/router_test.go cmd/memory-api/main.go cmd/memory-api/main_test.go`：通过。
- 本地 `go test ./internal/eventlog ./internal/http ./cmd/memory-api`：通过。
- 本地 `go test ./...`：通过。
- 服务器 `make test`：通过。
- 服务器真实 PostgreSQL EventLog contract：`go test ./internal/eventlog -run TestPGRepository -count=1 -v` 通过。
- 服务器 `make smoke`：通过，输出 `smoke ok`。

剩余问题：

- 运行中的 `memory-api` 和 `memory-worker` 容器尚未重建为包含本轮新代码的镜像；重启/部署需要用户确认。
- 当前自动入队粒度是单 TurnEvent 生成一个 ArchiveJob，后续仍需实现按 turn/thread/session 聚合归档策略。
- ArchiveJob 被 worker 完成后仍未自动触发 RAG indexing job。
- `/memory/turn-event` 的 OpenAPI 描述仍需补齐，当前 OpenAPI 仍不是完整生产 API 文档。

## 2026-07-02 Phase 1.18：Archive 完成后 RAG indexing job 生产链路

完成事项：

- 新增 forward-only migration `000014_archive_index_job_lease.sql`，为 `archive_index_jobs` 增加 attempts、max_attempts、locked_by、locked_until、completed_at 和 ready index。
- 新增 `PGArchiveIndexQueue`，实现 RAG indexing job 的 `Enqueue`、`Lease`、`Complete`、`Fail`。
- `PGArchiveIndexQueue.Enqueue` 会把 archive chunks 持久化到 `archive_chunks`，并创建幂等 `archive_index_jobs`。
- `PGArchiveIndexQueue.Lease` 使用事务和 `FOR UPDATE SKIP LOCKED`，并从 `archives + archive_chunks` 还原完整 `RAGIndexJob`。
- `Fail` 在未超过 `max_attempts` 时重置为 `pending`，达到上限后标记 `failed`。
- `ArchiveWorker` 在 Archive 创建成功且非 deduped 时读取 Markdown 正文、执行 chunking、enqueue RAG index job。
- Archive chunking 仍执行 Secret 脱敏，测试覆盖 ArchiveWorker enqueue 的 chunk metadata。
- `Runner` 支持 Archive loop 与 RAG index loop 并行运行。
- `memory-worker` 生产构造路径注入 `PGArchiveIndexQueue` 与 `RAGIndexWorker`，ArchiveWorker 使用同一个 RAG queue 入队。

验证命令：

- 本地红测 `go test ./migrations ./internal/jobs`：实现前按预期失败，缺少 `000014_archive_index_job_lease.sql`、`PGArchiveIndexQueue`、`NewArchiveWorkerWithIndexQueue` 和 Runner RAG 配置字段。
- 本地 `gofmt -w migrations/migrations_test.go internal/jobs/pg_archive_index_queue.go internal/jobs/pg_archive_index_queue_test.go internal/jobs/archive_worker.go internal/jobs/archive_worker_test.go internal/jobs/rag_index_worker.go internal/jobs/runner.go internal/jobs/runner_test.go cmd/memory-worker/main.go cmd/memory-worker/main_test.go`：通过。
- 本地 `go test ./internal/jobs ./cmd/memory-worker ./migrations`：通过。
- 本地 `go test ./...`：通过。
- 服务器 `make test`：通过。
- 服务器真实 PostgreSQL migration contract：`go test ./internal/db -run TestRunEmbeddedMigrationsContract -count=1 -v` 通过。
- 服务器真实 PostgreSQL RAG queue contract：`go test ./internal/jobs -run TestPGArchiveIndexQueue -count=1 -v` 通过。
- 服务器 `make smoke`：通过，输出 `smoke ok`。
- 服务器 PostgreSQL `schema_migrations`：已确认存在版本 `14`。

剩余问题：

- 运行中的 `memory-api` 和 `memory-worker` 容器尚未重建为包含 Phase 1.17/1.18 新代码的镜像；重启/部署需要用户确认。
- 当前 RAG indexing worker 仍使用 `rag.MemoryStore` 作为索引目标；后续必须切到 Qdrant 生产索引与 query-time filter 验收。
- Archive chunk 已持久化到 PostgreSQL，但 `qdrant_points` 和 Qdrant upsert 仍未接入生产 worker。
- 当前 RAG indexing job 只覆盖 Archive 创建后的初始 generation，后续还需要把 Archive 编辑后的 generation 自动 enqueue。

## 2026-07-02 Phase 1.19：RAG indexing worker 切到 QdrantStore

完成事项：

- 新增 `rag.QdrantStore`。
- `QdrantStore.Upsert` 调用 `llm.EmbeddingClient` 生成 embedding。
- `QdrantStore.Upsert` 调用 Qdrant `UpsertPoints` 写入 `memory_os` collection。
- Qdrant payload 包含 `doc_type`、`chunk_id`、`archive_id`、`org_id`、`project_id`、`user_id`、`visibility`、`permission_labels`、`index_generation`、`content_hash`、`source_event_ids`。
- `QdrantStore.Upsert` 成功后写入 PostgreSQL `qdrant_points`，状态为 `indexed`。
- Qdrant point id 使用 chunk id 派生的稳定 UUID，满足 Qdrant 点 ID 约束。
- `memory-worker` 生产构造路径不再默认使用 `rag.MemoryStore`，而是创建 Qdrant client、确保 collection、创建 OpenAI-compatible embedding client，并构造 `rag.QdrantStore`。
- worker 构造测试覆盖 Qdrant/LLM 配置被传入 RAG store。

验证命令：

- 本地红测 `go test ./internal/rag ./cmd/memory-worker`：实现前按预期失败，缺少 `NewQdrantStore` 与 `newRAGIndexStore`。
- 本地 `gofmt -w internal/rag/qdrant_store.go internal/rag/qdrant_store_test.go cmd/memory-worker/main.go cmd/memory-worker/main_test.go`：通过。
- 本地 `go test ./internal/rag ./cmd/memory-worker`：通过。
- 本地 `go test ./...`：通过。
- 服务器 `make test`：通过。
- 服务器真实 PostgreSQL QdrantStore contract：`go test ./internal/rag -run TestQdrantStore -count=1 -v` 通过。
- 服务器 `make smoke`：通过，输出 `smoke ok`。

剩余问题：

- 运行中的 `memory-worker` 容器尚未重建为包含 Phase 1.19 新代码的镜像；重启/部署需要用户确认。
- `QdrantStore.Filtered` 尚未实现，检索端仍未使用真实 Qdrant Search。
- Unified Retrieval 当前生产路径仍需切换到 Qdrant-backed ArchiveRAG，并验证 query-time payload filter。
- 真实 embedding provider/API key 需要通过环境变量配置，不能使用 `.env.example` 的占位值。

## 2026-07-02 Phase 1.20：Archive RAG 检索切到 Qdrant Search

完成事项：

- `rag.Service.Search` 新增生产向量检索分支：当底层 store 实现 `SearchStore` 时，不再走内存候选集过滤。
- `rag.QdrantStore.Search` 调用 `llm.EmbeddingClient` 为查询生成 embedding。
- `rag.QdrantStore.Search` 调用 Qdrant `SearchPoints`，并强制携带 `qdrant.PayloadFilter`，满足 query-time filter 下推要求。
- Qdrant search 结果只使用 payload 中的 `chunk_id` 做定位，正文从 PostgreSQL `archive_chunks` 读取，保持 PostgreSQL/Markdown 链路作为权威数据源。
- Qdrant 返回 stale 或已删除 chunk 时，PG 查不到会跳过该结果，不让可重建索引脏点导致整个检索失败。
- `memory-api` 生产 routerOptions 不再把 Archive RAG 注入为 `rag.MemoryStore`；改为创建 Qdrant client、确保 collection、创建 OpenAI-compatible embedding client，并注入 `rag.QdrantStore`。
- API 生产 Archive RAG 初始化失败时返回错误，避免静默退回非生产内存实现。

验证命令：

- 本地红测 `go test ./internal/rag -run 'TestQdrantStoreSearch' -count=1`：实现前按预期失败，缺少 `QdrantStore.Search` 和 `SearchRequest.Limit`。
- 本地红测 `go test ./cmd/memory-api -run 'TestRouterOptions' -count=1`：实现前按预期失败，`routerOptions` 不能返回错误且缺少生产 Archive RAG 工厂。
- 本地 `gofmt -w internal/rag/model.go internal/rag/service.go internal/rag/store.go internal/rag/qdrant_store.go internal/rag/qdrant_store_test.go cmd/memory-api/main.go cmd/memory-api/main_test.go`：通过。
- 本地 `go test ./internal/rag ./cmd/memory-api -count=1`：通过。
- 本地 `go test ./...`：通过。
- 服务器真实 PostgreSQL QdrantStore contract：`docker run --rm --network deploy_default ... go test ./internal/rag -run 'TestQdrantStore' -count=1 -v` 通过。
- 服务器 `make test`：通过。
- 服务器 `make smoke`：通过，输出 `smoke ok`。
- 服务器 `docker-compose -f deploy/docker-compose.yml ps`：API、Web、MCP、worker、PostgreSQL、Redis、Qdrant 均为 Up，PostgreSQL/Redis healthy。

剩余问题：

- 本轮仅同步源码并运行测试，没有重启或部署线上容器；运行中的 `memory-api` 是否已包含本轮代码取决于后续经用户确认的 rebuild/restart。
- `QdrantStore.Filtered` 仍保留为内存接口兼容空实现；生产检索路径已通过 `SearchStore` 走 `SearchPoints`，但后续可考虑移除或收窄旧接口以减少误用面。
- 真实端到端检索仍需要配置可用 embedding provider/API key 后，用浏览器和 HTTP 写入 TurnEvent -> Archive -> Index -> Search 完整验收。
- 当前 Qdrant filter 下推由 fake Qdrant client 合约测试证明请求参数，尚未用真实 Qdrant HTTP 抓包或集成测试验证 payload 过滤结果。

## 2026-07-02 Phase 1.21：真实 Qdrant query-time filter 集成验收

完成事项：

- 新增 `TestSearchPointsRealQdrantAppliesQueryTimeFilter`，通过 `QDRANT_TEST_URL` 显式启用真实 Qdrant 集成测试。
- 集成测试创建临时 collection，写入三条同向量点：
  - 允许命中的当前 user/current generation archive chunk。
  - 不同 user 的 archive chunk。
  - 同 user 但旧 `index_generation` 的 archive chunk。
- 测试使用 `BuildPayloadFilter` 构造 query-time filter，并通过真实 Qdrant `SearchPoints` 验证结果只返回允许命中的 chunk。
- 测试结束后删除临时 collection，避免污染 `memory_os` 生产 collection。
- 修复一次性 Docker 测试容器的代理绕行配置：`Makefile` 新增默认 `NO_PROXY=localhost,127.0.0.1,postgres,redis,qdrant`，并传入 Docker test/build-web/smoke 分支。
- `.env.example` 增加同样的 `NO_PROXY/no_proxy` 默认值，避免内网服务名被外部代理劫持。

验证命令：

- 本地 `go test ./internal/qdrant -run 'TestSearchPoints' -count=1 -v`：通过；未设置 `QDRANT_TEST_URL` 时真实 Qdrant 用例按预期 skip。
- 本地 `go test ./...`：通过。
- 服务器首次真实 Qdrant 红灯：`QDRANT_TEST_URL=http://qdrant:6333 ... go test ./internal/qdrant -run TestSearchPointsRealQdrantAppliesQueryTimeFilter` 失败，返回 `502 Bad Gateway`。
- 根因：一次性 Docker 容器默认存在 `HTTP_PROXY/http_proxy`，且 `NO_PROXY` 未包含 `qdrant`，导致访问 `http://qdrant:6333` 被代理截走。
- 服务器代理绕行验证：显式 `NO_PROXY=qdrant,postgres,redis,localhost,127.0.0.1` 后，`curl http://qdrant:6333/healthz` 返回 `200 OK`。
- 服务器真实 Qdrant 集成：`docker run --rm --network deploy_default ... QDRANT_TEST_URL=http://qdrant:6333 go test ./internal/qdrant -run 'TestSearchPointsRealQdrantAppliesQueryTimeFilter' -count=1 -v` 通过。
- 服务器 `make test`：通过。
- 服务器 `make smoke`：通过，输出 `smoke ok`。
- 服务器 `docker-compose -f deploy/docker-compose.yml ps`：API、Web、MCP、worker、PostgreSQL、Redis、Qdrant 均为 Up，PostgreSQL/Redis healthy。

剩余问题：

- 本轮没有重启或部署线上容器；Makefile 与 `.env.example` 改动已同步到服务器工作区，但运行中镜像不会自动包含这些文件变更。
- 真实 Qdrant filter 已证明 user 与 `index_generation` 隔离；后续仍需在完整 `/memory/search` 链路里验证 org/project/permission label/agent scope 的端到端隔离。
- 真实 embedding provider/API key 仍需通过生产环境变量配置后，才能完成 TurnEvent -> Archive -> RAG index -> Search 的完整生产验收。

## 2026-07-02 Phase 1.22：Archive RAG 检索强制 index_generation

完成事项：

- `retrieval.Service` 在 Archive RAG 配置存在时，强制要求 `ArchiveIndexGeneration > 0`。
- 缺少 `archive_index_generation` 时，服务直接返回错误，不再构造缺少 `index_generation` 的 Qdrant filter。
- 新增测试证明 Archive RAG 查询会把完整租户上下文传入 Qdrant filter：
  - `doc_type=archive_chunk`
  - `user_id`
  - `org_id`
  - `project_id`
  - `visibility`
  - `permission_labels`
  - `index_generation`
- 纯 Hot Memory 检索不受该校验影响，避免把 Archive RAG generation 要求错误施加到非 Archive 检索。

验证命令：

- 本地红测 `go test ./internal/retrieval -run 'TestSearchRejectsArchiveRAGWithoutIndexGeneration|TestSearchPassesFullArchiveRAGFilter' -count=1 -v`：实现前按预期失败，`Search()` 在缺少 generation 时返回 nil error。
- 本地目标测试：`go test ./internal/retrieval -run 'TestSearchRejectsArchiveRAGWithoutIndexGeneration|TestSearchPassesFullArchiveRAGFilter|TestSearchMergesHotMemoryAndArchiveWithTraceableSources' -count=1 -v` 通过。
- 本地 HTTP 相关测试：`go test ./internal/http -run 'TestMemorySearch' -count=1 -v` 通过。
- 本地 `go test ./...`：通过。
- 服务器目标测试：`docker run --rm --network deploy_default ... go test ./internal/retrieval -run 'TestSearchRejectsArchiveRAGWithoutIndexGeneration|TestSearchPassesFullArchiveRAGFilter' -count=1 -v` 通过。
- 服务器 `make test`：通过。
- 服务器 `make smoke`：通过，输出 `smoke ok`。
- 服务器 `docker-compose -f deploy/docker-compose.yml ps`：API、Web、MCP、worker、PostgreSQL、Redis、Qdrant 均为 Up，PostgreSQL/Redis healthy。

剩余问题：

- 本轮没有重启或部署线上容器；代码已同步到服务器工作区，但运行中服务镜像是否包含该行为仍取决于后续经用户确认的 rebuild/restart。
- `/memory/search` 仍需要真实 embedding provider 后做完整 TurnEvent -> Archive -> Qdrant index -> Search 浏览器/HTTP 验收。
- 当前强制使用请求传入的 `archive_index_generation`；后续生产化需要由 Archive 元数据服务按 archive/project 当前 generation 自动解析，减少客户端传错 generation 的风险。

## 2026-07-02 Phase 1.23：后端自动解析 Archive 当前 generation

完成事项：

- `retrieval.Service` 新增 `ArchiveGenerationResolver` 注入点。
- `/memory/search` 请求未传 `archive_index_generation` 时，Archive RAG 分支会尝试从 resolver 获取当前 scope 的 generation。
- resolver 返回 `0` 表示当前 user/org/project scope 没有 active archive，服务会安全跳过 Archive RAG，不再把缺失 generation 的 Qdrant filter 下发。
- 仍保留安全边界：如果 Archive RAG 已配置且没有 resolver，同时请求也没传 generation，则拒绝检索，避免旧 generation chunk 被召回。
- 新增 `PGArchiveGenerationResolver`，从 PostgreSQL `archives` 表按 `user_id/org_id/project_id/status='active'` 读取 `MAX(index_generation)`。
- `memory-api` 生产构造路径注入 `retrieval.NewPGArchiveGenerationResolver(pool)`，减少客户端传错/漏传 generation 的风险。

验证命令：

- 本地红测 `go test ./internal/retrieval -run 'TestSearchResolvesArchiveIndexGenerationWhenMissing|TestSearchSkipsArchiveRAGWhenNoArchiveGenerationExists' -count=1 -v`：实现前按预期失败，`Options` 缺少 `ArchiveGenerationResolver`。
- 本地目标测试：`go test ./internal/retrieval -count=1 -v` 通过；PG resolver contract 在未设置 `POSTGRES_TEST_DSN` 时按预期 skip，服务层 resolver 测试执行通过。
- 本地 `go test ./internal/retrieval ./cmd/memory-api -count=1`：通过。
- 本地 `go test ./...`：通过。
- 服务器真实 PostgreSQL resolver contract：`docker run --rm --network deploy_default ... POSTGRES_TEST_DSN=... go test ./internal/retrieval -run 'TestPGArchiveGenerationResolver|TestSearchResolvesArchiveIndexGenerationWhenMissing|TestSearchSkipsArchiveRAGWhenNoArchiveGenerationExists' -count=1 -v` 通过。
- 服务器 `make test`：通过。
- 服务器 `make smoke`：通过，输出 `smoke ok`。
- 服务器 `docker-compose -f deploy/docker-compose.yml ps`：API、Web、MCP、worker、PostgreSQL、Redis、Qdrant 均为 Up，PostgreSQL/Redis healthy。

剩余问题：

- 本轮没有重启或部署线上容器；代码已同步到服务器工作区，但运行中服务镜像是否包含该行为仍取决于后续经用户确认的 rebuild/restart。
- 当前 resolver 使用 scoped `MAX(index_generation)`，适配现阶段单一 generation filter；后续多 archive 且 generation 不一致时，需要升级为更精细的 archive_id + generation 组合过滤或索引任务统一 generation 策略。
- `/memory/search` 仍需要真实 embedding provider 后做完整 TurnEvent -> Archive -> Qdrant index -> Search 浏览器/HTTP 验收。

## 2026-07-02 Phase 1.24：严格 Memory Search smoke 门

完成事项：

- `cmd/memory-smoke` 新增 `SMOKE_REQUIRE_CONFIGURED_RETRIEVAL=true` 严格模式。
- 默认 smoke 仍兼容当前开发/过渡状态，可以接受 `/memory/search` 返回 `retrieval_not_configured` 或 `memory_search_forbidden`。
- 严格模式下，`/memory/search` 返回 `retrieval_not_configured` 或 `memory_search_forbidden` 会直接失败。
- 严格模式要求 smoke 看到真实 unified retrieval 响应，包含 `rerank_degraded=true`、Hot Memory source 和 Archive chunk source。
- 新增测试覆盖默认兼容模式和严格失败模式，避免最终交付时用 403/503 冒充检索可用。

验证命令：

- 本地红测 `go test ./cmd/memory-smoke -run 'TestMemorySearchSmoke' -count=1 -v`：实现前严格模式测试按预期失败。
- 本地 `go test ./cmd/memory-smoke -count=1 -v`：通过。
- 本地 `go test ./...`：通过。
- 服务器 `go test ./cmd/memory-smoke -count=1 -v`：通过。
- 服务器 `make test`：通过。
- 服务器默认 `make smoke`：通过，输出 `smoke ok`。
- 服务器严格 retrieval smoke：`SMOKE_REQUIRE_CONFIGURED_RETRIEVAL=true make smoke` 通过，输出 `smoke ok`。
- 服务器 `docker-compose -f deploy/docker-compose.yml ps`：API、Web、MCP、worker、PostgreSQL、Redis、Qdrant 均为 Up，PostgreSQL/Redis healthy。

剩余问题：

- 严格 smoke 已能证明 `/memory/search` 当前不是 403/503 兜底，但还不是完整的 TurnEvent -> Archive -> Qdrant index -> Search 自动造数验收。
- 后续需要增加一个受控端到端验收：写入 TurnEvent，等待 Archive/RAG worker 完成，随后通过 `/memory/search` 搜到该归档并验证 Secret 不泄露。

## 2026-07-02 Phase 1.25：Smoke 环境透传纠偏与 Pipeline E2E 门禁

完成事项：

- 纠正 Phase 1.24 的验收记录：重新验证后发现严格 retrieval smoke 并未真正通过生产 actor 场景，之前结论不应作为最终交付证据。
- `cmd/memory-smoke` 保留 strict retrieval 模式，并支持通过环境变量指定真实租户 actor：
  - `SMOKE_SEARCH_USER_ID`
  - `SMOKE_SEARCH_ORG_ID`
  - `SMOKE_SEARCH_PROJECT_ID`
  - `SMOKE_SEARCH_AGENT_ID`
  - `SMOKE_SEARCH_PERMISSION_LABEL`
- 修复 `Makefile` Docker smoke 分支未透传上述 actor 环境变量的问题。
- 新增 Makefile 合约测试，防止后续再次出现 smoke 容器吃默认 actor、导致 strict 验收假失败或假通过。
- `cmd/memory-smoke` 新增可选 Pipeline E2E 门禁：
  - 默认关闭：`SMOKE_ENABLE_PIPELINE_E2E=false`。
  - 开启后必须提供 `SMOKE_ADAPTER_TOKEN`，否则明确失败。
  - 目标流程是写入 TurnEvent，等待 Archive/RAG 进入检索，再验证 Secret 不泄露。

验证命令：

- 本地红测：`go test ./cmd/memory-smoke -run TestMakeSmokePassesStrictPipelineEnvironmentToDocker -count=1 -v`，实现前失败于 `SMOKE_SEARCH_USER_ID` 未透传。
- 本地目标测试：`go test ./cmd/memory-smoke -run TestMakeSmokePassesStrictPipelineEnvironmentToDocker -count=1 -v` 通过。
- 本地 `go test ./...`：通过。
- 服务器目标测试：`docker run --rm --network host ... go test ./cmd/memory-smoke -run TestMakeSmokePassesStrictPipelineEnvironmentToDocker -count=1 -v` 通过。
- 服务器 `make test`：通过。
- 服务器默认 `make smoke`：通过，输出 `smoke ok`。
- 服务器 strict retrieval smoke 使用真实 owner actor 后不再返回 `memory_search_forbidden`，但返回 200 空结果并失败：`results: []`。
- 服务器 Pipeline E2E 负向门禁：`SMOKE_ENABLE_PIPELINE_E2E=true make smoke` 按预期失败，错误为 `pipeline e2e smoke requires SMOKE_ADAPTER_TOKEN`。

失败根因：

- 服务器当前生产库没有 Hot Memory 数据。
- 现有 Archive/Chunk 测试数据多为早期字符串 ID，如 `user_1/org_1/project_1`，不属于当前 UUID 租户权限体系，无法作为生产 strict smoke 的可信数据。
- strict retrieval smoke 需要一个受控的生产级数据准备步骤：要么使用有效 Adapter Token 写入 TurnEvent 并等待 worker 产出 Archive/RAG，要么新增受控的非 dev 测试种子命令。

剩余问题：

- 正向 Pipeline E2E 尚未执行，因为没有提供 `SMOKE_ADAPTER_TOKEN`，且不能在日志或回复中明文读取/输出真实 token。
- strict retrieval smoke 当前能证明生产 API 不再因 actor env 漏传而 403，但还不能证明现有生产数据可召回。
- 下一步需要实现或启用受控端到端造数路径，完成 TurnEvent -> Archive -> Qdrant -> `/memory/search` 的正向生产验收。

## 2026-07-02 Phase 1.26：Pipeline E2E 受控造数与部署边界确认

完成事项：

- `cmd/memory-smoke` 的 Pipeline E2E 支持在未提供 `SMOKE_ADAPTER_TOKEN` 时，通过显式 `SMOKE_POSTGRES_DSN` 创建临时生产级测试 actor：
  - 创建临时 user/org/project。
  - 添加 owner membership。
  - 创建短期 Adapter Token。
  - token 只在 smoke 进程内使用，不写日志、不输出。
- Pipeline E2E 写入 TurnEvent 后新增响应校验：
  - 必须包含当前 marker 对应的 `event_id`。
  - 必须返回 `status:"accepted"`。
  - 避免 HTTP 2xx 但未真正接受事件时继续等待搜索超时。
- `Makefile` Docker smoke 分支新增可配置网络与完整环境透传：
  - `SMOKE_DOCKER_NETWORK`，默认仍为 `host`。
  - `SMOKE_API_URL`。
  - `SMOKE_QDRANT_URL`。
  - `SMOKE_LLM_BASE_URL`。
  - `SMOKE_LLM_API_KEY`。
  - `SMOKE_TIMEOUT`。
  - `SMOKE_POSTGRES_DSN`。
- `Makefile`、`.env.example`、`deploy/docker-compose.yml` 的 `NO_PROXY/no_proxy` 增加 compose 内部服务名：
  - `memory-api`
  - `memory-web`
  - `memory-mcp`
- 这允许 smoke 容器切到 `deploy_default` 网络后直接访问 `memory-api/qdrant/postgres`，避免代理劫持内网服务名。

验证命令：

- 本地红测：
  - `go test ./cmd/memory-smoke -run TestPipelineE2ESmokeCanProvisionActorFromPostgresDSN -count=1 -v` 实现前失败于缺少 provisioner。
  - `go test ./cmd/memory-smoke -run TestMakeSmokePassesStrictPipelineEnvironmentToDocker -count=1 -v` 实现前分别失败于缺少 `SMOKE_POSTGRES_DSN`、`SMOKE_DOCKER_NETWORK`、`SMOKE_TIMEOUT` 透传和 `NO_PROXY` 服务名。
  - `go test ./cmd/memory-smoke -run TestPipelineE2ESmokeRejectsTurnEventResponseWithoutAcceptedStatus -count=1 -v` 实现前只能等到搜索超时，不能精准指出 TurnEvent 响应未 accepted。
- 本地目标测试：`go test ./cmd/memory-smoke -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 服务器目标测试：`docker run --rm --network host ... go test ./cmd/memory-smoke -count=1 -v` 通过。
- 服务器全量测试：`make test` 通过。
- 服务器默认 smoke：`make smoke` 通过，输出 `smoke ok`。

服务器正向 E2E 结果：

- 命令形态：`SMOKE_DOCKER_NETWORK=deploy_default SMOKE_API_URL=http://memory-api:18081/healthz SMOKE_QDRANT_URL=http://qdrant:6333 SMOKE_TIMEOUT=3m SMOKE_ENABLE_PIPELINE_E2E=true SMOKE_PIPELINE_E2E_TIMEOUT=45s SMOKE_PIPELINE_E2E_MARKER=pipeline-fixed-1783000002 SMOKE_POSTGRES_DSN=<from memory-api container env> make smoke`。
- 结果：失败，`/memory/search` 返回 200 空结果，未找到 marker 对应 archive chunk。
- 事后查 PostgreSQL：
  - `turn_events` 中没有 `pipeline-fixed-1783000002` 对应 event。
  - `archive_jobs` 中没有对应 job。
  - `archives` 中没有对应 archive。
  - `archive_index_jobs` 中没有对应 index job。
  - `archive_chunks` 中没有对应 chunk。

结论：

- 当前服务器工作区代码已经具备受控 Pipeline E2E smoke 能力。
- 运行中的 API/worker 容器仍未体现当前源码的 PG TurnEvent/Archive Queue 链路；最可能原因是代码已同步但容器镜像尚未经用户确认 rebuild/restart。
- 继续修改源码不能改变运行中容器行为；下一步需要用户批准重建并重启 Memory OS 容器后，再运行正向 Pipeline E2E。

剩余问题：

- 未执行容器 rebuild/restart，因为这是部署操作，按项目规则需要用户确认。
- 正向 Pipeline E2E 尚未通过，不能作为生产完成证据。
- 当前临时测试 actor/token 会写入 PostgreSQL；后续应增加受控清理或软删除策略，但物理删除数据前必须单独确认。

## 2026-07-02 Phase 1.27：部署版本可验证性

完成事项：

- 新增 `internal/buildinfo`，提供非敏感构建元数据：
  - `version`
  - `commit`
  - `build_time`
  - `dirty`
- API 新增 `GET /version`，用于证明运行中容器是否来自当前源码构建。
- OpenAPI 增加 `/version` 路径，避免 API 文档与路由不一致。
- `deploy/Dockerfile.api`、`deploy/Dockerfile.worker`、`deploy/Dockerfile.mcp` 使用 `go build -ldflags` 注入 build info。
- `deploy/docker-compose.yml` 为 API、worker、MCP build 传入：
  - `BUILD_VERSION`
  - `BUILD_COMMIT`
  - `BUILD_TIME`
  - `BUILD_DIRTY`
- `Makefile` 新增 `prod-up` 目标，显式以生产模式重建并启动 API、worker、MCP、Web，并自动传入当前 Git commit、build time、dirty 状态。
- `prod-up` 只是部署命令入口，本轮未执行。

验证命令：

- 本地红测：
  - `go test ./internal/http -run 'TestVersionEndpointReturnsBuildMetadata|TestOpenAPIJSON' -count=1 -v` 实现前 `/version` 404、OpenAPI 缺 `/version`。
  - `go test ./internal/webdeploy -run 'TestBackendDockerfilesInjectBuildInfo|TestComposePassesBackendBuildInfoArgs|TestMakefileProductionDeployTargetSetsBuildInfo' -count=1 -v` 实现前 Dockerfile/compose/Makefile 缺 build info。
- 本地目标测试：`go test ./internal/http ./internal/webdeploy -run 'TestVersionEndpointReturnsBuildMetadata|TestOpenAPIJSON|TestBackendDockerfilesInjectBuildInfo|TestComposePassesBackendBuildInfoArgs|TestMakefileProductionDeployTargetSetsBuildInfo' -count=1 -v` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地 compose 静态校验：`docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml config` 通过。
- 服务器目标测试：`docker run --rm --network host ... go test ./internal/http ./internal/webdeploy -run 'TestVersionEndpointReturnsBuildMetadata|TestOpenAPIJSON|TestBackendDockerfilesInjectBuildInfo|TestComposePassesBackendBuildInfoArgs|TestMakefileProductionDeployTargetSetsBuildInfo' -count=1 -v` 通过。
- 服务器 compose 静态校验：`docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml config` 通过。
- 服务器全量测试：`make test` 通过。
- 服务器默认 smoke：`make smoke` 通过。

运行中容器状态：

- `curl http://127.0.0.1:18081/version` 返回 `404 Not Found`。
- 这证明当前运行中 API 容器仍不是包含 `/version` 的最新工作区代码。

下一步入口条件：

- 需要用户明确批准执行生产重建/重启。
- 批准后建议执行：
  - `cd /opt/memory-os && make prod-up`
  - `curl http://127.0.0.1:18081/version`
  - `make test`
  - `make smoke`
  - 正向 Pipeline E2E smoke
- 若重建后 `/version` 仍未出现，必须先排查 compose/project/image/tag，而不是继续功能开发。

## 2026-07-02 Phase 1.28：部署后验收入口固化

完成事项：

- 新增 `scripts/post-deploy-verify.sh`，作为生产重建/重启后的固定验收入口。
- `post-deploy-verify` 默认执行：
  - `docker-compose ... ps`
  - `curl /version`
  - `curl /healthz`
  - `curl /openapi.json`
  - `make smoke`
  - 正向 Pipeline E2E smoke
- 脚本支持通过环境变量覆盖每个命令，便于测试和故障定位：
  - `COMPOSE_PS_CMD`
  - `VERSION_CMD`
  - `HEALTHZ_CMD`
  - `OPENAPI_CMD`
  - `SMOKE_CMD`
  - `PIPELINE_E2E_CMD`
- `Makefile` 新增 `post-deploy-verify` 目标，调用 `scripts/post-deploy-verify.sh`。
- `scripts/post-deploy-verify.sh` 已设为可执行。
- 本轮仍未执行真实部署后验收，因为运行中容器尚未重建，`/version` 当前会 404。

验证命令：

- 本地红测：`go test ./internal/verify -run 'TestPostDeployVerifyRunsRuntimeGatesInOrder|TestMakefileExposesPostDeployVerifyTarget' -count=1 -v` 实现前失败于脚本和 Makefile target 缺失。
- 本地目标测试：`go test ./internal/verify -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地 compose 静态校验：`docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml config` 通过。
- 服务器目标测试：`docker run --rm --network host ... go test ./internal/verify -count=1 -v` 通过。
- 服务器 compose 静态校验：`docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml config` 通过。
- 服务器全量测试：`make test` 通过。
- 服务器默认 smoke：`make smoke` 通过。

下一步入口条件：

- 需要用户明确批准执行 `make prod-up`。
- 批准后执行顺序固定为：
  - `cd /opt/memory-os && make prod-up`
  - `cd /opt/memory-os && make post-deploy-verify`
- 如果 `post-deploy-verify` 的 Pipeline E2E 失败，必须根据其失败阶段继续查 `/memory/turn-event`、`archive_jobs`、`archive_index_jobs`、`archive_chunks` 和 Qdrant payload，不允许用默认 smoke 通过代替生产验收。

## 2026-07-02 Phase 1.29：Pipeline E2E 临时 Adapter Token 软撤销

完成事项：

- `auth.Repository` 增加 `RevokeAdapterToken(id, revokedAt)`。
- `auth.MemoryRepository` 与 `auth.PGRepository` 实现 Adapter Token 软撤销，只写 `revoked_at`，不物理删除数据。
- `auth.Service` 增加 `RevokeAdapterToken(id)`。
- `ValidateAdapterToken` 复用已有 token 校验逻辑，撤销后会拒绝继续使用。
- `cmd/memory-smoke` 的自动 PG 造数 Pipeline E2E 增加 cleanup：
  - 自动创建临时 Adapter Token 后保存 token id。
  - Pipeline E2E 结束时调用 cleanup 撤销该 token。
  - 手动传入 `SMOKE_ADAPTER_TOKEN` 时不自动撤销用户提供的 token。

验证命令：

- 本地红测：
  - `go test ./internal/auth -run 'TestAdapterTokenRevoke|TestPGRepositoryPasswordAndTokens' -count=1 -v` 实现前失败于缺少 `RevokeAdapterToken`。
  - `go test ./cmd/memory-smoke -run TestPipelineE2ESmokeCanProvisionActorFromPostgresDSN -count=1 -v` 实现前失败于 `pipelineE2EActor` 缺少 cleanup。
- 本地目标测试：`go test ./internal/auth ./cmd/memory-smoke -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 服务器目标测试：`docker run --rm --network host ... go test ./internal/auth ./cmd/memory-smoke -run 'TestAdapterTokenRevoke|TestPipelineE2ESmokeCanProvisionActorFromPostgresDSN' -count=1 -v` 通过。
- 服务器全量测试：`make test` 通过。
- 服务器默认 smoke：`make smoke` 通过。

剩余问题：

- 临时 user/org/project/membership 仍保留在 PostgreSQL 中，避免物理删除数据；后续可增加软禁用用户或标记测试资源的治理接口。
- 正向 Pipeline E2E 仍需在用户批准 `make prod-up` 后，通过 `make post-deploy-verify` 证明。

## 2026-07-02 Phase 1.30：Pipeline E2E cleanup 失败显式失败

完成事项：

- `cmd/memory-smoke` 的自动 PG 造数 Pipeline E2E 不再静默吞掉 cleanup 失败。
- 当自动 provision 的临时 Adapter Token 撤销失败时，`pipelineE2ESmoke` 会返回 `pipeline e2e actor cleanup failed` 错误。
- 修复 Go `:=` 在 `if` 块内遮蔽具名返回值 `err` 的问题，确保 defer 能写回最终返回错误。
- 主流程已有错误仍优先返回，不用 cleanup 错误覆盖原始故障，便于定位真正失败阶段。

验证命令：

- 本地红测：`go test ./cmd/memory-smoke -run TestPipelineE2ESmokeReturnsProvisionedActorCleanupError -count=1 -v` 实现前失败于 `pipelineE2ESmoke() error = nil`。
- 本地目标测试：`go test ./cmd/memory-smoke -run 'TestPipelineE2ESmokeCanProvisionActorFromPostgresDSN|TestPipelineE2ESmokeReturnsProvisionedActorCleanupError|TestPipelineE2ESmokeWritesTurnEventAndFindsArchiveChunk' -count=1 -v` 通过。
- 本地全量测试：`go test ./...` 通过。
- 服务器目标测试：`docker run --rm --network host ... go test ./cmd/memory-smoke -run 'TestPipelineE2ESmokeCanProvisionActorFromPostgresDSN|TestPipelineE2ESmokeReturnsProvisionedActorCleanupError|TestPipelineE2ESmokeWritesTurnEventAndFindsArchiveChunk' -count=1 -v` 通过。
- 服务器全量测试：`make test` 通过。
- 服务器默认 smoke：`make smoke` 通过。

剩余问题：

- 本轮仍未执行 `make prod-up`，因为这会重建并重启线上服务，需要用户明确批准。
- 运行中 API 容器此前已验证仍是旧镜像，`/version` 当前不可作为已部署证明；批准部署后必须执行 `make post-deploy-verify`。

## 2026-07-02 Phase 1.31：Post Deploy Pipeline E2E DSN 命令行暴露收敛

完成事项：

- `scripts/post-deploy-verify.sh` 保留 `PIPELINE_E2E_CMD` 覆盖能力，便于测试和故障定位。
- 默认 Pipeline E2E 路径改为 `run_pipeline_e2e` 函数执行，不再把 `SMOKE_POSTGRES_DSN` 拼入 `bash -lc` 命令字符串。
- PostgreSQL DSN 仍从运行中的 `deploy-memory-api-1` 容器读取，但只在子 shell 环境变量中传递给 `make smoke`。
- 新增静态测试，防止后续把 `SMOKE_POSTGRES_DSN` 重新内联到 `PIPELINE_E2E_CMD`。

验证命令：

- 本地红测：`go test ./internal/verify -run TestPostDeployVerifyDoesNotInlinePostgresDSNInDefaultCommand -count=1 -v` 实现前失败于脚本默认命令内联 `SMOKE_POSTGRES_DSN`。
- 本地目标测试：`go test ./internal/verify -run 'TestPostDeployVerifyRunsRuntimeGatesInOrder|TestPostDeployVerifyDoesNotInlinePostgresDSNInDefaultCommand|TestMakefileExposesPostDeployVerifyTarget' -count=1 -v` 通过。
- 本地脚本语法检查：`bash -n scripts/post-deploy-verify.sh` 通过。
- 本地全量测试：`go test ./...` 通过。
- 服务器目标测试：`docker run --rm --network host ... go test ./internal/verify -run 'TestPostDeployVerifyRunsRuntimeGatesInOrder|TestPostDeployVerifyDoesNotInlinePostgresDSNInDefaultCommand|TestMakefileExposesPostDeployVerifyTarget' -count=1 -v` 通过。
- 服务器脚本语法检查：`bash -n scripts/post-deploy-verify.sh` 通过。
- 服务器全量测试：`make test` 通过。
- 服务器默认 smoke：`make smoke` 通过。

剩余问题：

- DSN 仍会作为 `make smoke` 子进程环境变量存在，这是当前 smoke 连接 PostgreSQL 的必要输入；本轮已消除更高风险的命令字符串暴露。
- 本轮未执行 `make post-deploy-verify`，因为线上容器尚未经过用户批准的 `make prod-up` 重建，当前 `/version` 仍不是最新镜像证明。

## 2026-07-02 Phase 1.32：Post Deploy 验收日志目录安全化

完成事项：

- `scripts/post-deploy-verify.sh` 不再写入可预测固定路径 `/tmp/memory-os-post-deploy-*.log`。
- 新增 `LOG_DIR` 支持；未指定时默认使用 `mktemp -d /tmp/memory-os-post-deploy.XXXXXX` 创建本次验收专属目录。
- 脚本设置 `umask 077`，降低验收日志被其它本机用户读取的风险。
- 每个验收步骤写入 `LOG_DIR/<step>.log`，并在完成时输出日志目录，便于失败后定位。
- 新增测试覆盖：
  - 指定 `LOG_DIR` 时所有步骤日志必须写入该目录。
  - 脚本不得回退到固定 `/tmp/memory-os-post-deploy-` 文件路径。

验证命令：

- 本地红测：`go test ./internal/verify -run 'TestPostDeployVerifyUsesDedicatedLogDir|TestPostDeployVerifyDoesNotUseFixedTmpLogPath' -count=1 -v` 实现前失败于缺少专属 `LOG_DIR` 日志和存在固定 `/tmp` 路径。
- 本地目标测试：`go test ./internal/verify -run 'TestPostDeployVerifyRunsRuntimeGatesInOrder|TestPostDeployVerifyDoesNotInlinePostgresDSNInDefaultCommand|TestPostDeployVerifyUsesDedicatedLogDir|TestPostDeployVerifyDoesNotUseFixedTmpLogPath|TestMakefileExposesPostDeployVerifyTarget' -count=1 -v` 通过。
- 本地脚本语法检查：`bash -n scripts/post-deploy-verify.sh` 通过。
- 本地全量测试：`go test ./...` 通过。
- 服务器目标测试：`docker run --rm --network host ... go test ./internal/verify -run 'TestPostDeployVerifyRunsRuntimeGatesInOrder|TestPostDeployVerifyDoesNotInlinePostgresDSNInDefaultCommand|TestPostDeployVerifyUsesDedicatedLogDir|TestPostDeployVerifyDoesNotUseFixedTmpLogPath|TestMakefileExposesPostDeployVerifyTarget' -count=1 -v` 通过。
- 服务器脚本语法检查：`bash -n scripts/post-deploy-verify.sh` 通过。
- 服务器默认 smoke：`make smoke` 通过。
- 服务器全量测试：`make test` 通过。

剩余问题：

- 本轮未执行 `make post-deploy-verify`，因为线上容器仍需先经用户批准执行 `make prod-up` 重建。
- 生产部署后应保存 `post deploy verify logs: <LOG_DIR>` 输出路径，作为最终交付报告证据之一。

## 2026-07-02 Phase 1.33：Post Deploy 失败路径日志可诊断

完成事项：

- `scripts/post-deploy-verify.sh` 在任一验收步骤失败时，也会输出 `post deploy verify logs: <LOG_DIR>`。
- 放弃依赖 shell `ERR` trap，改为每个步骤显式检查退出码；这能覆盖函数调用与重定向场景，行为更稳定。
- `run_step`、`PIPELINE_E2E_CMD` 覆盖路径、默认 Pipeline E2E 路径均在失败时先报告日志目录再返回失败。
- 新增失败路径测试，模拟 `healthz` 失败并验证：
  - 脚本返回失败。
  - 输出包含 `LOG_DIR`。
  - 失败步骤日志仍写入 `LOG_DIR/healthz.log`。

验证命令：

- 本地红测：`go test ./internal/verify -run TestPostDeployVerifyPrintsLogDirOnFailure -count=1 -v` 实现前失败于输出缺少日志目录。
- 本地目标测试：`go test ./internal/verify -run 'TestPostDeployVerifyRunsRuntimeGatesInOrder|TestPostDeployVerifyDoesNotInlinePostgresDSNInDefaultCommand|TestPostDeployVerifyUsesDedicatedLogDir|TestPostDeployVerifyPrintsLogDirOnFailure|TestPostDeployVerifyDoesNotUseFixedTmpLogPath|TestMakefileExposesPostDeployVerifyTarget' -count=1 -v` 通过。
- 本地脚本语法检查：`bash -n scripts/post-deploy-verify.sh` 通过。
- 本地全量测试：`go test ./...` 通过。
- 服务器目标测试：`docker run --rm --network host ... go test ./internal/verify -run 'TestPostDeployVerifyRunsRuntimeGatesInOrder|TestPostDeployVerifyDoesNotInlinePostgresDSNInDefaultCommand|TestPostDeployVerifyUsesDedicatedLogDir|TestPostDeployVerifyPrintsLogDirOnFailure|TestPostDeployVerifyDoesNotUseFixedTmpLogPath|TestMakefileExposesPostDeployVerifyTarget' -count=1 -v` 通过。
- 服务器脚本语法检查：`bash -n scripts/post-deploy-verify.sh` 通过。
- 服务器默认 smoke：`make smoke` 通过。
- 服务器全量测试：`make test` 通过。

剩余问题：

- 仍未执行 `make prod-up` 和真实 `make post-deploy-verify`；等待用户明确批准生产重建/重启。
- 批准后如果任一步骤失败，最终交付报告必须引用脚本输出的 `LOG_DIR` 并检查对应步骤日志。

## 2026-07-02 Phase 1.34：Post Deploy stderr 输出收敛到日志

完成事项：

- `scripts/post-deploy-verify.sh` 的每个验收步骤不再只捕获 stdout。
- 普通步骤、`PIPELINE_E2E_CMD` 覆盖路径、默认 Pipeline E2E 路径均改为 stdout/stderr 一起写入 `LOG_DIR/<step>.log`。
- 终端输出只保留步骤名、日志目录和完成状态，避免失败命令的 stderr 直接刷屏并潜在暴露敏感诊断内容。
- 新增失败路径测试，模拟 `healthz` 往 stderr 输出标记后失败，并验证：
  - 终端输出不包含该 stderr 标记。
  - `LOG_DIR/healthz.log` 包含该 stderr 标记，便于排障。

验证命令：

- 本地红测：`go test ./internal/verify -run TestPostDeployVerifyCapturesStderrInStepLog -count=1 -v` 实现前失败于 stderr 直出终端。
- 本地目标测试：`go test ./internal/verify -run 'TestPostDeployVerifyRunsRuntimeGatesInOrder|TestPostDeployVerifyDoesNotInlinePostgresDSNInDefaultCommand|TestPostDeployVerifyUsesDedicatedLogDir|TestPostDeployVerifyPrintsLogDirOnFailure|TestPostDeployVerifyCapturesStderrInStepLog|TestPostDeployVerifyDoesNotUseFixedTmpLogPath|TestMakefileExposesPostDeployVerifyTarget' -count=1 -v` 通过。
- 本地脚本语法检查：`bash -n scripts/post-deploy-verify.sh` 通过。
- 本地全量测试：`go test ./...` 通过。
- 服务器目标测试：`docker run --rm --network host ... go test ./internal/verify -run 'TestPostDeployVerifyRunsRuntimeGatesInOrder|TestPostDeployVerifyDoesNotInlinePostgresDSNInDefaultCommand|TestPostDeployVerifyUsesDedicatedLogDir|TestPostDeployVerifyPrintsLogDirOnFailure|TestPostDeployVerifyCapturesStderrInStepLog|TestPostDeployVerifyDoesNotUseFixedTmpLogPath|TestMakefileExposesPostDeployVerifyTarget' -count=1 -v` 通过。
- 服务器脚本语法检查：`bash -n scripts/post-deploy-verify.sh` 通过。
- 服务器默认 smoke：`make smoke` 通过。
- 服务器全量测试：`make test` 通过。

剩余问题：

- 仍未执行 `make prod-up` 和真实 `make post-deploy-verify`；等待用户明确批准生产重建/重启。
- 最终生产验收失败时，终端不会直接显示失败命令 stderr，必须查看脚本输出的 `LOG_DIR/<step>.log`。

## 2026-07-02 Phase 1.35：Post Deploy LOG_DIR 权限收紧

完成事项：

- `scripts/post-deploy-verify.sh` 在创建或使用 `LOG_DIR` 后显式执行 `chmod 700 "$LOG_DIR"`。
- 这补齐了用户显式传入已有 `LOG_DIR` 时的权限边界；即使调用者预先创建了 `0755` 目录，脚本也会收紧到仅当前用户可读写执行。
- 继续保留 `umask 077`，用于约束后续新建日志文件权限。
- 新增测试覆盖：
  - 预先创建 `0755` 的 `LOG_DIR`。
  - 运行 `post-deploy-verify.sh` 后校验目录权限变为 `0700`。

验证命令：

- 本地红测：`go test ./internal/verify -run TestPostDeployVerifyRestrictsExistingLogDirPermissions -count=1 -v` 实现前失败于 `LOG_DIR mode = 755, want 700`。
- 本地目标测试：`go test ./internal/verify -run 'TestPostDeployVerifyRunsRuntimeGatesInOrder|TestPostDeployVerifyDoesNotInlinePostgresDSNInDefaultCommand|TestPostDeployVerifyUsesDedicatedLogDir|TestPostDeployVerifyRestrictsExistingLogDirPermissions|TestPostDeployVerifyPrintsLogDirOnFailure|TestPostDeployVerifyCapturesStderrInStepLog|TestPostDeployVerifyDoesNotUseFixedTmpLogPath|TestMakefileExposesPostDeployVerifyTarget' -count=1 -v` 通过。
- 本地脚本语法检查：`bash -n scripts/post-deploy-verify.sh` 通过。
- 本地全量测试：`go test ./...` 通过。
- 服务器目标测试：`docker run --rm --network host ... go test ./internal/verify -run 'TestPostDeployVerifyRunsRuntimeGatesInOrder|TestPostDeployVerifyDoesNotInlinePostgresDSNInDefaultCommand|TestPostDeployVerifyUsesDedicatedLogDir|TestPostDeployVerifyRestrictsExistingLogDirPermissions|TestPostDeployVerifyPrintsLogDirOnFailure|TestPostDeployVerifyCapturesStderrInStepLog|TestPostDeployVerifyDoesNotUseFixedTmpLogPath|TestMakefileExposesPostDeployVerifyTarget' -count=1 -v` 通过。
- 服务器脚本语法检查：`bash -n scripts/post-deploy-verify.sh` 通过。
- 服务器默认 smoke：`make smoke` 通过。
- 服务器全量测试：`make test` 通过。

剩余问题：

- 仍未执行 `make prod-up` 和真实 `make post-deploy-verify`；等待用户明确批准生产重建/重启。
- 最终生产验收报告需要记录 `LOG_DIR` 路径，并确认日志目录权限为 `0700`。

## 2026-07-02 Phase 1.36：Post Deploy LOG_DIR symlink 防护

完成事项：

- `scripts/post-deploy-verify.sh` 在创建或使用 `LOG_DIR` 前检查 `[[ -L "$LOG_DIR" ]]`。
- 如果 `LOG_DIR` 是符号链接，脚本直接失败并输出 `post deploy verify failed: LOG_DIR must not be a symlink`。
- 防止验收日志通过 symlink 写入非预期目录，也避免 `chmod 700 "$LOG_DIR"` 跟随 symlink 修改目标目录权限。
- 新增测试覆盖：
  - 构造 `LOG_DIR` 为 symlink。
  - 验证脚本拒绝执行并给出明确错误。
  - 验证没有通过 symlink 目标写入 `compose-ps.log`。

验证命令：

- 本地红测：`go test ./internal/verify -run TestPostDeployVerifyRejectsSymlinkLogDir -count=1 -v` 实现前失败于脚本接受 symlink LOG_DIR。
- 本地目标测试：`go test ./internal/verify -run 'TestPostDeployVerifyRunsRuntimeGatesInOrder|TestPostDeployVerifyDoesNotInlinePostgresDSNInDefaultCommand|TestPostDeployVerifyUsesDedicatedLogDir|TestPostDeployVerifyRestrictsExistingLogDirPermissions|TestPostDeployVerifyRejectsSymlinkLogDir|TestPostDeployVerifyPrintsLogDirOnFailure|TestPostDeployVerifyCapturesStderrInStepLog|TestPostDeployVerifyDoesNotUseFixedTmpLogPath|TestMakefileExposesPostDeployVerifyTarget' -count=1 -v` 通过。
- 本地脚本语法检查：`bash -n scripts/post-deploy-verify.sh` 通过。
- 本地全量测试：`go test ./...` 通过。
- 服务器目标测试：`docker run --rm --network host ... go test ./internal/verify -run 'TestPostDeployVerifyRunsRuntimeGatesInOrder|TestPostDeployVerifyDoesNotInlinePostgresDSNInDefaultCommand|TestPostDeployVerifyUsesDedicatedLogDir|TestPostDeployVerifyRestrictsExistingLogDirPermissions|TestPostDeployVerifyRejectsSymlinkLogDir|TestPostDeployVerifyPrintsLogDirOnFailure|TestPostDeployVerifyCapturesStderrInStepLog|TestPostDeployVerifyDoesNotUseFixedTmpLogPath|TestMakefileExposesPostDeployVerifyTarget' -count=1 -v` 通过。
- 服务器脚本语法检查：`bash -n scripts/post-deploy-verify.sh` 通过。
- 服务器默认 smoke：`make smoke` 通过。
- 服务器全量测试：`make test` 通过。

剩余问题：

- 仍未执行 `make prod-up` 和真实 `make post-deploy-verify`；等待用户明确批准生产重建/重启。
- 最终生产验收报告需要记录 `LOG_DIR` 路径，并确认该路径不是 symlink。

## 2026-07-02 Phase 1.37：Production API 缺少 PostgreSQL 时禁止启动

完成事项：

- `cmd/memory-api` 的 `buildServer` 增加 production 启动门禁。
- 当 `AppEnv == "production"` 且 `PostgresDSN` 为空时，API 直接返回 `postgres dsn is required in production`。
- 该门禁防止生产环境在没有 PostgreSQL 的情况下启动成“无 Auth/Tenant/Retrieval/EventLog/ArchiveQueue 注入”的半功能状态。
- development/default 模式仍保留无 PostgreSQL 启动能力，供轻量本地测试和 dev smoke 使用。
- 新增测试 `TestBuildServerRejectsMissingPostgresDSNInProduction`。

验证命令：

- 本地红测：`go test ./cmd/memory-api -run TestBuildServerRejectsMissingPostgresDSNInProduction -count=1 -v` 实现前失败于 production 缺 DSN 仍可启动。
- 本地目标测试：`go test ./cmd/memory-api -run 'TestBuildServer|TestBuildServerRejectsMissingAPIAddr|TestBuildServerRejectsMissingPostgresDSNInProduction|TestRouterOptionsConfiguresCoreServicesWhenPostgresPoolExists|TestRouterOptionsLeavesAuthOpenForDevelopmentSmoke|TestRouterOptionsReturnsArchiveRAGConfigurationError' -count=1 -v` 通过。
- 本地全量测试：`go test ./...` 通过。
- 服务器默认 smoke：`make smoke` 通过。
- 服务器全量测试：`make test` 通过，并覆盖 `cmd/memory-api`。

验证备注：

- 服务器单独 docker 目标测试命令运行超过 4 分钟且无完整结果，被手动终止；该命令不作为通过证据。
- 服务器 `make test` 完整通过，作为本轮服务器侧权威验证证据。

剩余问题：

- 仍未执行 `make prod-up` 和真实 `make post-deploy-verify`；等待用户明确批准生产重建/重启。
- 批准部署后，如果生产容器缺少 `POSTGRES_DSN`，API 应启动失败而不是降级为半功能服务。

## 2026-07-02 Phase 1.38：Production Worker 缺少 PostgreSQL 时禁止启动

完成事项：

- `cmd/memory-worker` 的 `buildWorker` 增加 production 启动门禁。
- 当 `AppEnv == "production"` 且 `PostgresDSN` 为空时，worker 直接返回 `postgres dsn is required in production`。
- 该门禁防止生产 worker 在没有 PostgreSQL 的情况下启动成“无 ArchiveQueue、无 ArchiveWorker、无 RAGIndexQueue、无 RAGIndexWorker”的空 runner。
- development/default 模式仍保留无 PostgreSQL 启动能力，供轻量本地测试使用。
- 新增测试 `TestBuildWorkerRejectsMissingPostgresDSNInProduction`。

验证命令：

- 本地红测：`go test ./cmd/memory-worker -run TestBuildWorkerRejectsMissingPostgresDSNInProduction -count=1 -v` 实现前失败于 production 缺 DSN 仍可返回空 runner。
- 本地目标测试：`go test ./cmd/memory-worker -run 'TestBuildWorker|TestBuildWorkerRejectsMissingPostgresDSNInProduction|TestWorkerLoggerOptionsUsesConfiguredEnvironment|TestBuildWorkerRunsMigrationsWhenPostgresDSNExists' -count=1 -v` 通过。
- 本地全量测试：`go test ./...` 通过。
- 服务器默认 smoke：`make smoke` 通过。
- 服务器全量测试：`make test` 通过，并覆盖 `cmd/memory-worker`。

剩余问题：

- 仍未执行 `make prod-up` 和真实 `make post-deploy-verify`；等待用户明确批准生产重建/重启。
- 批准部署后，如果生产 worker 容器缺少 `POSTGRES_DSN`，worker 应启动失败而不是空跑。

## 2026-07-02 Phase 1.39：Production Compose 必须显式提供 PostgreSQL 密码

完成事项：

- `deploy/docker-compose.yml` 移除 `POSTGRES_PASSWORD:-replace-me-local-only` 默认值。
- PostgreSQL 容器、`memory-api` 的 `POSTGRES_DSN`、`memory-worker` 的 `POSTGRES_DSN` 均改为 `${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}`。
- 防止生产 `make prod-up` 在调用者未显式提供 PostgreSQL 密码时静默使用本地占位密码。
- `Makefile dev-up` 保留 `POSTGRES_PASSWORD=${POSTGRES_PASSWORD:-replace-me-local-only}` 的本地开发兜底，避免安全收紧误伤开发启动路径。
- 新增测试覆盖：
  - `TestComposeRequiresExplicitPostgresPassword`：生产 compose 不允许出现 `replace-me-local-only`，且必须使用 required env marker。
  - `TestMakefileDevUpProvidesLocalOnlyPostgresPassword`：仅 `dev-up` 允许本地占位密码兜底，`prod-up` 不允许兜底。

验证命令：

- 本地红测：`go test ./internal/webdeploy -run 'Test(ComposeRequiresExplicitPostgresPassword|MakefileDevUpProvidesLocalOnlyPostgresPassword)' -count=1 -v` 实现前失败于 compose 仍默认 `replace-me-local-only`，且 `dev-up` 未显式提供本地兜底。
- 本地目标测试：`go test ./internal/webdeploy -run 'Test(ComposeRequiresExplicitPostgresPassword|MakefileDevUpProvidesLocalOnlyPostgresPassword)' -count=1 -v` 通过。
- 本地 compose 静态校验：`POSTGRES_PASSWORD=test-password docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml config` 通过。
- 本地全量测试：`go test ./...` 通过。
- 服务器直接 `go test` 备注：`ssh thinkpad "cd /opt/memory-os && go test ..."` 失败于 `go: command not found`，属于服务器环境缺少直接 Go 命令，不是代码失败；后续使用项目标准 Makefile/Docker fallback 验证。
- 服务器 compose 静态校验：`POSTGRES_PASSWORD=test-password docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml config` 通过。
- 服务器默认 smoke：`make smoke` 通过，输出 `smoke ok`。
- 服务器全量测试：`make test` 通过，并覆盖 `internal/webdeploy`。

剩余问题：

- 仍未执行 `make prod-up` 和真实 `make post-deploy-verify`；等待用户明确批准生产重建/重启。
- 批准部署前必须在服务器环境中显式提供真实 `POSTGRES_PASSWORD`，不能依赖 compose 默认值。

## 2026-07-02 Phase 1.40：Production 启动禁止半功能配置并完成服务器 post-deploy

完成事项：

- `cmd/memory-api` production 启动门禁扩展为必须显式配置 PostgreSQL、Redis、Qdrant 和 LLM embedding 配置。
- `cmd/memory-worker` production 启动门禁扩展为必须显式配置 PostgreSQL、Qdrant、ArchiveDir 和 LLM embedding 配置。
- API/Worker production 均拒绝占位 LLM 配置：
  - `LLM_API_KEY=replace-me`
  - `LLM_BASE_URL=http://example.local:8000`
- `deploy/docker-compose.yml` 移除运行容器的 `.env.example` 注入，避免样例配置进入生产容器。
- `deploy/docker-compose.yml` 为 API/Worker 显式要求 `LLM_BASE_URL`、`LLM_API_KEY`，并保留模型名默认值。
- 后端 Dockerfile 和 compose build args 增加 `GOPROXY`、`NO_PROXY`，修复服务器 `go mod download` 在生产重建中卡住的问题。
- 运行时 `NO_PROXY/no_proxy` 改为可通过环境变量扩展，支持内部 LLM provider 或测试 mock provider 不走代理。
- `scripts/post-deploy-verify.sh` 的 pipeline e2e 在 compose 网络中显式设置：
  - `SMOKE_WEB_URL=http://memory-web:18080`
  - `SMOKE_API_URL=http://memory-api:18081/healthz`
  - `SMOKE_QDRANT_URL=http://qdrant:6333`
- `Makefile smoke` Docker fallback 增加 `SMOKE_WEB_URL` 透传。

新增或变更测试：

- `TestBuildServerRejectsMissingRedisAddrInProduction`
- `TestBuildServerRejectsMissingQdrantURLInProduction`
- `TestBuildServerRejectsPlaceholderLLMConfigInProduction`
- `TestBuildWorkerRejectsMissingQdrantURLInProduction`
- `TestBuildWorkerRejectsMissingArchiveDirInProduction`
- `TestBuildWorkerRejectsPlaceholderLLMConfigInProduction`
- `TestBackendDockerfilesConfigureGoModuleProxy`
- `TestComposePassesBackendBuildProxyArgs`
- `TestComposeDoesNotInjectExampleEnvFileInProduction`
- `TestComposeRequiresLLMConfigForAPIAndWorker`
- `TestComposeRuntimeNoProxyCanBeExtended`
- `TestMakeSmokePassesStrictPipelineEnvironmentToDocker` 增补 `SMOKE_WEB_URL`
- `TestPostDeployVerifySetsWebURLForComposeNetworkPipeline`

验证命令：

- 本地红测：
  - `go test ./cmd/memory-api -run TestBuildServerRejectsPlaceholderLLMConfigInProduction -count=1 -v` 实现前失败于 `errInvalidProductionLLMConfig` 未定义。
  - `go test ./cmd/memory-worker -run TestBuildWorkerRejectsPlaceholderLLMConfigInProduction -count=1 -v` 实现前失败于 `errInvalidProductionLLMConfig` 未定义。
  - `go test ./internal/webdeploy -run 'TestCompose(DoesNotInjectExampleEnvFileInProduction|RequiresLLMConfigForAPIAndWorker)' -count=1 -v` 实现前失败于 compose 注入 `.env.example` 且未显式要求 LLM 配置。
  - `go test ./internal/webdeploy -run TestComposeRuntimeNoProxyCanBeExtended -count=1 -v` 实现前失败于运行时 `no_proxy` 不可扩展。
  - `go test ./cmd/memory-smoke -run TestMakeSmokePassesStrictPipelineEnvironmentToDocker -count=1 -v` 实现前失败于未透传 `SMOKE_WEB_URL`。
  - `go test ./internal/verify -run TestPostDeployVerifySetsWebURLForComposeNetworkPipeline -count=1 -v` 实现前失败于 post-deploy 未设置 compose 网络内 Web URL。
- 本地目标测试：
  - `go test ./cmd/memory-api -run 'TestBuildServerRejectsPlaceholderLLMConfigInProduction|TestBuildServerRejectsMissing(PostgresDSN|RedisAddr|QdrantURL)InProduction' -count=1 -v` 通过。
  - `go test ./cmd/memory-worker -run 'TestBuildWorkerRejectsPlaceholderLLMConfigInProduction|TestBuildWorkerRejectsMissing(PostgresDSN|QdrantURL|ArchiveDir)InProduction' -count=1 -v` 通过。
  - `go test ./internal/webdeploy -run 'TestCompose(DoesNotInjectExampleEnvFileInProduction|RequiresLLMConfigForAPIAndWorker|PassesBackendBuildProxyArgs|RuntimeNoProxyCanBeExtended)' -count=1 -v` 通过。
  - `go test ./cmd/memory-smoke -run TestMakeSmokePassesStrictPipelineEnvironmentToDocker -count=1 -v` 通过。
  - `go test ./internal/verify -run 'TestPostDeployVerifySetsWebURLForComposeNetworkPipeline|TestPostDeployVerifyRunsRuntimeGatesInOrder' -count=1 -v` 通过。
  - `bash -n scripts/post-deploy-verify.sh` 通过。
- 本地 compose 静态校验：`POSTGRES_PASSWORD=test-password LLM_BASE_URL=http://llm.local:8000 LLM_API_KEY=test-key NO_PROXY=localhost,127.0.0.1,postgres,redis,qdrant,memory-api,memory-web,memory-mcp,memory-llm-mock docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml config` 通过。
- 本地全量测试：`go test ./...` 通过。
- 服务器全量测试：`make test` 通过。
- 服务器生产重建：`make prod-up` 通过。
- 服务器基础验收：
  - `curl http://127.0.0.1:18081/healthz` 返回 `db`、`qdrant`、`redis` 全部 `ok`。
  - `curl http://127.0.0.1:18081/version` 返回 `0.4.0-dev`。
  - `curl http://127.0.0.1:18081/openapi.json` 可访问。
- 服务器 post-deploy 验收：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.OzYlcr`。

部署说明：

- 本轮服务器 post-deploy 使用临时 `memory-llm-mock` 容器作为 OpenAI-compatible provider，用于稳定验证 embedding/rerank 兼容链路。
- 该 mock provider 使用测试 key，不保存真实 secret，不作为最终生产模型供应商。
- 最终生产交付仍必须替换为真实 `LLM_BASE_URL` 和 `LLM_API_KEY`，并不得使用 `.env.example` 占位值。

剩余问题：

- Phase 1 仍未完全收口，后续还需要继续推进 repository contract、权限上下文、真实 UI API 化等 Phase 2+ 内容。
- 当前 post-deploy 通过证明部署链路和 RAG pipeline 在 mock provider 下可运行；真实 provider 的稳定性和凭据注入仍需在最终生产验收阶段单独证明。

## 2026-07-02 Phase 1.41：Production API 启动路径注入 Audit PG Service

完成事项：

- `internal/audit.Service` 增加 `Configured()`，用于启动注入和路由测试判断审计服务是否已绑定 repository。
- `internal/http.RouterOptions` 增加 `AuditService audit.Service` 字段，纳入生产路由选项。
- `cmd/memory-api` 的 production `routerOptions` 在 PostgreSQL pool 存在且非 development smoke 模式时，使用 `audit.NewPGRepository(pool)` 注入 `AuditService`。
- development smoke 模式继续保持开放轻量路径，不注入生产审计服务，避免本地 smoke 被生产依赖阻断。

新增或变更测试：

- `TestRouterOptionsConfiguresCoreServicesWhenPostgresPoolExists` 增加 `AuditService.Configured()` 断言。
- `TestRouterOptionsLeavesAuthOpenForDevelopmentSmoke` 增加 development smoke 不应配置 `AuditService` 的断言。

验证命令：

- 本地红测：`go test ./cmd/memory-api -run 'TestRouterOptions(ConfiguresCoreServicesWhenPostgresPoolExists|LeavesAuthOpenForDevelopmentSmoke)' -count=1 -v` 实现前失败于 `options.AuditService undefined`。
- 本地目标测试：`go test ./cmd/memory-api -run 'TestRouterOptions(ConfiguresCoreServicesWhenPostgresPoolExists|LeavesAuthOpenForDevelopmentSmoke)' -count=1 -v` 通过。
- 本地审计包测试：`go test ./internal/audit -count=1 -v` 通过；其中 `TestPGRepositorySavePersistsAuditLog` 因 `POSTGRES_TEST_DSN` 未设置按设计跳过。
- 本地全量测试：`go test ./...` 通过。
- 服务器全量测试：`make test` 通过。
- 服务器 smoke：`make smoke` 通过，输出 `smoke ok`。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp` 容器。
- 服务器健康检查：`curl http://127.0.0.1:18081/healthz` 返回 `db`、`qdrant`、`redis` 全部 `ok`。
- 服务器 post-deploy 验收：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.Z45i0a`。

部署说明：

- 本轮服务器部署继续使用临时 `memory-llm-mock` 作为 OpenAI-compatible provider 验证 pipeline，不写入真实 secret。
- 当前线上容器已运行本轮新镜像，API/Worker/MCP 均已重建启动。

剩余问题：

- `AuditService` 已接入生产启动选项，但管理 API 对审计服务的深度使用仍需在后续权限、Secret、Archive 等功能闭环中继续补齐。
- Phase 1 的 PG repository 基座仍需继续逐项收口，不能因此声明 v0.4 完成。
- 最终生产交付仍需要替换真实 LLM provider，并完成 Secret、权限隔离、浏览器验收和备份恢复验收。

## 2026-07-02 Phase 1.42：Secret Vault 生产配置门禁与 PG 注入

完成事项：

- `internal/config.Config` 增加 `SecretVaultKeyID` 和解码后的 `SecretVaultKey`。
- `config.Load()` 支持读取 `SECRET_VAULT_KEY_ID`、`SECRET_VAULT_KEY_B64`，并拒绝非法 base64 key。
- `cmd/memory-api` production 启动门禁增加 Secret Vault 配置校验，缺 key id 或 key 长度不是 AES 支持的 16/24/32 字节时拒绝启动。
- `internal/secret.Vault` 增加 `Configured()`，用于生产注入测试判断。
- `internal/http.RouterOptions` 增加 `SecretVault secret.Vault`。
- `cmd/memory-api` 的 production `routerOptions` 使用 `secret.NewPGRepository(pool)` 和 `secret.NewAESGCMCodec(...)` 注入 Secret Vault。
- `deploy/docker-compose.yml` 要求 memory-api 显式提供 `SECRET_VAULT_KEY_ID`、`SECRET_VAULT_KEY_B64`。
- `.env.example` 增加 Secret Vault 配置占位说明；生产 compose 不读取该样例文件。

新增或变更测试：

- `TestLoadReadsSecretVaultKey`
- `TestLoadRejectsInvalidSecretVaultKey`
- `TestBuildServerRejectsMissingSecretVaultConfigInProduction`
- `TestRouterOptionsConfiguresCoreServicesWhenPostgresPoolExists` 增加 `SecretVault.Configured()` 断言。
- `TestRouterOptionsLeavesAuthOpenForDevelopmentSmoke` 增加 development smoke 不应配置 `SecretVault` 的断言。
- `TestComposeRequiresSecretVaultConfigForAPI`

验证命令：

- 本地红测：
  - `go test ./internal/config -run 'TestLoad(ReadsSecretVaultKey|RejectsInvalidSecretVaultKey)' -count=1 -v` 实现前失败于 `Config` 缺少 `SecretVaultKeyID` / `SecretVaultKey`。
  - `go test ./cmd/memory-api -run 'Test(BuildServerRejectsMissingSecretVaultConfigInProduction|RouterOptionsConfiguresCoreServicesWhenPostgresPoolExists|RouterOptionsLeavesAuthOpenForDevelopmentSmoke)' -count=1 -v` 实现前失败于缺少 `errInvalidProductionSecretVaultConfig` 和 `RouterOptions.SecretVault`。
  - `go test ./internal/webdeploy -run TestComposeRequiresSecretVaultConfigForAPI -count=1 -v` 实现前失败于 compose 未要求 Secret Vault 配置。
- 本地目标测试：
  - `go test ./internal/config -run 'TestLoad(ReadsSecretVaultKey|RejectsInvalidSecretVaultKey)' -count=1 -v` 通过。
  - `go test ./cmd/memory-api -run 'Test(BuildServerRejectsMissingSecretVaultConfigInProduction|RouterOptionsConfiguresCoreServicesWhenPostgresPoolExists|RouterOptionsLeavesAuthOpenForDevelopmentSmoke)' -count=1 -v` 通过。
  - `go test ./internal/webdeploy -run TestComposeRequiresSecretVaultConfigForAPI -count=1 -v` 通过。
- 本地 compose 静态校验：带 `SECRET_VAULT_KEY_ID` 和非打印测试 `SECRET_VAULT_KEY_B64` 的 `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml config` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器全量测试：`make test` 通过。
- 服务器 smoke：`make smoke` 通过，输出 `smoke ok`。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp` 容器。
- 服务器健康检查：`curl http://127.0.0.1:18081/healthz` 返回 `db`、`qdrant`、`redis` 全部 `ok`。
- 服务器 post-deploy 验收：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.LGVDho`。

部署说明：

- 本轮服务器部署使用命令内临时生成的 Secret Vault 测试 key，只用于验证生产门禁和容器启动，不在回复、代码、日志或配置文件中写入明文 key。
- 该临时 key 不是最终生产 Secret Vault 主密钥；真正生产交付前必须改为服务器持久、安全注入的稳定密钥，否则后续 Secret Vault 已加密数据在换 key 后不可解密。
- 本轮仍使用临时 `memory-llm-mock` 作为 OpenAI-compatible provider 验证 pipeline，不作为最终生产模型供应商。

剩余问题：

- Secret Vault 已具备生产启动配置和 PG 注入基础，但还没有完整管理 API / UI 操作闭环，不能声明 Phase 3 完成。
- 最终生产密钥的持久化注入方案仍需单独落地，不能依赖每次部署临时生成。
- Phase 1 仍需继续审查 Archive PG service 注入、repository contract 覆盖和生产 API 路由真实化。

## 2026-07-03 Phase 1.43：Production API 启动路径注入 Archive PG Service

完成事项：

- `internal/archive.Service` 增加 `Configured()`，用于启动注入和路由测试判断 Archive service 是否已绑定 repository 和正文根目录。
- `internal/http.RouterOptions` 增加 `ArchiveService archive.Service`。
- `cmd/memory-api` production 启动门禁增加 `ArchiveDir` 校验，避免生产 API 在没有 Markdown Archive 正文根目录时启动成半功能状态。
- `cmd/memory-api` 的 production `routerOptions` 使用 `archive.NewPGRepository(pool)` 和 `cfg.ArchiveDir` 注入 `ArchiveService`。
- development smoke 模式继续不注入生产 Archive service，避免本地 smoke 被生产依赖阻断。

新增或变更测试：

- `TestBuildServerRejectsMissingArchiveDirInProduction`
- `TestRouterOptionsConfiguresCoreServicesWhenPostgresPoolExists` 增加 `ArchiveService.Configured()` 断言。
- `TestRouterOptionsLeavesAuthOpenForDevelopmentSmoke` 增加 development smoke 不应配置 `ArchiveService` 的断言。

验证命令：

- 本地红测：`go test ./cmd/memory-api -run 'Test(BuildServerRejectsMissingArchiveDirInProduction|RouterOptionsConfiguresCoreServicesWhenPostgresPoolExists|RouterOptionsLeavesAuthOpenForDevelopmentSmoke)' -count=1 -v` 实现前失败于 `errMissingProductionArchiveDir` 和 `RouterOptions.ArchiveService` 未定义。
- 本地目标测试：`go test ./cmd/memory-api -run 'Test(BuildServerRejectsMissingArchiveDirInProduction|RouterOptionsConfiguresCoreServicesWhenPostgresPoolExists|RouterOptionsLeavesAuthOpenForDevelopmentSmoke)' -count=1 -v` 通过。
- 本地 Archive 包测试：`go test ./internal/archive -count=1 -v` 通过；其中 PG 集成测试因 `POSTGRES_TEST_DSN` 未设置按设计跳过。
- 本地 API 包测试：`go test ./cmd/memory-api -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器全量测试：`make test` 通过。
- 服务器 smoke：`make smoke` 通过，输出 `smoke ok`。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp` 容器。
- 服务器健康检查：`curl http://127.0.0.1:18081/healthz` 返回 `db`、`qdrant`、`redis` 全部 `ok`。
- 服务器 post-deploy 验收：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.gB0Oos`。

部署说明：

- 本轮继续复用运行容器当前 Secret Vault 测试 key 作为 compose 解析和 API 启动环境，不在回复、代码或日志中输出明文 key。
- 本轮仍使用临时 `memory-llm-mock` 作为 OpenAI-compatible provider 验证 pipeline，不作为最终生产模型供应商。

剩余问题：

- ArchiveService 已进入 production API 启动选项，但 Archive 列表、详情、编辑、版本、删除、重建索引等生产管理 API/UI 仍未完整闭环。
- Markdown Archive 正文目录已有生产门禁和 volume，但还需要后续浏览器验收真实创建、编辑、刷新持久化。
- Phase 1 仍需继续收口生产路由使用真实 service，不能因为注入完成而声明 v0.4 完成。

## 2026-07-03 Phase 1.44：Archive 创建与编辑生产 API 接真实 ArchiveService

完成事项：

- 新增生产路由 `POST /memory/archive/create`。
- 新增生产路由 `POST /memory/archive/edit`。
- 两个路由均使用 `RouterOptions.ArchiveService`，未配置时返回 `503 archive_not_configured`，不回退到内存仓库或静态假数据。
- 创建接口写入 Markdown 正文文件和 Archive metadata。
- 编辑接口写入新正文、递增 `current_version` 和 `index_generation`，并复用 Archive service 的 secret-like 内容脱敏逻辑。
- OpenAPI 增加 `/memory/archive/create` 和 `/memory/archive/edit` 路径。
- API 响应只返回 Archive metadata，不返回正文内容，降低误回显 secret-like 内容风险。

新增或变更测试：

- `TestArchiveCreateReturnsServiceUnavailableWithoutArchiveService`
- `TestArchiveCreateAndEditUseConfiguredService`
- `TestOpenAPIJSON` 覆盖新增 OpenAPI 路由。

验证命令：

- 本地红测：`go test ./internal/http -run 'TestArchive(CreateReturnsServiceUnavailableWithoutArchiveService|CreateAndEditUseConfiguredService)' -count=1 -v` 实现前失败于生产 Archive 路由返回 404。
- 本地目标测试：`go test ./internal/http -run 'TestArchive(CreateReturnsServiceUnavailableWithoutArchiveService|CreateAndEditUseConfiguredService)|TestOpenAPIJSON' -count=1 -v` 通过。
- 本地 HTTP 包测试：`go test ./internal/http -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器全量测试：`make test` 通过。
- 服务器 smoke：`make smoke` 通过，输出 `smoke ok`。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp` 容器。
- 服务器健康检查：`curl http://127.0.0.1:18081/healthz` 返回 `db`、`qdrant`、`redis` 全部 `ok`。
- 服务器 OpenAPI 验证：`curl http://127.0.0.1:18081/openapi.json` 可见 `/memory/archive/create` 和 `/memory/archive/edit`。
- 服务器 post-deploy 验收：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.WvNiBN`。

部署说明：

- 本轮继续复用运行容器当前 Secret Vault 测试 key 作为 compose 解析和 API 启动环境，不在回复、代码或日志中输出明文 key。
- 本轮仍使用临时 `memory-llm-mock` 作为 OpenAI-compatible provider 验证 pipeline，不作为最终生产模型供应商。

剩余问题：

- Archive create/edit 已接真实服务，但还没有生产列表、详情读取正文、版本列表、删除、重建索引 API。
- Archive API 尚未接入完整登录/权限上下文，后续 Phase 2 权限闭环必须补齐。
- Nuxt 归档库页面仍需接这些真实 API 并做浏览器验收。

## 2026-07-03 Phase 1.45：Archive 详情与版本生产 API 接真实 ArchiveService

完成事项：

- 新增生产路由 `POST /memory/archive/detail`。
- 新增生产路由 `POST /memory/archive/versions`。
- 两个路由均使用 `RouterOptions.ArchiveService`，未配置时返回 `503 archive_not_configured`，不回退到内存仓库或静态假数据。
- `archive.Service.Detail()` 先读取 PostgreSQL metadata，再从 `metadata.file_path` 读取 Markdown 正文，保持 Markdown 文件作为 Archive 正文权威源。
- `archive.Service.Versions()` 通过 repository 返回 Archive 版本列表。
- `archive.Repository` contract 增加 `Versions(archiveID string) ([]Version, error)`。
- `archive.PGRepository.Versions()` 查询 `archive_versions`，按 `version ASC` 稳定返回版本历史。
- `/memory/archive/versions` 响应显式使用稳定 snake_case 字段，避免 Go struct 默认大写 JSON 字段泄漏到 API 契约。
- OpenAPI 增加 `/memory/archive/detail` 和 `/memory/archive/versions` 路径。

新增或变更测试：

- `TestServiceDetailReadsMarkdownContent`
- `TestServiceVersionsReturnsStoredVersions`
- `TestPGRepositoryRequiresPool` 增加 `Versions` nil pool 错误覆盖。
- `TestPGRepositorySaveEditCreatesVersionAuditAndIndexGeneration` 增加 PG versions 断言。
- `TestArchiveDetailAndVersionsUseConfiguredService`
- `TestArchiveDetailReturnsServiceUnavailableWithoutArchiveService`
- `TestOpenAPIJSON` 覆盖新增 OpenAPI 路由。

验证命令：

- 本地红测：`go test ./internal/archive -run 'Test(ServiceDetailReadsMarkdownContent|ServiceVersionsReturnsStoredVersions|PGRepositoryRequiresPool|PGRepositorySaveEditCreatesVersionAuditAndIndexGeneration)' -count=1 -v && go test ./internal/http -run 'TestArchive(DetailAndVersionsUseConfiguredService|DetailReturnsServiceUnavailableWithoutArchiveService)' -count=1 -v`，实现前 HTTP 侧失败于 detail/versions 路由返回 404。
- 本地目标测试：`go test ./internal/http -run 'TestArchive(DetailAndVersionsUseConfiguredService|DetailReturnsServiceUnavailableWithoutArchiveService)|TestOpenAPIJSON' -count=1 -v` 通过；过程中发现 versions 直接返回 Go struct 会输出大写字段，已改为稳定 snake_case 响应并重新验证通过。
- 本地 Archive 包测试：`go test ./internal/archive -count=1` 通过。
- 本地 HTTP 包测试：`go test ./internal/http -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器全量测试：`make test` 通过。
- 服务器 smoke：`make smoke` 通过，输出 `smoke ok`。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp` 容器。
- 服务器健康检查：`curl http://127.0.0.1:18081/healthz` 返回 `db`、`qdrant`、`redis` 全部 `ok`。
- 服务器 OpenAPI 验证：`curl http://127.0.0.1:18081/openapi.json` 可见 `/memory/archive/detail` 和 `/memory/archive/versions`。
- 服务器容器状态：`docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps` 显示 API、Web、MCP、worker 均为 `Up`，PostgreSQL 和 Redis 为 `healthy`。
- 服务器 post-deploy 验收：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.Fnu2Ez`。

部署说明：

- 本轮继续复用运行容器当前 Secret Vault 测试 key 作为 compose 解析和 API 启动环境，不在回复、代码或日志中输出明文 key。
- 本轮仍使用临时 `memory-llm-mock` 作为 OpenAI-compatible provider 验证 pipeline，不作为最终生产模型供应商。
- 本轮未删除数据卷、未改公开端口、未执行破坏性 migration。

剩余问题：

- Archive create/edit/detail/versions 已接真实服务，但还没有生产列表、删除、重建索引 API。
- Archive API 尚未接入完整登录/权限上下文，后续 Phase 2 权限闭环必须补齐。
- Nuxt 归档库页面仍需接这些真实 API 并做浏览器验收。
- Phase 1 仍未完成；Auth/Tenant/Secret/Audit/Retrieval access log 的 production API 使用面还需要继续收口。

## 2026-07-03 Phase 1.46：Archive 列表、软删除与重建索引生产 API

完成事项：

- 新增生产路由 `POST /memory/archive/list`。
- 新增生产路由 `POST /memory/archive/delete`。
- 新增生产路由 `POST /memory/archive/reindex`。
- 三个路由均使用 `RouterOptions.ArchiveService`，未配置时返回 `503 archive_not_configured`，不回退到内存仓库或静态假数据。
- `archive.Repository` contract 增加 `List`、`SoftDelete`、`MarkReindex`。
- `archive.Service.List()` 要求 user/org/project scope，默认只列 `active` Archive。
- `archive.Service.Delete()` 实现软删除：只把 `archives.status` 更新为 `deleted`，保留 Markdown 文件、版本记录和审计记录。
- `archive.Service.Reindex()` 读取 Markdown 正文，递增 `index_generation`，重新 chunk，并返回待索引 chunks。
- `PGRepository.List()` 使用 PostgreSQL scope filter 和 status filter 查询 Archive metadata。
- `PGRepository.SoftDelete()` 使用 request id 幂等保护，更新 status，并写入 `archive_edit_audit_logs`。
- `PGRepository.MarkReindex()` 使用 request id 幂等保护，更新 `index_generation`，并写入 `archive_index_generations`。
- `RouterOptions` 增加 `ArchiveIndexQueue`，生产启动路径注入 `jobs.NewPGArchiveIndexQueue(...)`。
- `/memory/archive/reindex` 会把重新生成的 chunks 入队到 Archive RAG index queue；未配置 index queue 时返回 `503 archive_index_not_configured`。
- OpenAPI 增加 `/memory/archive/list`、`/memory/archive/delete`、`/memory/archive/reindex`。

新增或变更测试：

- `TestServiceListFiltersArchives`
- `TestServiceDeleteSoftDeletesAndKeepsVersions`
- `TestServiceReindexIncrementsGenerationAndChunksMarkdown`
- `TestPGRepositoryRequiresPool` 增加 `List`、`SoftDelete`、`MarkReindex` nil pool 覆盖。
- `TestPGRepositoryListSoftDeleteAndMarkReindex`
- `TestArchiveListDeleteAndReindexUseConfiguredService`
- `TestArchiveReindexReturnsServiceUnavailableWithoutIndexQueue`
- `TestRouterOptionsConfiguresCoreServicesWhenPostgresPoolExists` 增加 `ArchiveIndexQueue` 注入断言。
- `TestRouterOptionsLeavesAuthOpenForDevelopmentSmoke` 增加 development smoke 不应配置 `ArchiveIndexQueue` 的断言。
- `TestOpenAPIJSON` 覆盖新增 OpenAPI 路由。

验证命令：

- 本地 service 红测：`go test ./internal/archive -run 'TestService(ListFiltersArchives|DeleteSoftDeletesAndKeepsVersions|ReindexIncrementsGenerationAndChunksMarkdown)' -count=1 -v`，实现前失败于 `Service.List`、`Service.Delete`、`Service.Reindex` 和请求类型缺失。
- 本地 PG 红测：`go test ./internal/archive -run 'TestPGRepository(RequiresPool|ListSoftDeleteAndMarkReindex)' -count=1 -v`，实现前失败于 `PGRepository.List`、`SoftDelete`、`MarkReindex` 缺失。
- 本地 HTTP 红测：`go test ./internal/http -run 'TestArchive(ListDeleteAndReindexUseConfiguredService|ReindexReturnsServiceUnavailableWithoutIndexQueue)' -count=1 -v`，实现前失败于 `RouterOptions.ArchiveIndexQueue` 缺失。
- 本地 Archive 包测试：`go test ./internal/archive -count=1` 通过；PG 集成测试在本地因 `POSTGRES_TEST_DSN` 未设置按设计跳过。
- 本地 HTTP 包测试：`go test ./internal/http -count=1` 通过。
- 本地 API 启动目标测试：`go test ./cmd/memory-api -run 'TestRouterOptions(ConfiguresCoreServicesWhenPostgresPoolExists|LeavesAuthOpenForDevelopmentSmoke)' -count=1 -v` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器全量测试：`make test` 通过。
- 服务器 smoke：`make smoke` 通过，输出 `smoke ok`。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp` 容器。
- 服务器健康检查：`curl http://127.0.0.1:18081/healthz` 返回 `db`、`qdrant`、`redis` 全部 `ok`。
- 服务器 OpenAPI 验证：`curl http://127.0.0.1:18081/openapi.json` 可见 `/memory/archive/list`、`/memory/archive/delete`、`/memory/archive/reindex`。
- 服务器容器状态：`docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps` 显示 API、Web、MCP、worker 均为 `Up`，PostgreSQL 和 Redis 为 `healthy`。
- 服务器 post-deploy 验收：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.ac3Oz0`。

部署说明：

- 本轮继续复用运行容器当前 Secret Vault 测试 key 作为 compose 解析和 API 启动环境，不在回复、代码或日志中输出明文 key。
- 本轮仍使用临时 `memory-llm-mock` 作为 OpenAI-compatible provider 验证 pipeline，不作为最终生产模型供应商。
- 本轮未删除数据卷、未改公开端口、未执行破坏性 migration。

剩余问题：

- Archive 后端生产治理 API 已覆盖 create/edit/detail/versions/list/delete/reindex，但尚未接入完整登录/权限上下文。
- Reindex 当前使用固定 `project:<project_id>:read` permission label 和 `project` visibility，后续 Phase 2 权限上下文完成后必须改为从真实权限上下文派生。
- Nuxt 归档库页面仍需接这些真实 API 并做浏览器验收。
- Phase 1 仍需继续收口 Auth/Tenant/Secret/Audit/Retrieval access log 的 production API 使用面，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.47：Archive 管理 API 接入 PAT 与租户权限上下文

完成事项：

- Archive 管理 API 在 `AuthService` 和 `TenantService` 配置时默认要求 PAT。
- 无 PAT 访问 Archive 管理 API 返回 `401 pat_required`。
- 无效 PAT 返回 `401 invalid_pat`。
- PAT 对应用户没有目标 org/project membership 时返回 `403 archive_forbidden`。
- 读操作使用 `memory:read` PAT scope；`memory:write` 可覆盖读权限。
- 写操作使用 `memory:write` PAT scope，并要求 `tenant.PermissionContext` 里存在 `project:<project_id>:write`。
- `POST /memory/archive/create` 不再信任请求体里的 `user_id`，生产 auth 路径使用 PAT subject 作为 Archive owner。
- `POST /memory/archive/list` 不再信任请求体里的 `user_id`，生产 auth 路径只列 PAT subject 自己的 Archive。
- `edit/delete/reindex/detail/versions` 先读取 Archive metadata 获取 org/project，再进行 PAT + membership 校验；未授权不会继续执行业务写操作。
- `reindex` 入队的 RAG index job 改为使用 `tenant.PermissionContext.PermissionLabels`，不再硬编码单一 `project:<project_id>:read` label。
- `archive.Service` 增加 `Metadata()`，用于 handler 在读取 Markdown 正文或执行写操作前先做权限判断。
- development/test 未配置 auth/tenant 时保持兼容，避免 dev smoke 被生产权限依赖阻断。

新增或变更测试：

- `TestArchiveAPIsRequirePATWhenAuthAndTenantConfigured`
- `TestArchiveAPIsRejectPATWithoutProjectMembership`
- `TestArchiveCreateUsesPATSubjectAndRequiresWritePermission`
- `TestArchiveReindexUsesPermissionContextLabels`

验证命令：

- 本地红测：`go test ./internal/http -run 'TestArchive(APIsRequirePATWhenAuthAndTenantConfigured|APIsRejectPATWithoutProjectMembership|CreateUsesPATSubjectAndRequiresWritePermission|ReindexUsesPermissionContextLabels)' -count=1 -v`，实现前失败于无 PAT 返回 200、无 membership 返回 200、member 写入返回 200、reindex job 仍使用硬编码 label。
- 本地目标测试：`go test ./internal/http -run 'TestArchive(APIsRequirePATWhenAuthAndTenantConfigured|APIsRejectPATWithoutProjectMembership|CreateUsesPATSubjectAndRequiresWritePermission|ReindexUsesPermissionContextLabels)' -count=1 -v` 通过。
- 本地 HTTP 包测试：`go test ./internal/http -count=1` 通过。
- 本地 Archive 包测试：`go test ./internal/archive -count=1` 通过。
- 本地 API 包测试：`go test ./cmd/memory-api -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器全量测试：`make test` 通过。
- 服务器 smoke：`make smoke` 通过，输出 `smoke ok`。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp` 容器。
- 服务器健康检查：`curl http://127.0.0.1:18081/healthz` 返回 `db`、`qdrant`、`redis` 全部 `ok`。
- 服务器 OpenAPI 验证：`curl http://127.0.0.1:18081/openapi.json` 可见 Archive 管理路由。
- 服务器运行时权限验证：无 PAT 调用 `POST /memory/archive/list` 返回 HTTP `401` 和 `{"error":"pat_required"}`。
- 服务器 post-deploy 验收：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.Mozmnl`。

部署说明：

- 本轮继续复用运行容器当前 Secret Vault 测试 key 作为 compose 解析和 API 启动环境，不在回复、代码或日志中输出明文 key。
- 本轮仍使用临时 `memory-llm-mock` 作为 OpenAI-compatible provider 验证 pipeline，不作为最终生产模型供应商。
- 本轮未删除数据卷、未改公开端口、未执行破坏性 migration。

剩余问题：

- Archive 管理 API 已有 PAT + membership + write label 基线，但还不是完整 RBAC；角色、资源级 permission labels、管理 API session 登录仍需后续 Phase 2 继续完善。
- Nuxt 归档库页面还未接入 PAT 登录态和这些真实 API 的浏览器验收。
- 其他管理 API，如 Secret、Tenant、Token、Hot Memory，仍需逐步接入同等安全边界。
- Phase 1 仍需继续收口 Auth/Tenant/Secret/Audit/Retrieval access log 的 production API 使用面，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.48：Secret Vault 管理 API 接入 PAT 与租户权限

完成事项：

- 新增生产路由 `POST /memory/secrets/create`。
- 新增生产路由 `POST /memory/secrets/list`。
- 新增生产路由 `POST /memory/secrets/disable`。
- Secret 管理 API 在 `AuthService` 和 `TenantService` 配置时默认要求 PAT。
- 无 PAT 创建 Secret 返回 `401 pat_required`。
- 创建和禁用 Secret 要求 `memory:write` PAT scope，并要求目标项目具备写权限标签。
- 列表 Secret 要求 `memory:read` PAT scope；`memory:write` 可覆盖读权限。
- 生产 auth 路径不信任请求体里的 `user_id`，使用 PAT subject 作为 Secret owner 和列表过滤 user。
- 禁用 Secret 前先读取 metadata 获取真实 org/project/owner，再做 PAT + membership 权限判断，避免通过请求体伪造 scope。
- API 响应只返回 Secret metadata：`secret_ref`、owner、org、project、name、status、current_version。
- API 响应不返回明文、nonce、ciphertext、encrypted_value 或其他 Secret material。
- `secret.Repository` contract 增加 `List(filter ListFilter)`。
- `secret.Vault` 增加 `List()` 和 `Metadata()`，用于管理 API metadata-only 查询与禁用前权限判断。
- `secret.PGRepository.List()` 只查询 `secrets` metadata 表，不读取 `secret_versions` 加密正文表。
- OpenAPI 增加 `/memory/secrets/create`、`/memory/secrets/list`、`/memory/secrets/disable`。
- development/test 未配置 auth/tenant 时保持兼容，避免 dev smoke 被生产权限依赖阻断。

新增或变更测试：

- `TestSecretAPIsRequirePATWhenAuthAndTenantConfigured`
- `TestSecretCreateRequiresWritePermissionAndReturnsMetadataOnly`
- `TestSecretListAndDisableUsePATSubjectAndPermission`
- `TestVaultListReturnsMetadataOnly`
- `TestPGRepositoryRequiresPool` 增加 `List` nil pool 覆盖。
- `TestPGRepositoryVaultLifecycle` 增加 `List` owner/org/project/status 过滤覆盖。
- `TestOpenAPIJSON` 覆盖新增 Secret 管理路由。

验证命令：

- 本地 HTTP 红测：`go test ./internal/http -run 'TestSecret(APIsRequirePATWhenAuthAndTenantConfigured|CreateRequiresWritePermissionAndReturnsMetadataOnly|ListAndDisableUsePATSubjectAndPermission)' -count=1 -v`，实现前失败于 Secret 管理路由不存在。
- 本地 Secret 红测：`go test ./internal/secret -run 'TestVaultListReturnsMetadataOnly|TestPGRepository(RequiresPool|VaultLifecycle)' -count=1 -v`，实现前失败于 `ListFilter` 和 repository/vault `List` 缺失。
- 本地目标测试：`go test ./internal/http -run 'TestSecret(APIsRequirePATWhenAuthAndTenantConfigured|CreateRequiresWritePermissionAndReturnsMetadataOnly|ListAndDisableUsePATSubjectAndPermission)' -count=1 -v` 通过。
- 本地 HTTP 包测试：`go test ./internal/http -count=1` 通过。
- 本地 Secret 包测试：`go test ./internal/secret -count=1` 通过；PG 集成测试在本地因 `POSTGRES_TEST_DSN` 未设置按设计跳过。
- 本地 API 包测试：`go test ./cmd/memory-api -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器全量测试：`make test` 通过。
- 服务器 smoke：`make smoke` 通过，输出 `smoke ok`。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp` 容器。
- 服务器健康检查：`curl http://127.0.0.1:18081/healthz` 返回 `db`、`qdrant`、`redis` 全部 `ok`。
- 服务器 OpenAPI 验证：`curl http://127.0.0.1:18081/openapi.json` 可见 `/memory/secrets/create`、`/memory/secrets/list`、`/memory/secrets/disable`。
- 服务器运行时权限验证：无 PAT 调用 `POST /memory/secrets/create` 返回 HTTP `401` 和 `{"error":"pat_required"}`。
- 服务器 post-deploy 验收：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.LdewvW`。

部署说明：

- 本轮继续复用运行容器当前 Secret Vault 测试 key 作为 compose 解析和 API 启动环境，不在回复、代码或日志中输出明文 key。
- 本轮仍使用临时 `memory-llm-mock` 作为 OpenAI-compatible provider 验证 pipeline，不作为最终生产模型供应商。
- 本轮未删除数据卷、未改公开端口、未执行破坏性 migration。

剩余问题：

- Secret Vault 后端已有 create/list/disable metadata-only 管理 API，但还不是完整 Secret Vault 阶段：Secret 注入审计、UI 操作闭环、浏览器验收仍需继续。
- Secret 权限当前接入 PAT + membership + project write label 基线，但完整 RBAC、角色管理、资源级 permission labels 仍需后续 Phase 2 继续完善。
- Nuxt Secret Vault 页面还未接入 PAT 登录态和这些真实 API 的浏览器验收。
- Phase 1 仍需继续收口 Tenant 管理 API、Token 管理 API、Audit 管理 API、Retrieval access log 的 production API 使用面，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.49：PAT 与 Adapter Token 管理 API 接入生产权限边界

完成事项：

- 新增生产路由 `POST /memory/tokens/pat/create`。
- 新增生产路由 `POST /memory/tokens/pat/list`。
- 新增生产路由 `POST /memory/tokens/pat/revoke`。
- 新增生产路由 `POST /memory/tokens/adapter/create`。
- 新增生产路由 `POST /memory/tokens/adapter/list`。
- 新增生产路由 `POST /memory/tokens/adapter/revoke`。
- PAT 管理 API 默认要求已有 PAT，未携带 PAT 返回 `401 pat_required`。
- PAT 创建、撤销要求 `memory:write` scope；PAT 列表要求 `memory:read` scope，`memory:write` 可覆盖读权限。
- PAT 管理 API 不信任请求体里的 `user_id`，统一使用当前 PAT subject。
- Adapter Token 创建和撤销要求 `memory:write` scope，并要求目标项目具备 `project:<project_id>:write` 权限标签。
- Adapter Token 列表要求 `memory:read` scope，并通过租户 membership 获取真实权限上下文。
- Adapter Token 管理 API 不信任请求体里的 `user_id`，统一使用当前 PAT subject。
- Adapter Token 撤销前先读取 token metadata 获取真实 org/project/user，再进行 PAT + tenant 权限判断。
- token 创建接口只在创建响应里返回明文 token 一次。
- token 列表和撤销响应只返回 metadata，不返回 token 明文、token hash 或其它 token material。
- `auth.Repository` contract 增加 `GetPAT`、`ListPATs`、`GetAdapterToken`、`ListAdapterTokens`。
- `auth.Service` 增加对应 get/list 方法。
- `auth.PGRepository` 增加 PAT 与 Adapter Token metadata 查询能力，并按 owner/scope/status 过滤。
- OpenAPI 增加 6 个 token 管理路由。

新增或变更测试：

- `TestPGRepositoryRequiresPool` 覆盖 token get/list nil pool。
- `TestPGRepositoryPasswordAndTokens` 覆盖 PAT get/list/revoke 与 Adapter Token get/list/revoke。
- `TestTokenAPIsRequirePATWhenAuthConfigured`
- `TestPATTokenLifecycleUsesPATSubjectAndReturnsPlainOnce`
- `TestAdapterTokenLifecycleRequiresProjectWriteAndReturnsMetadataOnlyOnList`
- `TestOpenAPIJSON` 覆盖新增 token 管理路由。

验证命令：

- 本地 Auth 红测：`go test ./internal/auth -run 'TestPGRepository(RequiresPool|PasswordAndTokens)' -count=1 -v`，实现前失败于 `GetPAT`、`ListPATs`、`GetAdapterToken`、`ListAdapterTokens` 缺失。
- 本地 HTTP 红测：`go test ./internal/http -run 'Test(TokenAPIsRequirePATWhenAuthConfigured|PATTokenLifecycleUsesPATSubjectAndReturnsPlainOnce|AdapterTokenLifecycleRequiresProjectWriteAndReturnsMetadataOnlyOnList)' -count=1 -v`，实现前失败于 token 管理路由 404。
- 本地 Auth 包测试：`go test ./internal/auth -count=1` 通过；PG 集成测试在本地因 `POSTGRES_TEST_DSN` 未设置按设计跳过。
- 本地 HTTP 包测试：`go test ./internal/http -count=1` 通过。
- 本地 API 包测试：`go test ./cmd/memory-api -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器全量测试：`make test` 通过。
- 服务器 smoke：`make smoke` 通过，输出 `smoke ok`。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp` 容器。
- 服务器健康检查：`curl http://127.0.0.1:18081/healthz` 返回 `db`、`qdrant`、`redis` 全部 `ok`。
- 服务器 OpenAPI 验证：`curl http://127.0.0.1:18081/openapi.json` 可见 `/memory/tokens/pat/create`、`/memory/tokens/pat/list`、`/memory/tokens/pat/revoke`、`/memory/tokens/adapter/create`、`/memory/tokens/adapter/list`、`/memory/tokens/adapter/revoke`。
- 服务器运行时权限验证：无 PAT 调用 `POST /memory/tokens/pat/create` 返回 HTTP `401` 和 `{"error":"pat_required"}`。
- 服务器 post-deploy 验收：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.4O00r8`。

部署说明：

- 本轮继续复用运行容器当前 Secret Vault 测试 key 作为 compose 解析和 API 启动环境，不在回复、代码或日志中输出明文 key。
- 本轮仍使用临时 `memory-llm-mock` 作为 OpenAI-compatible provider 验证 pipeline，不作为最终生产模型供应商。
- 本轮未删除数据卷、未改公开端口、未执行破坏性 migration。

剩余问题：

- token 创建目前是安全的一次性明文返回，但还未实现 request-id 级幂等重放语义；后续需要在管理 API 统一补齐幂等策略。
- 生产“第一个管理员 PAT”的初始化/登录闭环仍需后续 Auth/Tenant 管理阶段完成，当前 API 只允许已有 PAT 授权后继续管理 token。
- Nuxt Adapter Token 页面还未接入 PAT 登录态和这些真实 API 的浏览器验收。
- Phase 1 仍需继续收口 Tenant 管理 API、Audit 管理 API、Retrieval access log 的 production API 使用面，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.50：Tenant 管理 API 接入生产权限边界

完成事项：

- 新增生产路由 `POST /memory/tenant/users/create`。
- 新增生产路由 `POST /memory/tenant/orgs/create`。
- 新增生产路由 `POST /memory/tenant/orgs/list`。
- 新增生产路由 `POST /memory/tenant/projects/create`。
- 新增生产路由 `POST /memory/tenant/projects/list`。
- 新增生产路由 `POST /memory/tenant/memberships/add`。
- 新增生产路由 `POST /memory/tenant/memberships/list`。
- Tenant 管理 API 默认要求 PAT，未携带 PAT 返回 `401 pat_required`。
- 创建用户、创建组织、创建项目、添加成员等写操作要求 `memory:write` PAT scope。
- 组织创建自动给当前 PAT subject 创建 org-level owner membership，解决后续项目创建的授权入口。
- 项目创建要求当前 PAT subject 具备 org-level owner/admin 写权限，并自动给当前用户创建 project-level owner membership。
- 添加成员要求当前 PAT subject 具备目标项目写权限。
- 组织和项目列表不信任请求体里的 `user_id`，统一使用当前 PAT subject。
- `tenant.Repository` contract 增加 `ListOrgs`、`ListProjects`、`ListMemberships`。
- `tenant.Service` 增加 `ListOrgs`、`ListProjects`、`ListMemberships`、`RequireOrgWrite`。
- `tenant.PGRepository` 增加基于 memberships 的组织、项目、成员列表查询。
- OpenAPI 增加 7 个 Tenant 管理路由。

新增或变更测试：

- `TestServiceListsTenantResourcesAndOrgWritePermission`
- `TestPGRepositoryRequiresPool` 覆盖 Tenant list nil pool。
- `TestPGRepositoryCreatesTenantGraph` 覆盖 org/project/membership list 和 org write 权限。
- `TestTenantAPIsRequirePATWhenAuthConfigured`
- `TestTenantOrgProjectAndMembershipManagementUsesPATSubject`
- `TestOpenAPIJSON` 覆盖新增 Tenant 管理路由。

验证命令：

- 本地 Tenant 红测：`go test ./internal/tenant -run 'TestServiceListsTenantResourcesAndOrgWritePermission|TestPGRepository(RequiresPool|CreatesTenantGraph)' -count=1 -v`，实现前失败于 `ListOrgs`、`ListProjects`、`ListMemberships`、`RequireOrgWrite` 缺失。
- 本地 HTTP 红测：`go test ./internal/http -run 'TestTenant(APIsRequirePATWhenAuthConfigured|OrgProjectAndMembershipManagementUsesPATSubject)' -count=1 -v`，实现前失败于 Tenant 管理路由和测试 helper 缺失。
- 本地 Tenant 包测试：`go test ./internal/tenant -count=1` 通过；PG 集成测试在本地因 `POSTGRES_TEST_DSN` 未设置按设计跳过。
- 本地 HTTP 包测试：`go test ./internal/http -count=1` 通过。
- 本地 API 包测试：`go test ./cmd/memory-api -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器全量测试：`make test` 通过。
- 服务器 smoke：`make smoke` 通过，输出 `smoke ok`。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp` 容器。
- 服务器健康检查：`curl http://127.0.0.1:18081/healthz` 返回 `db`、`qdrant`、`redis` 全部 `ok`。
- 服务器 OpenAPI 验证：`curl http://127.0.0.1:18081/openapi.json` 可见 `/memory/tenant/users/create`、`/memory/tenant/orgs/create`、`/memory/tenant/orgs/list`、`/memory/tenant/projects/create`、`/memory/tenant/projects/list`、`/memory/tenant/memberships/add`、`/memory/tenant/memberships/list`。
- 服务器运行时权限验证：无 PAT 调用 `POST /memory/tenant/orgs/create` 返回 HTTP `401` 和 `{"error":"pat_required"}`。
- 服务器 post-deploy 验收：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.6FjPig`。

部署说明：

- 本轮继续复用运行容器当前 Secret Vault 测试 key 作为 compose 解析和 API 启动环境，不在回复、代码或日志中输出明文 key。
- 本轮仍使用临时 `memory-llm-mock` 作为 OpenAI-compatible provider 验证 pipeline，不作为最终生产模型供应商。
- 本轮未删除数据卷、未改公开端口、未执行破坏性 migration。

剩余问题：

- Tenant 管理 API 已有 create/list 基线，但还没有用户禁用、组织/项目编辑、成员移除/角色变更等完整治理能力。
- API 仍未实现 request-id 级幂等重放语义，后续管理写接口需要统一补齐。
- 生产“第一个管理员 PAT”的初始化/登录闭环仍需后续 Auth 管理阶段完成。
- Nuxt 用户、组织、项目、成员页面还未接入这些真实 API 做浏览器验收。
- Phase 1 仍需继续收口 Audit 管理 API、Retrieval access log 管理面和 UI 生产化，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.51：Audit 与 Retrieval Access Log 管理 API

完成事项：

- 新增生产路由 `POST /memory/audit/list`。
- 新增生产路由 `POST /memory/retrieval/access-log/list`。
- 两个管理 API 默认要求 PAT，未携带 PAT 返回 `401 pat_required`。
- 两个管理 API 均要求 `memory:read` scope，并通过 `tenant.PermissionContext` 校验 org/project membership。
- Audit 查询按 org/project/resource/actor 过滤，返回结构化 audit metadata。
- Retrieval access log 查询按 org/project/actor/request 过滤，返回 request 和 result/access source refs。
- Retrieval access log 响应只返回 `query_hash`，不返回原始 query 文本。
- `audit.Repository` contract 增加 `List(filter)`。
- `audit.Service` 增加 `List(filter)`，要求 org/project scope。
- `audit.PGRepository` 增加按 org/project/resource 过滤的审计日志查询。
- `retrieval.AccessLogReader` 增加 `ListRequests`、`ListResults`。
- `retrieval.MemoryAccessLog` 保存 query hash 和 source refs，供测试/开发查询。
- `retrieval.PGAccessLog` 增加 SQL 层 org/project 过滤查询。
- `RouterOptions` 增加 `RetrievalAccessLog`，生产启动路径注入 PG access log reader。
- OpenAPI 增加 `/memory/audit/list` 和 `/memory/retrieval/access-log/list`。

新增或变更测试：

- `TestPGRepositoryRequiresPool` 覆盖 audit list nil pool。
- `TestPGRepositorySavePersistsAuditLog` 覆盖 audit list。
- `TestPGAccessLogRequiresPool` 覆盖 access log list nil pool。
- `TestPGAccessLogPersistsRequestAndDedupesRequestID` 覆盖 request list。
- `TestPGAccessLogPersistsResultAndAccessLogIdempotently` 覆盖 result list。
- `TestAuditAndAccessLogAPIsRequirePATWhenConfigured`
- `TestAuditAndAccessLogListUseTenantPermissionAndDoNotExposeQueryText`
- `TestOpenAPIJSON` 覆盖新增管理路由。

验证命令：

- 本地 Audit 红测：`go test ./internal/audit -run 'TestPGRepository(RequiresPool|SavePersistsAuditLog)' -count=1 -v`，实现前失败于 `List` 和 `ListFilter` 缺失。
- 本地 Retrieval 红测：`go test ./internal/retrieval -run 'TestPGAccessLog(RequiresPool|PersistsRequestAndDedupesRequestID|PersistsResultAndAccessLogIdempotently)' -count=1 -v`，实现前失败于 `ListRequests`、`ListResults`、`AccessLogListFilter` 缺失。
- 本地 HTTP 红测：`go test ./internal/http -run 'TestAuditAndAccessLog(APIsRequirePATWhenConfigured|ListUseTenantPermissionAndDoNotExposeQueryText)' -count=1 -v`，实现前失败于管理路由、`RetrievalAccessLog` 注入点和 audit import 缺失。
- 本地 Audit 包测试：`go test ./internal/audit -count=1` 通过；PG 集成测试在本地因 `POSTGRES_TEST_DSN` 未设置按设计跳过。
- 本地 Retrieval 包测试：`go test ./internal/retrieval -count=1` 通过；PG 集成测试在本地因 `POSTGRES_TEST_DSN` 未设置按设计跳过。
- 本地 HTTP 包测试：`go test ./internal/http -count=1` 通过。
- 本地 API 包测试：`go test ./cmd/memory-api -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器全量测试：`make test` 通过。
- 服务器 smoke：`make smoke` 通过，输出 `smoke ok`。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp` 容器。
- 服务器健康检查：`curl http://127.0.0.1:18081/healthz` 返回 `db`、`qdrant`、`redis` 全部 `ok`。
- 服务器 OpenAPI 验证：`curl http://127.0.0.1:18081/openapi.json` 可见 `/memory/audit/list` 和 `/memory/retrieval/access-log/list`。
- 服务器运行时权限验证：无 PAT 调用 `POST /memory/audit/list` 返回 HTTP `401` 和 `{"error":"pat_required"}`。
- 服务器 post-deploy 验收：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.H9El6f`。

部署说明：

- 本轮继续复用运行容器当前 Secret Vault 测试 key 作为 compose 解析和 API 启动环境，不在回复、代码或日志中输出明文 key。
- 本轮仍使用临时 `memory-llm-mock` 作为 OpenAI-compatible provider 验证 pipeline，不作为最终生产模型供应商。
- 本轮未删除数据卷、未改公开端口、未执行破坏性 migration。

剩余问题：

- Audit 与 access log 已具备后端查询 API，但 Nuxt 审计/检索日志页面尚未接入这些真实 API。
- 当前查询接口还没有分页游标，只有 limit；后续生产 UI 需要补游标/时间范围过滤。
- 写接口仍未统一 request-id 级幂等重放语义。
- 生产“第一个管理员 PAT”的初始化/登录闭环仍需后续 Auth 管理阶段完成。
- Phase 1 的 PG 基座基本接近收口，但 UI 生产化、浏览器验收、完整 RBAC 和后续 Phase 仍未完成，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.52：Nuxt 组织 / 项目真实 API 接入与部署验收

完成事项：

- 登录页从本地假会话改为 PAT 登录验证。
- PAT 仅保存到浏览器 localStorage，不写入后端、日志或页面快照。
- `useApi` 默认从 Auth store 注入 `Authorization: Bearer`。
- `AppShell` 的组织 / 项目下拉改为调用真实租户 API。
- 组织页接入 `POST /memory/tenant/orgs/list` 和 `POST /memory/tenant/orgs/create`。
- 项目页接入 `POST /memory/tenant/projects/list` 和 `POST /memory/tenant/projects/create`。
- 修复无组织用户仍用旧默认 `org_1/project_1` 请求项目导致 UUID 校验错误的问题。
- 部署配置 `NO_PROXY` 默认加入 `memory-llm-mock`，修复代理环境下 pipeline e2e 调用 mock provider 返回 502 的问题。
- 增加配置测试，要求 Makefile、backend Dockerfile、compose 的 NO_PROXY 覆盖 `memory-llm-mock`。

修改模块：

- `web/pages/login.vue`
- `web/components/AppShell.vue`
- `web/pages/orgs/index.vue`
- `web/pages/projects/index.vue`
- `web/composables/useApi.ts`
- `web/composables/useAuth.ts`
- `web/stores/auth.ts`
- `web/stores/context.ts`
- `Makefile`
- `deploy/Dockerfile.api`
- `deploy/Dockerfile.worker`
- `deploy/Dockerfile.mcp`
- `deploy/docker-compose.yml`
- `cmd/memory-smoke/main_test.go`
- `internal/webdeploy/web_dockerfile_test.go`

新增或变更测试：

- `TestMakeSmokePassesStrictPipelineEnvironmentToDocker` 增加 `memory-llm-mock` NO_PROXY 覆盖。
- `TestBackendDockerfilesConfigureGoModuleProxy` 增加 backend Dockerfile NO_PROXY 覆盖。
- `TestComposePassesBackendBuildProxyArgs` 增加 compose build arg NO_PROXY 覆盖。
- `TestComposeRuntimeNoProxyCanBeExtended` 增加 compose runtime `no_proxy` 覆盖。

验证命令：

- 本地受影响测试：`go test ./internal/webdeploy ./cmd/memory-smoke -count=1` 通过。
- 本地前端构建：`cd web && npm run build` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp`；后续 Web 修复单独重建 `memory-web`。
- 服务器健康检查：`curl http://127.0.0.1:18081/healthz` 返回 `db`、`qdrant`、`redis` 全部 `ok`。
- 服务器 OpenAPI 验证：`curl http://127.0.0.1:18081/openapi.json` 可见 tenant org/project 管理路由。
- 服务器租户 API 权限验证：无 PAT 调用 `POST /memory/tenant/orgs/list` 返回 `401 pat_required`。
- 服务器租户 API 正向验证：临时 PAT 创建真实组织与项目后，org list 和 project list 均可查回；临时 PAT 已撤销。
- 服务器全量测试：`make test` 通过。
- 服务器最终 post-deploy 验证：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.PUCPjH`。

浏览器验收：

- 打开 `http://ddns.08121.top:18080/login`，确认登录页为 PAT 登录中文文案。
- 未登录打开 `/orgs` 自动回到登录页。
- 使用短期浏览器验收 PAT 登录后进入控制台。
- 无组织用户不再显示 `org_1/project_1` UUID 错误。
- 在组织页创建真实组织 `Codex Browser Org`，刷新后可见。
- 在项目页创建真实项目 `Codex Browser Project`，刷新后仍可见。
- 侧栏组织 / 项目下拉显示真实 API 返回的数据。
- 浏览器验收 PAT 已撤销，并清理浏览器 localStorage 中的 PAT。

部署说明：

- 本轮继续复用运行容器当前 Secret Vault 测试 key 作为 compose 解析和 API 启动环境，不在回复、代码或日志中输出明文 key。
- 本轮仍使用临时 `memory-llm-mock` 作为 OpenAI-compatible provider 验证 pipeline，不作为最终生产模型供应商。
- 本轮未删除数据卷、未改公开端口、未执行破坏性 migration。
- 浏览器验收创建了可追踪测试用户、组织和项目；按 stop condition 未做物理删除，如需清理需单独确认。

剩余问题：

- 登录仍是 PAT 粘贴式入口，尚未完成 password login / first-admin bootstrap 的生产闭环。
- 组织 / 项目已支持 create/list，但编辑、禁用、删除、成员角色调整还未完成。
- 总览页、归档、热记忆、Secret、Token、Qdrant、检索测试等页面仍需继续消除静态数据并做真实浏览器验收。
- 写接口仍未统一 request-id 级幂等重放语义。
- Phase 1.52 只能算管理台真实 API 接入的一个可验证切片，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.53：Token 管理台真实 API 接入与浏览器验收

完成事项：

- `web/pages/tokens/index.vue` 从随机假 `adapter_once_*` 改为真实 PAT / Adapter Token 管理页。
- PAT 列表接入 `POST /memory/tokens/pat/list`。
- PAT 创建接入 `POST /memory/tokens/pat/create`。
- PAT 撤销接入 `POST /memory/tokens/pat/revoke`。
- Adapter Token 列表接入 `POST /memory/tokens/adapter/list`。
- Adapter Token 创建接入 `POST /memory/tokens/adapter/create`。
- Adapter Token 撤销接入 `POST /memory/tokens/adapter/revoke`。
- 创建 token 后只临时显示一次明文；列表只展示 metadata，不展示明文或 hash。
- Adapter Token 创建绑定当前真实 org/project/agent 上下文。
- 无项目上下文时禁用 Adapter Token 创建并显示提示。
- 页面展示 PAT / Adapter Token scopes、prefix、expires、status。

修改模块：

- `web/pages/tokens/index.vue`

验证命令：

- 本地前端构建：`cd web && npm run build` 通过。
- 服务器 Web 重建：`docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-web` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器健康检查：`curl http://127.0.0.1:18081/healthz` 返回 `db`、`qdrant`、`redis` 全部 `ok`。
- 服务器最终 post-deploy 验证：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.cdIE4p`。

浏览器验收：

- 打开 `http://ddns.08121.top:18080/tokens/`，确认页面为真实 Token 管理页。
- 使用短期浏览器验收 PAT 登录并加载真实组织 / 项目上下文。
- 页面显示真实 PAT metadata，不显示 token hash。
- 创建真实 PAT 后，页面显示一次性明文，并刷新 PAT metadata 列表。
- 隐藏一次性 PAT 明文。
- 创建真实 Adapter Token 后，页面显示一次性明文，并刷新 Adapter Token metadata 列表。
- 通过页面撤销新建 PAT，状态变为 `revoked`。
- 通过页面撤销新建 Adapter Token，状态变为 `revoked`。
- 刷新/列表 metadata 只保留 prefix、scope、过期时间和状态。
- 浏览器验收用登录 PAT 已撤销，数据库确认该测试用户 `active_pat=0`、`active_adapter=0`。
- 浏览器 localStorage 中 PAT、operator email、org/project 上下文已清理。

安全检查：

- 本轮没有把真实 Secret 写入代码、配置、日志或交付报告。
- Playwright 验收中出现的一次性短期 token 均为测试 token，已撤销；最终交付报告不记录明文 token。
- Token 列表 API 和页面 metadata 均未展示 token hash 或明文。

部署说明：

- 本轮未删除数据卷、未改公开端口、未执行破坏性 migration。
- 浏览器验收创建了可追踪测试用户、组织和项目；按 stop condition 未做物理删除，如需清理需单独确认。

剩余问题：

- Token 页面已完成 create/list/revoke 基线，但还没有分页、筛选器 UI、复制后自动隐藏倒计时。
- 生产“第一个管理员 PAT”的初始化/登录闭环仍未完成。
- 总览页、归档、热记忆、Secret、Qdrant、检索测试等页面仍需继续消除静态数据并做真实浏览器验收。
- Phase 1.53 仍是管理台真实 API 接入的一部分，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.54：Secret Vault 管理台真实 API 接入与浏览器验收

完成事项：

- `web/pages/secrets/index.vue` 从静态说明页改为真实 Secret Vault 管理页。
- Secret 列表接入 `POST /memory/secrets/list`。
- Secret 创建接入 `POST /memory/secrets/create`。
- Secret 禁用接入 `POST /memory/secrets/disable`。
- 创建请求使用当前真实 org/project 上下文，未选择项目时禁用创建。
- 创建成功后立即清空明文输入框。
- 页面列表只展示 metadata：`name`、`secret_ref`、`status`、owner、project、version。
- 页面支持 active / disabled 状态过滤。
- 禁用后刷新页面并切到 disabled 过滤仍能查回同一条 metadata。
- 页面和 API 响应不展示 Secret 明文。

修改模块：

- `web/pages/secrets/index.vue`
- `docs/production-delivery-log.md`

验证命令：

- 本地后端目标测试：`go test ./internal/http -run 'TestSecret(APIsRequirePATWhenAuthAndTenantConfigured|CreateRequiresWritePermissionAndReturnsMetadataOnly|ListAndDisableUsePATSubjectAndPermission)' -count=1` 通过。
- 本地前端构建：`cd web && npm run build` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 本地测试明文仓库扫描：通过，未发现本轮浏览器验收测试明文残留在仓库文件中。
- 服务器 Web 重建：`docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-web` 通过。
- 服务器容器状态：API、Web、MCP、worker、PostgreSQL、Redis、Qdrant 均为 `Up`，PostgreSQL/Redis healthy。
- 服务器健康检查：`curl http://127.0.0.1:18081/healthz` 可访问。
- 服务器最终 post-deploy 验证：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.smOvLJ`。

浏览器验收：

- 打开 `http://ddns.08121.top:18080/secrets/`，使用短期浏览器验收 PAT 加载真实组织 / 项目上下文。
- 页面通过真实 UI 表单创建 Secret，返回 `secret_ref_zq0Zzq3d-koFp7LtMZhgxzgF`。
- 创建后明文输入框已清空。
- active 列表显示 Secret 名称、`secret_ref`、状态、owner、project、version。
- 页面正文未出现测试 Secret 明文。
- 通过页面禁用该 Secret。
- 刷新后切换 disabled 过滤，仍能查回同一条 Secret metadata。
- disabled 页面正文未出现测试 Secret 明文。
- 浏览器验收用短期 PAT 已撤销，数据库确认该 PAT `active_pat=0`。
- 浏览器 localStorage 中 PAT、operator email、org/project 上下文已清理。
- 本地临时 token 文件、一次性本机服务、`.playwright-mcp` 验收产物已清理。

安全检查：

- 本轮没有把真实 Secret 写入代码、配置或交付报告。
- Secret 明文只用于一次性浏览器验收，已确认未残留在仓库文件中。
- Playwright 验收中出现的一次性短期 PAT 已撤销；最终交付报告不记录明文 token。
- Secret 列表 API 和页面 metadata 均未展示密文、hash 或明文。

部署说明：

- 本轮只重建 `memory-web`，未改公开端口，未删除数据卷，未执行破坏性 migration。
- 浏览器验收创建了可追踪测试用户、组织、项目和 disabled Secret metadata；按 stop condition 未做物理删除，如需清理需单独确认。

剩余问题：

- Secret 页面已完成 create/list/disable 基线，但还没有轮换、版本详情、审计日志入口和复制 `secret_ref` 的受控 UI。
- Secret Vault 生产主密钥仍需最终持久安全注入方案，不能长期依赖临时测试 key。
- 总览页、归档、热记忆、Qdrant、检索测试等页面仍需继续消除静态数据并做真实浏览器验收。
- Phase 1.54 仍是管理台真实 API 接入的一部分，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.55：Qdrant 状态 API 与管理台真实状态页

完成事项：

- 新增 Qdrant collection info 读取能力，调用真实 `GET /collections/memory_os`。
- 新增 `qdrant.StatusService`，聚合 Qdrant collection 状态、query-time filter 声明、payload 必需字段、PostgreSQL `qdrant_points` 状态统计和 `archive_index_jobs` 状态统计。
- 新增 `qdrant.PGStatusStore`，从 PostgreSQL 读取 indexed/pending 等 point 状态与 archive index job 状态。
- 新增生产管理 API `POST /memory/qdrant/status`。
- `/memory/qdrant/status` 未配置时返回 `503 qdrant_status_not_configured`，不回退静态假数据。
- `/memory/qdrant/status` 在 AuthService 配置后要求 `memory:read` PAT。
- OpenAPI 增加 `/memory/qdrant/status`。
- `cmd/memory-api` production 启动路径注入真实 Qdrant client 和 PG status store。
- `web/pages/qdrant/index.vue` 从静态卡片改为真实 API 驱动页面。
- Qdrant 页面展示 collection、point/vector 计数、vector 配置、query-time filter 强制状态、payload 字段、point 状态和索引任务状态。

修改模块：

- `internal/qdrant/client.go`
- `internal/qdrant/client_test.go`
- `internal/qdrant/status.go`
- `internal/http/router.go`
- `internal/http/router_test.go`
- `cmd/memory-api/main.go`
- `cmd/memory-api/main_test.go`
- `web/pages/qdrant/index.vue`
- `docs/production-delivery-log.md`

新增或变更测试：

- `TestCollectionInfoReadsQdrantCollectionStatus`
- `TestQdrantStatusRequiresPATAndConfiguredService`
- `TestQdrantStatusUsesRealServiceAndReturnsIndexStats`
- `TestOpenAPIJSON` 增加 `/memory/qdrant/status` 覆盖。
- `TestRouterOptionsConfiguresCoreServicesWhenPostgresPoolExists` 增加 `QdrantStatusService` 注入断言。
- `TestRouterOptionsLeavesAuthOpenForDevelopmentSmoke` 增加 development smoke 不注入 Qdrant status service 断言。

验证命令：

- 本地红灯：`go test ./internal/qdrant ./internal/http ./cmd/memory-api -run 'Test(CollectionInfoReadsQdrantCollectionStatus|QdrantStatus|OpenAPIJSON|RouterOptionsConfiguresCoreServicesWhenPostgresPoolExists|RouterOptionsLeavesAuthOpenForDevelopmentSmoke)' -count=1` 实现前按预期失败，缺少 `CollectionInfo`、`StatusService`、`QdrantStatusService` 和路由。
- 本地目标测试：`go test ./internal/qdrant ./internal/http ./cmd/memory-api -count=1` 通过。
- 本地前端构建：`cd web && npm run build` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器全量测试：`make test` 通过。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp`、`memory-web`。
- 服务器 post-deploy 验证：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.XFTDNx`。
- 服务器 OpenAPI 验证：`curl http://127.0.0.1:18081/openapi.json` 可见 `/memory/qdrant/status`。
- 服务器 API 正向验证：短期只读 PAT 调用 `/memory/qdrant/status` 返回 `collection_name=memory_os`、`collection_status=green`、`points_count=20`、`query_time_filter_enforced=true`。

浏览器验收：

- 打开 `http://ddns.08121.top:18080/qdrant/`，使用短期只读 PAT 加载真实 Qdrant 状态页。
- 页面显示真实 `memory_os` collection。
- 页面显示 Query-Time Filter 为“已强制”。
- 页面显示必需 payload 字段，包括 `user_id` 和 `index_generation`。
- 页面显示 `Qdrant Point 状态` 区块。
- 页面显示 `索引任务` 区块。
- 页面未出现登录错误或加载失败。
- 浏览器验收用短期 PAT 已撤销，数据库确认该 PAT `active_pat=0`。
- 浏览器 localStorage 中 PAT 已清理。
- 本地临时 token 文件和 `.playwright-mcp` 验收产物已清理。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- 浏览器验收中的短期 PAT 已撤销，最终交付报告不记录明文 token。
- `make secret-scan` 通过。

部署说明：

- 本轮重建 API/worker/MCP/Web，未改公开端口，未删除数据卷，未执行破坏性 migration。
- Qdrant 状态页现在能证明 collection 与索引统计来自运行时服务，不再是静态假页面。

剩余问题：

- Qdrant 状态页还没有 snapshot 创建/恢复入口，只展示状态。
- payload schema 依赖 Qdrant 当前 collection 返回，若 collection 为空可能为空；最终验收仍需通过完整 TurnEvent -> Archive -> Index 链路证明 payload 写入。
- 检索测试页仍硬编码 `user_1` 和 generation，下一步需要改为真实当前用户/项目上下文和后端 generation resolver。
- Phase 1.55 仍是管理台真实 API 接入的一部分，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.56：生产检索鉴权与检索测试页真实上下文

完成事项：

- `/memory/search` 在 AuthService 配置后要求 `memory:read` PAT。
- `/memory/search` 不再信任请求体里的 `actor.user_id`，改为使用 PAT subject 覆盖请求 actor。
- 保留 tenant permission context 校验，跨项目无 membership 仍返回 403。
- `web/pages/search-test.vue` 移除硬编码 `user_1` 和 `archive_index_generation: 2`。
- 检索测试页从 localStorage 恢复当前 PAT、组织、项目、Agent 上下文。
- 检索测试页在未登录或未选择组织 / 项目时显示中文阻断提示。
- 检索测试页请求后端时留空 `actor.user_id`，由后端 PAT subject 接管。
- `cmd/memory-smoke` 适配新安全边界：普通 search smoke 支持 `SMOKE_SEARCH_PAT`，pipeline E2E 写入继续用 Adapter Token，检索改用 PAT。
- pipeline E2E 的 Postgres 临时 actor provisioning 同时签发短期 Adapter Token 和短期 PAT，并在 cleanup 中 revoke 两者。

修改模块：

- `internal/http/router.go`
- `internal/http/router_test.go`
- `internal/webdeploy/web_dockerfile_test.go`
- `web/pages/search-test.vue`
- `cmd/memory-smoke/main.go`
- `cmd/memory-smoke/main_test.go`
- `Makefile`
- `docs/production-delivery-log.md`

新增或变更测试：

- `TestMemorySearchRequiresPATWhenAuthConfigured`
- `TestMemorySearchUsesPATSubjectInsteadOfRequestActorUser`
- `TestSearchTestPageUsesAuthenticatedRuntimeContext`
- `TestMemorySearchSmokeUsesPATWhenConfigured`
- `TestPipelineE2ESmokeCanProvisionActorFromPostgresDSN` 更新为 TurnEvent/Search 分离 token。
- `TestPipelineE2ESmokeWritesTurnEventAndFindsArchiveChunk` 更新为 Adapter Token + PAT 双 token。

验证命令：

- 本地红灯：`go test ./internal/http -run 'TestMemorySearch(RequiresPATWhenAuthConfigured|UsesPATSubjectInsteadOfRequestActorUser)' -count=1` 实现前按预期失败，旧 handler 未要求 PAT 且信任请求 actor。
- 本地红灯：`go test ./internal/webdeploy -run TestSearchTestPageUsesAuthenticatedRuntimeContext -count=1` 实现前按预期失败，页面仍含 `user_id: 'user_1'`。
- 本地红灯：`go test ./cmd/memory-smoke -run 'TestMemorySearchSmokeUsesPATWhenConfigured|TestPipelineE2ESmoke(CanProvisionActorFromPostgresDSN|WritesTurnEventAndFindsArchiveChunk)' -count=1` 实现前失败，旧 smoke 没有 Search PAT 模型。
- 本地目标测试：`go test ./internal/http ./internal/webdeploy ./cmd/memory-api -count=1` 通过。
- 本地前端构建：`cd web && npm run build` 通过。
- 本地 smoke 相关测试：`go test ./cmd/memory-smoke ./internal/http ./internal/webdeploy ./cmd/memory-api -count=1` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器全量测试：`make test` 通过。
- 服务器前端构建：`make build-web` 通过。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp`、`memory-web`。
- 服务器第一次 post-deploy 验证暴露 smoke 旧鉴权假设，日志目录 `/tmp/memory-os-post-deploy.mpROsV`，失败点为 `/memory/search` 返回 `401 pat_required`。
- 修复 smoke 后服务器全量测试：`make test` 通过。
- 修复 smoke 后服务器 post-deploy 验证：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.HZb77P`。

浏览器验收：

- 打开 `http://ddns.08121.top:18080/search-test/`，未登录时被重定向到 `/login`。
- 注入短期浏览器验收 PAT 和最新归档所属组织 / 项目上下文后，页面正常进入检索测试页。
- 页面显示真实组织 ID、项目 ID 和 `codex` Agent。
- 点击“运行检索”后页面调用真实 `/memory/search`。
- 页面返回 `rerank_degraded: true`。
- 页面展示 `archive_chunk` source ref，包含 archive_id、chunk_id 和 source_event_ids。
- 页面返回 `access_log_count: 1`。
- 页面返回内容中的测试 Secret 已显示为 `secret_ref:...`，未展示原始明文。
- 浏览器验收已捕获页面状态；本地 Playwright 临时产物已清理，未保留临时截图文件。
- 浏览器 localStorage 中 PAT、operator email、org/project/agent 上下文已清理。
- 浏览器验收用短期 PAT 已撤销。
- 服务器临时 provisioning / cleanup 程序和 `/tmp/memory-os-browser-smoke.json` 已清理。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- `/memory/search` 的生产路径现在不会信任请求体伪造的 `actor.user_id`。
- 浏览器验收中的短期 PAT 已撤销，最终交付报告不记录明文 token。
- Smoke 中 TurnEvent 与 Search token 已分离：Adapter Token 只用于写事件，PAT 用于检索。
- `make secret-scan` 通过。

部署说明：

- 本轮重建 API/worker/MCP/Web，未改公开端口，未删除数据卷，未执行破坏性 migration。
- 第二次修复只影响 smoke 工具和 Makefile，未再次重建服务二进制。
- post-deploy 失败根因已修复为验证脚本适配问题，不是运行中服务不可用；修复后 post-deploy 完整通过。

剩余问题：

- 检索测试页已接真实 API 和真实上下文，但仍是测试页，不是完整检索治理 UI。
- 当前浏览器正向结果来自 pipeline E2E 归档数据，后续仍需补 Hot Memory 正向展示和跨 user/org/project/agent 的浏览器隔离用例。
- 总览页、归档页、热记忆页等仍需继续消除静态数据并做真实浏览器验收。
- Phase 1.56 仍是管理台真实 API 接入的一部分，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.57：Hot Memory 管理 API 与真实管理台页面

完成事项：

- 新增生产 Hot Memory 管理 API：
  - `POST /memory/hot-memory/create`
  - `POST /memory/hot-memory/list`
  - `POST /memory/hot-memory/promote`
  - `POST /memory/hot-memory/demote`
  - `POST /memory/hot-memory/mark-used`
  - `POST /memory/hot-memory/delete`
- OpenAPI 增加 Hot Memory 管理接口路径。
- `cmd/memory-api` production router options 注入 `HotMemoryService`。
- Hot Memory API 在服务未配置时返回 `503 hot_memory_not_configured`。
- Hot Memory API 在 AuthService 配置后要求 PAT。
- 创建 Hot Memory 时，生产路径使用 PAT subject 覆盖请求体里的 `user_id`。
- Hot Memory action 在更新前读取 memory 元数据并进行项目权限校验，避免越权更新后再检查。
- `web/pages/hot-memory/index.vue` 从静态假数据改为真实 API 页面。
- 页面支持创建、列表、提升、降权、标记使用和软删除。
- 修复生产 PG 路径新建 Hot Memory 初始 `hot_score=0` 的问题：service 在写 repository 前统一计算初始 hot_score。

修改模块：

- `internal/hotmemory/service.go`
- `internal/hotmemory/service_test.go`
- `internal/http/router.go`
- `internal/http/router_test.go`
- `internal/webdeploy/web_dockerfile_test.go`
- `cmd/memory-api/main.go`
- `cmd/memory-api/main_test.go`
- `web/pages/hot-memory/index.vue`
- `docs/production-delivery-log.md`

新增或变更测试：

- `TestHotMemoryAPIsRequireConfiguredServiceAndPAT`
- `TestHotMemoryCreateListAndLifecycleUsePATSubject`
- `TestHotMemoryPageUsesRealAPI`
- `TestServiceUpsertSetsInitialHotScoreBeforeRepository`
- `TestRouterOptionsConfiguresCoreServicesWhenPostgresPoolExists` 增加 `HotMemoryService` 注入断言。
- `TestRouterOptionsLeavesAuthOpenForDevelopmentSmoke` 增加 development smoke 不注入 HotMemoryService 断言。

验证命令：

- 本地红灯：`go test ./internal/http ./internal/webdeploy -run 'TestHotMemory|TestHotMemoryPageUsesRealAPI' -count=1` 实现前按预期失败，缺少 `HotMemoryService` route 注入且页面仍有 `hm_1` 静态假数据。
- 本地目标测试：`go test ./internal/http -run 'TestHotMemory|TestOpenAPIJSON' -count=1` 通过。
- 本地页面测试和前端构建：`go test ./internal/webdeploy -run TestHotMemoryPageUsesRealAPI -count=1 && cd web && npm run build` 通过。
- 本地相关包测试：`go test ./internal/hotmemory ./internal/http ./internal/webdeploy ./cmd/memory-api -count=1` 通过。
- 本地 hot_score 红灯：`go test ./internal/hotmemory -run TestServiceUpsertSetsInitialHotScoreBeforeRepository -count=1` 修复前按预期失败，repository 接收 `hot_score=0`。
- hot_score 修复后本地测试：`go test ./internal/hotmemory ./internal/http ./cmd/memory-api -count=1` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器全量测试：`make test` 通过。
- 服务器前端构建：`make build-web` 通过。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp`、`memory-web`。
- 服务器 post-deploy 验证：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.l3vsZy`。
- hot_score 修复后二次服务器全量测试：`make test` 通过。
- hot_score 修复后二次服务器 post-deploy 验证：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.dJh2dI`。

浏览器验收：

- 打开 `http://ddns.08121.top:18080/hot-memory/`，未登录时重定向到 `/login`。
- 注入短期浏览器验收 PAT 和最新归档所属组织 / 项目上下文后，页面正常进入 Hot Memory 管理页。
- 页面显示真实组织 / 项目 / agent 上下文。
- 页面初始空状态正常显示。
- 使用页面创建 Hot Memory，列表出现真实 `memory_id`。
- 首次浏览器验收发现新建项 `hot_score=0.00`，已作为生产 bug 修复。
- 修复部署后再次创建 Hot Memory，新建项 `hot_score=8.00`。
- 页面点击“提升”，切换到“已提升”过滤后能看到该项。
- 页面点击“降权”，切换到“已降权”过滤后能看到该项。
- 页面点击“标记使用”，页面显示 `used 1` 和 `access 1`。
- 页面点击“删除”后，该项不再出现在当前列表。
- 浏览器 localStorage 中 PAT、operator email、org/project/agent 上下文已清理。
- 浏览器验收用短期 PAT 已撤销。
- 服务器临时 provisioning / cleanup 程序和 `/tmp/memory-os-hot-browser-smoke.json` 已清理。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- Hot Memory 创建路径使用 Secret sanitizer；浏览器验收未写入真实 Secret。
- 浏览器验收中的短期 PAT 已撤销，最终交付报告不记录明文 token。
- Hot Memory action 在执行前做 project permission 和 owner 检查。

部署说明：

- 本轮重建 API/worker/MCP/Web，未改公开端口，未删除数据卷，未执行破坏性 migration。
- 浏览器验收创建了测试 Hot Memory，并通过 UI 软删除；按 stop condition 未执行物理删除。

剩余问题：

- Hot Memory 页面已完成基础治理操作，但还没有 source 详情、批量操作、审计日志入口和 Qdrant 向量状态展示。
- Hot Memory 当前页面列表只覆盖当前 PAT subject 的 project scope；agent_specific、user/org scope 的浏览器隔离矩阵还需要后续补充。
- 总览页和归档详情页仍需继续强化真实 API 验收。
- Phase 1.57 仍是管理台真实 API 接入的一部分，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.58：Archive 列表与详情治理页真实 API 化

完成事项：

- `web/pages/archive/index.vue` 从静态归档列表改为真实 `POST /memory/archive/list`。
- Archive 列表页使用当前 PAT、组织和项目上下文加载数据。
- Archive 列表页补齐登录提示、上下文缺失提示、加载态、错误态、空状态和状态过滤。
- `web/pages/archive/[id].vue` 从静态正文和 source refs 改为真实 Archive 详情治理页。
- Archive 详情页接入：
  - `POST /memory/archive/detail`
  - `POST /memory/archive/versions`
  - `POST /memory/archive/edit`
  - `POST /memory/archive/reindex`
  - `POST /memory/archive/delete`
- Archive 详情页支持真实正文编辑、版本历史查看、重建索引和软删除。
- 新增静态回归测试，防止 Archive 页面重新引入 `archive_1`、`archive_2`、演示中文标题和静态 source refs。

修改模块：

- `web/pages/archive/index.vue`
- `web/pages/archive/[id].vue`
- `internal/webdeploy/web_dockerfile_test.go`
- `docs/production-delivery-log.md`

新增或变更测试：

- `TestArchivePagesUseRealAPI`

验证命令：

- 本地红灯：`go test ./internal/webdeploy -run TestArchivePagesUseRealAPI -count=1` 实现前按预期失败，失败点为 `archive_1` 静态演示数据。
- 本地窄测试：`go test ./internal/webdeploy -run TestArchivePagesUseRealAPI -count=1` 通过。
- 本地相关包测试：`go test ./internal/webdeploy ./internal/http ./cmd/memory-api -count=1` 通过。
- 本地前端构建：`cd web && npm run build` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器相关包测试：`PATH=/usr/local/go/bin:$PATH GOPROXY=https://goproxy.cn,direct go test ./...` 通过。
- 服务器前端构建：`cd web && npm run build` 通过。
- 服务器 secret scan：`make secret-scan` 通过。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp`、`memory-web`。
- 服务器 post-deploy 验证：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.EsDRhT`。

浏览器验收：

- In-app Browser 控制通道对公网导航连续超时，停止机械重试后切换 Chrome 控制插件完成验收。
- Chrome 打开 `http://ddns.08121.top:18080/archive/`，未登录时重定向到 `/login`。
- 通过登录页真实输入短期 PAT 进入管理台。
- 浏览器显示真实组织 / 项目 / agent 上下文。
- Archive 列表页展示临时真实 Archive：`Browser Archive Smoke ...`，版本 1，索引代次 1。
- Archive 列表页未出现静态演示文案 `部署记录` 和 `Adapter 对话转写`。
- Archive 详情页展示真实 metadata、Markdown 正文和版本历史。
- 在浏览器中编辑 Markdown 正文并点击“保存并生成新版本”，页面刷新后显示版本 2、索引代次 2，版本历史出现“管理台手动修订”。
- 在浏览器中点击“触发重建索引”，页面刷新后显示索引代次 3。
- 在浏览器中点击“软删除 Archive”，页面刷新后显示状态“已删除”，版本历史保留。
- 浏览器验收用短期 PAT 已撤销。
- Chrome 临时标签已关闭。
- 服务器临时 provisioning 程序 `tmp/browser-archive-provision` 已删除。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- 浏览器验收写入的 Archive 内容为安全测试文本，不包含真实 Secret。
- 浏览器验收中的短期 PAT 已撤销，最终交付报告不记录明文 token。
- PostgreSQL 仍未对宿主机开放；临时 provision 改用 Docker 内部网络访问 `postgres:5432`。
- 本轮部署未改公开端口，未删除数据卷，未执行破坏性 migration。

剩余问题：

- Archive 页面已完成列表、详情、编辑、版本、重索引和软删除基础治理，但还没有创建 Archive 的管理台入口。
- Archive 详情页暂未展示 RAG index job 细节、chunk 列表和审计日志入口。
- In-app Browser 对公网导航不稳定，本轮已用 Chrome 完成浏览器验收；后续可继续优先尝试 In-app Browser，失败时按记录切换 Chrome。
- Phase 1.58 仍是管理台真实 API 接入的一部分，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.59：Archive 创建入口与审计入口

完成事项：

- Archive 列表页新增“创建 Archive”表单。
- 创建入口调用真实 `POST /memory/archive/create`。
- 创建入口用 `manual_archive_request` TurnEvent 包装用户输入，生成 Markdown Archive。
- 创建成功后自动刷新列表，显示新 Archive 的版本和索引代次。
- Archive 详情页新增“审计日志”区域。
- 审计日志区域调用真实 `POST /memory/audit/list`。
- 审计日志按 `resource_type: 'archive'` 和当前 `archive_id` 过滤。
- 通用审计日志为空时展示明确空状态，并提示版本历史仍保留编辑与删除审计线索。
- 扩展 `TestArchivePagesUseRealAPI`，防止 Archive 创建入口和审计入口退回静态实现。

修改模块：

- `web/pages/archive/index.vue`
- `web/pages/archive/[id].vue`
- `internal/webdeploy/web_dockerfile_test.go`
- `docs/production-delivery-log.md`

新增或变更测试：

- `TestArchivePagesUseRealAPI` 增加 `/memory/archive/create`、`manual_archive_request`、`/memory/audit/list` 和 `resource_type: 'archive'` 断言。

验证命令：

- 本地红灯：`go test ./internal/webdeploy -run TestArchivePagesUseRealAPI -count=1` 实现前按预期失败，失败点为缺少 `/memory/archive/create`。
- 本地窄测试：`go test ./internal/webdeploy -run TestArchivePagesUseRealAPI -count=1` 通过。
- 本地相关包测试：`go test ./internal/webdeploy ./internal/http ./cmd/memory-api -count=1` 通过。
- 本地前端构建：`cd web && npm run build` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器全量测试：`PATH=/usr/local/go/bin:$PATH GOPROXY=https://goproxy.cn,direct go test ./...` 通过。
- 服务器前端构建：`cd web && npm run build` 通过。
- 服务器 secret scan：`make secret-scan` 通过。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp`、`memory-web`。
- 服务器 post-deploy 验证：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.HoG6UK`。

浏览器验收：

- Chrome 打开 `http://ddns.08121.top:18080/archive/`，未登录时重定向到 `/login`。
- 通过登录页真实输入短期 PAT 进入管理台。
- 浏览器显示真实临时组织 / 项目 / agent 上下文。
- Archive 列表页显示“创建 Archive”表单和“创建真实 Archive”按钮。
- 在浏览器中填写标题 `Browser UI Archive Create Smoke` 和安全测试正文。
- 点击“创建真实 Archive”后，页面显示 `Archive 已创建：archive_manual_...`。
- 新 Archive 出现在列表中，状态活跃，版本 1，索引代次 1。
- 打开新 Archive 详情页，显示真实 metadata、Markdown 正文、版本历史和“审计日志”区域。
- 审计日志区域显示真实 `/memory/audit/list` 入口和当前 Archive 的空状态。
- 将浏览器创建的测试 Archive 通过详情页软删除，状态变为“已删除”，版本历史保留。
- 浏览器验收用短期 PAT 已撤销。
- Chrome 临时标签已关闭。
- 服务器临时 provisioning 程序 `tmp/browser-archive-create-provision` 已删除。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- 浏览器验收写入的 Archive 内容为安全测试文本，不包含真实 Secret。
- 浏览器验收中的短期 PAT 已撤销，最终交付报告不记录明文 token。
- PostgreSQL 仍未对宿主机开放；临时 provision 使用 Docker 内部网络访问 `postgres:5432`。
- 本轮部署未改公开端口，未删除数据卷，未执行破坏性 migration。

剩余问题：

- Archive 详情页有通用审计日志入口，但 Archive 编辑/删除的专用版本审计仍主要通过“版本历史”展示，后续可补专用 edit audit 展示。
- Archive 创建入口当前是手动归档表单，尚未提供批量导入、从 TurnEvent 选择归档或模板化归档。
- Archive 页面还没有展示 RAG index job 明细和 chunk 列表。
- Phase 1.59 仍是管理台真实 API 接入的一部分，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.60：Archive 详情页 RAG 索引状态

完成事项：

- 新增 `POST /memory/archive/index-status`。
- 新接口先读取 Archive 元数据，再复用 Archive 项目授权逻辑校验 `memory:read`。
- 新接口按当前 `archive_id + index_generation` 聚合 `archive_index_jobs`、`archive_chunks` 和 `qdrant_points` 状态。
- Archive 详情页新增“RAG 索引状态”区域。
- 页面展示当前 Archive 当前索引代次的索引任务、Archive Chunk、Qdrant Point 状态。
- 页面提供“刷新索引状态”按钮，不再让用户只能从全局 Qdrant 页面猜测单个 Archive 的索引进度。
- OpenAPI 新增 `/memory/archive/index-status` 路径。

修改模块：

- `internal/qdrant/status.go`
- `internal/http/router.go`
- `internal/http/router_test.go`
- `internal/webdeploy/web_dockerfile_test.go`
- `web/pages/archive/[id].vue`
- `docs/production-delivery-log.md`

新增或变更测试：

- 新增 `TestArchiveIndexStatusRequiresArchivePermission`，覆盖未登录 401、越权 403、授权后返回当前 Archive 索引状态。
- 扩展 `TestArchivePagesUseRealAPI`，要求 Archive 详情页调用 `/memory/archive/index-status` 并展示 `RAG 索引状态`。

验证命令：

- 本地红灯：`go test ./internal/http -run TestArchiveIndexStatusRequiresArchivePermission -count=1` 实现前按预期失败，失败点为新接口 404。
- 本地红灯：`go test ./internal/webdeploy -run TestArchivePagesUseRealAPI -count=1` 实现前按预期失败，失败点为缺少 `/memory/archive/index-status`。
- 本地窄测试：`go test ./internal/http -run TestArchiveIndexStatusRequiresArchivePermission -count=1` 通过。
- 本地相关包测试：`go test ./internal/http ./internal/qdrant ./internal/webdeploy -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地前端构建：`cd web && npm run build` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器相关包测试：`PATH=/usr/local/go/bin:$PATH go test ./internal/http ./internal/qdrant ./internal/webdeploy -count=1` 通过。
- 服务器全量测试：`PATH=/usr/local/go/bin:$PATH go test ./...` 通过。
- 服务器前端构建：`cd web && npm run build` 通过。
- 服务器 secret scan：`make secret-scan` 通过。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp`、`memory-web`。
- 服务器健康检查：`curl -fsS http://127.0.0.1:18081/healthz` 通过，返回 `db`、`redis`、`qdrant` 均为 `ok`。
- 服务器 OpenAPI 检查：`curl -fsS http://127.0.0.1:18081/openapi.json | grep /memory/archive/index-status` 通过。
- 服务器 post-deploy 验证：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.OAwMNO`。
- Web 静态产物检查：`docker exec deploy-memory-web-1 grep -R 'RAG 索引状态\|/memory/archive/index-status' /usr/share/nginx/html/_nuxt` 通过。
- Web 访问检查：`curl -fsSL http://127.0.0.1:18080/archive` 返回 Nuxt 静态页面。

失败命令、根因和修复：

- 首次服务器 `make prod-up` 失败，根因为当前 shell 未注入 compose 必需变量，compose 在构建前拒绝执行。
- 第二次部署后 `memory-api` 启动失败，日志为 `SECRET_VAULT_KEY_B64 invalid`。
- 根因为从现有容器继承环境变量时使用 `awk -F=` 截断了 base64 末尾的 `=` padding。
- 修复方式：改用只删除变量名前缀的读取方式，并验证补回 padding 后可解码为 32 字节，不更换 Vault key，不修改数据库。
- 数据库中存在 3 条 Secret 记录，因此没有生成新 Vault key，避免造成已存 Secret 不可解密。

部署状态：

- `deploy-memory-api-1`、`deploy-memory-worker-1`、`deploy-memory-mcp-1`、`deploy-memory-web-1` 均已运行。
- 公开端口保持不变：Web `18080`、API `18081`、MCP `18082`、Qdrant `18083`。
- 本轮未删除数据卷，未执行破坏性 migration。

浏览器 / 页面验收：

- In-app Browser 在公网导航时超时并重置，未继续消耗时间。
- 改用服务器侧 Web 访问和 nginx 静态产物检查确认新前端已发布。
- 由于本轮是只读状态展示切片，未创建新的浏览器临时 PAT，未写入新的业务测试数据。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- 命令输出只记录环境变量存在性和长度，不输出明文值。
- Secret Vault key 未更换，避免影响现有 Secret 解密。
- 新接口只返回状态计数、Archive ID、index_generation 和时间，不返回 chunk 正文、payload、Secret 或 token。
- 新接口必须通过 Archive 所属项目授权后才能访问，避免把全局 Qdrant 聚合数据泄露到 Archive 详情页。

剩余问题：

- Archive 详情页现在展示聚合状态，但还没有展开具体 chunk 列表、失败 job 错误明细和重试入口。
- `make prod-up` 依赖调用方提前注入生产环境变量；服务器目前没有 `.env`，后续应补安全的运维加载方式，避免再次从容器环境继承。
- In-app Browser 对公网页面仍不稳定，后续完整 UI 验收可继续优先 Chrome 或稳定的浏览器控制通道。
- Phase 1.60 仍是管理台真实 API 接入的一部分，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.61：Archive 索引诊断明细

完成事项：

- `POST /memory/archive/index-status` 在原有聚合状态基础上新增索引任务明细。
- 索引任务明细包含 `idempotency_key`、`status`、`error_message`、`attempts`、`max_attempts`、锁定状态和时间字段。
- `POST /memory/archive/index-status` 新增 Archive chunk 明细。
- Chunk 明细包含 `chunk_id`、`chunk_index`、`vector_status`、`content_hash`、标题路径、来源事件 ID、Qdrant point ID 和 Qdrant vector 状态。
- Chunk 明细不返回 Markdown 正文，不返回 Qdrant payload，避免泄露正文和敏感字段。
- Archive 详情页新增“索引任务明细”区域，展示失败原因和尝试次数。
- Archive 详情页新增“Chunk 明细”区域，展示 chunk 与 Qdrant point 的对应关系。
- 新增 PGStatusStore 级测试，在配置 `POSTGRES_TEST_DSN` 时可验证真实 SQL 查询明细。

修改模块：

- `internal/qdrant/status.go`
- `internal/qdrant/status_test.go`
- `internal/http/router.go`
- `internal/http/router_test.go`
- `internal/webdeploy/web_dockerfile_test.go`
- `web/pages/archive/[id].vue`
- `docs/production-delivery-log.md`

新增或变更测试：

- `TestArchiveIndexStatusRequiresArchivePermission` 增加 `index_jobs` 和 `archive_chunks` 响应断言。
- 新增 `TestPGStatusStoreArchiveIndexStatsReturnsJobAndChunkDetails`，覆盖真实 PG 查询的 job/chunk/point 明细。
- `TestArchivePagesUseRealAPI` 增加 `indexStatus.index_jobs`、`indexStatus.archive_chunks`、`失败原因`、`Chunk 明细` 约束。

验证命令：

- 本地红灯：`go test ./internal/http -run TestArchiveIndexStatusRequiresArchivePermission -count=1` 实现前按预期失败，失败点为缺少 `ArchiveIndexJobStatus` / `ArchiveChunkStatus` 类型和字段。
- 本地红灯：`go test ./internal/webdeploy -run TestArchivePagesUseRealAPI -count=1` 实现前按预期失败，失败点为缺少 `indexStatus.index_jobs`。
- 本地相关包测试：`go test ./internal/http ./internal/qdrant ./internal/webdeploy -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地前端构建：`cd web && npm run build` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器相关包测试：`PATH=/usr/local/go/bin:$PATH go test ./internal/http ./internal/qdrant ./internal/webdeploy -count=1` 通过。
- 服务器全量测试：`PATH=/usr/local/go/bin:$PATH go test ./...` 通过。
- 服务器前端构建：`cd web && npm run build` 通过。
- 服务器 secret scan：`make secret-scan` 通过。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp`、`memory-web`。
- 服务器健康检查：`curl -fsS http://127.0.0.1:18081/healthz` 通过，返回 `db`、`redis`、`qdrant` 均为 `ok`。
- Web 静态产物检查：`docker exec deploy-memory-web-1 grep -R '索引任务明细\|Chunk 明细\|失败原因' /usr/share/nginx/html/_nuxt` 通过。
- 服务器 post-deploy 验证：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.dUPwPH`。

部署状态：

- `deploy-memory-api-1`、`deploy-memory-worker-1`、`deploy-memory-mcp-1`、`deploy-memory-web-1` 均已运行。
- 公开端口保持不变：Web `18080`、API `18081`、MCP `18082`、Qdrant `18083`。
- 本轮未删除数据卷，未执行破坏性 migration。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- 命令输出不包含生产密钥明文。
- 新增接口明细不返回 chunk 正文、不返回 Qdrant payload、不返回 Secret 或 token。
- 新增明细继续复用 Archive 所属项目授权，未授权用户无法查看索引诊断数据。

剩余问题：

- Archive 详情页已经展示聚合状态、失败 job 错误明细和 chunk 明细，但还没有失败 job 重试入口。
- `make prod-up` 仍依赖调用方提前注入生产环境变量；服务器目前没有 `.env`，后续应补安全的运维加载方式。
- In-app Browser 对公网页面仍不稳定，后续完整 UI 验收可继续优先 Chrome 或稳定的浏览器控制通道。
- Phase 1.61 仍是管理台真实 API 接入的一部分，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.62：Archive 失败索引任务重试入口

完成事项：

- 新增 PG Archive RAG 索引队列的失败任务重试能力。
- 新增 `POST /memory/archive/index-retry`，用于重试当前 Archive 当前 `index_generation` 下的失败索引任务。
- 重试语义固定为只恢复 `status = failed` 的当前代次任务，不生成新的 `index_generation`，不编辑 Markdown，不删除数据。
- 重试时把匹配的 failed job 恢复为 `pending`，清空错误、锁定字段、完成时间，并把 attempts 重置为 0。
- 当确实恢复了 failed job 时，同步把当前代次非 stale 的 `archive_chunks` 和对应 `qdrant_points` 的 `vector_status` 设回 `pending`，让 worker 可以重新处理。
- 新接口复用 Archive metadata 和项目写权限校验；未登录用户访问真实 Archive 返回 401。
- Archive 详情页新增“重试失败索引任务”按钮，仅在当前状态存在 failed job 时启用，点击后调用真实 API 并刷新索引状态。
- OpenAPI 增加 `/memory/archive/index-retry` 路由描述。

修改模块：

- `internal/jobs/pg_archive_index_queue.go`
- `internal/jobs/pg_archive_index_queue_test.go`
- `internal/http/router.go`
- `internal/http/router_test.go`
- `internal/webdeploy/web_dockerfile_test.go`
- `web/pages/archive/[id].vue`
- `docs/production-delivery-log.md`

新增或变更测试：

- `TestPGArchiveIndexQueueRequiresPool` 增加 `RetryFailed` 空 pool 保护断言。
- 新增 `TestPGArchiveIndexQueueRetryFailedResetsCurrentGeneration`，覆盖 failed job 恢复、attempts/error/lock 清理、chunk/qdrant point 状态重置、二次调用幂等和重新 lease。
- 新增 `TestArchiveIndexRetryRequiresWritePermission`，覆盖未登录 401、项目角色不足 403、owner 成功重试和响应字段。
- `TestArchivePagesUseRealAPI` 增加 `/memory/archive/index-retry`、`retryIndexJobs`、`重试失败索引任务` 约束。

验证命令：

- 本地红灯：`go test ./internal/jobs ./internal/http ./internal/webdeploy -count=1` 实现前按预期失败，失败点为 `PGArchiveIndexQueue.RetryFailed` 缺失、`/memory/archive/index-retry` 未注册、前端缺少 retry 标记。
- 本地窄测试：`go test ./internal/jobs ./internal/http ./internal/webdeploy -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地前端构建：`cd web && npm run build` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器窄测试：`PATH=/usr/local/go/bin:$PATH go test ./internal/jobs ./internal/http ./internal/webdeploy -count=1` 通过。
- 服务器全量测试：`PATH=/usr/local/go/bin:$PATH go test ./...` 通过。
- 服务器前端构建：`cd web && npm run build` 通过。
- 服务器 secret scan：`make secret-scan` 通过。
- 服务器生产重建：`make prod-up` 通过，重新创建 `memory-api`、`memory-worker`、`memory-mcp`、`memory-web`。
- 服务器健康检查：`curl -fsS http://127.0.0.1:18081/healthz` 通过。
- 服务器 OpenAPI 检查：`curl -fsS http://127.0.0.1:18081/openapi.json` 后确认包含 `/memory/archive/index-retry`。
- Web 静态产物检查：`docker exec deploy-memory-web-1 grep -R '重试失败索引任务\|/memory/archive/index-retry' /usr/share/nginx/html/_nuxt` 通过。
- 服务器 post-deploy 验证：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.6OK0UV`。
- 线上认证探测：对真实存在的 Archive 未登录调用 `POST /memory/archive/index-retry` 返回 401，响应 `{"error":"pat_required"}`。

失败命令、根因和修复：

- 首次服务器 `docker compose -f ... ps` 失败，根因为服务器 Docker CLI 未启用 compose v2 子命令；改用项目既有 `docker-compose`/`make` 路径。
- 首次 `docker-compose ... ps` 失败，根因为服务器没有 `.env`，compose 需要 `SECRET_VAULT_KEY_ID` 等变量插值；未继续直接重试。
- 首次 `make post-deploy-verify` 失败在 `compose-ps`，日志目录 `/tmp/memory-os-post-deploy.cy5NE7`，根因为脚本内部同样需要 compose 环境变量。
- 修复方式：复用现有运行中容器环境变量进行本次验证和部署，不打印变量值，不轮换 Secret Vault key；随后 `make post-deploy-verify` 通过。
- 未登录探测第一次使用不存在的 archive_id，返回 400 `archive not found`，不能证明认证保护；随后改用数据库中真实 active archive_id，返回 401 `pat_required`。

部署状态：

- `deploy-memory-api-1`、`deploy-memory-worker-1`、`deploy-memory-mcp-1`、`deploy-memory-web-1` 均已运行。
- `deploy-postgres-1`、`deploy-redis-1` 仍为 healthy；`deploy-qdrant-1` 正常运行。
- 公开端口保持不变：Web `18080`、API `18081`、MCP `18082`、Qdrant `18083`。
- 本轮未删除数据卷，未执行 migration，未改公网端口。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- 命令输出没有展示生产密钥明文。
- 新接口不返回 chunk 正文、不返回 Qdrant payload、不返回 Secret 或 token。
- 新接口对真实 Archive 未登录访问返回 401，项目角色不足测试返回 403。
- 重试操作只影响当前 Archive 当前 generation 下 failed job 和可重建索引状态，不影响 Markdown 正文权威源。

剩余问题：

- `make prod-up` 和 `make post-deploy-verify` 仍依赖调用方提前注入生产环境变量；服务器目前没有 `.env`，后续应补安全的运维加载方式，避免从容器环境继承。
- 当前 retry 是 Archive/generation 级别，不支持按单个 job 或单个 chunk 选择性重试；这对当前单 Archive 当前代次恢复足够，后续可按运维需求扩展。
- In-app Browser 对公网页面仍不稳定，后续完整 UI 验收可继续优先 Chrome 或稳定的浏览器控制通道。
- Phase 1.62 仍是管理台真实 API 接入与 RAG 运维闭环的一部分，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.63：生产部署环境加载稳定化

完成事项：

- 新增 `scripts/load-prod-env.sh`，统一为生产 compose 命令加载必需环境变量。
- `make prod-up` 自动 source `scripts/load-prod-env.sh`，不再需要每次手工粘贴长串 export。
- `scripts/post-deploy-verify.sh` 自动 source `scripts/load-prod-env.sh`，`make post-deploy-verify` 不再因为 compose 插值缺少变量而失败。
- env 加载顺序为：优先读取 `MEMORY_OS_ENV_FILE` 指定文件，其次读取仓库根目录 `.env.production` / `.env`，最后在已有生产容器运行时从 `deploy-memory-api-1` 和 `deploy-postgres-1` 继承当前 shell 环境变量。
- 从运行中容器继承环境只导出到当前 shell，不打印变量值，不写回文件，不轮换 Secret Vault key。
- 对 `SECRET_VAULT_KEY_B64` 自动补齐 base64 padding，避免再次出现因为 `=` padding 被截断导致 API 启动失败。

修改模块：

- `Makefile`
- `scripts/load-prod-env.sh`
- `scripts/post-deploy-verify.sh`
- `internal/verify/verify_script_test.go`
- `docs/production-delivery-log.md`

新增或变更测试：

- 新增 `TestProductionCommandsLoadEnvironmentWithoutInliningSecrets`，约束 `prod-up` 和 `post-deploy-verify` 必须 source env loader。
- 同一测试约束 env loader 不包含 `echo $POSTGRES_PASSWORD`、`echo $LLM_API_KEY`、`echo $SECRET_VAULT_KEY_B64`、`cat > .env`、`tee .env` 等危险标记。
- post-deploy verify 既有测试改为显式注入测试假环境变量，避免本地无生产 env 时误判脚本逻辑失败。

验证命令：

- 本地红灯：`go test ./internal/verify -run TestProductionCommandsLoadEnvironmentWithoutInliningSecrets -count=1` 实现前按预期失败，失败点为缺少 `scripts/load-prod-env.sh`。
- 本地 verify 包测试：`go test ./internal/verify -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地脚本语法检查：`bash -n scripts/load-prod-env.sh scripts/post-deploy-verify.sh` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器 verify 包测试：`PATH=/usr/local/go/bin:$PATH go test ./internal/verify -count=1` 通过。
- 服务器 secret scan：`make secret-scan` 通过。
- 服务器 post-deploy 验证：直接执行 `make post-deploy-verify` 通过，未手工导出 compose 变量，日志目录 `/tmp/memory-os-post-deploy.7xWA8H`。
- 服务器生产重建：直接执行 `make prod-up` 通过，未手工导出 compose 变量。
- 部署后健康检查：`curl -fsS http://127.0.0.1:18081/healthz` 通过。
- 部署后 post-deploy 验证：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.FUMn5V`。

部署状态：

- `deploy-memory-api-1`、`deploy-memory-worker-1`、`deploy-memory-mcp-1` 已重建并运行。
- `deploy-memory-web-1` 继续运行。
- `deploy-postgres-1`、`deploy-redis-1` 为 healthy；`deploy-qdrant-1` 正常运行。
- 公开端口保持不变：Web `18080`、API `18081`、MCP `18082`、Qdrant `18083`。
- 本轮未删除数据卷，未执行 migration，未写入真实 Secret 文件。

安全检查：

- 本轮没有把真实 Secret、API key、密码、私钥或 cookie 写入仓库、日志或回复。
- `scripts/load-prod-env.sh` 只导出当前 shell 变量，不打印值，不写 `.env`。
- 服务器验证过程中没有输出生产密钥明文。
- Secret Vault key 未轮换，避免影响已有 Secret 解密。

剩余问题：

- 当前 loader 支持从运行中容器继承环境，适合已部署服务器的日常重建；全新机器首次启动仍需要通过 `MEMORY_OS_ENV_FILE` 或 shell 环境安全提供生产变量。
- 后续可以在运维文档中补充推荐的 `/etc/memory-os/production.env` 权限和创建流程，但实际写入真实 Secret 文件前仍应单独确认。
- Phase 1.63 是部署稳定性改进，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.64：组织 / 项目软删除治理入口

完成事项：

- 为 Tenant 组织和项目新增 `status` 字段，默认 `active`。
- 新增 forward-only migration `000015_tenant_soft_delete.sql`，只添加字段和索引，不删除数据。
- Tenant repository / service 支持组织和项目软删除，语义为 `status = deleted`。
- 组织 / 项目列表默认只返回 active 资源。
- 删除项目后，`PermissionContext` 不再允许继续使用该项目。
- 删除操作不物理删除 membership，不破坏审计与历史关联数据。
- 新增 `POST /memory/tenant/orgs/delete`。
- 新增 `POST /memory/tenant/projects/delete`。
- 删除接口复用 PAT `memory:write` 和 org owner/admin 权限校验；未登录 401，权限不足 403。
- Nuxt 组织管理页新增“删除组织”按钮，调用真实 API 并刷新列表。
- Nuxt 项目管理页新增“删除项目”按钮，调用真实 API 并刷新列表。
- OpenAPI 增加组织 / 项目删除接口描述。

修改模块：

- `internal/tenant/model.go`
- `internal/tenant/repository.go`
- `internal/tenant/service.go`
- `internal/tenant/pg_repository.go`
- `internal/tenant/service_test.go`
- `internal/tenant/pg_repository_test.go`
- `internal/http/router.go`
- `internal/http/router_test.go`
- `internal/webdeploy/web_dockerfile_test.go`
- `migrations/000015_tenant_soft_delete.sql`
- `migrations/migrations_test.go`
- `web/stores/context.ts`
- `web/pages/orgs/index.vue`
- `web/pages/projects/index.vue`
- `docs/production-delivery-log.md`

新增或变更测试：

- 新增 `TestServiceSoftDeletesTenantResources`，覆盖 member 不能删除、owner 删除项目 / 组织、列表过滤 deleted、删除项目后权限上下文拒绝。
- 新增 `TestPGRepositorySoftDeletesTenantResources`，覆盖真实 PG 软删除语义和 membership 不被物理删除。
- `TestPGRepositoryRequiresPool` 增加 DeleteProject / DeleteOrg 空 pool 保护断言。
- 新增 `TestTenantOrgProjectDeleteRequiresWritePermission`，覆盖未登录 401、member 403、owner 删除成功、列表过滤 deleted。
- 新增 `TestTenantPagesUseRealDeleteAPI`，约束组织 / 项目页面必须调用真实删除 API。
- 新增 `TestTenantSoftDeleteMigrationContainsRequiredColumnsAndIndexes`，约束 migration 只做新增字段 / 索引且不包含 DROP / DELETE 破坏性语句。

验证命令：

- 本地红灯：`go test ./internal/tenant ./internal/http ./internal/webdeploy ./migrations -count=1` 实现前按预期失败，失败点为缺少 DeleteProject / DeleteOrg、缺少 delete 路由、前端缺少删除 API、缺少 migration。
- 本地相关包测试：`go test ./internal/tenant ./internal/http ./internal/webdeploy ./migrations -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地前端构建：`cd web && npm run build` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器相关包测试：`PATH=/usr/local/go/bin:$PATH go test ./internal/tenant ./internal/http ./internal/webdeploy ./migrations -count=1` 通过。
- 服务器全量测试：`PATH=/usr/local/go/bin:$PATH go test ./...` 通过。
- 服务器前端构建：`cd web && npm run build` 通过。
- 服务器 secret scan：`make secret-scan` 通过。
- 服务器生产重建：`make prod-up` 通过。
- 服务器 post-deploy 验证：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.SzgiiU`。
- 服务器 DB schema 检查：`information_schema.columns` 确认 `orgs.status` 与 `projects.status` 均存在。
- 服务器 OpenAPI 检查：确认 `/memory/tenant/orgs/delete` 与 `/memory/tenant/projects/delete` 均存在。
- Web 静态产物检查：确认发布产物包含 `删除组织`、`删除项目`、`/memory/tenant/orgs/delete`、`/memory/tenant/projects/delete`。

部署状态：

- `deploy-memory-api-1`、`deploy-memory-worker-1`、`deploy-memory-mcp-1`、`deploy-memory-web-1` 均已重建并运行。
- `deploy-postgres-1`、`deploy-redis-1` 为 healthy；`deploy-qdrant-1` 正常运行。
- 公开端口保持不变：Web `18080`、API `18081`、MCP `18082`、Qdrant `18083`。
- 本轮未删除数据卷，未执行破坏性 migration，未物理删除组织 / 项目 / membership 数据。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- 删除接口不返回 Secret、token、hash 或内部堆栈。
- 删除接口使用 PAT 和 org 写权限校验，member 无法删除组织或项目。
- migration 只添加字段和索引，不包含 `DROP TABLE`、`DROP COLUMN`、`DELETE FROM orgs`、`DELETE FROM projects`。

剩余问题：

- 本轮只实现软删除，没有实现组织 / 项目重命名编辑；后续仍需补编辑入口。
- 组织软删除后，当前实现通过 `orgs.status = deleted` 让列表不可见，但没有级联把项目 status 改为 deleted；项目列表因 join org active 条件也不可见。后续可按治理需求增加显式级联状态展示。
- 完整浏览器自动点击验收仍待稳定浏览器控制通道继续补。
- Phase 1.64 仍是 Tenant 治理能力切片，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.65：Tenant 组织 / 项目编辑重命名

完成事项：

- Tenant repository / service 新增组织和项目编辑能力，只允许修改 `name` 与 `slug`。
- 编辑操作复用 org owner/admin 写权限；未登录返回 401，member 权限不足返回 403。
- deleted 组织 / 项目不可编辑，不恢复已删除资源，不改变 ID、status 或 membership。
- PostgreSQL repository 只更新 `status = active` 的组织 / 项目，并更新 `updated_at`。
- 项目编辑额外要求所属组织仍为 active，避免软删除组织下的项目被继续治理。
- 新增 `POST /memory/tenant/orgs/edit`。
- 新增 `POST /memory/tenant/projects/edit`。
- OpenAPI 增加组织 / 项目编辑接口描述。
- Nuxt 组织管理页新增“编辑组织 / 保存组织 / 取消”行内编辑入口，调用真实 API 后刷新列表。
- Nuxt 项目管理页新增“编辑项目 / 保存项目 / 取消”行内编辑入口，调用真实 API 后刷新列表。

修改模块：

- `internal/tenant/repository.go`
- `internal/tenant/service.go`
- `internal/tenant/pg_repository.go`
- `internal/tenant/service_test.go`
- `internal/tenant/pg_repository_test.go`
- `internal/http/router.go`
- `internal/http/router_test.go`
- `internal/webdeploy/web_dockerfile_test.go`
- `web/pages/orgs/index.vue`
- `web/pages/projects/index.vue`
- `docs/production-delivery-log.md`

新增或变更测试：

- 新增 `TestServiceUpdatesTenantResources`，覆盖 member 不能编辑、owner 编辑成功、列表刷新可见新 name/slug、deleted 资源不可编辑。
- 新增 `TestPGRepositoryUpdatesTenantResources`，覆盖真实 PostgreSQL 更新 active 资源和拒绝更新 deleted 资源。
- `TestPGRepositoryRequiresPool` 增加 UpdateOrg / UpdateProject 空 pool 保护断言。
- 新增 `TestTenantOrgProjectEditRequiresWritePermission`，覆盖未登录 401、member 403、owner 编辑成功、列表反映编辑结果。
- 新增 `TestTenantPagesUseRealEditAPI`，约束组织 / 项目页面必须调用真实 edit API 并提供保存按钮。

验证命令：

- 本地红灯：`go test ./internal/tenant ./internal/http ./internal/webdeploy -count=1` 实现前按预期失败，失败点为缺少 UpdateOrg / UpdateProject、缺少 edit 路由、前端缺少 edit API marker。
- 本地相关包测试：`go test ./internal/tenant ./internal/http ./internal/webdeploy -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地前端构建：`cd web && npm run build` 通过。
- 服务器全量测试：`make test` 通过。
- 服务器前端构建：`cd web && npm run build` 通过。
- 服务器生产重建：`make prod-up` 通过。
- 服务器 post-deploy 验证：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.tNj0xN`。
- 服务器 OpenAPI 检查：确认 `/memory/tenant/orgs/edit` 与 `/memory/tenant/projects/edit` 均存在。
- Web 静态产物检查：确认发布产物包含 `保存组织`、`保存项目`、`/memory/tenant/orgs/edit`、`/memory/tenant/projects/edit`。
- 服务器运行时路由检查：无 token 调用 `/memory/tenant/orgs/edit` 与 `/memory/tenant/projects/edit` 均返回 401，而不是 404。

部署状态：

- `deploy-memory-api-1`、`deploy-memory-worker-1`、`deploy-memory-mcp-1`、`deploy-memory-web-1` 已通过 `make prod-up` 重建并启动。
- post-deploy 验收已完成，包含 version、healthz、OpenAPI、smoke 和 Pipeline E2E。
- 公开端口保持不变：Web `18080`、API `18081`、MCP `18082`、Qdrant `18083`。
- 本轮没有新增 migration，没有物理删除数据，没有修改数据卷。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- 编辑接口不返回 Secret、token、hash 或内部堆栈。
- 编辑接口使用 PAT 和 org 写权限校验，member 无法编辑组织或项目。
- 未引入新的日志敏感字段；OpenAPI 只暴露路由和摘要。

剩余问题：

- 本轮只实现组织 / 项目 name 与 slug 编辑；成员角色编辑、组织恢复、项目恢复仍未实现。
- 完整浏览器自动点击验收仍待浏览器控制通道继续补；本轮完成了服务器运行时 API / OpenAPI / Web 静态产物验证。
- Phase 1.65 仍是 Tenant 治理能力切片，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.66：Tenant 成员角色治理与软移除

完成事项：

- Tenant repository / service 新增成员角色更新能力。
- Tenant repository / service 新增成员软移除能力，语义为 `membership.status = disabled`，不物理删除 membership。
- 成员角色限定为现有 `owner`、`admin`、`member`，拒绝未知角色。
- 成员角色更新和软移除复用 org owner/admin 写权限；普通 member 不能治理成员。
- 软移除后，`PermissionContext` 会因 membership 非 active 拒绝该成员继续访问项目。
- 新增 `POST /memory/tenant/memberships/update-role`。
- 新增 `POST /memory/tenant/memberships/remove`。
- OpenAPI 增加成员角色更新和成员移除接口描述。
- Nuxt 项目管理页新增“当前项目成员”治理区，支持成员列表、添加成员、保存角色、移除成员。
- 本轮按提速策略只同步到服务器工作区并完成代码级验证，暂不单独 `prod-up`；后续与下一个核心切片合并部署。

修改模块：

- `internal/tenant/repository.go`
- `internal/tenant/service.go`
- `internal/tenant/pg_repository.go`
- `internal/tenant/service_test.go`
- `internal/tenant/pg_repository_test.go`
- `internal/http/router.go`
- `internal/http/router_test.go`
- `internal/webdeploy/web_dockerfile_test.go`
- `web/pages/projects/index.vue`
- `docs/production-delivery-log.md`

新增或变更测试：

- 新增 `TestServiceManagesMembershipRoleAndRemoval`，覆盖 member 不能治理、owner 更新角色、owner 软移除、移除后权限上下文拒绝。
- 新增 `TestPGRepositoryManagesMembershipRoleAndRemoval`，覆盖真实 PostgreSQL role 更新、status disabled、membership 不物理删除。
- `TestPGRepositoryRequiresPool` 增加 UpdateMembershipRole / RemoveMembership 空 pool 保护断言。
- 新增 `TestTenantMembershipRoleAndRemoveRequiresWritePermission`，覆盖未登录 401、member 403、owner 更新角色成功、owner 移除成员成功、列表反映 disabled。
- 新增 `TestProjectPageUsesRealMembershipGovernanceAPI`，约束项目页必须调用真实成员治理 API。

验证命令：

- 本地红灯：`go test ./internal/tenant ./internal/http ./internal/webdeploy -count=1` 实现前按预期失败，失败点为缺少 UpdateMembershipRole / RemoveMembership、缺少 update/remove 路由、项目页缺少成员治理 API。
- 本地相关包测试：`go test ./internal/tenant ./internal/http ./internal/webdeploy -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地前端构建：`cd web && npm run build` 通过。
- 服务器同步源码：已同步到 `thinkpad:/opt/memory-os`。
- 服务器全量测试：`make test` 通过。
- 服务器前端构建：`cd web && npm run build` 通过。
- 服务器 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。

部署状态：

- 本轮未执行 `make prod-up`，避免每个小切片都重建镜像拖慢整体交付。
- 服务器工作区已包含本轮代码，下一次核心切片完成后统一执行 `make prod-up` 与 `make post-deploy-verify`。
- 未新增 migration，没有物理删除数据，没有修改公开端口。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- 成员软移除只修改 membership 状态，不删除审计关联数据。
- 成员治理接口使用 PAT 和 org 写权限校验，member 无法更新角色或移除成员。
- 前端只展示用户 ID、角色和状态，不展示 token、hash、Secret 或内部错误堆栈。

剩余问题：

- 本轮代码尚未部署到运行中容器；需要和下一核心切片合并部署后再做 OpenAPI / Web 产物 / 运行时 API 验收。
- 成员治理当前只支持已有用户 ID，不包含用户搜索或邀请流程。
- 完整浏览器点击验收仍待统一补齐。
- Phase 1.66 仍是生产可用版核心治理切片，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.67：Backup / Restore Manifest 完整性校验

完成事项：

- `scripts/backup.sh` 的 `manifest.json` 新增核心备份产物 sha256：
  - PostgreSQL dump。
  - Markdown Archive tarball。
  - Qdrant snapshot。
- `scripts/backup.sh` 的 manifest 新增 Qdrant snapshot 相对文件路径，方便恢复前定位和审计。
- `scripts/restore.sh` 在 dry-run 和真实 restore 前均要求存在 `manifest.json`。
- `scripts/restore.sh` 在执行任何恢复命令前校验 PostgreSQL、Archive、Qdrant 三类产物 sha256。
- checksum 不一致时立即拒绝恢复，并输出 `checksum mismatch`，避免损坏或篡改备份进入恢复链路。
- checksum 实现不依赖 `jq`，兼容 `sha256sum` 和 macOS / BSD `shasum -a 256`。

修改模块：

- `scripts/backup.sh`
- `scripts/restore.sh`
- `internal/backup/backup_script_test.go`
- `internal/restore/restore_script_test.go`
- `docs/production-delivery-log.md`

新增或变更测试：

- `TestBackupScriptDryRunCreatesAuditableBackup` 增加 manifest sha256 和 Qdrant snapshot 文件路径断言。
- 新增 `TestRestoreScriptRejectsBackupChecksumMismatch`，覆盖恢复前校验失败必须停止。
- restore fixture 增加 `manifest.json`，使 dry-run 恢复也必须走完整性校验。

验证命令：

- 本地红灯：`go test ./internal/backup ./internal/restore -count=1` 实现前按预期失败，失败点为 manifest 缺少 sha256、restore 不拒绝 checksum mismatch。
- 本地目标测试：`go test ./internal/backup ./internal/restore -count=1` 通过。
- 本地全量测试：`go test ./...` 通过。
- 本地前端构建：`cd web && npm run build` 通过。
- 本地 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。
- 服务器同步源码：已同步到 `thinkpad:/opt/memory-os`。
- 服务器全量测试：`make test` 通过。
- 服务器前端构建：`cd web && npm run build` 通过。
- 服务器 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。

部署状态：

- 本轮修改的是脚本和测试，未执行 `make prod-up`。
- 未执行真实恢复，未修改生产数据库、Archive volume 或 Qdrant 数据。
- 后续统一部署 / 验收时需要执行一次 dry-run backup / dry-run restore，并在专门测试环境完成真实恢复演练。

安全检查：

- manifest 只记录文件路径和 checksum，不记录数据库密码、token、Secret、私钥或 cookie。
- restore checksum 校验发生在真实恢复确认和恢复命令执行前，降低错误备份写入生产的风险。
- 本轮没有读取或输出真实 `POSTGRES_DSN`、`SECRET_VAULT_KEY_B64` 或 token。

剩余问题：

- 仍需在最终交付前执行实际备份产物生成，并把 `manifest.json`、PostgreSQL dump、Archive tarball、Qdrant snapshot 的存在性和 checksum 作为交付证据。
- 仍需在测试环境执行真实 restore 后运行 `make smoke`，本轮只完成脚本级完整性防护。
- Phase 1.67 是备份恢复安全基座切片，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.68：Backup / Restore Dry-Run 验收入口

完成事项：

- 新增 `make backup-restore-dry-run`，一键执行 dry-run 备份和 dry-run 恢复审计。
- 该入口只设置 `DRY_RUN=1`，不包含 `CONFIRM_RESTORE=I_UNDERSTAND`，不能误触真实恢复。
- dry-run 备份会生成 PostgreSQL dump 占位文件、Markdown Archive tarball、Qdrant snapshot 占位文件和 manifest checksum。
- dry-run 恢复会基于刚生成的 `BACKUP_DIR` 校验 manifest checksum，并输出恢复审计命令。
- 服务器执行时可通过 `BACKUP_ROOT` 和 `RESTORE_AUDIT_DIR` 指向 `/tmp`，避免污染项目目录。

修改模块：

- `Makefile`
- `internal/verify/verify_script_test.go`
- `docs/production-delivery-log.md`

新增或变更测试：

- 新增 `TestMakefileExposesBackupRestoreDryRunTarget`，约束 Makefile 必须暴露安全 dry-run 入口。
- 测试同时断言 dry-run 入口调用 `scripts/backup.sh`、`scripts/restore.sh`，并禁止真实恢复确认标记进入该入口。

验证命令：

- 本地红灯：`go test ./internal/verify -run TestMakefileExposesBackupRestoreDryRunTarget -count=1` 实现前按预期失败，失败点为缺少 `backup-restore-dry-run` target。
- 本地目标测试：`go test ./internal/verify -run TestMakefileExposesBackupRestoreDryRunTarget -count=1` 通过。
- 本地相关包测试：`go test ./internal/verify -count=1` 通过。
- 本地 dry-run 实测：`make backup-restore-dry-run` 通过，输出 `backup-restore dry-run completed`。
- 本地全量测试：`go test ./...` 通过。
- 服务器同步源码：已同步 `Makefile` 和 `internal/verify/verify_script_test.go` 到 `thinkpad:/opt/memory-os`。
- 服务器全量测试：`make test` 通过。
- 服务器 dry-run 实测：`BACKUP_ROOT=$(mktemp -d) RESTORE_AUDIT_DIR=$(mktemp -d)/restore make backup-restore-dry-run` 通过。
- 服务器 secret scan：`make secret-scan` 通过，输出 `secret scan ok`。

部署状态：

- 本轮未执行 `make prod-up`，因为修改的是 Makefile 验收入口和测试，不需要重建运行中容器。
- 未执行真实 restore，未修改生产数据库、Archive volume 或 Qdrant 数据。
- 本轮曾在本地生成 dry-run restore 审计目录，已清理；服务器 dry-run 产物位于 `/tmp` 临时目录。

安全检查：

- 新入口不包含真实恢复确认值，不会绕过 `scripts/restore.sh` 的真实恢复保护。
- 本轮没有写入或输出真实 Secret、数据库密码、token、私钥或 cookie。
- backup / restore 证据只包含文件路径、checksum 和命令文本，不包含敏感明文。

剩余问题：

- 最终交付前仍需在隔离测试环境执行真实 restore 并运行 `make smoke`。
- 当前入口是 dry-run 验收门禁，不等于生产备份恢复全链路已完成。
- Phase 1.68 继续补强交付证据链，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.69：合并部署与运行时验收

完成事项：

- 将 Phase 1.66 成员治理、Phase 1.67 备份恢复 manifest 完整性、Phase 1.68 backup/restore dry-run 验收入口合并部署到服务器。
- 部署前检查端口 `18080`、`18081`、`18082`、`18083`，占用者均为当前 Memory OS Docker proxy。
- `make preflight` 因当前生产容器已占用端口返回端口占用；经 `docker ps` 确认不是外部冲突，而是现有 Memory OS 服务。
- 执行 `make prod-up` 重建并启动 `memory-api`、`memory-worker`、`memory-mcp`、`memory-web`。
- 执行 `make post-deploy-verify`，完成 compose、version、healthz、OpenAPI、smoke、pipeline-e2e 运行时验收。
- 运行时确认 OpenAPI 已发布 `/memory/tenant/memberships/update-role` 与 `/memory/tenant/memberships/remove`。
- 运行时确认生产环境 `/dev/smoke/archive` 返回 `404`，dev smoke endpoint 未暴露。
- 运行时确认核心容器处于 Up 状态，PostgreSQL 和 Redis 为 healthy。

验证命令：

- 端口检查：`ss -tlnp | grep -E ':(18080|18081|18082|18083)\b'`，占用者为 `docker-proxy`。
- 容器归属检查：`docker ps --format ...`，确认端口来自 `deploy-memory-web-1`、`deploy-memory-api-1`、`deploy-memory-mcp-1`、`deploy-qdrant-1`。
- 部署命令：`make prod-up` 通过，后端和 Web 镜像构建成功，容器重建并启动。
- 运行时验收：`make post-deploy-verify` 通过，输出 `post deploy verify completed`，日志目录 `/tmp/memory-os-post-deploy.PDvpcM`。
- OpenAPI 运行时检查：`curl -fsS http://127.0.0.1:18081/openapi.json | grep -E 'memberships/(update-role|remove)'` 通过。
- dev endpoint 关闭检查：`curl -X POST http://127.0.0.1:18081/dev/smoke/archive` 返回 `404`。
- 容器状态检查：`docker ps` 显示 `deploy-memory-web-1`、`deploy-memory-api-1`、`deploy-memory-worker-1`、`deploy-memory-mcp-1`、`deploy-postgres-1`、`deploy-qdrant-1`、`deploy-redis-1` 运行中。

部署状态：

- 服务器 `thinkpad:/opt/memory-os` 已运行本轮合并后的镜像。
- 未修改公开端口。
- 未删除容器数据卷。
- 未执行真实 restore。

安全检查：

- `make prod-up` 使用 `APP_ENV=production ENABLE_DEV_ENDPOINTS=false`。
- 生产 dev smoke endpoint 已通过运行时 `404` 验证。
- 本轮部署输出未包含真实 Secret 明文。

剩余问题：

- 需要继续补浏览器自动验收，尤其是项目成员治理区的真实 UI 操作。
- 需要继续补真实备份产物生成和隔离环境 restore 演练。
- 需要继续推进 Archive / Hot Memory / Unified Retrieval 的完整生产闭环。
- 本轮只是合并部署和运行时验收，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.70：Web 轻量浏览器验收

完成事项：

- 使用 in-app browser 打开线上 Web：`http://ddns.08121.top:18080/`。
- 页面自动进入 `/login`，可见中文登录页文案：
  - `MEMORY OS 登录`
  - `登录管理台`
  - `当前后端使用 Personal Access Token 访问管理 API`
  - `进入记忆控制台`
- 新构建页面脚本为 `/_nuxt/l3Re4QsW.js`，与部署后容器静态产物一致。
- 服务器侧确认 `/_nuxt/builds/latest.json` 返回 `Content-Type: application/json`，body 为合法 JSON。
- 服务器侧确认容器内存在 `/_nuxt/builds/latest.json` 和 `/_nuxt/builds/meta/<id>.json`。

浏览器观察：

- 首次打开时浏览器控制台保留了部署切换瞬间的旧 Nuxt 错误：
  - 旧脚本：`/_nuxt/BUYcWDXh.js`
  - 错误：旧 app manifest 请求被 HTML fallback 影响，提示 malformed app manifest。
- 强刷新后页面加载新脚本 `/_nuxt/l3Re4QsW.js`。
- 新标签干净加载后仍显示中文登录页，未观察到新的页面不可用问题。

根因判断：

- 当前静态产物和 nginx 对 `/_nuxt/builds/latest.json` 的响应正确。
- 该错误更符合部署切换期间浏览器旧客户端缓存旧 JS，旧 JS 请求已不存在的旧 build manifest，被 SPA fallback 返回 HTML。
- 当前新会话未复现页面不可用，但后续仍应优化静态资源缓存策略，避免部署切换期旧客户端噪音。

验证命令：

- 浏览器加载：in-app browser 打开 `http://ddns.08121.top:18080/`，页面跳转 `/login` 并显示中文登录页。
- 静态产物检查：`docker exec deploy-memory-web-1 find /usr/share/nginx/html -maxdepth 4 -type f | grep -E 'builds|manifest|_nuxt'`。
- manifest 响应检查：`curl -sS -D - http://127.0.0.1:18080/_nuxt/builds/latest.json`，返回 `Content-Type: application/json`。

剩余问题：

- 本轮只是只读加载验收，没有执行登录、成员治理、归档、检索等写操作。
- 后续需要使用浏览器控制工具做完整 UI 主链路验收，并把每页真实 API 操作纳入交付证据。
- 建议后续补 nginx 针对 `/_nuxt/builds/` 缺失 JSON 的明确 404 或缓存策略测试，减少旧客户端部署切换期误报。

## 2026-07-03 Phase 1.71：Tenant Web 主链路验收与 membership migration 修复

完成事项：

- 使用浏览器真实登录页输入 Personal Access Token，进入管理台。
- 浏览器创建新组织，并刷新验证组织持久化。
- 浏览器创建新项目，并刷新验证项目持久化。
- 浏览器在项目成员治理区添加第二用户为项目成员，并刷新验证成员持久化。
- 浏览器验证成员角色治理时发现真实 P1 缺陷：
  - `POST /memory/tenant/memberships/update-role` 返回 `403`。
  - 响应根因包含 `column "updated_at" of relation "memberships" does not exist`。
  - 代码中的 PG repository 已写 `updated_at = now()`，但既有生产数据库 schema 缺少该列。
- 按 TDD 补充迁移测试，并新增 forward-only migration：
  - `migrations/000016_membership_updated_at.sql`
  - 只执行 `ALTER TABLE memberships ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();`
- 部署后确认 migration `16` 已应用，`memberships.updated_at` 已存在。
- 重新验证成员角色更新 API，从 `403` 修复为 `200`。
- 浏览器刷新项目页，确认第二用户角色从 `member` 变为 `admin`。
- 浏览器点击“移除成员”，刷新后确认成员状态为 `disabled`，符合软删除语义。
- 数据库确认被移除成员最终状态为 `admin|disabled`。
- UI 验收 PAT 已撤销，本地临时 token 文件已清理。

修改模块：

- `migrations/000016_membership_updated_at.sql`
- `migrations/migrations_test.go`
- `docs/production-delivery-log.md`

新增或变更测试：

- 新增 `TestTenantMembershipUpdatedAtMigrationContainsRequiredColumn`，防止后续再出现 repository 使用列但 migration 未补齐的问题。
- 测试同时拒绝 `DROP TABLE`、`DROP COLUMN`、`DELETE FROM memberships` 等破坏性语句。

验证命令：

- 本地红灯：`go test ./migrations -run TestTenantMembershipUpdatedAtMigrationContainsRequiredColumn -count=1` 实现前按预期失败，失败点为缺少 `000016_membership_updated_at.sql`。
- 本地迁移测试：`go test ./migrations -count=1` 通过。
- 本地相关测试：`go test ./internal/tenant ./internal/http -count=1` 通过。
- 服务器全量测试：`make test` 通过。
- 部署命令：`make prod-up` 通过，API 重启并应用嵌入 migration。
- schema 验证：
  - `schema_migrations` 存在 version `16`。
  - `information_schema.columns` 确认 `memberships.updated_at` 存在。
- API 复验：`POST /memory/tenant/memberships/update-role` 返回 `200`，响应 role 为 `admin`。
- 浏览器复验：项目页刷新后目标成员 role 显示为 `admin`。
- 浏览器移除：目标成员刷新后 status 显示为 `disabled`。
- 数据库复验：目标成员 role/status 为 `admin|disabled`。
- 部署后验收：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.OvEZPZ`。
- 安全扫描：`make secret-scan` 通过，输出 `secret scan ok`。

部署状态：

- 服务器 `thinkpad:/opt/memory-os` 已运行包含 migration 000016 的新 API 镜像。
- 未删除数据卷。
- 未执行破坏性 migration。
- 未修改公开端口。

安全检查：

- UI 验收 PAT 只用于本轮浏览器验收，明文未写入仓库、日志或交付报告。
- 数据库只保存 PAT hash。
- 验收结束后 PAT 已撤销。
- 本地 `/tmp` 临时 token 文件和 curl payload / response 文件已清理。

剩余问题：

- Tenant 组织 / 项目 / 成员治理主链路已完成浏览器验收，但用户搜索 / 邀请流程仍未实现。
- 后续仍需继续浏览器验收 Secret Vault、Adapter Token、Archive、Hot Memory、Qdrant 状态和检索测试页。
- Phase 1.71 修复了一个真实 P1 schema 缺陷，但不能声明 v0.4 完成。

## 2026-07-03 Phase 1.72：Secret Vault 与 Token 管理浏览器验收

完成事项：

- 创建短期 UI 安全验收用户、组织、项目和 PAT；PAT 明文只短暂保存在本地 `/tmp`，数据库只保存 hash。
- 使用浏览器真实登录页输入 PAT，进入管理台。
- 浏览器打开 `Secret Vault` 页面，确认页面显示真实组织 / 项目上下文。
- 浏览器创建 Secret：
  - 使用一次性假明文。
  - 创建成功后刷新页面。
  - Secret 名称和 `secret_ref` metadata 可见。
  - 明文字段刷新后为空。
  - 页面正文未出现假明文。
- 浏览器禁用 Secret：
  - active 列表刷新后不再显示该 Secret。
  - 切换 disabled 列表后显示该 Secret，状态为 `disabled`。
  - disabled 列表仍不显示假明文。
- 数据库确认 Secret 版本保存为 ciphertext / nonce：
  - 最新验收 Secret 状态为 `disabled`。
  - `secret_versions.ciphertext` 和 `nonce` 存在。
- 浏览器打开 `Adapter Token` 页面，确认页面显示真实组织 / 项目上下文。
- 浏览器创建 Adapter Token：
  - 创建响应出现一次性明文面板。
  - 点击“我已保存，立即隐藏”后刷新页面。
  - 刷新后一次性面板不再显示。
  - 刷新后页面只显示 Adapter Token metadata。
  - 页面正文不再出现 Adapter Token 明文模式。
- 浏览器撤销 Adapter Token：
  - 刷新后状态显示 `codex · revoked`。
  - 服务端确认最近 Adapter Token 状态为 revoked。
- 浏览器创建 PAT：
  - 创建响应出现一次性明文面板。
  - metadata 中出现新 PAT 名称。
  - 点击隐藏并刷新后 PAT 明文不再显示。
- 浏览器撤销新建 PAT：
  - 刷新后该 PAT 状态显示 `revoked`。
  - 当前登录 PAT 未误撤销，直到验收结束才撤销。
- 验收结束后撤销本轮登录 PAT，并清理本地 `/tmp` token 文件。

修改模块：

- `docs/production-delivery-log.md`

验证命令：

- 浏览器验收：真实登录页输入 PAT，进入 `Secret Vault` 和 `Adapter Token` 页面完成上述写入、刷新和撤销流程。
- Secret 数据库检查：
  - 最新验收 Secret 输出 `disabled|1|62|12`，代表 status/version/ciphertext bytes/nonce bytes。
- Token 数据库检查：
  - `adapter_tokens` 表不存在 plain 字段。
  - `personal_access_tokens` 表不存在 plain 字段。
  - 最近 Adapter Token 状态为 `revoked`。
  - Secret 名称中没有验收假明文标记。
- 部署后验收：`make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.9wfxOR`。
- 安全扫描：`make secret-scan` 通过，输出 `secret scan ok`。
- 验收 PAT 清理：本轮登录 PAT 已更新为 `revoked`，本地临时 token 文件已删除。

部署状态：

- 本轮未修改后端或前端代码，未执行 `make prod-up`。
- 运行中服务保持 Phase 1.71 部署版本。
- 未修改公开端口。
- 未删除数据卷。

安全检查：

- Secret 明文只使用一次性假值，未写入回复、日志、交付文档或数据库文本字段。
- Secret 列表和 disabled 列表均只展示 metadata。
- Adapter Token 和 PAT 明文只在创建响应中显示一次，隐藏并刷新后不可见。
- Token 表没有明文字段。
- 本轮验收凭据已撤销。

剩余问题：

- Secret Vault 已完成浏览器创建、禁用、metadata-only 验收；尚需后续验证禁用后注入链路拒绝使用。
- Adapter Token / PAT 已完成浏览器创建、隐藏、撤销验收；尚需后续通过 Adapter 写入 TurnEvent 验证绑定 token 的实际 ingest 链路。
- 后续继续验收 Archive、Hot Memory、Qdrant 状态和检索测试页。
- Phase 1.72 是安全治理页面验收切片，不能声明 v0.4 完成。

## 2026-07-03 Phase 1.73：Archive RAG 主链路修复、Hot Memory 与 Unified Search 验收

完成事项：

- 创建短期 UI 主链路验收用户、组织、项目和 PAT；PAT 明文只保存在本地 `/tmp` 临时文件，数据库只保存 hash。
- 使用浏览器真实登录页输入 PAT，进入管理台。
- 浏览器确认顶部组织 / 项目上下文自动选中本轮验收组织和项目。
- 浏览器打开 `Markdown 归档库` 页面，创建真实 Archive。
- 浏览器刷新归档列表，确认新 Archive 持久化存在。
- 浏览器打开 Archive 详情页，确认 Markdown 文件路径、状态、版本 1、索引代次 1 和版本历史可见。
- 浏览器编辑 Archive 正文并保存，确认页面显示版本 2、索引代次 2，textarea value 包含编辑验收标记。
- 浏览器触发 Archive 重建索引时发现真实 P1 缺陷：
  - 页面返回 `archive_reindex_enqueue_failed`。
  - 根因是 `archive_chunks.heading_path` / `source_event_ids` 为 NOT NULL 数组列，但队列写入 nil slice 时 pgx 发送 SQL NULL。
- 按 TDD 增加回归测试：
  - `TestPGArchiveIndexQueueEnqueueStoresEmptyChunkArrays`
  - 修复前在服务器隔离测试库中失败，错误为 `null value in column "heading_path" ... violates not-null constraint`。
- 修复 `PGArchiveIndexQueue` 写入边界：
  - nil `HeadingPath` / `SourceEventIDs` 统一规范化为空数组。
  - 新增测试完成自身 job，避免污染同包队列测试。
- 部署后复测 Archive reindex：
  - API 返回 `index_generation: 4`、`chunks: 1`。
  - 数据库当前代次 index job 为 `completed`。
  - 当前代次 chunk 对应 Qdrant point 为 `indexed`。
  - 浏览器刷新 Archive 详情页后显示索引代次 4，索引任务 `completed`，Qdrant point `indexed`。
- 浏览器打开 Hot Memory 页面并点击创建 Hot Memory；浏览器控制等待阶段超时，但后端确认该 UI 点击已创建成功。
- 通过真实 API 验证 Hot Memory 生命周期：
  - `promote` 返回状态 `promoted`。
  - `mark-used` 后 `used_count/access_count` 从 0 增加到 1。
  - `demote` 返回状态 `demoted`。
  - 创建单独临时 Hot Memory 后调用 `delete`，返回状态 `deleted`。
- 通过 `/memory/search` 验证 Unified Retrieval：
  - 搜索本轮 Hot Memory marker 返回 2 条结果。
  - 来源包含 `hot_memory` 和 `archive_chunk`。
  - `context` 包含 Hot Memory marker。
  - `marked_used_count` 为 1。
  - `retrieval_requests` 写入 1 条。
  - `retrieval_results` 写入 2 条，来源分别为 `hot_memory` 和 `archive_chunk`。
- 通过 `/memory/qdrant/status` 验证 Qdrant 状态：
  - collection 为 `memory_os`。
  - collection 状态为 `green`。
  - `query_time_filter_enforced` 为 `true`。
  - required payload fields 包含 `doc_type`、`user_id`、`org_id`、`project_id`、`visibility`、`permission_labels`、`index_generation`。
  - `points_by_status.indexed` 为 41。

修改模块：

- `internal/jobs/pg_archive_index_queue.go`
- `internal/jobs/pg_archive_index_queue_test.go`
- `docs/production-delivery-log.md`

新增或变更测试：

- 新增 `TestPGArchiveIndexQueueEnqueueStoresEmptyChunkArrays`，防止 Markdown chunk 无 heading/source refs 时重建索引入队失败。
- 测试使用真实 PostgreSQL schema，覆盖 NOT NULL 数组列写入行为。

验证命令：

- 红灯复现：
  - 在服务器隔离测试库执行 `go test ./internal/jobs -run TestPGArchiveIndexQueueEnqueueStoresEmptyChunkArrays -count=1`。
  - 修复前失败：`null value in column "heading_path" of relation "archive_chunks" violates not-null constraint`。
- 绿灯验证：
  - 隔离测试库执行 `go test ./internal/jobs -run TestPGArchiveIndexQueueEnqueueStoresEmptyChunkArrays -count=1` 通过。
  - 隔离测试库执行 `go test ./internal/jobs -count=1` 通过。
  - 服务器执行 `make test` 通过。
- 部署：
  - 服务器执行 `make prod-up` 通过。
  - `curl http://127.0.0.1:18081/healthz` 返回 db/qdrant/redis 均 ok。
  - `make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.Md7Bus`。
- Archive RAG 复验：
  - `POST /memory/archive/reindex` 返回 `archive_id`、`index_generation: 4`、`chunks: 1`。
  - 数据库确认当前 Archive 代次 job 为 `completed`。
  - 数据库确认当前 Archive 代次 Qdrant point 为 `indexed`。
  - 浏览器刷新 Archive 详情页显示索引代次 4、job `completed`、Qdrant point `indexed`。
- Unified Retrieval 复验：
  - `POST /memory/search` 返回来源 `hot_memory` 和 `archive_chunk`。
  - `retrieval_requests` / `retrieval_results` 写入成功。
- Qdrant 状态复验：
  - `POST /memory/qdrant/status` 返回 `query_time_filter_enforced: true` 和完整 required payload fields。

部署状态：

- 服务器 `thinkpad:/opt/memory-os` 已运行包含 Archive RAG 入队修复的新镜像。
- API、worker、MCP 容器已重建并重启。
- Web 容器保持运行。
- 未修改公开端口。
- 未执行破坏性 migration。
- 临时隔离测试数据库已清理。

安全检查：

- 本轮只使用一次性非敏感测试内容。
- PAT 明文未写入仓库、交付日志或回复。
- Secret 明文没有参与本轮 Archive/Hot Memory/Search 测试。
- Unified Search 使用 PAT 主体覆盖请求 actor user_id，并在当前 org/project 权限上下文下检索。

剩余问题：

- Hot Memory 页面创建按钮的业务请求已成功，但浏览器控制会话在等待阶段连续超时；本轮未完成 Hot Memory 页面刷新后可见状态、按钮 promote/demote/delete 的浏览器点击验收。
- Search 测试页和 Qdrant 状态页本轮通过 API 验证，尚需恢复浏览器控制后完成页面级验收截图 / DOM 证据。
- 全局 Qdrant 状态仍显示历史 `failed` index jobs 为 2；本轮 Archive 当前代次无 failed，需要后续追踪历史 failed job 来源并决定是否重试或归档为 P3/P2。
- Phase 1.73 修复了一个真实 P1，但不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.74：Hot Memory、Qdrant 状态与检索测试页浏览器验收

完成事项：

- 创建新的短期 UI 页面验收用户、组织、项目和 PAT；数据库只保存 PAT hash。
- 使用独立浏览器控制工具打开生产 Web 登录页，并通过真实登录表单进入管理台。
- 登录后页面顶部组织 / 项目上下文显示本轮验收组织和项目：
  - `UI Page Org 1783034425080809000`
  - `UI Page Project 1783034425080809000`
- 浏览器打开 `Hot Memory 管理` 页面，确认页面使用当前组织 / 项目上下文。
- 浏览器在 Hot Memory 页面执行真实创建：
  - 创建事实 `ui2-hot-1783034425080809000`。
  - 创建后页面列表显示 memory id、状态 `活跃`、`used 0`、`access 0`。
- 浏览器点击 `标记使用`：
  - 页面刷新后显示 `used 1`、`access 1`。
- 浏览器点击 `提升`：
  - 页面当前活跃过滤条件下该 memory 消失。
  - 数据库确认该 memory 最终状态为 `promoted`。
- 浏览器创建第二条 Hot Memory 并点击 `降权`：
  - 页面当前活跃过滤条件下该 memory 消失。
  - 数据库确认该 memory 最终状态为 `demoted`。
- 浏览器创建第三条 Hot Memory 并点击 `删除`：
  - 页面当前活跃过滤条件下该 memory 消失。
  - 数据库确认该 memory 最终状态为 `deleted`。
- 浏览器打开 `Qdrant 状态` 页面，确认页面真实展示：
  - Collection 为 `memory_os`。
  - 状态为 `green`。
  - Point 统计为 `41 points`。
  - Vector config 为 `1024 · Cosine`。
  - Query-Time Filter 显示 `已强制`。
  - required payload fields 显示 `doc_type`、`user_id`、`org_id`、`project_id`、`visibility`、`permission_labels`、`index_generation`。
  - 索引任务显示 `completed 45`、`failed 2`。
  - Qdrant point 状态显示 `indexed 41`。
- 浏览器打开 `检索测试` 页面：
  - 页面显示当前组织 / 项目 / Agent。
  - 输入 `ui2-hot-1783034425080809000`。
  - 点击 `运行检索`。
  - 页面展示 `/memory/search` 真实返回 JSON。
  - 页面显示 `rerank_degraded: true`。
  - context 包含 `ui2-hot-1783034425080809000`。
  - results 来源为 `hot_memory`。
  - 页面显示 `access_log_count: 1` 和 `marked_used_count: 1`。
- 数据库复核：
  - 第一条 Hot Memory 最终为 `promoted`，`used_count/access_count` 为 `2/2`。
  - 第二条 Hot Memory 最终为 `demoted`。
  - 第三条 Hot Memory 最终为 `deleted`。
  - `retrieval_requests` 写入 `web-search-1783034797887`。
  - `retrieval_results` 中 `hot_memory` 来源写入 1 条。

修改模块：

- `docs/production-delivery-log.md`

验证命令：

- 浏览器页面验收：
  - 生产登录页：`http://ddns.08121.top:18080/login`
  - Hot Memory 页面：`http://ddns.08121.top:18080/hot-memory`
  - Qdrant 状态页面：`http://ddns.08121.top:18080/qdrant`
  - 检索测试页面：`http://ddns.08121.top:18080/search-test`
- API 健康检查：
  - `curl http://127.0.0.1:18081/healthz` 返回 db/qdrant/redis 均 ok。
- 数据库核对：
  - 查询 `hot_memories` 确认 promoted / demoted / deleted 状态。
  - 查询 `retrieval_requests` 和 `retrieval_results` 确认检索审计落库。

部署状态：

- 本轮未修改运行时代码，未执行 `make prod-up`。
- 运行服务保持 Phase 1.73 部署版本。
- 未修改公开端口。
- 未执行 migration。
- 未删除数据卷。

安全检查：

- 本轮使用短期 PAT 登录浏览器；验收完成后撤销。
- PAT 明文不写入交付日志或最终回复。
- Hot Memory / Search 使用非敏感测试内容。

剩余问题：

- Qdrant 状态页仍显示历史 failed index jobs 为 2；本轮页面验收只证明当前状态可见，后续仍需追踪 failed job 来源。
- Hot Memory 页面当前没有直接查看 deleted 过滤条件，删除通过“活跃列表消失 + 数据库状态 deleted”证明；后续可补 disabled/deleted 状态过滤 UI。
- Search 测试页已验证 Hot Memory 召回；Archive Chunk 召回在 Phase 1.73 已通过 API 和 Archive 详情页证明，后续可在页面上构造 Archive marker 再跑一次混合来源展示。

## 2026-07-03 Phase 1.75：Adapter Token 到 Archive RAG / Search 生产闭环验收

完成事项：

- 继续推进生产级闭环，不从头重来。
- 创建短期 E2E 用户、组织、项目、PAT 和 Adapter Token；数据库只保存 token hash。
- 使用 Adapter Token 调用生产接口 `POST /memory/turn-event`。
- 发现并修复真实 P1：
  - 普通 TurnEvent 没有 sanitizer warnings 时，`event.Warnings` 为 nil slice。
  - PG repository 写入 `turn_event_payloads.warnings` 时传入 SQL NULL。
  - 数据库字段为 `TEXT[] NOT NULL`，导致生产入口返回 400。
- 按 TDD 补充 PG repository 回归测试：
  - `TestPGRepositoryStoresEmptyWarningsArray`
  - 红测复现 `SQLSTATE 23502`。
  - 修复后转绿。
- 修复后重新部署 API / worker / MCP / Web。
- 重新执行 Adapter E2E：
  - 第一次写入返回 `accepted`、`deduped:false`。
  - 重复相同 request/event 返回 `accepted`、`deduped:true`。
- Worker 链路自动完成：
  - `turn_events` 写入 1 条。
  - `archive_jobs` 状态为 `completed`。
  - `archives` 状态为 `active`。
  - `archive_chunks` 生成 1 条。
  - `archive_index_jobs` 状态为 `completed`。
  - `qdrant_points` 生成 1 条。
- 使用 PAT 调用生产 `POST /memory/search`：
  - 返回 1 条结果。
  - `context` 包含 E2E marker。
  - 结果来源为 `archive_chunk`。
  - source refs 包含 `archive_id`、`chunk_id`、`source_event_ids`。
  - `access_log_count: 1`。
- 使用 PAT 调用生产 `POST /memory/qdrant/status`：
  - Collection 为 `memory_os`。
  - Collection 状态为 `green`。
  - `query_time_filter_enforced: true`。
  - required payload fields 包含 `doc_type`、`user_id`、`org_id`、`project_id`、`visibility`、`permission_labels`、`index_generation`。
  - `index_jobs_by_status` 为 `completed: 48`，本轮确认没有 failed index job。
- 数据库复核：
  - `retrieval_requests` 写入 1 条。
  - `memory_access_logs` 写入 1 条。
  - `retrieval_results` 写入 1 条。
- 验收结束后撤销短期 PAT 和 Adapter Token。
- 删除服务器 `/tmp` 中的明文 token、payload、search response 和 qdrant status 临时文件。

修改模块：

- `internal/eventlog/pg_repository.go`
- `internal/eventlog/pg_repository_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无新增 API。
- 修复现有 `POST /memory/turn-event` 对普通无 warning 事件的持久化行为。

验证命令和结果：

- 红测：
  - `go test ./internal/eventlog -run TestPGRepositoryStoresEmptyWarningsArray -count=1`
  - 修复前失败：`null value in column "warnings" ... violates not-null constraint`。
- 修复后单测：
  - `go test ./internal/eventlog -run TestPGRepositoryStoresEmptyWarningsArray -count=1` 通过。
  - `go test ./internal/eventlog -count=1` 通过。
- 部署：
  - `make prod-up` 通过。
- 健康检查：
  - `curl http://127.0.0.1:18081/healthz` 返回 db/qdrant/redis 均 ok。
- Adapter E2E：
  - `POST /memory/turn-event` 首次 accepted。
  - 重复请求 deduped。
- RAG / Search E2E：
  - Archive job、index job、Qdrant point、search result、access log 均有数据库证据。

部署状态：

- 服务器 `thinkpad:/opt/memory-os` 已运行包含 TurnEvent warnings 修复的新镜像。
- API、worker、MCP 容器已重建。
- Web 镜像命中缓存并保持运行。
- 未修改公开端口。
- 未执行 migration。
- 未删除数据卷。

安全检查：

- 本轮 E2E marker 为非敏感测试内容。
- PAT 和 Adapter Token 明文只存在服务器 `/tmp` 临时文件中，验收后已删除。
- PAT 和 Adapter Token 已撤销。
- 交付日志未记录 token 明文。
- 本轮没有写入真实 Secret。
- `make prod-up` 使用现有 `scripts/load-prod-env.sh` 注入环境变量，未打印 Secret 明文。

剩余问题：

- 本轮证明 Adapter Token -> TurnEvent -> Archive -> Archive RAG -> Qdrant -> Search 主链路可用。
- 仍需继续做 Secret Vault 全链路浏览器验收、跨租户隔离批量验收、备份恢复验收、前端剩余页面真实写操作验收。
- 不能声明 v0.4 生产级完全体完成。

## Latest Phase Pointer

- 最新完成切片：`2026-07-03 Phase 1.86：Qdrant 状态页补齐 Hot Memory point 统计`
- 详细证据位置：本文搜索 `Phase 1.86`。
- 下一步入口：MCP `memory_search` 与 HTTP `/memory/search` 一致性验收；但服务器根分区仅剩约 675MB，继续部署前建议先确认磁盘清理或扩容方案。

## 2026-07-03 Phase 1.86：Qdrant 状态页补齐 Hot Memory point 统计

完成事项：

- Qdrant 状态 API 从只展示 Archive `qdrant_points`，增强为同时展示：
  - `points_by_status`：Archive + Hot Memory 合计。
  - `archive_points_by_status`：Archive RAG chunk point 统计。
  - `hot_memory_points_by_status`：Hot Memory vector point 统计。
- Qdrant 状态页新增：
  - `Qdrant Point 状态汇总`
  - `Archive Points`
  - `Hot Memory Points`
- 继续保持页面读取真实 API，不使用静态假数据。
- 浏览器验收确认线上页面展示 Archive 与 Hot Memory 两类 point 统计。

修改模块：

- `internal/qdrant/status.go`
- `internal/qdrant/status_test.go`
- `internal/http/router.go`
- `internal/http/router_test.go`
- `web/pages/qdrant/index.vue`
- `docs/production-delivery-log.md`

新增或变更 API：

- 变更 `POST /memory/qdrant/status` 响应字段：
  - 新增 `archive_points_by_status`
  - 新增 `hot_memory_points_by_status`
  - 保留 `points_by_status` 作为合计字段，兼容既有页面和调用方。

新增 migration：

- 无。

验证命令和结果：

- 本地红灯：
  - `go test ./internal/http -run TestQdrantStatusUsesRealServiceAndReturnsIndexStats -count=1`
  - 结果：失败，`qdrant.IndexStats` 缺少 `ArchivePointsByStatus` / `HotMemoryPointsByStatus`。
  - `go test ./internal/qdrant -run TestPGStatusStoreIndexStatsSeparatesArchiveAndHotMemoryPoints -count=1`
  - 结果：失败，`IndexStats` 缺少分离统计字段。
- 本地绿灯：
  - `go test ./internal/http -run TestQdrantStatusUsesRealServiceAndReturnsIndexStats -count=1` 通过。
  - `go test ./internal/qdrant -run 'TestPGStatusStoreIndexStatsSeparatesArchiveAndHotMemoryPoints|TestPGStatusStoreArchiveIndexStatsReturnsJobAndChunkDetails' -count=1` 通过。
  - `go test ./internal/qdrant ./internal/http -count=1` 通过。
  - `go test ./...` 通过。
  - `npm --prefix web run build` 通过。
- 服务器验证：
  - `make test` 通过。
  - `make build-web` 通过。
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-api memory-web` 通过。
  - `curl -fsS http://127.0.0.1:18081/healthz` 返回 `status=ok`，db/qdrant/redis 均 `ok`。
  - `make smoke` 通过。
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps` 显示 API、Web、MCP、worker、PostgreSQL、Redis、Qdrant 均运行。

真实服务器 API 验收：

- 使用临时 PAT 调用真实 `POST /memory/qdrant/status`。
- 验收结果：
  - `collection_name=memory_os`
  - `query_time_filter_enforced=true`
  - `archive_points_by_status.indexed=52`
  - `hot_memory_points_by_status.indexed=1`
  - `points_by_status.indexed=53`
- 临时 PAT 已撤销。
- 临时凭据文件已删除。

浏览器验收：

- 打开线上 Web：`http://ddns.08121.top:18080/qdrant`
- 通过登录页使用临时 PAT 完成真实登录。
- 页面可见内容包含：
  - `Qdrant 索引状态`
  - `Archive Points`
  - `Hot Memory Points`
  - `indexed`
  - `Archive Points indexed 52`
  - `Hot Memory Points indexed 1`
- 页面未出现 `请先登录` 错误。
- 浏览器已登出。
- 临时 PAT 已撤销。
- 本地和服务器临时凭据文件已删除。

部署状态：

- 已部署到 `thinkpad` 生产 compose。
- 本轮重建/重启 `memory-api` 与 `memory-web`。
- 本轮未修改公开端口。
- 本轮未新增 migration。
- 本轮未删除数据卷。

安全检查：

- 临时 PAT 明文只短暂存在于本地和服务器 `/tmp/memory-os-browser-qdrant-*`。
- 验收结束后临时 PAT 已 revoke。
- 验收结束后临时文件已删除。
- 未在回复、日志记录或交付文档中写入 token 明文。

调试记录：

- 直接执行 `docker-compose ps` 时因未加载生产环境变量失败；后续使用 `. scripts/load-prod-env.sh` 后正常。
- 浏览器只读 evaluate 不能直接写 `localStorage`；改用登录页真实表单完成登录验收。
- 临时 PAT 第一次 SQL 插入因 users schema 字段理解错误失败；读取 migration 与生产表结构后修正为 UUID `id`。

剩余问题：

- 服务器根分区部署后只剩约 675MB，可导致后续构建、日志写入、备份或 Qdrant/PostgreSQL 写入失败；这是当前生产稳定性 P1 运维风险。
- 仍需继续补齐：
  - MCP `memory_search` 与 HTTP `/memory/search` 一致性验收。
  - 多 Agent Adapter。
  - import/export/backup restore 最终闭环。
  - 最终安全扫描和交付报告。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.76：Secret Vault 安全闭环生产验收

完成事项：

- 审计 Secret API、Vault、PG repository、Injector 和现有测试。
- 确认 Secret API 只返回 metadata：
  - `secret_ref`
  - `owner_user_id`
  - `org_id`
  - `project_id`
  - `name`
  - `status`
  - `current_version`
- 确认 Secret Vault 明文只用于加密前输入和解密使用路径，PG repository 只保存 nonce / ciphertext / key_id。
- 确认 Injector：
  - 默认注入目标为 env。
  - 拒绝 LLM prompt 注入。
  - 注入时写 `secret.inject` 审计。
  - 审计 metadata 只保存 tool / purpose / injection_target，不保存明文。
- 创建短期生产 E2E 用户、组织、项目、PAT 和 Adapter Token。
- 通过生产 API `POST /memory/secrets/create` 创建一次性假密钥。
- 验证创建响应：
  - 只包含 metadata。
  - 不包含 `plaintext`。
  - 不包含 `ciphertext`。
- 通过生产 API `POST /memory/secrets/list` 验证列表只返回 metadata。
- 通过生产 API `POST /memory/secrets/disable` 禁用 Secret，返回状态为 `disabled`。
- 运行 Secret 安全单测：
  - 禁用 Secret 后不可使用。
  - 禁止注入到 LLM prompt。
  - Secret 注入会写审计。
- 执行生产泄露扫描：
  - `secrets` metadata 无假密钥明文。
  - `audit_logs` 无假密钥明文。
  - `turn_event_payloads` 无假密钥明文。
  - `archives` 无假密钥明文。
  - `archive_chunks` 无假密钥明文。
  - `hot_memories` 无假密钥明文。
  - `qdrant_points.payload` 无假密钥明文。
  - Markdown archive 文件目录无假密钥明文。
  - API / worker / MCP 日志无假密钥明文。
  - `secret_versions` 存在 ciphertext 记录，ciphertext 不等于明文。
- 执行 Adapter 脱敏链路验收：
  - 含 `sk-test-*` 格式假密钥的 TurnEvent 被 sanitizer 替换为 `secret_ref_<event_id>_*`。
  - `turn_event_payloads` 不含原假密钥。
  - `turn_event_payloads.warnings` 包含 `secret_ref_replaced`。
  - Archive chunk 不含原假密钥。
  - Archive chunk 包含替换后的 `secret_ref_*`。
  - Qdrant payload 不含原假密钥。
  - Markdown archive 文件不含原假密钥。
  - API / worker 日志不含原假密钥。
- 验收结束后撤销短期 PAT 和 Adapter Token，并删除服务器 `/tmp` 临时文件。

修改模块：

- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

验证命令和结果：

- Secret 生产 API：
  - `POST /memory/secrets/create` 成功，响应无 plaintext / ciphertext。
  - `POST /memory/secrets/list` 成功，列表无 plaintext。
  - `POST /memory/secrets/disable` 成功，状态为 disabled。
- Secret 安全测试：
  - `go test ./internal/secret -run "TestVaultDisablePreventsUse|TestInjectorRejectsDisabledSecret|TestInjectorRejectsLLMPromptTarget|TestInjectorReplacesSecretRefAndAudits" -count=1` 通过。
- 泄露扫描：
  - DB metadata / audit / event payload / archive / chunks / hot memory / qdrant payload 均为 0 matches。
  - Markdown artifacts grep 为 0 matches。
  - API / worker / MCP 日志 grep 为 0 matches。
- Adapter 脱敏：
  - `turn_payload_event_secret_matches=0`
  - `turn_payload_secret_ref_replacements=1`
  - `turn_payload_warnings=secret_ref_replaced`
  - `archive_chunk_event_secret_matches=0`
  - `archive_chunk_secret_ref_replacements=1`
  - `qdrant_payload_event_secret_matches=0`
  - `artifact_event_secret_matches=0`
  - `api_log_event_secret_matches=0`
  - `worker_log_event_secret_matches=0`

部署状态：

- 本轮未修改运行时代码，未执行 `make prod-up`。
- 运行服务保持 Phase 1.75 部署版本。
- 未修改公开端口。
- 未执行 migration。
- 未删除数据卷。

安全检查：

- 本轮使用一次性假密钥，不是真实 Secret。
- 假密钥明文只存在于服务器 `/tmp` 临时请求文件中，验收后已删除。
- 短期 PAT 和 Adapter Token 已撤销。
- 交付日志未记录 token 明文或假密钥完整值。

剩余问题：

- Secret Vault 后端安全闭环已通过生产 API 与数据层扫描。
- 仍需补 Secret Vault 页面级浏览器验收，确认 UI 只展示 metadata 且错误态稳定。
- 仍需做跨 user/org/project/agent 隔离批量验收、备份恢复验收、前端剩余页面真实写操作验收。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.77：跨租户 / 跨项目 / Agent-Specific 隔离生产验收

完成事项：

- 审计检索隔离相关实现：
  - Qdrant `BuildPayloadFilter` 强制要求 user_id、visibility，非 private 检索要求 permission_labels。
  - Archive RAG 检索 filter 包含 user_id、org_id、project_id、visibility、permission_labels、doc_type、index_generation。
  - Qdrant `SearchPoints` 缺少 query-time filter 会直接拒绝。
  - Hot Memory filter 包含 doc_type、user_id、scope、visibility、org_id、project_id、permission_labels。
  - `agent_specific` Hot Memory 要求 agent_id，并只在同 Agent filter 下召回。
- 创建短期生产隔离主体：
  - User A / Org A / Project A。
  - User A / Org A / Project A2。
  - User B / Org B / Project B。
  - 两枚短期 PAT。
  - 三枚短期 Adapter Token。
- 通过生产 Adapter Token 写入三组 Archive TurnEvent：
  - Project A marker。
  - Project A2 marker。
  - Project B marker。
- 确认三组 Archive 均完成：
  - `archive_jobs=completed`
  - `archive_index_jobs=completed`
  - 每组 1 个 Qdrant point。
- 通过生产 Hot Memory API 写入三条 Hot Memory：
  - User A / Project A / project scope。
  - User A / Project A / agent_specific / codex。
  - User B / Project B / project scope。
- 执行生产 `/memory/search` 检索矩阵：
  - User A / Project A 搜 Project A Archive marker：命中 archive_chunk。
  - User A / Project A 搜 User B / Project B Archive marker：不包含 B marker，不包含 B archive_id。
  - User A / Project A 搜 User A / Project A2 Archive marker：不包含 A2 marker，不包含 A2 archive_id。
  - User B / Project B 搜 Project B Archive marker：命中 archive_chunk。
  - User A / Project A / codex 搜 project scope Hot Memory：命中。
  - User A / Project A / claude 搜同一 project scope Hot Memory：命中，证明同用户同项目 project scope 可跨 Agent。
  - User A / Project A / codex 搜 agent_specific Hot Memory：命中。
  - User A / Project A / claude 搜 codex agent_specific Hot Memory：不包含该 marker 和 memory_id。
  - User A / Project A 搜 User B / Project B Hot Memory：不包含 B marker 和 B memory_id。
- 执行 raw Qdrant payload filter 验证：
  - A filter + A archive 返回 1 个 point。
  - A filter + B archive 返回 0 个 point。
  - A filter 返回的 point payload 包含 A marker。
  - Cross filter 不包含 B marker。
- 数据库复核：
  - 本轮 `retrieval_requests` 写入 9 条。
  - 本轮 `memory_access_logs` 写入 12 条。
  - 本轮 `retrieval_results` 写入 12 条。
  - 本轮 Archive Qdrant points 为 3。
  - 本轮 Hot Memories 为 3。
- 验收结束后撤销两枚短期 PAT 和三枚 Adapter Token。
- 删除服务器 `/tmp` 中的明文 token、请求和响应临时文件。

修改模块：

- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

验证命令和结果：

- 生产 API：
  - `POST /memory/turn-event` 写入三组隔离 Archive 事件。
  - `POST /memory/hot-memory/create` 写入三条隔离 Hot Memory。
  - `POST /memory/search` 执行 9 个正/负例检索。
- 正例结果：
  - Project A Archive marker 命中 archive_chunk。
  - Project B Archive marker 命中 archive_chunk。
  - User A project scope Hot Memory 在 codex / claude 下均命中。
  - User A codex agent_specific Hot Memory 仅 codex 命中。
- 负例结果：
  - User A / Project A 搜不到 User B / Project B Archive marker 和 archive_id。
  - User A / Project A 搜不到 User A / Project A2 Archive marker 和 archive_id。
  - User A / Project A / claude 搜不到 codex agent_specific marker 和 memory_id。
  - User A / Project A 搜不到 User B / Project B Hot Memory marker 和 memory_id。
- Raw Qdrant filter：
  - `qdrant_filter_a_count=1`
  - `qdrant_filter_cross_count=0`
  - `qdrant_filter_a_contains_marker=true`
  - `qdrant_filter_cross_contains_b_marker=false`

部署状态：

- 本轮未修改运行时代码，未执行 `make prod-up`。
- 运行服务保持 Phase 1.75 部署版本。
- 未修改公开端口。
- 未执行 migration。
- 未删除数据卷。

安全检查：

- 本轮使用非敏感隔离 marker。
- 短期 PAT / Adapter Token 已撤销。
- 明文 token 文件和请求/响应临时文件已删除。
- 未写入真实 Secret。

剩余问题：

- 跨 user / org / project / agent_specific 隔离生产负例已通过。
- 仍需补 Secret Vault 页面级浏览器验收、备份恢复验收、前端剩余页面真实写操作验收和最终交付报告。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.78：Secret Vault 页面级浏览器验收与登出修复

完成事项：

- 审计 `web/pages/secrets/index.vue`，确认页面调用真实 API：
  - `POST /memory/secrets/list`
  - `POST /memory/secrets/create`
  - `POST /memory/secrets/disable`
- 创建短期 UI 验收用户、组织、项目和 PAT。
- 使用生产 Web 登录页通过真实 PAT 登录。
- 浏览器打开 `http://ddns.08121.top:18080/secrets`。
- 页面确认：
  - 当前组织 / 项目显示本轮真实 UUID。
  - 创建区显示 Secret 明文保护提示。
  - 列表区标题为 `Secret Metadata`。
- 浏览器创建 Secret：
  - 输入非敏感假密钥。
  - 创建成功后页面提示 `页面只保留 metadata`。
  - 密码输入框立即清空。
  - 页面正文不包含假密钥明文。
  - 列表显示 name、secret_ref、status、owner、project、version。
- 浏览器刷新页面：
  - active Secret metadata 仍可见。
  - 页面正文仍不包含假密钥明文。
- 浏览器禁用 Secret：
  - 页面提示已禁用。
  - active 列表中该 Secret 消失。
- 浏览器切换 disabled 过滤：
  - disabled metadata 可见。
  - 显示 `secret_ref · disabled`。
  - 显示 owner、project、version。
  - 页面正文不包含假密钥明文。
- 页面验收过程中发现真实 P2：
  - AppShell `登出` 按钮只执行 `auth.logout()`。
  - 当前页面不会跳转到 `/login`，用户仍停留在管理页视图。
- 按 TDD 修复：
  - 新增 `TestAppShellLogoutNavigatesToLogin`。
  - 修复前红测失败。
  - AppShell 增加 `handleLogout()`，执行 `auth.logout()` 后 `router.push('/login')`。
  - 按钮改为 `@click="handleLogout"`。
  - 修复后测试通过。
- 重新构建 Web 并部署生产服务。
- 浏览器复验登出：
  - 点击 `登出` 后 URL 跳转到 `http://ddns.08121.top:18080/login`。
  - 页面显示 `登录管理台` 和 `Personal Access Token` 输入入口。
- 验收结束后撤销短期 PAT。
- 删除服务器和本地临时 PAT 文件。

修改模块：

- `web/components/AppShell.vue`
- `internal/webdeploy/web_dockerfile_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

验证命令和结果：

- 红测：
  - `go test ./internal/webdeploy -run TestAppShellLogoutNavigatesToLogin -count=1`
  - 修复前失败：缺少 `handleLogout`。
- 修复后：
  - `go test ./internal/webdeploy -run TestAppShellLogoutNavigatesToLogin -count=1` 通过。
- 前端构建：
  - `make build-web` 通过。
- 部署：
  - `make prod-up` 通过。
- 健康检查：
  - `curl http://127.0.0.1:18081/healthz` 返回 db / qdrant / redis 均 ok。
- 浏览器验收：
  - 登录页真实 PAT 登录成功。
  - Secret 页面创建、刷新、禁用、disabled 过滤均通过。
  - 登出跳转 `/login` 通过。

部署状态：

- 服务器 `thinkpad:/opt/memory-os` 已运行包含 AppShell 登出修复的新 Web 镜像。
- API、worker、MCP、Web 容器已重建并启动。
- 未修改公开端口。
- 未执行 migration。
- 未删除数据卷。

安全检查：

- 本轮使用非敏感假密钥。
- 页面未展示假密钥明文。
- PAT 明文未写入日志或交付文档。
- 短期 PAT 已撤销。
- 临时 PAT 文件已删除。

剩余问题：

- Secret Vault 页面级浏览器验收已完成。
- 仍需备份恢复验收、前端剩余页面真实写操作补齐、最终交付报告。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.79：Backup / Restore 生产验收

完成事项：

- 审计备份恢复脚本：
  - `scripts/backup.sh`
  - `scripts/restore.sh`
  - `Makefile backup-restore-dry-run`
  - `internal/backup/backup_script_test.go`
  - `internal/restore/restore_script_test.go`
  - `internal/verify/verify_script_test.go`
- 确认 restore 默认 `DRY_RUN=1`。
- 确认真实 restore 必须显式设置 `CONFIRM_RESTORE=I_UNDERSTAND`。
- 确认 restore 会校验 manifest 中 PostgreSQL / Archive / Qdrant checksum。
- 执行 `make backup-restore-dry-run`：
  - 生成 dry-run PostgreSQL dump placeholder。
  - 生成 Markdown archive tarball。
  - 生成 dry-run Qdrant snapshot placeholder。
  - 生成 manifest。
  - 生成 restore 审计命令。
- 检查 dry-run 产物：
  - `manifest.json` 存在。
  - `postgres/memory_os.sql` 存在。
  - `archives/markdown-archive.tar.gz` 存在。
  - `qdrant/dry-run.snapshot` 存在。
  - restore audit 中 `postgres.restore.command`、`archives.restore.command`、`qdrant.restore.command` 均存在。
- 发现真实生产 backup 缺口：
  - `make backup` 没有加载 `scripts/load-prod-env.sh`。
  - `docker-compose` 插值缺少 `POSTGRES_PASSWORD`，真实 backup 失败。
- 按 TDD 修复：
  - 扩展 `TestProductionCommandsLoadEnvironmentWithoutInliningSecrets`。
  - 修复前红测失败。
  - `Makefile backup` 改为 `. scripts/load-prod-env.sh && scripts/backup.sh`。
  - `Makefile restore` 改为 `. scripts/load-prod-env.sh && scripts/restore.sh`。
  - 修复后测试通过。
- 执行非破坏性真实 backup：
  - `RUN_ID=phase-179-real-20260703T001809Z`
  - `BACKUP_ROOT=/opt/memory-os/backups`
  - `QDRANT_URL=http://127.0.0.1:18083`
  - `make backup` 成功。
- 检查真实 backup 产物：
  - PostgreSQL dump：`postgres/memory_os.sql`，大小 476363 bytes。
  - Markdown archive tarball：`archives/markdown-archive.tar.gz`，大小 104 bytes。
  - Qdrant snapshot：`qdrant/memory_os-1184664180328560-2026-07-03-00-18-09.snapshot`，大小 1378816 bytes。
  - `manifest.json` 存在，PostgreSQL / Archive / Qdrant 均有 sha256。
  - PostgreSQL dump 包含 `CREATE TABLE`。
  - Qdrant snapshot 文件数量为 1。
- 对真实 backup 执行 restore dry-run：
  - `make restore` with `DRY_RUN=1` 成功。
  - restore audit 目录：`/opt/memory-os/artifacts/restore-phase-179-real-20260703T001832Z`。
  - 生成 PostgreSQL / Archive / Qdrant 三条恢复审计命令。
  - 未执行真实 restore，未覆盖生产数据。

修改模块：

- `Makefile`
- `internal/verify/verify_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

验证命令和结果：

- Dry-run：
  - `RUN_ID=phase-179-dry-run-20260703T001639Z make backup-restore-dry-run` 通过。
- 红测：
  - `go test ./internal/verify -run TestProductionCommandsLoadEnvironmentWithoutInliningSecrets -count=1` 修复前失败。
- 修复后：
  - `go test ./internal/verify -run TestProductionCommandsLoadEnvironmentWithoutInliningSecrets -count=1` 通过。
- 真实 backup：
  - `RUN_ID=phase-179-real-20260703T001809Z BACKUP_ROOT=/opt/memory-os/backups QDRANT_URL=http://127.0.0.1:18083 make backup` 通过。
- 真实 backup 的 restore dry-run：
  - `BACKUP_DIR=/opt/memory-os/backups/phase-179-real-20260703T001809Z RESTORE_AUDIT_DIR=/opt/memory-os/artifacts/restore-phase-179-real-20260703T001832Z DRY_RUN=1 make restore` 通过。

部署状态：

- 本轮未执行 `make prod-up`。
- 未修改运行服务容器。
- 未修改公开端口。
- 未执行 migration。
- 未执行真实 restore。
- 未删除数据卷。

安全检查：

- 备份目录权限受脚本 `umask 077` 保护。
- 生产环境变量通过 `scripts/load-prod-env.sh` 加载。
- 本轮未输出 secret 明文。
- restore 审计命令不执行真实恢复，真实恢复仍需显式确认。

剩余问题：

- Backup / restore dry-run 与真实 backup 验收已完成。
- 出于安全边界，本轮没有对生产库执行真实 restore。
- 仍需补前端剩余页面真实写操作验收和最终交付报告。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.80：Adapter Token 管理台真实 API 浏览器验收

完成事项：

- 审计 Adapter Token 管理台页面：
  - `web/pages/tokens/index.vue`
  - 确认 PAT create/list/revoke 使用真实 API。
  - 确认 Adapter Token create/list/revoke 使用真实 API。
  - 确认 Adapter Token 绑定当前组织、项目和 Agent。
  - 确认创建响应的一次性明文 token 只保存在页面临时状态。
  - 确认列表只展示 metadata。
- 审计后端 Adapter Token API：
  - `internal/http/router.go`
  - `AdapterTokenCreateHandler`
  - `AdapterTokenListHandler`
  - `AdapterTokenRevokeHandler`
  - 确认 create/list/revoke 均经过 PAT 认证和项目权限上下文。
- 在服务器创建短期 UI 验收主体：
  - 测试用户。
  - 测试组织。
  - 测试项目。
  - owner membership。
  - 90 分钟短期 PAT。
- 使用内置浏览器完成真实 UI 验收：
  - 登录生产 Web：`http://ddns.08121.top:18080/login`。
  - 打开 Token 管理页：`http://ddns.08121.top:18080/tokens`。
  - 页面显示测试组织和测试项目，证明上下文来自真实 API。
  - 点击“创建真实 Adapter Token”。
  - 页面显示 `Adapter Token 一次性明文`。
  - 页面显示 `Adapter Token Metadata`。
  - 页面显示 `codex · active`。
  - 刷新页面后 metadata 仍存在。
  - 刷新后完整 Adapter Token 明文不再出现。
  - 刷新后页面只保留 `prefix: adapter`。
  - 在 Adapter Token Metadata 区块点击“撤销”。
  - 页面显示 `codex · revoked`。
  - 再次刷新后仍显示 `codex · revoked`。
  - 页面无 `Token 列表加载失败`、`token_forbidden` 或 `invalid_*` 错误。
- 数据库侧确认：
  - 本轮测试用户、组织、项目下创建了 1 个 Adapter Token。
  - 该 Adapter Token 的 `revoked_at` 非空。
  - 短期 PAT 已撤销。
- 清理：
  - 删除服务器临时 PAT 明文文件。
  - 删除本机临时 PAT 明文文件。

修改模块：

- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

验证命令和结果：

- 服务器容器状态：
  - `docker-compose -f deploy/docker-compose.yml ps` 通过，API、MCP、Web、worker、PostgreSQL、Redis、Qdrant 均运行。
- 浏览器验收：
  - 登录生产 Web 通过。
  - Adapter Token 创建通过。
  - 创建后一次性明文显示通过。
  - 刷新后完整明文消失通过。
  - metadata 持久化通过。
  - Adapter Token 撤销通过。
  - 撤销后刷新持久化通过。
- 数据库校验：
  - Adapter Token 计数为 1。
  - 已撤销 Adapter Token 计数为 1。
  - 短期 PAT 从 active 更新为 revoked。

部署状态：

- 本轮未执行 `make prod-up`。
- 未修改运行服务容器。
- 未修改公开端口。
- 未执行 migration。
- 未删除数据卷。

安全检查：

- 未在日志、文档或回复中记录 PAT / Adapter Token 明文。
- 浏览器输出只记录 token 前缀遮罩，不记录完整 token。
- 列表刷新后不展示完整 Adapter Token 明文。
- 短期 PAT 已撤销。
- 临时 PAT 明文文件已删除。

剩余问题：

- Adapter Token 管理台真实写操作验收已完成。
- 仍需继续补齐组织、项目、归档、热记忆、检索测试、Qdrant 状态等页面的浏览器验收矩阵。
- 仍需 MCP、多 Adapter、import/export、最终安全扫描和交付报告。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.82：Markdown Archive 管理台真实 API 浏览器验收

完成事项：

- 审计归档库页面：
  - `web/pages/archive/index.vue`
  - 确认 Archive list/create 使用真实 API。
  - 确认创建入口使用 `manual_archive_request` 事件生成 Markdown 归档。
- 审计归档详情页：
  - `web/pages/archive/[id].vue`
  - 确认 detail、versions、edit、reindex、index-status、delete、audit/list 均使用真实 API。
  - 确认软删除后编辑、重建索引和删除按钮禁用。
- 审计后端 Archive API：
  - `ArchiveCreateHandler`
  - `ArchiveListHandler`
  - `ArchiveDetailHandler`
  - `ArchiveVersionsHandler`
  - `ArchiveEditHandler`
  - `ArchiveReindexHandler`
  - `ArchiveIndexStatusHandler`
  - `ArchiveDeleteHandler`
- 在服务器创建短期 UI 验收主体：
  - 测试登录用户。
  - 测试组织。
  - 测试项目。
  - owner membership。
  - 90 分钟短期 PAT。
- 使用内置浏览器完成真实归档库验收：
  - 登录生产 Web：`http://ddns.08121.top:18080/login`。
  - 打开归档库：`http://ddns.08121.top:18080/archive`。
  - 页面显示测试组织和测试项目，证明上下文来自真实 API。
  - 通过“创建真实 Archive”创建归档。
  - 创建响应返回 `archive_id`。
  - 列表显示标题、archive_id、版本 1、索引代次。
  - 刷新列表后归档仍存在。
  - 打开详情页。
  - 详情页读取 Markdown 正文 textarea，确认包含本轮唯一 marker。
  - 详情页显示版本历史、RAG 索引状态和审计日志区域。
- 使用内置浏览器完成真实编辑验收：
  - 修改 Markdown 正文。
  - 点击“保存并生成新版本”。
  - 页面显示保存成功提示。
  - 版本历史显示版本 2。
  - 刷新详情后编辑后的 marker 仍存在。
- 使用内置浏览器完成真实重建索引验收：
  - 点击“触发重建索引”。
  - 索引代次从 2 提升到 3。
  - 返回切分 chunk 数 1。
  - 刷新索引状态后显示任务、chunk 和 Qdrant point 状态。
- 使用内置浏览器完成真实软删除验收：
  - 点击“软删除 Archive”。
  - 详情页显示已删除状态。
  - 保存、重建索引和软删除按钮均禁用。
  - active 过滤列表下该归档不再显示。
  - deleted 过滤列表下该归档可见。
- 数据库和文件侧确认：
  - Archive metadata：`status=deleted`，`current_version=2`，`index_generation=3`。
  - Archive versions：2 条，版本范围 1 到 2。
  - Archive chunks：当前 generation 为 3，chunk 数 1。
  - Archive index jobs：generation 3 completed 1。
  - Qdrant point：payload 中 `index_generation=3`，`vector_status=indexed`，数量 1。
  - Qdrant payload 包含 `user_id`、`org_id`、`project_id`、`archive_id` 过滤字段。
  - 容器内 Markdown 文件存在，且包含编辑后的唯一 marker。
  - 短期 PAT 已撤销。
- 清理：
  - 删除服务器临时 PAT 明文文件。
  - 删除本机临时 PAT 明文文件。

修改模块：

- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

验证命令和结果：

- 浏览器验收：
  - Archive 创建通过。
  - Archive 列表刷新持久化通过。
  - Archive 详情读取通过。
  - Archive 正文读取通过。
  - Archive 编辑通过。
  - Archive 编辑刷新持久化通过。
  - Archive 版本历史更新通过。
  - Archive 重建索引通过。
  - Archive index_generation 提升通过。
  - Archive 索引状态读取通过。
  - Archive 软删除通过。
  - Archive 软删除后按钮禁用通过。
  - active / deleted 过滤语义通过。
- 数据库和文件校验：
  - `archive_metadata|archive_manual_1783039333719-436b492fac81c8|deleted|2|3`
  - `archive_versions|2|1|2`
  - `archive_chunks|3|1|pending`
  - `index_jobs|3|completed|1`
  - `qdrant_points|3|indexed|1`
  - `payload_filter_fields|1|1|1|1`
  - `markdown_file_container|exists|252|edited_marker=1`
  - 短期 PAT 从 active 更新为 revoked。

部署状态：

- 本轮未执行 `make prod-up`。
- 未修改运行服务容器。
- 未修改公开端口。
- 未执行 migration。
- 未删除数据卷。

安全检查：

- 未在日志、文档或回复中记录 PAT 明文。
- 测试内容只包含安全的 `secret_ref`，未写入 Secret 明文。
- Qdrant payload 只确认过滤字段存在，未输出敏感 payload。
- 软删除保留版本和文件审计线索。
- 短期 PAT 已撤销。
- 临时 PAT 明文文件已删除。

调试记录：

- 宿主机直接检查 Archive `file_path` 显示 missing，因为该路径是容器内 `/data/memory-os/archives/...`。
- 改为在 `memory-api` 容器内检查后确认 Markdown 文件存在。
- `qdrant_points` 的 `index_generation` 位于 JSONB payload，而不是独立列；改用 `payload->>'index_generation'` 查询。

剩余问题：

- Markdown Archive 管理台真实写操作验收已完成。
- 仍需继续补齐热记忆、检索测试、Qdrant 状态等页面的浏览器验收矩阵。
- 仍需 MCP、多 Adapter、import/export、最终安全扫描和交付报告。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.81：组织 / 项目 / 成员治理管理台真实 API 浏览器验收

完成事项：

- 审计组织管理台页面：
  - `web/pages/orgs/index.vue`
  - 确认组织 list/create/edit/delete 均调用真实租户 API。
  - 确认组织删除为后端软删除语义，列表不再显示 `status=deleted` 组织。
- 审计项目管理台页面：
  - `web/pages/projects/index.vue`
  - 确认项目 list/create/edit/delete 均调用真实租户 API。
  - 确认成员 list/add/update-role/remove 均调用真实租户 API。
  - 确认成员移除为 `disabled` 语义，按钮禁用，审计状态可见。
- 审计后端租户 API：
  - `TenantOrgCreateHandler`
  - `TenantOrgListHandler`
  - `TenantOrgEditHandler`
  - `TenantOrgDeleteHandler`
  - `TenantProjectCreateHandler`
  - `TenantProjectListHandler`
  - `TenantProjectEditHandler`
  - `TenantProjectDeleteHandler`
  - `TenantMembershipAddHandler`
  - `TenantMembershipUpdateRoleHandler`
  - `TenantMembershipRemoveHandler`
- 在服务器创建短期 UI 验收主体：
  - 测试登录用户。
  - 待添加项目成员用户。
  - 90 分钟短期 PAT。
- 使用内置浏览器完成真实组织管理验收：
  - 登录生产 Web：`http://ddns.08121.top:18080/login`。
  - 打开组织管理页：`http://ddns.08121.top:18080/orgs`。
  - 创建真实组织。
  - 刷新后组织仍显示为 active。
  - 编辑组织 name / slug。
  - 刷新后新 name / slug 持久化。
  - 软删除组织。
  - 刷新后测试组织不再显示。
- 使用内置浏览器完成真实项目管理验收：
  - 打开项目管理页：`http://ddns.08121.top:18080/projects`。
  - 在测试组织下创建真实项目。
  - 刷新后项目仍显示为 active。
  - 编辑项目 name / slug。
  - 刷新后新 name / slug 持久化。
  - 添加待添加用户为项目 member。
  - 刷新后成员仍显示。
  - 将该成员角色从 member 更新为 admin。
  - 刷新后角色仍为 admin。
  - 移除该成员。
  - 刷新后该成员 membership 状态为 disabled，保存角色和移除按钮均禁用。
  - 软删除项目。
  - 刷新后测试项目不再显示。
- 数据库侧确认：
  - 测试组织状态为 `deleted`。
  - 测试项目状态为 `deleted`。
  - 待添加成员 membership 状态为 `disabled`。
  - 测试用户 active 项目数量为 0。
  - 短期 PAT 已撤销。
- 清理：
  - 删除服务器临时 PAT 明文文件。
  - 删除本机临时 PAT 明文文件。

修改模块：

- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

验证命令和结果：

- 浏览器验收：
  - 组织创建通过。
  - 组织刷新持久化通过。
  - 组织编辑通过。
  - 组织编辑刷新持久化通过。
  - 组织软删除通过。
  - 组织软删除刷新持久化通过。
  - 项目创建通过。
  - 项目刷新持久化通过。
  - 项目编辑通过。
  - 项目编辑刷新持久化通过。
  - 成员添加通过。
  - 成员添加刷新持久化通过。
  - 成员角色更新通过。
  - 成员角色刷新持久化通过。
  - 成员移除变为 disabled 通过。
  - 项目软删除通过。
  - 项目软删除刷新持久化通过。
- 数据库校验：
  - `org_status|tenant-ui-org-edited-20260703083208|deleted`
  - `project_status|tenant-ui-project-edited-20260703083208|deleted`
  - `member_membership|t|disabled`
  - `visible_projects_after_delete|0`
  - 短期 PAT 从 active 更新为 revoked。

部署状态：

- 本轮未执行 `make prod-up`。
- 未修改运行服务容器。
- 未修改公开端口。
- 未执行 migration。
- 未删除数据卷。

安全检查：

- 未在日志、文档或回复中记录 PAT 明文。
- 测试只作用于本轮新建的组织、项目和成员。
- 删除操作均验证为软删除 / 禁用语义。
- 短期 PAT 已撤销。
- 临时 PAT 明文文件已删除。

调试记录：

- 项目创建时 Playwright locator click 曾超时。
- 调查后确认页面无错误、按钮未卡住、数据库项目数仍为 0，说明请求未触发。
- 改用内置浏览器可见 DOM 节点点击后，项目创建成功。
- 该问题判定为浏览器自动化点击等待问题，不是产品功能缺陷。

剩余问题：

- 组织 / 项目 / 成员治理管理台真实写操作验收已完成。
- 仍需继续补齐归档库、热记忆、检索测试、Qdrant 状态等页面的浏览器验收矩阵。
- 仍需 MCP、多 Adapter、import/export、最终安全扫描和交付报告。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 日志顺序说明

- Phase 1.82：Markdown Archive 管理台真实 API 浏览器验收已完成。
- 完整证据段见本文件中的 `## 2026-07-03 Phase 1.82：Markdown Archive 管理台真实 API 浏览器验收`。
- 该段因日志锚点重复插入在文件中部，后续整理最终交付报告时需要按 Phase 编号重新排序。

## 2026-07-03 Phase 1.83：Hot Memory 编辑能力补齐与管理台真实 API 浏览器验收

完成事项：

- 审计 Hot Memory 页面：
  - `web/pages/hot-memory/index.vue`
  - 确认已有 create/list/promote/demote/mark-used/delete 真实 API。
  - 发现缺少生产计划要求的“编辑热记忆”能力。
- 按 TDD 补齐 Hot Memory edit：
  - 新增 `hotmemory.EditRequest`。
  - 新增 `Service.Edit`。
  - 编辑时更新 `fact`、`fact_hash`、`confidence`。
  - 编辑时复用 Secret sanitizer，将疑似明文 secret 替换为 `secret_ref_hot_memory_*`。
  - 拒绝编辑已删除 memory。
  - 内存 repository 更新 dedupe 映射，防止编辑后 scope 内重复 fact。
  - PG repository `Update` 支持更新 `fact`、`fact_hash`、`confidence`。
  - 新增 `/memory/hot-memory/edit` 路由。
  - OpenAPI 增加 `/memory/hot-memory/edit`。
  - 管理台新增编辑表单和“保存编辑”按钮。
- 部署：
  - 同步代码到 `thinkpad:/opt/memory-os`。
  - `make build-web` 通过。
  - `make test` 通过。
  - `make prod-up` 完成，API、worker、MCP、Web 均重建启动。
  - OpenAPI 确认包含 `/memory/hot-memory/edit`。
- 使用内置浏览器完成真实 Hot Memory 管理台验收：
  - 登录生产 Web：`http://ddns.08121.top:18080/login`。
  - 打开 Hot Memory 页面：`http://ddns.08121.top:18080/hot-memory`。
  - 页面显示测试组织和测试项目，证明上下文来自真实 API。
  - 创建 Hot Memory。
  - 刷新后 Hot Memory 仍存在。
  - 点击“编辑”，确认进入编辑模式。
  - 编辑 fact，内容包含测试用假 secret。
  - 点击“保存编辑”。
  - 刷新后编辑 marker 仍存在。
  - 页面不显示原始假 secret。
  - 页面显示 `secret_ref_hot_memory` 替换结果。
  - 点击“提升”，promoted 过滤下可见。
  - 点击“降权”，demoted 过滤下可见。
  - 点击“标记使用”，`used/access` 从 0/0 变为 1/1。
  - 刷新后切回 demoted 过滤，编辑内容和 used/access 计数仍持久化。
  - 点击“删除”，当前过滤列表不再显示该 memory。
- 数据库侧确认：
  - `status=deleted`。
  - `used_count=1`。
  - `access_count=1`。
  - 编辑 marker 存在。
  - 原始假 secret 不存在。
  - `secret_ref_hot_memory` 存在。
  - deleted memory 不再被 active 查询返回。
  - 短期 PAT 已撤销。
- 清理：
  - 删除服务器临时 PAT 明文文件。
  - 删除本机临时 PAT 明文文件。

修改模块：

- `internal/hotmemory/model.go`
- `internal/hotmemory/service.go`
- `internal/hotmemory/repository.go`
- `internal/hotmemory/pg_repository.go`
- `internal/hotmemory/service_test.go`
- `internal/hotmemory/pg_repository_test.go`
- `internal/http/router.go`
- `internal/http/router_test.go`
- `internal/webdeploy/web_dockerfile_test.go`
- `web/pages/hot-memory/index.vue`
- `docs/production-delivery-log.md`

新增或变更 API：

- 新增 `POST /memory/hot-memory/edit`。
- 请求体：
  - `memory_id`
  - `fact`
  - `confidence`
- 响应：
  - Hot Memory metadata。

验证命令和结果：

- 本地目标测试：
  - `go test ./internal/hotmemory -run 'TestServiceEditUpdatesFactHashConfidenceAndSanitizesSecrets|TestBuildUpdateSQLUpdatesMutableFactCountersAndStatus' -count=1` 通过。
  - `go test ./internal/webdeploy -run TestHotMemoryPageUsesRealAPI -count=1` 通过。
  - `go test ./internal/http -run TestHotMemoryCreateListAndLifecycleUsePATSubject -count=1` 通过。
  - `go test ./internal/hotmemory ./internal/webdeploy -count=1` 通过。
- 服务器验证：
  - `make build-web` 通过。
  - `make test` 通过。
  - `make prod-up` 通过。
  - `docker-compose -f deploy/docker-compose.yml ps` 显示 API、MCP、Web、worker、PostgreSQL、Redis、Qdrant 均运行。
  - `curl http://127.0.0.1:18081/healthz` 通过。
  - `curl http://127.0.0.1:18081/openapi.json` 确认 `edit_path=True`。
  - `make secret-scan` 通过。
- 浏览器验收：
  - Hot Memory 创建通过。
  - 创建后刷新持久化通过。
  - Hot Memory 编辑通过。
  - 编辑后刷新持久化通过。
  - 假 secret 脱敏替换通过。
  - promote 通过。
  - demote 通过。
  - mark-used 通过。
  - mark-used 刷新持久化通过。
  - delete 通过。
  - delete 后列表不可见通过。
- 数据库校验：
  - `hot_memory|hm_beb794f63705ee59|deleted|1|1|0.8|t|f|t`
  - `visible_after_delete|0`
  - 短期 PAT 从 active 更新为 revoked。

部署状态：

- 已执行 `make prod-up`。
- 未修改公开端口。
- 未执行 migration。
- 未删除数据卷。

安全检查：

- 未在日志、文档或回复中记录 PAT 明文。
- 编辑测试中的假 secret 未进入 Hot Memory 明文。
- Hot Memory fact 中只保留 `secret_ref_hot_memory`。
- 短期 PAT 已撤销。
- 临时 PAT 明文文件已删除。
- `make secret-scan` 通过。

调试记录：

- 首次浏览器编辑验收未生效，数据库确认 fact 未变化。
- 根因是自动化脚本填到了创建 textarea，而不是 memory article 内编辑 textarea。
- 改为先作用域到目标 article，再填写第二个 textarea，保存后验证通过。
- 服务器 `/` 磁盘剩余约 2.5GB，Docker images 占用约 191.5GB，是后续部署稳定性风险；本轮未擅自删除镜像或数据。

剩余问题：

- Hot Memory 管理台真实写操作验收已完成。
- Hot Memory 当前仍未证明写入 Qdrant point；本轮数据库抽样未找到对应 `qdrant_points` 记录。这与生产计划“Hot Memory Qdrant 保存向量”不完全一致，需作为后续 P2/P1 风险继续补齐。
- 仍需继续补齐检索测试、Qdrant 状态等页面的浏览器验收矩阵。
- 仍需 MCP、多 Adapter、import/export、最终安全扫描和交付报告。
- 不能声明 v0.4 生产级完全体完成。

## Phase 1.84：Unified Retrieval 检索测试页结构化验收

时间：2026-07-03 09:26:43 CST

完成事项：

- 使用 codebase-memory 定位 Unified Retrieval 服务、检索页和相关测试。
- 按 TDD 先补检索页验收测试，确认红灯：
  - 新增测试要求检索页显示 Hot Memory、Archive RAG、压缩上下文、source refs、`marked_used_count`、`access_log_count`。
  - 初次运行失败于缺少 `hotMemoryResults`，证明测试覆盖了当前缺口。
- 改造 `web/pages/search-test.vue`：
  - 保留真实 `/memory/search` 调用。
  - 使用登录 PAT 和当前组织 / 项目上下文，不硬编码 user、org、project 或 generation。
  - 结构化展示 `rerank_degraded`、结果数、`marked_used_count`、`access_log_count`。
  - 单独展示“压缩上下文”。
  - 按 `source.kind` 拆分展示“Hot Memory 结果”和“Archive RAG 结果”。
  - 显示 Source refs。
  - 保留原始 JSON 便于调试。
- 同步到 `thinkpad:/opt/memory-os` 并部署 Web。
- 使用临时验收主体创建真实 Archive、触发 reindex、创建 Hot Memory。
- 使用浏览器访问生产 Web 检索测试页完成真实检索验收。
- 验收结束后软删除本轮测试 Archive 和 Hot Memory，撤销临时 PAT，删除服务器临时凭据目录。

修改模块：

- `web/pages/search-test.vue`
- `internal/webdeploy/web_dockerfile_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无新增 API。
- 继续使用既有 `POST /memory/search`。

验证命令和结果：

- 本地红灯：
  - `go test ./internal/webdeploy -run TestSearchTestPageDisplaysUnifiedRetrievalEvidence -count=1`
  - 结果：失败，缺少 `hotMemoryResults`。
- 本地绿灯：
  - `go test ./internal/webdeploy -run TestSearchTestPageDisplaysUnifiedRetrievalEvidence -count=1` 通过。
  - `go test ./internal/webdeploy -count=1` 通过。
- 服务器验证：
  - `make test` 通过。
  - `make build-web` 通过。
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-web` 通过。
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps` 显示 API、MCP、Web、worker、PostgreSQL、Redis、Qdrant 均运行。
  - `curl -fsS http://127.0.0.1:18081/healthz` 通过。
  - `curl -fsS http://127.0.0.1:18081/openapi.json` 可访问，大小 6412 bytes。
  - `make smoke` 通过。

浏览器验收：

- 登录生产 Web：`http://ddns.08121.top:18080/login`。
- 打开检索测试页：`http://ddns.08121.top:18080/search-test`。
- 使用唯一查询 marker 执行检索。
- 页面确认：
  - 显示“Hot Memory 结果”。
  - 显示“Archive RAG 结果”。
  - 显示“压缩上下文”。
  - 显示“Source refs”。
  - 页面可见唯一 marker。
  - 原始 JSON 中 `result_count=2`。
  - 原始 JSON 中 `source_kinds=[hot_memory, archive_chunk]`。
  - 原始 JSON 中 `marked_used_count=1`。
  - 原始 JSON 中 `access_log_count=2`。

数据库与索引证据：

- API 级 `/memory/search` 响应：
  - `result_count=2`
  - `marked_used_count=1`
  - `access_log_count=2`
  - `source_kinds=hot_memory,archive_chunk`
  - `context_has_marker=True`
- Qdrant tracking 表确认 Archive chunk 已写入：
  - collection：`memory_os`
  - `archive_id=archive_search_ui_20260703091958`
  - `project_id=8b5c2cb8-c826-40e0-973e-72921b4933b6`
  - `index_generation=2`
  - `vector_status=indexed`
- Web 检索 request 确认：
  - `retrieval_requests=1`
  - `retrieval_results=2`
  - `retrieval_results` rank 1 为 `hot_memory`
  - `retrieval_results` rank 2 为 `archive_chunk`
  - Hot Memory `used_count=2`
  - Hot Memory `access_count=2`

部署状态：

- 已部署到 `thinkpad` 生产 compose。
- 本轮未修改公开端口。
- 本轮未执行 migration。
- 本轮未删除数据卷。
- compose 过程中实际重建/重启了 `memory-api` 和 `memory-web`；健康检查、OpenAPI、smoke 均通过。

安全检查：

- 临时 PAT 明文只存在于服务器 `/tmp/memory-os-search-ui-*`，未写入仓库、日志或交付报告。
- 验收结束后临时 PAT 已撤销。
- 验收结束后服务器临时凭据目录已删除。
- 本轮测试 Archive 和 Hot Memory 均已软删除。
- 未输出真实 token、密码、私钥、cookie 或 API key。

调试记录：

- 首次远程 heredoc 被本地 shell 引号打散，未执行数据库插入；改用 `ssh thinkpad 'bash -s' <<'EOF'`。
- 首次 psql 读取宿主 `/tmp` SQL 文件失败，因为文件不在 Postgres 容器内；改为通过 stdin 管道传给 `psql`。
- 首次 Archive reindex 返回 400，根因是缺少 `request_id`；读取 `ArchiveReindexHandler` 后补齐字段。
- 临时 env 中 `QUERY` 含空格未加引号导致 source 失败；修正为带引号的 env 行。
- 查询 `qdrant_points.doc_type` 失败，根因是该表将租户/Archive 信息放在 `payload` JSONB 中；改为查询 `payload`。
- 多次脚本中 `psql` 吞掉后续 heredoc stdin，导致后续命令未执行；后续改为 `</dev/null`。

剩余问题：

- Unified Retrieval 检索测试页真实验收已完成。
- Archive RAG 通过 Qdrant tracking 证明已写入向量索引。
- Hot Memory 当前仍走 PostgreSQL 检索并能 mark_used/access log，但仍未补齐“Hot Memory 写入 Qdrant 向量”的生产要求。
- 仍需继续补齐 Qdrant 状态页、MCP 一致性、多 Adapter、import/export、最终安全扫描和交付报告。
- 不能声明 v0.4 生产级完全体完成。

## Phase 1.85：Hot Memory Qdrant 向量索引生产切片

时间：2026-07-03 09:38:59 CST

完成事项：

- 使用 codebase-memory 定位 Hot Memory、Qdrant、Archive RAG index、生产 API 注入路径。
- 确认 `qdrant_points` 表专用于 Archive chunk，含 `chunk_id` 外键，不能安全复用保存 Hot Memory。
- 新增独立 tracking 表 `hot_memory_qdrant_points`。
- 新增 Hot Memory `VectorIndex` 抽象。
- 新增 `QdrantIndex`：
  - 对 Hot Memory fact 调 embedding。
  - 写入 Qdrant 单 collection：`memory_os`。
  - payload 包含 `doc_type=hot_memory`、`memory_id`、`org_id`、`project_id`、`user_id`、`agent_id`、`scope`、`visibility`、`permission_labels`、`status`、`fact_hash`。
  - 写入 `hot_memory_qdrant_points` tracking 表。
  - Search 使用 Qdrant `SearchPoints`，强制 query-time filter。
  - Search 后回读 PostgreSQL 权威 Hot Memory metadata。
  - Delete 写入 `status=deleted` tombstone payload，配合 query-time status filter 避免被召回。
- `BuildFilter` 增加 `status=[active,promoted,demoted]`。
- 生产 API `routerOptions` 注入 Hot Memory PG repository + QdrantIndex。
- Unified Retrieval 的 Hot Memory search 在生产路径中优先使用 Qdrant query-time filter。

修改模块：

- `cmd/memory-api/main.go`
- `cmd/memory-api/main_test.go`
- `internal/hotmemory/model.go`
- `internal/hotmemory/service.go`
- `internal/hotmemory/filter.go`
- `internal/hotmemory/qdrant_index.go`
- `internal/hotmemory/qdrant_index_test.go`
- `internal/hotmemory/service_test.go`
- `migrations/000017_hot_memory_qdrant_points.sql`
- `migrations/migrations_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无新增 HTTP API。
- 既有 Hot Memory API 的生产行为增强：
  - `POST /memory/hot-memory/create` 成功后同步写 Qdrant。
  - `POST /memory/hot-memory/edit` 成功后同步更新 Qdrant。
  - `POST /memory/hot-memory/delete` 成功后写入 deleted tombstone payload。
  - `POST /memory/search` 的 Hot Memory 召回走 Qdrant query-time filter。

新增 migration：

- `000017_hot_memory_qdrant_points.sql`
- 新表：
  - `hot_memory_qdrant_points`
- 用途：
  - 记录 Hot Memory Qdrant point、payload、collection、vector status。

验证命令和结果：

- 本地红灯：
  - `go test ./internal/hotmemory -run TestServiceWithVectorIndexIndexesMutationsAndSearchesQdrant -count=1`
  - 结果：失败，`NewServiceWithVectorIndex` 未定义。
  - `go test ./migrations -run TestHotMemoryQdrantMigrationContainsRequiredPointTracking -count=1`
  - 结果：失败，`000017_hot_memory_qdrant_points.sql` 不存在。
- 本地绿灯：
  - `go test ./internal/hotmemory -run 'TestServiceWithVectorIndexIndexesMutationsAndSearchesQdrant|TestQdrantIndex' -count=1` 通过。
  - `go test ./migrations -run TestHotMemoryQdrantMigrationContainsRequiredPointTracking -count=1` 通过。
  - `go test ./cmd/memory-api -run 'TestRouterOptions|TestBuildServer' -count=1` 通过。
  - `go test ./internal/hotmemory ./internal/retrieval ./internal/http ./cmd/memory-api ./migrations -count=1` 通过。
  - `go test ./...` 通过。
- 服务器验证：
  - `make test` 通过。
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-api` 通过。
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps` 显示 API、MCP、Web、worker、PostgreSQL、Redis、Qdrant 均运行。
  - `curl -fsS http://127.0.0.1:18081/healthz` 通过。
  - `make smoke` 通过。
  - `SELECT to_regclass('public.hot_memory_qdrant_points')` 返回 `hot_memory_qdrant_points`。

真实服务器验收：

- 创建临时用户、组织、项目、PAT。
- 调用真实 `POST /memory/hot-memory/create` 创建 Hot Memory。
- 调用真实 `POST /memory/search` 搜索唯一 marker。
- 调用真实 `POST /memory/hot-memory/delete` 删除 Hot Memory。
- 删除后再次调用真实 `POST /memory/search`。
- 验收结果：
  - `memory_id=hm_0cf5a838e6ec0db0`
  - 删除前 `before_result_count=1`
  - 删除前 `before_source_kinds=hot_memory`
  - 删除前 `before_marked_used_count=1`
  - 删除后 `after_result_count=0`
  - `hot_memory_qdrant_points.vector_status=indexed`
  - `payload.doc_type=hot_memory`
  - `payload.memory_id=hm_0cf5a838e6ec0db0`
  - 删除后 `payload.status=deleted`
  - `hot_memories.status=deleted`
  - 临时 PAT 已撤销。
  - 临时凭据目录已删除。

部署状态：

- 已部署到 `thinkpad` 生产 compose。
- 本轮重建/重启 `memory-api`。
- 本轮未修改公开端口。
- 本轮新增非破坏性向前 migration。
- 本轮未删除数据卷。

安全检查：

- 临时 PAT 明文只存在服务器 `/tmp/memory-os-hot-qdrant-*`。
- 验收结束后临时 PAT 已撤销。
- 验收结束后临时凭据目录已删除。
- 未输出真实 token、密码、私钥、cookie 或 API key。
- Hot Memory 写入仍走 Secret sanitizer，Qdrant payload 不包含 Secret 明文字段。

调试记录：

- 服务器宿主没有 `go`，直接 `go test` 失败；按项目既有方式使用 Docker 化 `make test` 验证。
- 服务器磁盘剩余约 2.7GB，仍是部署稳定性风险；本轮未擅自删除 Docker 镜像或数据卷。
- API compose 构建没有 buildx，经典构建较慢但成功。

剩余问题：

- Hot Memory 写入 Qdrant 向量的核心生产缺口已完成。
- 仍需继续补齐：
  - Qdrant 状态页对 Hot Memory points 的可视化。
  - MCP `memory_search` 与 HTTP `/memory/search` 一致性验收。
  - 多 Agent Adapter。
  - import/export/backup restore 最终闭环。
  - 最终安全扫描和交付报告。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.87：MCP memory_search 内部统一检索与 tool-call 入口

完成事项：

- `internal/mcp` 新增可注入 `Handler`：
  - `memory_search` 不再返回静态 `ok`。
  - `memory_search` 调用统一 `retrieval.Service.Search`。
  - 返回 `request_id`、`context`、`results`、source refs、`access_log_count`、`marked_used_count`。
  - retrieval 未配置时返回 `retrieval_not_configured`，不再冒充成功。
- `cmd/memory-mcp` 新增 `/tools/call`：
  - `POST /tools/call` 接收 `name` 与 `arguments`。
  - 调用 `mcp.Handler.HandleTool`。
  - 非 `POST` 返回 405。
  - tool 错误返回结构化 JSON。
- 新增测试证明 MCP `memory_search` 与 HTTP `/memory/search` 使用同一 retrieval 请求语义：
  - 同一个 `retrieval.Service`。
  - 同样的 actor/scope/visibility/permission labels/index generation。
  - 返回 Hot Memory 与 Archive chunk source refs。
  - 写 access log、mark_used。
  - 不泄露跨租户 fixture。

修改模块：

- `internal/mcp/schema.go`
- `internal/mcp/schema_test.go`
- `cmd/memory-mcp/main.go`
- `cmd/memory-mcp/main_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- MCP 服务新增轻量 tool-call HTTP 入口：
  - `POST /tools/call`
  - 请求示例字段：`name`、`arguments`
  - `memory_search` 返回 `ToolResponse.search`

新增 migration：

- 无。

验证命令和结果：

- 本地红灯：
  - `go test ./internal/mcp -run 'TestHandleToolRunsMemorySearch|TestHandleToolMemorySearchMatchesHTTPRetrievalSemantics|TestHandleToolMemorySearchRejectsUnconfiguredRetrieval' -count=1`
  - 结果：失败，`NewHandler` / `HandlerOptions` 不存在。
  - `go test ./cmd/memory-mcp -run 'TestToolsCallRunsMemorySearch|TestToolsCallRejectsInvalidMethod' -count=1`
  - 结果：失败，`Server.Handler` 与 `routes()` 不存在。
- 本地绿灯：
  - `go test ./internal/mcp -run 'TestHandleToolRunsMemorySearch|TestHandleToolMemorySearchMatchesHTTPRetrievalSemantics|TestHandleToolMemorySearchRejectsUnconfiguredRetrieval' -count=1` 通过。
  - `go test ./internal/mcp ./cmd/memory-mcp -count=1` 通过。
  - `go test ./internal/mcp ./cmd/memory-mcp ./internal/retrieval -count=1` 通过。
  - `go test ./...` 通过。
- 服务器验证：
  - 已同步 `internal/mcp` 与 `cmd/memory-mcp` 相关文件到 `thinkpad:/opt/memory-os`。
  - `make test` 通过。

部署状态：

- 本轮未部署、未重启容器。
- 本轮未修改公开端口。
- 本轮未新增 migration。
- 本轮未删除数据卷。

安全检查：

- 本轮未创建真实 PAT、Adapter Token 或 Secret。
- 测试 fixture 未包含真实密钥。
- MCP `memory_search` 返回上下文不包含跨租户 fixture `cross_tenant_leaked`。

剩余问题：

- 线上 `memory-mcp` 容器尚未重建，因此 `/tools/call` 尚未上线。
- `memory-mcp` 生产启动路径还未注入 PostgreSQL/Qdrant/LLM retrieval stack；当前完成的是内部 handler 和服务入口能力。
- 服务器根分区仍约 675MB，部署前必须先清理或扩容，否则可能影响构建、日志、备份、Qdrant/PostgreSQL 写入。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.88：memory-mcp 生产 retrieval stack 注入

完成事项：

- `cmd/memory-mcp` 生产启动路径新增 retrieval stack 注入：
  - 校验生产 `POSTGRES_DSN`。
  - 校验生产 `QDRANT_URL`。
  - 校验 embedding 配置：`LLM_BASE_URL`、`LLM_API_KEY`、`EMBEDDING_MODEL`。
  - 有 PostgreSQL pool 时构造真实 `retrieval.Service` 并注入 `mcp.Handler`。
  - 生产配置校验在建立 PostgreSQL 连接前执行，避免配置错误时先触发数据库连接噪音。
- MCP `memory_search` 生产栈包含：
  - Hot Memory PG repository。
  - Hot Memory Qdrant vector index。
  - Archive RAG Qdrant store。
  - PG archive generation resolver。
  - PG access log。
  - Qdrant 单 collection `memory_os`。
  - OpenAI-compatible embedding client。
- 开发或无 PG pool 场景仍可启动 MCP skeleton，但 `memory_search` 会返回 `retrieval_not_configured`，不冒充成功。

修改模块：

- `cmd/memory-mcp/main.go`
- `cmd/memory-mcp/main_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无新增公开端口。
- `POST /tools/call` 的 `memory_search` 在生产构建后可使用真实 retrieval stack。

新增 migration：

- 无。

验证命令和结果：

- 本地红灯：
  - `go test ./cmd/memory-mcp -run 'TestBuildServerRejectsMissingPostgresDSNInProduction|TestBuildServerInjectsProductionRetrievalWhenPoolExists' -count=1`
  - 结果：失败，`buildServer` 仍只接收 addr 字符串，且不存在生产错误与 `buildServerWithPool`。
- 本地绿灯：
  - `go test ./cmd/memory-mcp -run 'TestBuildServer|TestBuildServerRejects|TestBuildServerInjectsProductionRetrievalWhenPoolExists|TestToolsCall' -count=1` 通过。
  - `go test ./cmd/memory-mcp ./internal/mcp ./internal/retrieval ./internal/hotmemory ./internal/rag -count=1` 通过。
  - `go test ./...` 通过。
- 服务器验证：
  - 已同步 `cmd/memory-mcp/main.go`、`cmd/memory-mcp/main_test.go` 到 `thinkpad:/opt/memory-os`。
  - `make test` 通过。

部署状态：

- 本轮未部署、未重启容器。
- 本轮未修改公开端口。
- 本轮未新增 migration。
- 本轮未删除数据卷或 Docker 镜像。

安全检查：

- 本轮未创建真实 PAT、Adapter Token 或 Secret。
- 没有输出真实 token、API key、密码、私钥或 cookie。
- 生产 embedding key 仅通过环境变量读取，不写入代码或日志。

磁盘审计：

- 服务器根分区：约 `674MB` 可用，`100%` 使用率。
- `docker system df` 显示：
  - Images：约 `194.2GB`。
  - Containers：约 `820.7MB`。
  - Local Volumes：约 `3.098GB`，其中约 `431.4MB` reclaimable。
  - Build Cache：`0B`。
- 结论：Docker 镜像堆积是当前部署风险主因；继续重建 MCP 容器前必须确认镜像清理或扩容方案。

剩余问题：

- 线上 `memory-mcp` 容器尚未重建，因此生产 retrieval 注入尚未上线。
- 尚未做线上 `/tools/call` 真实 MCP 检索验收。
- 服务器磁盘不足仍是 P1 运维风险。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.89：Docker 清理 dry-run 预检

完成事项：

- 新增只读 Docker 清理预检脚本 `scripts/docker-cleanup-plan.sh`。
- 新增 Makefile 入口 `make docker-cleanup-plan`。
- 为 Docker 清理预检补充验证测试：
  - 脚本必须声明 `DRY_RUN_ONLY=1`。
  - 脚本必须输出 Docker 磁盘、运行中镜像、dangling 镜像和镜像清单。
  - 脚本必须明确排除 `docker volume prune`。
  - Makefile 入口不得直接执行 `docker image prune`。
- 将预检报告默认限制为前 80 条，避免服务器日志被大量 dangling 镜像刷屏。

修改模块：

- `Makefile`
- `scripts/docker-cleanup-plan.sh`
- `internal/verify/verify_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

验证命令和结果：

- 本地红灯：
  - `go test ./internal/verify -run 'TestDockerCleanupPlan|TestMakefileExposesDockerCleanupPlan' -count=1`
  - 结果：失败，脚本不存在且 Makefile 缺少 `docker-cleanup-plan` target。
- 本地绿灯：
  - `go test ./internal/verify -run 'TestDockerCleanupPlan|TestMakefileExposesDockerCleanupPlan' -count=1` 通过。
  - `go test ./internal/verify -count=1` 通过。
  - `bash scripts/docker-cleanup-plan.sh` 通过；本地 Docker daemon 未运行时只输出 warning，不执行删除。
- 服务器验证：
  - 已同步 `Makefile`、`scripts/docker-cleanup-plan.sh`、`internal/verify/verify_script_test.go` 到 `thinkpad:/opt/memory-os`。
  - 服务器目标测试通过：
    - `docker run --rm -e GOPROXY=https://goproxy.cn,direct -v "$PWD":/src -w /src golang:1.25-bookworm go test ./internal/verify -run 'TestDockerCleanupPlan|TestMakefileExposesDockerCleanupPlan' -count=1`
  - 服务器只读预检通过：
    - `DOCKER_CLEANUP_PLAN_LIMIT=25 make docker-cleanup-plan`

服务器预检证据：

- 根分区：`/dev/nvme0n1p2`，`233G` 总量，`221G` 已用，`674M` 可用，使用率 `100%`。
- Docker 镜像：`577` 个，总计约 `194.2GB`。
- Docker 容器：`26` 个，全部 active，总计约 `820.7MB`。
- Docker volumes：`17` 个，总计约 `3.098GB`，本轮明确不清理 volume。
- Build cache：`0B`。
- 运行中镜像包含 Memory OS 当前服务镜像：`deploy-memory-api`、`deploy-memory-mcp`、`deploy-memory-web`、`deploy-memory-worker`。
- dangling 镜像数量很大，且单个候选镜像有多个 `2GB+`，是当前最安全的第一阶段清理目标。

部署状态：

- 本轮未部署、未重启容器。
- 本轮未修改公开端口。
- 本轮未删除镜像、容器、网络或 volume。
- 本轮未执行 `docker image prune`、`docker system prune`、`docker volume prune`。

安全检查：

- 未输出真实 token、API key、密码、私钥或 cookie。
- 清理脚本只打印建议命令，不执行破坏性命令。
- `docker volume prune` 被明确排除，避免误删 PostgreSQL、Redis、Qdrant 或 Archive 相关持久化数据。

下一步确认点：

- 建议分两段执行磁盘释放：
  - 第一段：执行 `docker image prune -f`，仅删除 dangling 镜像。
  - 第二段：如果第一段释放空间不足，再评估 `docker image prune -a --filter "until=24h" -f`，删除未被运行容器使用且超过 24 小时的镜像。
- 两段都不清理 volume。
- 需要用户明确确认后才能执行任何删除命令。

剩余问题：

- 服务器磁盘仍只有约 `674MB` 可用，仍不适合继续 rebuild/deploy。
- 线上 `memory-mcp` 尚未重建，Phase 1.88 的生产 retrieval stack 仍未上线验收。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.90：Docker 镜像清理受保护执行入口

完成事项：

- 新增 `scripts/docker-cleanup-images.sh`，用于执行受保护的 Docker 镜像清理。
- 新增 Makefile 入口 `make docker-cleanup-images`。
- 脚本默认 `DRY_RUN=1`，只生成审计命令并退出。
- 真实执行必须同时满足：
  - `DRY_RUN=0`
  - `CONFIRM_DOCKER_IMAGE_CLEANUP=I_UNDERSTAND_IMAGE_DELETE`
- 脚本只支持两种 image-only 模式：
  - `DOCKER_IMAGE_CLEANUP_MODE=dangling`：计划命令为 `docker image prune -f`。
  - `DOCKER_IMAGE_CLEANUP_MODE=unused-24h`：计划命令为 `docker image prune -a --filter "until=24h" -f`。
- 脚本在真实执行前后写入 Docker 磁盘审计文件：
  - `docker-system-before.txt`
  - `docker-system-after.txt`
  - `docker-image-cleanup.command`

修改模块：

- `Makefile`
- `scripts/docker-cleanup-images.sh`
- `internal/verify/verify_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

验证命令和结果：

- 本地红灯：
  - `go test ./internal/verify -run 'TestDockerCleanupImages|TestMakefileExposesDockerCleanupImages' -count=1`
  - 结果：失败，`scripts/docker-cleanup-images.sh` 不存在且 Makefile 缺少 `docker-cleanup-images` target。
- 本地绿灯：
  - `go test ./internal/verify -run 'TestDockerCleanupImages|TestMakefileExposesDockerCleanupImages' -count=1` 通过。
  - `go test ./internal/verify -count=1` 通过。
  - `DRY_RUN=1 DOCKER_IMAGE_CLEANUP_MODE=dangling ... bash scripts/docker-cleanup-images.sh` 通过，输出 planned command，不执行删除。
- 服务器验证：
  - 已同步 `Makefile`、`scripts/docker-cleanup-images.sh`、`internal/verify/verify_script_test.go` 到 `thinkpad:/opt/memory-os`。
  - 服务器目标测试通过：
    - `docker run --rm -e GOPROXY=https://goproxy.cn,direct -v "$PWD":/src -w /src golang:1.25-bookworm go test ./internal/verify -run 'TestDockerCleanupImages|TestMakefileExposesDockerCleanupImages' -count=1`
  - 服务器 dry-run 通过：
    - `DRY_RUN=1 DOCKER_IMAGE_CLEANUP_MODE=dangling DOCKER_CLEANUP_AUDIT_DIR=$PWD/artifacts/docker-cleanup-dry-run-test bash scripts/docker-cleanup-images.sh`
  - 服务器拒绝路径通过：
    - `DRY_RUN=0 DOCKER_IMAGE_CLEANUP_MODE=dangling ... bash scripts/docker-cleanup-images.sh`
    - 结果：拒绝执行，并提示 `CONFIRM_DOCKER_IMAGE_CLEANUP=I_UNDERSTAND_IMAGE_DELETE`。

部署状态：

- 本轮未部署、未重启容器。
- 本轮未修改公开端口。
- 本轮未删除镜像、容器、网络或 volume。
- 本轮未执行真实 `docker image prune`。

安全检查：

- 脚本不包含 `docker system prune`、`docker volume prune`、`docker container prune`。
- Makefile target 不直接执行 `docker image prune`，只能调用受保护脚本。
- 未输出真实 token、API key、密码、私钥或 cookie。
- 清理入口仅处理 Docker images，不触碰 PostgreSQL、Redis、Qdrant 或 Archive 持久化 volume。

下一步确认点：

- 如果用户确认第一阶段清理，应执行：
  - `DRY_RUN=0 DOCKER_IMAGE_CLEANUP_MODE=dangling CONFIRM_DOCKER_IMAGE_CLEANUP=I_UNDERSTAND_IMAGE_DELETE make docker-cleanup-images`
- 执行后必须立即重新检查：
  - `df -h /`
  - `docker system df`
  - `docker-compose -f deploy/docker-compose.yml ps`
- 如果释放空间不足，再单独确认第二阶段 `unused-24h` 清理。

剩余问题：

- 服务器磁盘仍只有约 `674MB` 可用，仍不适合继续 rebuild/deploy。
- 线上 `memory-mcp` 尚未重建，Phase 1.88 的生产 retrieval stack 仍未上线验收。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.91：prod-up 磁盘门禁接入

完成事项：

- 将生产部署入口 `make prod-up` 接入 `scripts/preflight.sh`。
- `prod-up` 现在执行顺序为：
  - 先 `. scripts/load-prod-env.sh`，确保 Compose 所需变量已加载。
  - 再执行 `ALLOW_EXISTING_DEPLOYMENT=1 scripts/preflight.sh`。
  - 只有 preflight 通过后才允许继续 `docker-compose ... up -d --build`。
- 复用现有 preflight 门禁：
  - 端口检查。
  - 当前 compose 部署端口豁免。
  - 默认最小可用磁盘 `MIN_DISK_KB=41943040`，约 40GB。

修改模块：

- `Makefile`
- `internal/verify/verify_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

验证命令和结果：

- 本地红灯：
  - `go test ./internal/verify -run 'TestProductionCommandsLoadEnvironmentWithoutInliningSecrets' -count=1`
  - 结果：失败，`prod-up` 未执行 preflight。
- 本地绿灯：
  - `go test ./internal/verify -run 'TestProductionCommandsLoadEnvironmentWithoutInliningSecrets' -count=1` 通过。
  - `go test ./internal/preflight -count=1` 通过。
- 服务器验证：
  - 已同步 `Makefile`、`internal/verify/verify_script_test.go` 到 `thinkpad:/opt/memory-os`。
  - 服务器目标测试通过：
    - `docker run --rm -e GOPROXY=https://goproxy.cn,direct -v "$PWD":/src -w /src golang:1.25-bookworm go test ./internal/verify -run 'TestProductionCommandsLoadEnvironmentWithoutInliningSecrets' -count=1`
  - 服务器 preflight 包通过：
    - `docker run --rm -e GOPROXY=https://goproxy.cn,direct -v "$PWD":/src -w /src golang:1.25-bookworm go test ./internal/preflight -count=1`
  - 服务器真实 preflight 拒绝路径通过：
    - `. scripts/load-prod-env.sh && ALLOW_EXISTING_DEPLOYMENT=1 scripts/preflight.sh`
    - 结果：当前部署端口 `18080`、`18081`、`18082`、`18083` 被识别为 current deployment；随后磁盘门禁拒绝继续。
    - 输出证据：`available disk 689048KB is below required 41943040KB`。

部署状态：

- 本轮未部署、未重启容器。
- 本轮未修改公开端口。
- 本轮未删除镜像、容器、网络或 volume。
- 本轮未执行真实 `docker image prune`。

安全检查：

- 未输出真实 token、API key、密码、私钥或 cookie。
- `prod-up` 在 build 前会被磁盘门禁阻止，避免满盘时继续构建导致服务或数据风险。
- 生产环境变量仍通过 `scripts/load-prod-env.sh` 加载，不写入 `.env`，不打印 secret。

剩余问题：

- 服务器磁盘仍不足，约 `689048KB` 可用，仍不适合继续 rebuild/deploy。
- 线上 `memory-mcp` 尚未重建，Phase 1.88 的生产 retrieval stack 仍未上线验收。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.92：Docker dangling 清理执行与防复发

完成事项：

- 在用户明确确认后执行第一阶段 Docker dangling 镜像清理。
- 清理命令通过受保护脚本执行：
  - `DRY_RUN=0 DOCKER_IMAGE_CLEANUP_MODE=dangling CONFIRM_DOCKER_IMAGE_CLEANUP=I_UNDERSTAND_IMAGE_DELETE make docker-cleanup-images`
- 清理范围：
  - 只删除 dangling Docker images。
  - 不删除 containers。
  - 不删除 networks。
  - 不删除 volumes。
  - 不触碰 PostgreSQL、Redis、Qdrant 或 Archive 数据。
- `make prod-up` 成功 `docker-compose ... up -d --build` 后自动执行 dangling-only 清理，避免后续开发部署持续堆积 `<none>:<none>` 镜像。

修改模块：

- `Makefile`
- `internal/verify/verify_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

验证命令和结果：

- 本地红灯：
  - `go test ./internal/verify -run 'TestProductionCommandsLoadEnvironmentWithoutInliningSecrets' -count=1`
  - 结果：失败，`prod-up` 成功 build 后未自动清理 dangling images。
- 本地绿灯：
  - `go test ./internal/verify -run 'TestProductionCommandsLoadEnvironmentWithoutInliningSecrets|TestDockerCleanupImages|TestMakefileExposesDockerCleanupImages' -count=1` 通过。
  - `go test ./internal/verify -count=1` 通过。
- 服务器验证：
  - 已同步 `Makefile`、`internal/verify/verify_script_test.go` 到 `thinkpad:/opt/memory-os`。
  - 服务器目标测试通过：
    - `docker run --rm -e GOPROXY=https://goproxy.cn,direct -v "$PWD":/src -w /src golang:1.25-bookworm go test ./internal/verify -run 'TestProductionCommandsLoadEnvironmentWithoutInliningSecrets|TestDockerCleanupImages|TestMakefileExposesDockerCleanupImages' -count=1`
  - 服务器 preflight 通过：
    - `. scripts/load-prod-env.sh && ALLOW_EXISTING_DEPLOYMENT=1 scripts/preflight.sh`
    - 输出证据：`preflight ok: ports=[18080 18081 18082 18083] available_disk_kb=168728628`。

清理结果：

- `docker image prune -f` 回收空间：`33.02GB`。
- 根分区从约 `673MB` 可用恢复到约 `161GB` 可用。
- 根分区状态：
  - 清理前：`233G` 总量，`221G` 已用，约 `673MB` 可用，`100%`。
  - 清理后：`233G` 总量，约 `60G` 已用，约 `161G` 可用，`28%`。
- Docker 状态：
  - Images：从 `577` 个下降到 `56` 个。
  - Images size：从约 `194.2GB` 下降到约 `22.17GB`。
  - Containers：`26` 个，全部保留。
  - Local Volumes：`17` 个，全部保留。

运行状态复查：

- `docker-compose -f deploy/docker-compose.yml ps` 在加载生产环境变量后通过。
- Memory OS 容器运行中：
  - `deploy-memory-api-1`
  - `deploy-memory-mcp-1`
  - `deploy-memory-web-1`
  - `deploy-memory-worker-1`
  - `deploy-postgres-1`
  - `deploy-qdrant-1`
  - `deploy-redis-1`
- `/healthz` 返回：
  - `status=ok`
  - `db=ok`
  - `qdrant=ok`
  - `redis=ok`

安全检查：

- 未输出真实 token、API key、密码、私钥或 cookie。
- 未执行 `docker volume prune`。
- 未执行 `docker system prune`。
- 未删除数据库、Qdrant、Redis、Archive 或 compose volume。
- 防复发机制仍只执行 dangling image 清理，不清理 named unused images。

剩余问题：

- 线上 `memory-mcp` 尚未重建，Phase 1.88 的生产 retrieval stack 仍未上线验收。
- 仍需执行线上 `/tools/call memory_search` 验收。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.93：MCP 生产检索配置修复与线上验收

完成事项：

- 重建并启动 `memory-mcp`，补齐 MCP 生产环境检索依赖配置。
- 修复 `memory-mcp` 缺少生产 `POSTGRES_DSN`、`QDRANT_URL`、`LLM_BASE_URL`、`LLM_API_KEY`、`EMBEDDING_MODEL` 时启动后崩溃的问题。
- 修复 `memory-mcp` 启动日志 `env` 写死为 `development` 的问题，改为读取 `cfg.AppEnv`。
- 增加 compose 静态测试，防止 `memory-mcp` 再次漏传生产 retrieval 环境变量。
- 增加 MCP logger 测试，防止生产日志环境字段再次写死。
- 线上验证 `memory-mcp` `/healthz` 可用。
- 线上验证 `/tools/call memory_search` 返回 `code:"ok"`，不再是 `retrieval_not_configured` 或启动失败状态。

修改模块：

- `deploy/docker-compose.yml`
- `cmd/memory-mcp/main.go`
- `cmd/memory-mcp/main_test.go`
- `internal/webdeploy/web_dockerfile_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无新增 API。
- 修复 MCP `memory_search` 线上可用性。

新增 migration：

- 无。

验证命令和结果：

- 本地目标测试：
  - `go test ./internal/webdeploy -run 'TestComposePassesProductionRetrievalEnvToMCP' -count=1` 通过。
  - `go test ./internal/webdeploy -count=1` 通过。
  - `go test ./cmd/memory-mcp -run 'TestMCPLoggerOptionsUsesConfiguredEnvironment|TestBuildServer|TestToolsCall' -count=1` 通过。
  - `go test ./cmd/memory-mcp -count=1` 通过。
- 服务器目标测试：
  - `docker run --rm --network deploy_default ... go test ./internal/webdeploy -run 'TestComposePassesProductionRetrievalEnvToMCP' -count=1` 通过。
  - `docker run --rm --network deploy_default ... go test ./cmd/memory-mcp -run 'TestMCPLoggerOptionsUsesConfiguredEnvironment|TestBuildServer|TestToolsCall' -count=1` 通过。
- 服务器部署：
  - `memory-mcp` 重建并启动成功。
  - `deploy-memory-mcp-1` 状态为 `Up`。
- 服务器运行时验收：
  - `curl http://127.0.0.1:18082/healthz` 返回 `{"status":"ok"}`。
  - `docker logs --tail 20 deploy-memory-mcp-1` 显示 `memory-mcp starting` 且 `env` 为 `production`。
  - `POST http://127.0.0.1:18082/tools/call` 调用 `memory_search` 返回 `{"code":"ok", ...}`。
  - `make smoke` 通过，输出 `smoke ok`。

磁盘与容器复查：

- 根分区保持健康：`233G` 总量，约 `60G` 已用，约 `161G` 可用，使用率约 `28%`。
- Docker 状态：
  - Images：`56` 个，`23` 个 active，`22.18GB`。
  - Containers：`26` 个，全部 active。
  - Local Volumes：`17` 个，`14` 个 active，未删除 volume。
- 核心容器运行中：
  - `deploy-memory-api-1`
  - `deploy-memory-mcp-1`
  - `deploy-memory-web-1`
  - `deploy-memory-worker-1`
  - `deploy-postgres-1`
  - `deploy-qdrant-1`
  - `deploy-redis-1`

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- 未删除 volume、数据库、Qdrant、Redis 或 Archive 数据。
- MCP production 配置通过环境变量注入，未把敏感值写入代码或交付日志。
- `memory_search` 本轮输入为非敏感测试查询。

剩余问题：

- MCP `memory_search` 已证明线上调用路径可用，但本次测试 actor/query 没有命中数据，结果为空；完整语义一致性仍需要用同一生产 fixture 对比 HTTP `/memory/search` 与 MCP `memory_search` 的返回来源、context、access log 和 mark_used。
- 仍需继续推进多 Agent Adapter、import/export/backup restore 最终闭环、最终安全扫描和交付报告。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.94：MCP 与 HTTP 检索生产一致性门禁

完成事项：

- `cmd/memory-smoke` 的 Pipeline E2E 在 HTTP `/memory/search` 命中 Archive chunk 后，新增可选 MCP 一致性检查。
- 当 `SMOKE_MCP_URL` 存在时，smoke 会使用同一 marker、actor、org、project、agent、permission label 调用 MCP `/tools/call memory_search`。
- MCP 一致性检查要求：
  - MCP 返回 `code:"ok"`。
  - MCP 返回内容包含同一 E2E marker。
  - MCP 结果来源包含 `archive_chunk`。
  - MCP 响应不得泄露 secret marker。
- `make smoke` Docker fallback 增加 `SMOKE_MCP_URL` 透传。
- `scripts/post-deploy-verify.sh` 默认 Pipeline E2E 增加 `SMOKE_MCP_URL=http://memory-mcp:18082`，因此生产部署后验收会自动覆盖 MCP 与 HTTP 检索一致性。
- 增加测试覆盖 MCP 命中、一致性失败和 post-deploy compose 网络 MCP URL 注入。

修改模块：

- `cmd/memory-smoke/main.go`
- `cmd/memory-smoke/main_test.go`
- `Makefile`
- `scripts/post-deploy-verify.sh`
- `internal/verify/verify_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无新增 API。
- 增强生产验收门禁：MCP `memory_search` 必须与 HTTP `/memory/search` 在同一生产 fixture 下召回同一 Archive chunk marker。

新增 migration：

- 无。

验证命令和结果：

- 本地红灯：
  - `go test ./cmd/memory-smoke -run 'TestPipelineE2ESmokeChecksMCPMemorySearchWhenConfigured|TestPipelineE2ESmokeRejectsMCPMismatchWhenConfigured|TestMakeSmokePassesStrictPipelineEnvironmentToDocker' -count=1`
  - 结果：失败，`pipelineE2ESmoke` 未调用 MCP，MCP mismatch 未失败，Makefile 未透传 `SMOKE_MCP_URL`。
  - `go test ./internal/verify -run TestPostDeployVerifySetsMCPURLForComposeNetworkPipeline -count=1`
  - 结果：失败，post-deploy pipeline 未设置 `memory-mcp` URL。
- 本地绿灯：
  - `go test ./cmd/memory-smoke -run 'TestPipelineE2ESmokeChecksMCPMemorySearchWhenConfigured|TestPipelineE2ESmokeRejectsMCPMismatchWhenConfigured|TestMakeSmokePassesStrictPipelineEnvironmentToDocker' -count=1` 通过。
  - `go test ./internal/verify -run 'TestPostDeployVerifySetsMCPURLForComposeNetworkPipeline|TestPostDeployVerifySetsWebURLForComposeNetworkPipeline|TestPostDeployVerifyRunsRuntimeGatesInOrder' -count=1` 通过。
  - `bash -n scripts/post-deploy-verify.sh` 通过。
  - `go test ./cmd/memory-smoke ./internal/verify -count=1` 通过。
  - `go test ./...` 通过。
- 服务器验证：
  - 同步本轮改动到 `thinkpad:/opt/memory-os`。
  - `PATH=/usr/local/go/bin:$PATH go test ./cmd/memory-smoke ./internal/verify -count=1` 通过。
  - `make post-deploy-verify` 通过，日志目录 `/tmp/memory-os-post-deploy.B9QOOn`。

部署状态：

- 本轮修改的是 smoke 工具、Makefile 和 post-deploy 验收脚本。
- 未重建 API、worker、MCP、Web 运行镜像。
- 运行中 `memory-mcp` 继续保持 Phase 1.93 部署版本。
- 未修改公开端口。
- 未执行 migration。
- 未删除数据卷。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- Post-deploy pipeline 仍不打印临时 PAT、Adapter Token、PostgreSQL DSN 或 E2E marker 明细。
- MCP 一致性检查复用 smoke 的 secret marker 扫描，避免把含敏感标记的响应当作通过。

剩余问题：

- MCP/HTTP 一致性现在进入 post-deploy 门禁，但日志只输出 `smoke ok`，不会打印 marker 或 token；后续如需更强交付可增加非敏感摘要输出，例如 `mcp consistency ok`，但不能输出 token 或 Secret。
- 仍需继续推进多 Agent Adapter、import/export/backup restore 最终闭环、最终安全扫描和交付报告。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.95：Restore 真实恢复目标环境门禁

完成事项：

- 强化 `scripts/restore.sh` 的真实恢复安全边界。
- `DRY_RUN=0` 且 `CONFIRM_RESTORE=I_UNDERSTAND` 后，仍必须声明恢复目标环境：
  - `RESTORE_TARGET_ENV=test`：允许继续走测试环境恢复命令。
  - `RESTORE_TARGET_ENV=production`：必须额外提供 `CONFIRM_PRODUCTION_RESTORE=I_UNDERSTAND_PRODUCTION_DATA_OVERWRITE`。
- 缺少 `RESTORE_TARGET_ENV` 时，真实恢复会在执行 compose / psql / tar / Qdrant upload 前拒绝。
- 生产恢复缺少额外确认时，真实恢复同样在执行任何恢复命令前拒绝。
- 保持 dry-run 行为不变，`make backup-restore-dry-run` 仍只生成备份占位、校验 manifest checksum 和恢复审计命令，不修改生产数据。

修改模块：

- `scripts/restore.sh`
- `internal/restore/restore_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

验证命令和结果：

- 本地红灯：
  - `go test ./internal/restore -run 'TestRestoreScriptRejectsRealRestoreWithoutTargetEnvironment|TestRestoreScriptRequiresExtraConfirmationForProductionTarget' -count=1`
  - 结果：失败，旧脚本在确认值存在后会继续走恢复命令，没有要求目标环境。
- 本地绿灯：
  - `go test ./internal/restore -count=1` 通过。
  - `go test ./internal/backup ./internal/restore ./internal/verify -count=1` 通过。
  - `bash -n scripts/restore.sh scripts/backup.sh` 通过。
  - `make backup-restore-dry-run` 通过。
- 服务器验证：
  - 同步 `scripts/restore.sh` 与 `internal/restore/restore_script_test.go` 到 `thinkpad:/opt/memory-os`。
  - `PATH=/usr/local/go/bin:$PATH go test ./internal/backup ./internal/restore ./internal/verify -count=1` 通过。
  - `bash -n scripts/restore.sh scripts/backup.sh` 通过。
  - `BACKUP_ROOT=$(mktemp -d) RESTORE_AUDIT_DIR=$(mktemp -d)/restore make backup-restore-dry-run` 通过。

部署状态：

- 本轮只修改恢复脚本与测试。
- 未执行真实 restore。
- 未重建或重启线上容器。
- 未修改公开端口。
- 未执行 migration。
- 未删除数据卷。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- 真实恢复现在至少需要两层门禁：通用恢复确认 + 目标环境声明。
- 生产恢复需要第三层显式生产覆盖确认。
- dry-run 验收只生成命令审计文件和 checksum 校验，不触碰 PostgreSQL、Archive volume 或 Qdrant 数据。

剩余问题：

- 本轮增强了真实 restore 安全门禁，但还没有完成“隔离测试环境真实 restore 后运行 smoke”的最终恢复演练。
- 后续需要创建隔离恢复环境或临时 compose project，把备份恢复到测试环境后执行 `make smoke`，并把结果写入最终交付证据。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.96：Restore Rehearsal 安全演练入口

完成事项：

- 新增 `scripts/restore-rehearsal.sh`，作为恢复演练的统一入口。
- 默认模式为 `RESTORE_REHEARSAL_MODE=dry-run`，只执行：
  - 备份目录校验。
  - restore manifest checksum 校验。
  - restore 审计命令生成。
  - rehearsal plan 记录。
- dry-run rehearsal 不启动新服务、不执行真实 `psql`、不上传 Qdrant snapshot、不修改 Archive 目录。
- 新增 Makefile 入口 `restore-rehearsal-dry-run`，要求显式传入 `BACKUP_DIR`。
- rehearsal 入口拒绝生产 compose project 名：
  - `deploy`
  - `memory-os`
  - `memory_os`
  - `memoryos`
  - `production`
  - `prod`
- real rehearsal 模式必须显式提供 `CONFIRM_RESTORE_REHEARSAL=I_UNDERSTAND_TEST_RESTORE`。
- real rehearsal 会以 `RESTORE_TARGET_ENV=test` 调用 `scripts/restore.sh`，避免误用生产恢复路径。
- 本轮没有执行 real rehearsal，只完成安全入口和 dry-run 证据链。

修改模块：

- `scripts/restore-rehearsal.sh`
- `Makefile`
- `internal/restore/restore_rehearsal_script_test.go`
- `internal/verify/verify_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

验证命令和结果：

- 本地验证：
  - `go test ./internal/restore -run 'TestRestoreRehearsal|TestRestoreScript' -count=1` 通过。
  - `go test ./internal/verify -run 'TestMakefileExposesRestoreRehearsalDryRunTarget|TestMakefileExposesBackupRestoreDryRunTarget' -count=1` 通过。
  - `go test ./internal/backup ./internal/restore ./internal/verify -count=1` 通过。
  - `go test ./...` 通过。
  - `bash -n scripts/restore.sh scripts/backup.sh scripts/restore-rehearsal.sh` 通过。
  - 使用 dry-run backup fixture 执行 `make restore-rehearsal-dry-run` 通过。
- 服务器验证：
  - 同步本轮脚本、Makefile 和测试到 `thinkpad:/opt/memory-os`。
  - `PATH=/usr/local/go/bin:$PATH go test ./internal/backup ./internal/restore ./internal/verify -count=1` 通过。
  - `bash -n scripts/restore.sh scripts/backup.sh scripts/restore-rehearsal.sh` 通过。
  - 使用服务器 `/tmp` dry-run backup fixture 执行 `make restore-rehearsal-dry-run` 通过。

部署状态：

- 本轮只新增恢复演练脚本、Makefile 入口和测试。
- 未执行真实 restore。
- 未启动隔离恢复环境。
- 未重建或重启线上容器。
- 未修改公开端口。
- 未执行 migration。
- 未删除数据卷。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- rehearsal dry-run 只写审计文件和 plan，不触碰生产 PostgreSQL、Qdrant、Redis 或 Archive volume。
- real rehearsal 必须显式确认，并默认走 `RESTORE_TARGET_ENV=test`。
- 生产 project 名被脚本拒绝，降低误恢复到当前生产 compose project 的风险。

剩余问题：

- 仍未完成真正的隔离测试环境 restore + `make smoke` 演练。
- 下一步需要补专用 restore compose override 或一次性测试环境，让 real rehearsal 能恢复到非生产服务后运行 smoke。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.97：隔离 Restore Rehearsal Compose 基座

完成事项：

- 新增专用隔离 compose 文件 `deploy/docker-compose.restore-rehearsal.yml`。
- 该 compose 使用独立 project name：`memory-os-restore-rehearsal`。
- 该 compose 不声明任何 `ports:`，不占用生产公网端口：
  - 不占用 Web `18080`。
  - 不占用 API `18081`。
  - 不占用 MCP `18082`。
  - 不占用 Qdrant `18083`。
- 该 compose 不复用生产 volume 名：
  - 不复用 `postgres_data`。
  - 不复用 `redis_data`。
  - 不复用 `qdrant_data`。
  - 不复用 `archive_data`。
- 该 compose 使用独立 rehearsal volume：
  - `restore_rehearsal_pg`
  - `restore_rehearsal_redis`
  - `restore_rehearsal_qdrant`
  - `restore_rehearsal_archive`
- 该 compose 复用已构建镜像：
  - `deploy-memory-api`
  - `deploy-memory-worker`
  - `deploy-memory-mcp`
- API / Worker / MCP 使用内部服务名连接 PostgreSQL 和 Qdrant：
  - `POSTGRES_DSN=...@postgres:5432...`
  - `QDRANT_URL=http://qdrant:6333`
  - `ARCHIVE_DIR=/data/memory-os`
- 新增静态测试，防止 restore rehearsal compose 暴露端口或复用生产 volume。

修改模块：

- `deploy/docker-compose.restore-rehearsal.yml`
- `internal/webdeploy/web_dockerfile_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

验证命令和结果：

- 本地红灯：
  - `go test ./internal/webdeploy -run TestRestoreRehearsalComposeIsIsolated -count=1`
  - 结果：失败，`deploy/docker-compose.restore-rehearsal.yml` 不存在。
- 本地绿灯：
  - `go test ./internal/webdeploy -run TestRestoreRehearsalComposeIsIsolated -count=1` 通过。
  - `docker-compose -f deploy/docker-compose.restore-rehearsal.yml config` 通过。
  - 解析后的 compose config 未出现 `published:` 端口映射。
  - `go test ./internal/webdeploy ./internal/restore ./internal/verify -count=1` 通过。
  - `go test ./...` 通过。
- 服务器验证：
  - 同步 `deploy/docker-compose.restore-rehearsal.yml` 与相关测试到 `thinkpad:/opt/memory-os`。
  - `PATH=/usr/local/go/bin:$PATH go test ./internal/webdeploy ./internal/restore ./internal/verify -count=1` 通过。
  - `. scripts/load-prod-env.sh && docker-compose -f deploy/docker-compose.restore-rehearsal.yml config` 通过。
  - 解析后的服务器 compose config 未出现 `published:` 端口映射。
  - 使用服务器 `/tmp` dry-run backup fixture 执行 `make restore-rehearsal-dry-run` 通过。

部署状态：

- 本轮只新增隔离 rehearsal compose 和静态测试。
- 未执行真实 restore。
- 未启动隔离恢复环境。
- 未重建或重启线上容器。
- 未修改公开端口。
- 未执行 migration。
- 未删除数据卷。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- 隔离 compose 不暴露宿主机端口，避免和生产服务冲突。
- 隔离 compose 不复用生产 volume，避免误写生产 PostgreSQL、Redis、Qdrant 或 Archive 数据。
- 真实 restore 仍未执行，生产数据未被触碰。

剩余问题：

- 隔离 compose 已具备，但 real rehearsal 还不能直接宣称完成：`restore.sh` 当前 Qdrant snapshot upload 仍从宿主机访问 `QDRANT_URL`，而隔离 compose 不暴露 Qdrant 端口。
- 下一步需要补“在 compose 网络内执行 Qdrant snapshot upload”的恢复路径，随后才能在隔离环境执行真实 restore + `make smoke`。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.98：Qdrant Snapshot 网络内 Restore 路径

完成事项：

- `scripts/restore.sh` 新增 `QDRANT_RESTORE_DOCKER_NETWORK` 支持。
- 未设置 `QDRANT_RESTORE_DOCKER_NETWORK` 时，保持原有宿主机 `curl` restore 命令。
- 设置 `QDRANT_RESTORE_DOCKER_NETWORK` 时，生成并执行一次性 `curlimages/curl` 容器命令：
  - 挂载备份目录中的 Qdrant snapshot 为只读 `/snapshot`。
  - 使用 `--network <compose-network>` 进入隔离 compose 网络。
  - 向网络内 `QDRANT_URL` 执行 `/collections/<collection>/snapshots/upload`。
- `scripts/restore-rehearsal.sh` real 模式自动注入：
  - `QDRANT_RESTORE_DOCKER_NETWORK=${RESTORE_REHEARSAL_PROJECT}_default`
- 这样隔离 rehearsal compose 不需要暴露 Qdrant 宿主机端口，也能在真实 restore 时访问网络内 `http://qdrant:6333`。
- 增加测试覆盖：
  - restore dry-run 生成 docker-network Qdrant restore 命令。
  - rehearsal real mode 会把隔离网络名传给 restore。

修改模块：

- `scripts/restore.sh`
- `scripts/restore-rehearsal.sh`
- `internal/restore/restore_script_test.go`
- `internal/restore/restore_rehearsal_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

验证命令和结果：

- 本地红灯：
  - `go test ./internal/restore -run 'TestRestoreScriptCanRestoreQdrantSnapshotInsideDockerNetwork|TestRestoreRehearsalRealModePassesQdrantDockerNetwork' -count=1`
  - 结果：失败，restore 仍生成宿主机 curl 命令，rehearsal real mode 未传网络名。
- 本地绿灯：
  - `go test ./internal/restore -count=1` 通过。
  - `bash -n scripts/restore.sh scripts/restore-rehearsal.sh` 通过。
  - `go test ./internal/restore ./internal/verify ./internal/webdeploy -count=1` 通过。
  - `go test ./...` 通过。
  - 使用 dry-run backup fixture 执行 `scripts/restore.sh`，确认 `qdrant.restore.command` 包含：
    - `docker run --rm --network memory-os-restore-rehearsal_default`
    - `curlimages/curl:8.10.1`
    - `snapshot=@/snapshot/...`
- 服务器验证：
  - 同步 `scripts/restore.sh`、`scripts/restore-rehearsal.sh` 和相关测试到 `thinkpad:/opt/memory-os`。
  - `PATH=/usr/local/go/bin:$PATH go test ./internal/restore -count=1` 通过。
  - `bash -n scripts/restore.sh scripts/restore-rehearsal.sh` 通过。
  - 使用服务器 `/tmp` dry-run backup fixture 执行 `scripts/restore.sh`，确认 `qdrant.restore.command` 包含：
    - `docker run --rm --network memory-os-restore-rehearsal_default`
    - `http://qdrant:6333/collections/memory_os/snapshots/upload`
    - `snapshot=@/snapshot/dry-run.snapshot`

部署状态：

- 本轮只修改 restore/rehearsal 脚本和测试。
- 未执行真实 restore。
- 未启动隔离恢复环境。
- 未重建或重启线上容器。
- 未修改公开端口。
- 未执行 migration。
- 未删除数据卷。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- Qdrant restore 网络内路径通过一次性容器和只读 snapshot 挂载实现，不需要暴露 Qdrant 宿主机端口。
- dry-run 只生成审计命令，不上传 snapshot，不改 Qdrant 数据。
- 真实 restore 仍需 `CONFIRM_RESTORE`、`RESTORE_TARGET_ENV` 和 rehearsal confirmation 门禁。

剩余问题：

- 已具备隔离 compose 和网络内 Qdrant restore 命令路径，但仍未执行真实 restore rehearsal。
- 下一步需要让 `restore-rehearsal.sh` real mode 自动启动隔离 compose、执行 restore、运行 smoke、最后清理隔离环境。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.99：Restore Rehearsal Real Mode 编排

完成事项：

- `scripts/restore-rehearsal.sh` real mode 新增完整编排顺序：
  - 启动隔离 rehearsal compose。
  - 执行 `scripts/restore.sh`，目标环境固定为 `RESTORE_TARGET_ENV=test`。
  - 执行 smoke 命令。
  - 清理隔离 rehearsal compose。
- 新增可覆盖命令：
  - `RESTORE_REHEARSAL_UP_CMD`
  - `RESTORE_REHEARSAL_DOWN_CMD`
  - `RESTORE_CMD`
  - `SMOKE_CMD`
- 默认 up 命令使用：
  - `docker-compose -p <RESTORE_REHEARSAL_PROJECT> -f deploy/docker-compose.restore-rehearsal.yml up -d postgres redis qdrant memory-api memory-worker memory-mcp`
- 默认 down 命令使用：
  - `docker-compose -p <RESTORE_REHEARSAL_PROJECT> -f deploy/docker-compose.restore-rehearsal.yml down -v`
- real mode 使用 shell trap，确保 restore 或 smoke 失败时仍执行 cleanup。
- 增加测试覆盖：
  - real mode 顺序必须是 `up -> restore -> smoke -> down`。
  - restore 失败时必须执行 `down`，且不能继续 smoke。
  - real mode 继续传递 `QDRANT_RESTORE_DOCKER_NETWORK=<project>_default`。

修改模块：

- `scripts/restore-rehearsal.sh`
- `internal/restore/restore_rehearsal_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

验证命令和结果：

- 本地红灯：
  - `go test ./internal/restore -run TestRestoreRehearsalRealModeRunsUpRestoreSmokeAndCleanup -count=1`
  - 结果：失败，旧 real mode 只有 `restore -> smoke`，没有 up/down。
- 本地绿灯：
  - `go test ./internal/restore -run 'TestRestoreRehearsalRealModeRunsUpRestoreSmokeAndCleanup|TestRestoreRehearsalRealModeCleansUpWhenRestoreFails' -count=1` 通过。
  - `go test ./internal/restore -count=1` 通过。
  - `go test ./internal/restore ./internal/verify ./internal/webdeploy -count=1` 通过。
  - `go test ./...` 通过。
  - `bash -n scripts/restore-rehearsal.sh scripts/restore.sh` 通过。
- 服务器验证：
  - 同步 `scripts/restore-rehearsal.sh` 和 `internal/restore/restore_rehearsal_script_test.go` 到 `thinkpad:/opt/memory-os`。
  - `PATH=/usr/local/go/bin:$PATH go test ./internal/restore ./internal/verify ./internal/webdeploy -count=1` 通过。
  - `bash -n scripts/restore-rehearsal.sh scripts/restore.sh` 通过。
  - 服务器生产容器状态复查通过，API / Web / Worker / MCP 均保持运行。
  - 根分区仍约 `161G` 可用，使用率约 `28%`。

部署状态：

- 本轮只修改 restore rehearsal 编排脚本和测试。
- 未执行真实 restore。
- 未启动隔离恢复环境。
- 未重建或重启线上容器。
- 未修改公开端口。
- 未执行 migration。
- 未删除生产数据卷。

安全检查：

- 本轮没有写入真实 Secret、API key、密码、私钥或 cookie。
- real mode 仍需要 `CONFIRM_RESTORE_REHEARSAL=I_UNDERSTAND_TEST_RESTORE`。
- `RESTORE_REHEARSAL_PROJECT` 仍拒绝生产项目名。
- cleanup 使用传入的 rehearsal project；项目名门禁降低误执行 `down -v` 到生产 project 的风险。
- 本轮未实际执行 `down -v`，只通过测试验证失败路径会清理。

剩余问题：

- real mode 编排已经具备，但真实恢复演练尚未执行。
- 下一步需要在服务器启动隔离 rehearsal compose，使用真实备份执行 real rehearsal，确认 restore 后 smoke 通过，并记录隔离环境 cleanup 证据。
- 执行真实 rehearsal 会创建并删除隔离 volume，不触碰生产数据；但仍属于 Docker 资源创建/删除操作，执行前需再次确认当前目标 project 和命令。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.100：Docker 清理与防复发

完成事项：

- 在 `thinkpad:/opt/memory-os` 执行只读盘点，确认仓库本体约 `295M`，异常占用主要来自 Docker 镜像层。
- 执行安全清理：仅运行 Docker image prune，不删除容器、volume、网络、PostgreSQL、Archive、Qdrant、备份。
- 根分区从约 `60G used / 161G free / 28%` 改善为约 `47G used / 174G free / 22%`。
- 新增 Docker 悬空镜像定时清理安装脚本。
- 定时任务默认每天 `04:43` 执行 dangling image cleanup，并写入 `/opt/memory-os/artifacts/docker-cleanup-cron.log`。
- `prod-up` 仍在构建成功后执行 dangling image cleanup，避免每次重建留下悬空镜像层。

修改模块：

- `Makefile`
- `scripts/install-docker-cleanup-cron.sh`
- `internal/verify/verify_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

验证命令和结果：

- 本地：
  - `bash -n scripts/install-docker-cleanup-cron.sh scripts/docker-cleanup-images.sh scripts/docker-cleanup-plan.sh` 通过。
  - `go test ./internal/verify -run 'TestDockerCleanup|TestInstallDockerCleanup|TestMakefileExposesInstallDockerCleanup' -count=1` 通过。
  - `bash scripts/install-docker-cleanup-cron.sh` dry-run 通过。
- 服务器：
  - `bash -n scripts/install-docker-cleanup-cron.sh scripts/docker-cleanup-images.sh scripts/docker-cleanup-plan.sh` 通过。
  - `PATH=/usr/local/go/bin:$PATH go test ./internal/verify -run "TestDockerCleanup|TestInstallDockerCleanup|TestMakefileExposesInstallDockerCleanup" -count=1` 通过。
  - `DRY_RUN=0 CONFIRM_CRON_INSTALL=I_UNDERSTAND bash scripts/install-docker-cleanup-cron.sh` 安装成功。
  - `crontab -l | grep "memory-os docker dangling image cleanup"` 可查到定时任务。

部署状态：

- 未重建或重启 Memory OS 线上容器。
- 未执行 migration。
- 未删除任何 Docker volume。
- 未删除 PostgreSQL、Redis、Qdrant 或 Archive 数据。
- 已安装服务器 cron：每天只清理 Docker dangling images。

安全检查：

- 清理脚本仅允许 `dangling` 与显式 `unused-24h` 两种 image prune 模式。
- cron 固定使用 `DOCKER_IMAGE_CLEANUP_MODE=dangling`。
- 脚本和测试明确禁止 `docker volume prune`、`docker system prune`、`docker container prune`。
- 真实清理和真实 cron 安装均需要确认环境变量。

剩余问题：

- 服务器上仍有约 `8.2GB` 活跃镜像层，被运行中容器引用，不应清理。
- Docker volume 有约 `431M` 可回收空间，但 volume 可能包含生产数据或历史状态，本轮没有清理。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.101：Restore Rehearsal 隔离 Compose 安全修复

完成事项：

- 修复 `restore.sh` 对 `COMPOSE_T480_FILE=` 的处理：显式传空时不再自动回退到生产 T480 overlay。
- `restore.sh` 的 PostgreSQL restore 命令支持单 compose 文件，用于隔离恢复演练。
- 修复 `restore-rehearsal.sh` real mode：调用 restore 时强制注入隔离环境变量。
  - `COMPOSE_FILE=deploy/docker-compose.restore-rehearsal.yml`
  - `COMPOSE_T480_FILE=`
  - `COMPOSE_PROJECT_NAME=memory-os-restore-rehearsal`
  - `QDRANT_URL=http://qdrant:6333`
  - `QDRANT_RESTORE_DOCKER_NETWORK=memory-os-restore-rehearsal_default`
- 修复 `restore-rehearsal.sh` dry-run：审计命令也指向隔离 compose，避免 dry-run 产物看起来像生产恢复。
- real mode 默认 smoke 改为一次性 Go 容器接入 rehearsal Docker 网络，验证隔离环境内部的 API / Web / MCP / Qdrant，而不是宿主生产端口。
- 补充测试覆盖：
  - `restore.sh` 支持单 compose 文件。
  - real rehearsal 必须把隔离 compose/project/Qdrant URL 传给 restore。
  - dry-run 审计不得包含 `docker-compose.t480.yml`。

修改模块：

- `scripts/restore.sh`
- `scripts/restore-rehearsal.sh`
- `internal/restore/restore_script_test.go`
- `internal/restore/restore_rehearsal_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

验证命令和结果：

- 本地红灯：
  - `go test ./internal/restore -run 'TestRestoreScriptSupportsSingleComposeFileForRehearsal|TestRestoreRehearsalRealModePassesIsolatedComposeToRestore' -count=1`
  - 结果：失败，证明旧脚本仍会拼入生产 T480 overlay，且 real rehearsal 未传隔离 compose/project。
- 本地绿灯：
  - `bash -n scripts/restore.sh scripts/restore-rehearsal.sh` 通过。
  - `go test ./internal/restore -count=1` 通过。
  - `go test ./internal/restore ./internal/verify ./internal/webdeploy -count=1` 通过。
  - `go test ./...` 通过。
- 服务器验证：
  - 同步 restore 脚本和测试到 `thinkpad:/opt/memory-os`。
  - `bash -n scripts/restore.sh scripts/restore-rehearsal.sh` 通过。
  - `PATH=/usr/local/go/bin:$PATH go test ./internal/restore ./internal/verify ./internal/webdeploy -count=1` 通过。
  - 使用真实备份目录执行 rehearsal dry-run，通过。
  - dry-run 生成的 PostgreSQL restore 审计命令只包含 `deploy/docker-compose.restore-rehearsal.yml`，不包含 `docker-compose.t480.yml`。
  - dry-run 生成的 Qdrant restore 审计命令包含 `--network memory-os-restore-rehearsal_default` 和 `http://qdrant:6333`。

部署状态：

- 未执行真实 restore。
- 未启动或删除 rehearsal compose volume。
- 未重建或重启线上容器。
- 未修改公开端口。
- 未执行 migration。
- 未删除生产数据。

安全检查：

- 本轮修复了真实恢复演练可能误用生产 compose overlay 的 P1 风险。
- dry-run 和 real mode 现在都能从审计命令证明目标是隔离 compose。
- production restore 的双确认门禁仍保留。
- rehearsal real mode 仍需要 `CONFIRM_RESTORE_REHEARSAL=I_UNDERSTAND_TEST_RESTORE`。
- rehearsal project 名仍拒绝 `deploy`、`production`、`prod` 等生产名。

剩余问题：

- 真实隔离恢复演练尚未执行。
- 下一步真实 rehearsal 会创建并在结束时删除隔离 Docker volume，不触碰生产 volume；由于涉及 `down -v` 删除测试 volume，执行前需要用户确认。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.102：Restore Rehearsal Preflight 门禁

完成事项：

- 新增 `scripts/restore-rehearsal-preflight.sh`，用于真实恢复演练前只读检查。
- 新增 `make restore-rehearsal-preflight` 入口。
- real mode 在 `up` 之前强制执行 preflight。
- preflight 检查内容：
  - `BACKUP_DIR` 必须存在。
  - `manifest.json` 必须存在。
  - `RESTORE_REHEARSAL_PROJECT` 不能是 `deploy`、`production`、`prod` 等生产 project 名。
  - `deploy/docker-compose.restore-rehearsal.yml` 必须存在。
  - rehearsal compose 不得包含 `ports:`。
  - rehearsal compose 不得复用 `postgres_data`、`redis_data`、`qdrant_data`、`archive_data` 等生产 volume。
  - rehearsal compose 必须包含 `restore_rehearsal_pg`、`restore_rehearsal_qdrant`、`restore_rehearsal_archive` 等隔离标记。
  - Docker 中不得存在同名 compose project 的历史容器或 volume 残留。
- preflight 会写入审计文件 `preflight.txt`，记录 backup、project、compose 和 `status=ok`。

修改模块：

- `Makefile`
- `scripts/restore-rehearsal-preflight.sh`
- `scripts/restore-rehearsal.sh`
- `internal/restore/restore_rehearsal_script_test.go`
- `internal/verify/verify_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

验证命令和结果：

- 本地：
  - `bash -n scripts/restore-rehearsal-preflight.sh scripts/restore-rehearsal.sh scripts/restore.sh` 通过。
  - `go test ./internal/restore ./internal/verify ./internal/webdeploy -count=1` 通过。
  - `go test ./...` 通过。
- 服务器：
  - 同步相关脚本和测试到 `thinkpad:/opt/memory-os`。
  - `bash -n scripts/restore-rehearsal-preflight.sh scripts/restore-rehearsal.sh scripts/restore.sh` 通过。
  - `PATH=/usr/local/go/bin:$PATH go test ./internal/restore ./internal/verify ./internal/webdeploy -count=1` 通过。
  - 对真实备份目录执行 preflight 通过：
    - `backup_dir=/opt/memory-os/backups/phase-179-real-20260703T001809Z`
    - `project=memory-os-restore-rehearsal`
    - `compose_file=deploy/docker-compose.restore-rehearsal.yml`
    - `status=ok`

部署状态：

- 未执行真实 restore。
- 未执行 rehearsal `up`。
- 未执行 `down -v`。
- 未创建或删除 Docker volume。
- 未重建或重启线上容器。
- 线上容器保持 Up。
- 根分区约 `174G` 可用。

安全检查：

- preflight 是只读检查，不删除数据。
- 检测到同名 rehearsal 容器或 volume 残留会拒绝继续。
- real mode 在创建 rehearsal 环境前必须先通过 preflight。
- 本轮没有写入真实 Secret。
- 本轮没有暴露 PostgreSQL 或 Redis 端口。

剩余问题：

- 真实隔离恢复演练尚未执行。
- 执行真实演练仍需要用户确认，因为结束时会删除隔离测试 volume。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.103：Production Router 依赖门禁

完成事项：

- 为 `internal/http.RegisterRoutes` 增加生产依赖校验。
- `AppEnv=production` 时，如果关键服务未配置，router 会立即 panic，不再静默 fallback 到内存仓库或未配置 handler。
- 生产必须配置的依赖：
  - Auth
  - Tenant
  - Unified Retrieval
  - Retrieval Access Log
  - Hot Memory
  - TurnEvent EventLog
  - Audit
  - Secret Vault
  - Archive
  - Archive Queue
  - Archive Index Queue
  - Qdrant Status
- development/test 路径保持兼容，仍允许轻量 router 和显式 dev endpoints。
- 补充测试证明生产缺依赖时会失败，避免未来把半成品路由带到生产。

修改模块：

- `internal/http/router.go`
- `internal/http/router_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

验证命令和结果：

- 本地：
  - `go test ./internal/http -count=1` 通过。
  - `go test ./cmd/memory-api ./internal/http -count=1` 通过。
  - `go test ./...` 通过。
- 服务器：
  - 同步 `internal/http/router.go` 和 `internal/http/router_test.go` 到 `thinkpad:/opt/memory-os`。
  - `PATH=/usr/local/go/bin:$PATH go test ./cmd/memory-api ./internal/http -count=1` 通过。
  - `PATH=/usr/local/go/bin:$PATH go test ./...` 通过。

部署状态：

- 本轮未重启线上服务。
- 本轮未执行 migration。
- 本轮未修改端口。
- 本轮未删除数据。

安全检查：

- 生产 router 不再允许缺服务时自动使用内存 fallback。
- dev smoke endpoints 仍只在 `AppEnv=development && EnableDevEndpoints=true` 时注册。
- 本轮没有写入真实 Secret。

剩余问题：

- 该门禁已在代码和测试层验证，但尚未重建部署线上 API。
- 真实隔离恢复演练尚未执行。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.104：OpenAPI / Router 一致性门禁

完成事项：

- 新增 OpenAPI 覆盖测试，从 `internal/http/router.go` 自动提取所有非 dev 的 `engine.GET` / `engine.POST` 注册路由。
- 测试要求每个生产路由都在 `/openapi.json` 的 `paths` 中出现，并包含对应 HTTP method。
- 修复发现的 OpenAPI 漏项：
  - `POST /memory/turn-event`
- 后续新增生产路由时，如果忘记同步 OpenAPI，`go test ./internal/http` 会失败。

修改模块：

- `internal/http/router.go`
- `internal/http/router_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- OpenAPI 文档新增 `POST /memory/turn-event` 描述。
- 运行时 handler 未变更。

新增 migration：

- 无。

验证命令和结果：

- 本地红灯：
  - `go test ./internal/http -run TestOpenAPICoversRegisteredProductionRoutes -count=1`
  - 结果：失败，发现 OpenAPI 缺少 `POST /memory/turn-event`。
- 本地绿灯：
  - `go test ./internal/http -run TestOpenAPICoversRegisteredProductionRoutes -count=1` 通过。
  - `go test ./internal/http ./cmd/memory-api -count=1` 通过。
  - `go test ./...` 通过。
- 服务器：
  - 同步 `internal/http/router.go` 和 `internal/http/router_test.go` 到 `thinkpad:/opt/memory-os`。
  - `PATH=/usr/local/go/bin:$PATH go test ./internal/http ./cmd/memory-api -count=1` 通过。
  - `PATH=/usr/local/go/bin:$PATH go test ./...` 通过。
  - 线上当前 `/openapi.json` 可解析，返回 OpenAPI `3.0.3`，当前运行镜像包含 `45` 个 paths。

部署状态：

- 本轮未重启线上 API。
- 新 OpenAPI 补齐已进入服务器工作区代码，但尚未构建部署到线上镜像。
- 未执行 migration。
- 未修改端口。
- 未删除数据。

安全检查：

- 本轮没有写入真实 Secret。
- 本轮没有暴露内部栈或敏感配置。
- 测试只解析源码和 OpenAPI JSON，不访问数据库或生产数据。

剩余问题：

- 新 OpenAPI 补齐需要下次确认部署后才会出现在当前线上容器。
- OpenAPI 当前仍是轻量手写 spec，不等价于完整 schema 级契约；本轮只保证 route path/method 覆盖。
- 真实隔离恢复演练尚未执行。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.105：Post-deploy OpenAPI Runtime 校验

完成事项：

- 新增 `scripts/validate-openapi-runtime.py`，用于解析运行中服务的 OpenAPI JSON。
- `scripts/post-deploy-verify.sh` 新增 `openapi-validate` 步骤。
- post-deploy 现在不仅检查 `/openapi.json` HTTP 200，还会验证：
  - OpenAPI 版本是 `3.0.3`。
  - 关键生产路径存在。
  - 关键路径包含正确 HTTP method。
- 当前强制校验路径：
  - `GET /healthz`
  - `GET /openapi.json`
  - `GET /version`
  - `POST /memory/turn-event`
  - `POST /memory/search`
  - `POST /memory/qdrant/status`
- `post-deploy-verify` 的日志目录现在会包含 `openapi-validate.log`。

修改模块：

- `scripts/post-deploy-verify.sh`
- `scripts/validate-openapi-runtime.py`
- `internal/verify/verify_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无运行时 handler 变更。
- 新增部署后 OpenAPI 内容校验工具。

新增 migration：

- 无。

验证命令和结果：

- 本地：
  - `bash -n scripts/post-deploy-verify.sh` 通过。
  - `python3 -m py_compile scripts/validate-openapi-runtime.py` 通过。
  - `go test ./internal/verify -count=1` 通过。
  - `go test ./...` 通过。
- 运行中线上 API 现状检查：
  - `python3 scripts/validate-openapi-runtime.py http://ddns.08121.top:18081/openapi.json` 失败。
  - 失败原因：当前运行容器尚未部署 Phase 1.104 的 OpenAPI 补齐，缺少 `POST /memory/turn-event`。
  - 结论：新门禁可以正确暴露“服务器工作区代码已修，但线上运行镜像未更新”的差异。
- 服务器：
  - 同步脚本和测试到 `thinkpad:/opt/memory-os`。
  - `bash -n scripts/post-deploy-verify.sh` 通过。
  - `python3 -m py_compile scripts/validate-openapi-runtime.py` 通过。
  - `PATH=/usr/local/go/bin:$PATH go test ./internal/verify -count=1` 通过。
  - `PATH=/usr/local/go/bin:$PATH go test ./...` 通过。

部署状态：

- 本轮未重启线上服务。
- 本轮未执行 migration。
- 本轮未修改端口。
- 本轮未删除数据。
- 新 post-deploy 门禁已进入服务器工作区，但需要下次确认部署后才会用于验证新镜像。

安全检查：

- Runtime validator 只读取 OpenAPI JSON，不读取数据库、Secret、日志或备份。
- 本轮没有写入真实 Secret。

剩余问题：

- 当前线上运行镜像仍缺 `/memory/turn-event` OpenAPI path；需要确认部署新镜像后再运行 `make post-deploy-verify`。
- 真实隔离恢复演练尚未执行。
- 不能声明 v0.4 生产级完全体完成。

## 2026-07-03 Phase 1.106：Post-deploy OpenAPI Validator 稳定化

完成事项：

- 将 `post-deploy-verify` 的 OpenAPI 校验源拆为 `OPENAPI_SPEC_SOURCE`。
- 默认 `OPENAPI_VALIDATE_CMD` 使用 `shell_quote` 引用 `OPENAPI_SPEC_SOURCE`，避免 URL 或文件路径中包含特殊字符时产生 shell 拆分问题。
- 保留 `OPENAPI_VALIDATE_CMD` 覆盖能力，方便测试和特殊部署环境替换。
- 验证 `scripts/validate-openapi-runtime.py` 同时支持 URL 和本地 JSON 文件。

修改模块：

- `scripts/post-deploy-verify.sh`
- `scripts/validate-openapi-runtime.py`
- `internal/verify/verify_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

验证命令和结果：

- 本地：
  - `bash -n scripts/post-deploy-verify.sh` 通过。
  - `python3 -m py_compile scripts/validate-openapi-runtime.py` 通过。
  - 使用临时 OpenAPI JSON 文件运行 `python3 scripts/validate-openapi-runtime.py <file>` 通过。
  - `go test ./internal/verify -count=1` 通过。
  - `go test ./...` 通过。
- 服务器：
  - 同步脚本和测试到 `thinkpad:/opt/memory-os`。
  - `bash -n scripts/post-deploy-verify.sh` 通过。
  - `python3 -m py_compile scripts/validate-openapi-runtime.py` 通过。
  - 使用临时 OpenAPI JSON 文件运行 validator 通过。
  - `PATH=/usr/local/go/bin:$PATH go test ./internal/verify -count=1` 通过。
  - `PATH=/usr/local/go/bin:$PATH go test ./...` 通过。

部署状态：

- 本轮未重启线上服务。
- 本轮未执行 migration。
- 本轮未修改端口。
- 本轮未删除数据。

安全检查：

- 新增 shell quote 只处理 URL/文件路径，不读取或输出 Secret。
- 本轮没有写入真实 Secret。

剩余问题：

- 当前线上运行镜像仍缺 `/memory/turn-event` OpenAPI path，需要确认部署后运行新的 post-deploy 门禁。
- 真实隔离恢复演练尚未执行。
- 不能声明 v0.4 生产级完全体完成。

## Latest Phase Pointer

## 2026-07-03 Phase 1.107：生产部署与 Post-deploy 验证闭环

完成事项：

- 在 `thinkpad:/opt/memory-os` 执行生产构建与容器替换。
- 重建并启动 `memory-api`、`memory-worker`、`memory-mcp`、`memory-web`。
- `prod-up` 完成后自动清理 dangling Docker images，回收约 `600.7MB`。
- 修复 `post-deploy-verify` 的 `pipeline-e2e` 默认执行方式：不再在宿主机直接 `make smoke`，改为显式在 `deploy_default` Docker 网络内运行 Go smoke 容器，避免服务器安装 Go 时绕过 compose DNS。
- 部署后验证已覆盖 compose 状态、版本、healthz、OpenAPI JSON、OpenAPI runtime coverage、默认 smoke、pipeline e2e。

修改模块：

- `scripts/post-deploy-verify.sh`
- `scripts/verify.sh`
- `internal/verify/verify_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

验证命令和结果：

- 服务器 `PATH=/usr/local/go/bin:$PATH make prod-up`：通过。
- 服务器 `PATH=/usr/local/go/bin:$PATH go test ./internal/verify -count=1`：通过。
- 服务器 `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过。
- post-deploy 日志目录：`/tmp/memory-os-post-deploy.liDiQU`。
- OpenAPI runtime validator：`openapi ok: 46 paths`。

失败命令、根因和修复：

- 首次 `make post-deploy-verify` 失败于 `pipeline-e2e`。
- 根因：`pipeline-e2e` 设置了 `SMOKE_API_URL=http://memory-api:18081/healthz`，但默认调用 `make smoke`；服务器存在 Go 时 Makefile 直接在宿主机运行 smoke，宿主机无法解析 `memory-api` compose 服务名。
- 修复：默认 `pipeline-e2e` 改为 `docker run --rm --network deploy_default ... golang:1.25-bookworm go run ./cmd/memory-smoke`，并补回归测试。

部署状态：

- 生产容器状态：`memory-api`、`memory-worker`、`memory-mcp`、`memory-web`、`postgres`、`redis`、`qdrant` 均运行中。
- 对外端口保持不变：Web `18080`、API `18081`、MCP `18082`、Qdrant `18083`。
- 未执行生产数据删除。
- 未 commit、未 push。

安全检查：

- `post-deploy-verify` 仍从运行中 API 容器读取 `POSTGRES_DSN` 后仅传入子进程环境，不写入命令字符串。
- post-deploy 日志目录使用 `mktemp -d` 且权限收敛为 `0700`。
- 本轮没有输出或写入真实 Secret 明文。

剩余问题：

- 该切片只证明生产部署和自动化部署后验证闭环，不代表 v0.4 全部 Phase 完成。

## 2026-07-03 Phase 1.108：真实隔离 Restore Rehearsal 通过

完成事项：

- 在 `thinkpad:/opt/memory-os` 使用真实备份目录执行隔离恢复演练：
  - `BACKUP_DIR=/opt/memory-os/backups/phase-179-real-20260703T001809Z`
  - `RESTORE_REHEARSAL_MODE=real`
  - `CONFIRM_RESTORE_REHEARSAL=I_UNDERSTAND_TEST_RESTORE`
- 恢复演练使用独立 compose project：`memory-os-restore-rehearsal`。
- 恢复演练结束后执行 `down -v`，隔离测试容器、网络、测试卷均已删除。
- PostgreSQL dump 恢复成功。
- Markdown Archive tarball 恢复成功。
- Qdrant snapshot upload 恢复成功。
- restore 内部 smoke 通过。
- 应用容器启动后的最终 smoke 通过。

修改模块：

- `scripts/restore.sh`
- `scripts/restore-rehearsal.sh`
- `deploy/docker-compose.restore-rehearsal.yml`
- `internal/restore/restore_script_test.go`
- `internal/restore/restore_rehearsal_script_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

验证命令和结果：

- 本地 `go test ./internal/restore -count=1`：通过。
- 服务器 `PATH=/usr/local/go/bin:$PATH go test ./internal/restore -count=1`：通过。
- 服务器 restore compose config 校验：通过，包含 `memory-web`，不再包含 `docker-entrypoint-initdb.d` 或 `../migrations`。
- 服务器 Qdrant curl 容器 NO_PROXY 诊断：`healthz check passed`。
- 服务器真实恢复演练：通过。
- 最终服务器健康检查：
  - `df -h /`：`172G` 可用，`23%` 使用率。
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps`：生产容器均 Up。
  - `curl -fsS http://127.0.0.1:18081/healthz`：返回 `status=ok`，db/qdrant/redis 均 `ok`。
  - `python3 scripts/validate-openapi-runtime.py http://127.0.0.1:18081/openapi.json`：`openapi ok: 46 paths`。
  - `docker ps -a --filter name=memory-os-restore-rehearsal`：无残留。
  - `docker volume ls --filter name=memory-os-restore-rehearsal`：无残留。

失败命令、根因和修复：

- 第一次真实恢复演练失败：restore compose 缺少 `POSTGRES_PASSWORD` 等生产变量。
- 根因：`restore-rehearsal.sh` 默认真实路径没有加载 `scripts/load-prod-env.sh`。
- 修复：默认真实 compose up 路径在当前 shell source `scripts/load-prod-env.sh`。

- 第二次失败：`no such service: memory-web`。
- 根因：`restore-rehearsal.sh` 默认 up/smoke 覆盖 Web，但 `deploy/docker-compose.restore-rehearsal.yml` 缺少 `memory-web` 服务。
- 修复：补 `memory-web` 服务，使用 `deploy-memory-web` 镜像。

- 第三、四次失败：PostgreSQL restore 出现大量 `relation already exists` / FK 错误。
- 根因：恢复演练在 restore 前启动 API/worker，应用自动 migration 抢先建表；同时 restore compose 的 Postgres 初始化挂载了 migrations。
- 修复：restore compose 移除 `/docker-entrypoint-initdb.d` migration 挂载；默认演练拆为 `infra up -> wait -> restore -> app up -> smoke -> cleanup`。

- Qdrant restore/health 曾返回 `502`。
- 根因：一次性 curl 容器没有传入 `NO_PROXY/no_proxy`，内部服务名 `qdrant` 被代理劫持。
- 修复：Qdrant health wait 和 snapshot upload 的 curl 容器都注入 `NO_PROXY/no_proxy`。

- Qdrant snapshot upload 曾出现 `curl: (26)`。
- 根因：备份 snapshot 文件权限为 `0600 root`，`curlimages/curl` 默认非 root 用户无法读取只读挂载文件。
- 修复：snapshot upload 容器使用 `--user 0:0` 读取只读挂载，不放宽备份文件权限。

- 最终 smoke 曾失败于 `memory-web:80` connection refused。
- 根因：Web Nginx 实际监听 `18080`。
- 修复：restore rehearsal 默认 `SMOKE_WEB_URL` 改为 `http://memory-web:18080`。

部署状态：

- 生产服务未因 restore rehearsal 被删除或替换。
- restore rehearsal 仅创建并删除隔离测试 project、网络、容器和测试卷。
- 未修改公开端口。
- 未 commit、未 push。

安全检查：

- 真实备份文件权限未放宽。
- Qdrant snapshot 通过只读 volume mount 上传。
- restore/rehearsal 审计文件只记录命令形态，不记录 Secret 明文。
- 本轮没有在文档中写入真实密码、token、API key、私钥或 cookie。

剩余问题：

- 真实恢复演练已通过，但仍属于 Phase 1 生产基座/运维门禁的一部分。
- v0.4 生产级完全体仍未完成，后续还需要继续推进 UI 真实化、权限浏览器验收、Secret 泄漏扫描、跨租户隔离浏览器验收等 Phase 任务。

## Latest Phase Pointer

- 最新完成切片：`2026-07-03 Phase 1.108：真实隔离 Restore Rehearsal 通过`。
- 详细证据位置：本文搜索 `Phase 1.108`。
- 下一步入口：继续推进 Phase 2/Phase 10 的管理台真实操作闭环与浏览器验收，优先消灭“页面能看不能改/删”的生产级缺口。
- 当前运维状态：生产容器已部署并通过 post-deploy；真实隔离 restore rehearsal 已通过；磁盘阻塞已解除并有 dangling image 自动清理；OpenAPI runtime coverage 门禁已生效。

---

## 2026-07-03 Phase 1.109：用户管理真实列表 API 与管理台入口

完成事项：

- 新增用户 metadata 列表能力，避免管理台缺少“用户页”的 Phase 10 缺口继续扩大。
- 新增生产 API：`POST /memory/tenant/users/list`。
- 该接口默认需要 PAT `memory:read`，未认证访问返回 `401 pat_required`。
- 返回内容只包含用户 metadata，不返回密码、token、secret 或 credential。
- 新增 Nuxt 用户管理页 `/users`，支持按状态筛选、查询用户列表、创建用户。
- AppShell 新增“用户”导航入口。
- OpenAPI 路由覆盖校验已同步更新。

修改模块：

- `internal/tenant/repository.go`
- `internal/tenant/service.go`
- `internal/tenant/pg_repository.go`
- `internal/tenant/service_test.go`
- `internal/tenant/pg_repository_test.go`
- `internal/http/router.go`
- `internal/http/router_test.go`
- `web/stores/context.ts`
- `web/components/AppShell.vue`
- `web/pages/users/index.vue`
- `docs/production-delivery-log.md`

新增或变更 API：

- `POST /memory/tenant/users/list`
  - Auth：PAT `memory:read`
  - Request：`{"status":"active|disabled|deleted|"}`
  - Response：`{"items":[{"id","email","display_name","status","created_at","updated_at"}]}`

新增 migration：

- 无。本轮复用已有 `users` 表与字段。

测试命令和结果：

- 本地红灯测试：
  - `go test ./internal/tenant ./internal/http -run 'TestServiceListsUsers|TestPGRepositoryRequiresPool|TestPGRepositoryCreatesTenantGraph|TestTenantUserListRequiresPATAndReturnsUserMetadata' -count=1`
  - 结果：按预期失败，原因是 `ListUsers` contract 与 `/memory/tenant/users/list` route 尚未实现。
- 本地目标测试：
  - `go test ./internal/tenant ./internal/http -run 'TestServiceListsUsers|TestPGRepositoryRequiresPool|TestPGRepositoryCreatesTenantGraph|TestTenantUserListRequiresPATAndReturnsUserMetadata|TestOpenAPICoversRegisteredProductionRoutes' -count=1`
  - 结果：通过。
- 本地包测试：
  - `go test ./internal/tenant ./internal/http -count=1`
  - 结果：通过。
- 服务器目标测试：
  - `PATH=/usr/local/go/bin:$PATH go test ./internal/tenant ./internal/http -count=1`
  - 结果：通过。
- 服务器前端构建：
  - `npm --prefix web run build`
  - 结果：通过，Nuxt 构建完成。
- 服务器生产部署：
  - `PATH=/usr/local/go/bin:$PATH make prod-up`
  - 结果：通过，API/worker/MCP/Web 镜像重新构建并启动。
- 服务器部署后验证：
  - `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`
  - 结果：通过，包含 compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。

运行时验证：

- `curl -X POST http://127.0.0.1:18081/memory/tenant/users/list`
  - 结果：`401 {"error":"pat_required"}`，确认生产路由存在且默认需要认证。
- `curl -fsSL http://127.0.0.1:18080/users/`
  - 结果：返回 Nuxt 静态 HTML，确认生产 Web 已包含用户页。
- `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps`
  - 结果：`memory-api`、`memory-web`、`memory-worker`、`memory-mcp` 均 Up，Postgres/Redis healthy，Qdrant Up。

失败命令、根因和修复：

- `docker compose -f ... ps` 失败：服务器 Docker CLI 不支持 compose v2 子命令。
  - 修复：沿用项目实际工具链 `docker-compose`。
- `docker-compose -f ... ps` 首次失败：未加载生产 `.env`，缺少 `LLM_BASE_URL`。
  - 修复：执行 `. scripts/load-prod-env.sh` 后重查状态。
- `curl http://127.0.0.1:18080/users` 返回 301。
  - 根因：Nginx/静态目录规范化到 `/users/`。
  - 修复：使用 `curl -L` 或访问 `/users/`，页面可正常返回。

部署状态：

- 已部署到 `thinkpad:/opt/memory-os`。
- 未修改公开端口。
- 未删除生产数据。
- 未 commit、未 push。
- `make prod-up` 自动清理 dangling image，回收约 `600.8MB`。

安全检查：

- 用户列表 API 不返回密码 hash、PAT、Adapter Token、Secret 明文。
- 未认证访问返回 `401 pat_required`。
- 本轮未写入真实 API key、token、cookie、私钥、密码或助记词。
- `codebase-memory-mcp` 本轮不可用，`list_projects` 和 `index_status` 均返回 `Transport closed`，已降级使用 `rg` 和文件读取完成审查。

权限隔离结果：

- 本轮完成的是认证门禁和 metadata-only 返回验证。
- 细粒度跨用户/org/project 的浏览器级隔离验收尚未完成，继续列为后续 Phase 2/Phase 10 必做项。

剩余问题：

- `/users` 页已接真实 API，但还未完成浏览器自动化写入与刷新验证。
- 权限管理页仍未补齐，是 Phase 10 的剩余显性缺口。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 继续补齐“权限页”与角色/权限标签真实管理闭环。
- 对用户页执行浏览器自动验收：登录、筛选、创建用户、刷新后验证持久化、错误态验证。

## Latest Phase Pointer

- 最新完成切片：`2026-07-03 Phase 1.109：用户管理真实列表 API 与管理台入口`。
- 详细证据位置：本文搜索 `Phase 1.109`。
- 下一步入口：补齐权限管理页与用户页浏览器自动验收，继续消灭 Phase 10 管理台真实操作缺口。
- 当前运维状态：生产容器已重新部署并通过 post-deploy；`/users/` 生产页面可访问；`/memory/tenant/users/list` 生产 API 已上线且默认需要认证。

---

## 2026-07-03 Phase 1.110：权限管理页接入真实成员治理 API

完成事项：

- 新增 Nuxt 权限管理页 `/permissions`。
- AppShell 新增“权限”导航入口。
- 权限页复用现有真实 API：
  - `POST /memory/tenant/users/list`
  - `POST /memory/tenant/memberships/list`
  - `POST /memory/tenant/memberships/add`
  - `POST /memory/tenant/memberships/update-role`
  - `POST /memory/tenant/memberships/remove`
- 页面展示 `owner/admin/member` 角色对应的权限标签预览：
  - `user:<user_id>:read`
  - `org:<org_id>:member`
  - `project:<project_id>:read`
  - `project:<project_id>:write`
  - `secret:<project_id>:use`
- 页面支持真实添加授权、保存角色、移除授权，不使用静态假数据。

修改模块：

- `internal/webdeploy/web_dockerfile_test.go`
- `web/components/AppShell.vue`
- `web/stores/context.ts`
- `web/pages/permissions/index.vue`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。本轮复用既有 membership governance API，避免新增 schema 和额外权限模型。

新增 migration：

- 无。

TDD 记录：

- 先新增失败测试：
  - `go test ./internal/webdeploy -run TestPermissionsPageUsesRealMembershipGovernanceAPI -count=1`
  - 结果：失败，原因是 `web/pages/permissions/index.vue` 不存在。
- 实现权限页和导航后重跑：
  - `go test ./internal/webdeploy -run TestPermissionsPageUsesRealMembershipGovernanceAPI -count=1`
  - 结果：通过。

测试命令和结果：

- 本地 `gofmt -w internal/webdeploy/web_dockerfile_test.go`：完成。
- 本地 `go test ./internal/webdeploy -count=1`：通过。
- 本地 `npm --prefix web run build`：通过，Nuxt client/server build 完成。
- 服务器 `PATH=/usr/local/go/bin:$PATH go test ./internal/webdeploy -count=1`：通过。
- 服务器 `npm --prefix web run build`：通过。
- 服务器 `PATH=/usr/local/go/bin:$PATH make prod-up`：通过，API/worker/MCP/Web 重新构建并启动。
- 服务器 `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过，包含 compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。

运行时验证：

- `nuxt generate` 预渲染路由包含 `/permissions`，总路由数从 15/16 类似页面集合增加到包含权限页。
- `curl -fsSL http://127.0.0.1:18080/permissions/`：返回 Nuxt 静态 HTML。
- `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps`：`memory-api`、`memory-web`、`memory-worker`、`memory-mcp` 均 Up，Postgres/Redis healthy，Qdrant Up。

浏览器验收：

- Playwright 打开 `http://ddns.08121.top:18080/permissions/`。
- 未登录状态下被重定向到 `http://ddns.08121.top:18080/login`。
- 页面快照显示登录页文案、PAT 输入框和“进入记忆控制台”按钮。
- 控制台出现的 `401 Unauthorized` 来自 `/memory/tenant/memberships/list`、`/memory/tenant/orgs/list`、`/memory/tenant/users/list`，属于未登录认证门禁返回，不是匿名可访问。

失败命令、根因和修复：

- `go test ./internal/webdeploy -run TestPermissionsPageUsesRealMembershipGovernanceAPI -count=1` 初次失败。
  - 根因：权限页尚未存在。
  - 修复：新增 `/permissions` 页面并加入导航。

部署状态：

- 已部署到 `thinkpad:/opt/memory-os`。
- 未修改公开端口。
- 未删除生产数据。
- 未 commit、未 push。
- `make prod-up` 自动清理 dangling image，回收约 `600.7MB`。

安全检查：

- 本轮未新增 Secret 存储或解密路径。
- 权限页只展示权限标签模板和 user/org/project metadata，不展示 PAT、Adapter Token、Secret 明文或密码。
- 未认证访问管理 API 返回 `401`。
- 本轮未在代码、日志、文档或回复中写入真实 API key、token、cookie、私钥、密码或助记词。
- `codebase-memory-mcp` 本轮仍不可用，`list_projects`、`index_status`、`search_graph` 均返回 `Transport closed`，已按规则降级为 `rg` 和直接文件读取。

权限隔离结果：

- 后端 membership API 的 PAT/owner/member 权限测试沿用既有 `internal/http` 和 `internal/tenant` 覆盖。
- 本轮新增页面的静态契约测试确认它调用真实 membership governance API。
- 登录后浏览器写操作验收尚未完成，因为当前浏览器没有可安全复用的 PAT；后续总验收需创建短期测试 PAT 并执行添加成员、保存角色、移除授权、刷新持久化检查。

剩余问题：

- 权限页已上线，但还不是完整 RBAC/permission label 自定义系统；当前遵循现有 `owner/admin/member` 固定角色模型。
- 登录后浏览器自动化写操作验收未完成。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 创建短期测试 PAT，在浏览器里完成 `/users` 和 `/permissions` 写操作验收。
- 继续推进 Phase 10 管理台剩余页面的浏览器验收与错误态验证。

## Latest Phase Pointer

- 最新完成切片：`2026-07-03 Phase 1.110：权限管理页接入真实成员治理 API`。
- 详细证据位置：本文搜索 `Phase 1.110`。
- 下一步入口：执行登录态浏览器验收，优先覆盖用户页和权限页的写入、刷新持久化、错误态。
- 当前运维状态：生产容器已重新部署并通过 post-deploy；`/permissions/` 生产页面可访问；未登录访问会回到登录页并触发管理 API 的 `401` 门禁。

---

## 2026-07-03 Phase 1.111：用户页与权限页登录态浏览器写操作验收

完成事项：

- 创建短期浏览器验收主体：
  - 测试 owner 用户。
  - 测试组织。
  - 测试项目。
  - 30 分钟短期 PAT，scope 为 `memory:read` 与 `memory:write`。
- 通过真实登录页输入短期 PAT，进入管理台。
- 在 `/users/` 页面通过 UI 创建用户。
- 刷新 `/users/` 后验证新用户仍存在，证明写入走 PostgreSQL 持久化。
- 在 `/permissions/` 页面通过 UI 添加项目授权。
- 将授权角色从 `member` 修改为 `admin`。
- 验证 `admin` 角色权限标签展示：
  - `project:<project_id>:write`
  - `secret:<project_id>:use`
- 通过 UI 移除授权。
- 刷新 `/permissions/` 后验证授权状态仍为 `disabled`，证明移除状态持久化。
- 撤销短期 PAT。
- 验证撤销后的 PAT 再访问管理 API 返回 `401`。
- 浏览器登出。
- 对本轮创建的测试数据做软清理：
  - 2 个测试用户改为 `disabled`。
  - 1 个测试项目改为 `deleted`。
  - 1 个测试组织改为 `deleted`。

修改模块：

- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

测试命令和结果：

- 服务器 `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过，包含 compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。
- 服务器 `curl -fsS http://127.0.0.1:18081/healthz`：通过，db/qdrant/redis 均 `ok`。
- 服务器 `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps`：API、Web、Worker、MCP 均 Up，Postgres/Redis healthy，Qdrant Up。

浏览器验收：

- 登录页：
  - 通过真实登录页填写短期 PAT。
  - 登录后 URL 为 `/`。
  - 页面显示“原生记忆控制台”和测试 operator。
- 用户页 `/users/`：
  - 页面显示“用户管理”和“创建真实用户”。
  - 通过 UI 输入测试邮箱与显示名并点击“创建真实用户”。
  - 创建后页面显示测试邮箱。
  - 刷新后测试邮箱仍显示。
- 权限页 `/permissions/`：
  - 页面显示测试组织/项目、当前 owner 授权与新增用户选项。
  - 通过 UI 选择新用户和 `member`，点击“添加真实授权”。
  - 页面显示“授权已添加”和新用户邮箱。
  - 在该用户授权卡片中选择 `admin`，点击“保存角色”。
  - 页面显示“角色已保存”，且显示 `project:<project_id>:write` 与 `secret:<project_id>:use`。
  - 点击“移除授权”。
  - 页面显示“授权已移除”和 `状态：disabled`。
  - 刷新后仍显示该用户授权状态为 `disabled`。

安全检查：

- 短期 PAT 明文未写入代码、文档、日志或回复。
- 交付日志只记录非敏感 ID/行为结果，不记录 PAT 明文、token hash 或真实凭据。
- PAT 已撤销，撤销后访问 `/memory/tenant/users/list` 返回 `401`。
- 浏览器已登出。
- 测试数据采用软清理，没有物理删除生产数据。

失败命令、根因和修复：

- 第一次自动化创建验收数据超时。
  - 根因：默认 30 秒执行窗口不足，且自动化内核重置后不能确认 token 是否可用。
  - 修复：不复用不确定状态，重新创建新的短期验收主体。
- 第二次 psql 自动化失败。
  - 根因：自动化传 SQL 的方式不适配 `execFile` stdin。
  - 修复：改用 base64 管道传输 SQL，经 `select 1` 验证后再执行正式写入。
- 直接写 localStorage 失败。
  - 根因：浏览器自动化的 `evaluate` 是只读 DOM scope。
  - 修复：改用真实登录页表单输入短期 PAT。

部署状态：

- 本轮未改运行时代码，未重新部署。
- 生产服务在浏览器验收后通过 post-deploy 与 healthz 检查。
- 未修改公开端口。
- 未 commit、未 push。

权限隔离结果：

- UI 登录态操作使用短期 PAT，scope 包含 `memory:read`/`memory:write`。
- 权限页写操作依赖现有 owner 项目权限；member 添加、admin 保存、disabled 移除均通过真实 API 完成。
- 撤销 PAT 后接口返回 `401`，证明 token revocation 生效。

剩余问题：

- 本轮覆盖了 `/users` 与 `/permissions` 的登录态主流程，但尚未覆盖所有管理台页面的浏览器写操作与错误态。
- 仍需继续执行 Archive、Hot Memory、Secret Vault、Adapter Token、Qdrant 状态、检索测试页的浏览器验收。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 继续按 Phase 10 浏览器验收清单推进剩余页面。
- 优先验收 Secret Vault 与 Adapter Token，因为它们涉及敏感边界与撤销语义。

## 2026-07-03 Phase 1.112：Secret Vault 与 Adapter Token 登录态浏览器验收

完成事项：

- 创建短期浏览器验收主体：
  - 测试 owner 用户。
  - 测试组织。
  - 测试项目。
  - 30 分钟短期 PAT，scope 为 `memory:read` 与 `memory:write`。
- 通过真实登录页输入短期 PAT，进入管理台。
- 在 `/secrets/` 页面通过 UI 创建 Secret。
- 验证 Secret 创建后页面只显示 metadata，后端列表 API 也只返回 metadata。
- 验证 Secret 明文没有出现在页面正文或列表 API 响应中。
- 在 `/secrets/` 页面通过 UI 禁用 Secret。
- 验证刷新后 Secret 状态保持 `disabled`。
- 在 `/tokens/` 页面通过 UI 创建 PAT。
- 验证 PAT 明文只在一次性面板显示，关闭后面板消失。
- 验证 PAT 列表 API 只返回 metadata，不返回 `token` 或 `token_hash`。
- 在 `/tokens/` 页面通过 UI 创建 Adapter Token。
- 验证 Adapter Token 明文只在一次性面板显示，关闭后面板消失。
- 验证 Adapter Token 列表 API 只返回 metadata，不返回 `token` 或 `token_hash`。
- 在 `/tokens/` 页面通过 UI 撤销本轮创建的 PAT 和 Adapter Token。
- 刷新 `/tokens/` 后验证两类 token 状态均保持 `revoked`。
- 撤销短期登录 PAT。
- 验证撤销后的 PAT 再访问管理 API 返回 `401`。
- 对本轮创建的测试数据做软清理：
  - 1 个测试用户改为 `disabled`。
  - 1 个测试项目改为 `deleted`。
  - 1 个测试组织改为 `deleted`。
  - 本轮 PAT 与 Adapter Token 均保持 `revoked`。
  - 本轮 Secret 保持 `disabled`，用于审计语义验证。

修改模块：

- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

测试命令和结果：

- 服务器 `source scripts/load-prod-env.sh && docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps && curl -fsS http://127.0.0.1:18081/healthz`：通过，API/Web/Worker/MCP 均 Up，Postgres/Redis healthy，healthz 返回 db/qdrant/redis 均 `ok`。
- 服务器 `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过，包含 compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。

浏览器验收：

- 登录页：
  - 通过真实登录页填写短期 PAT。
  - 登录后 URL 为 `/`。
  - 页面显示“原生记忆控制台”和测试 operator。
- Secret Vault 页 `/secrets/`：
  - 页面显示“Secret Vault 管理”。
  - 当前组织和项目为本轮测试组织/项目。
  - “创建真实 Secret”按钮可用。
  - 通过 UI 输入 Secret 名称和测试明文，点击“创建真实 Secret”。
  - 创建后页面显示测试 Secret 名称和“页面只保留 metadata”提示。
  - 页面正文不包含测试 Secret 明文。
  - `/memory/secrets/list` 返回 `200`，存在本轮 Secret metadata，状态为 `active`，版本为 `1`。
  - Secret 列表 API 响应不包含测试 Secret 明文。
  - 通过 UI 点击“禁用”。
  - active 列表不再返回该 Secret。
  - disabled 列表返回该 Secret，状态为 `disabled`。
  - 刷新页面后仍显示 Secret Vault 页面，页面正文不包含测试 Secret 明文。
- Token 页 `/tokens/`：
  - 页面显示“Token 管理”。
  - 通过 UI 创建 PAT 后出现“PAT 一次性明文”面板。
  - 点击“我已保存，立即隐藏”后，一次性明文面板消失。
  - PAT metadata 存在且状态为 `active`。
  - PAT metadata 不包含 `token` 字段，不包含 `token_hash` 字段，只包含 `token_prefix` 等 metadata。
  - 通过 UI 创建 Adapter Token 后出现“Adapter Token 一次性明文”面板。
  - 点击“我已保存，立即隐藏”后，一次性明文面板消失。
  - Adapter Token metadata 存在且状态为 `active`。
  - Adapter Token metadata 不包含 `token` 字段，不包含 `token_hash` 字段，只包含 `token_prefix` 等 metadata。
  - 通过页面“撤销”按钮撤销本轮创建的 PAT 与 Adapter Token。
  - API 与刷新后的页面均显示两类 token 状态为 `revoked`。

安全检查：

- 短期登录 PAT、创建出的 PAT 明文、Adapter Token 明文和测试 Secret 明文均未写入代码、文档、日志或回复。
- 交付日志只记录行为结果，不记录 token 明文、token hash、Secret 明文或真实凭据。
- Secret API 的列表响应未返回明文。
- Token API 的列表响应未返回 `token` 或 `token_hash`。
- 短期登录 PAT 已撤销，撤销后访问 `/memory/tokens/pat/list` 返回 `401`。
- 浏览器已回到登录页。
- 测试数据采用软清理，没有物理删除生产数据。

失败命令、根因和修复：

- 首次服务器 compose 健康检查失败。
  - 根因：直接运行 compose 未加载生产环境变量，`LLM_BASE_URL` 等必需变量缺失。
  - 修复：改用 `source scripts/load-prod-env.sh` 后执行 compose 与 healthz 检查。
- 第一次创建验收主体失败。
  - 根因：线上 `memberships` 表使用 `role_id`，不是文本 `role` 字段。
  - 修复：读取真实 schema，改用 `roles.name='owner'` 对应的 `role_id`。
- 第二次创建验收主体失败。
  - 根因：线上 PAT 表名为 `personal_access_tokens`，不是 `pat_tokens`。
  - 修复：读取真实 auth 表结构后改用实际表名。
- Secret 创建后的等待方法失败。
  - 根因：当前浏览器封装不支持完整 Playwright `waitForFunction`。
  - 修复：改用短等待 + 后端 API 查询确认创建结果。
- 密码框值读取失败。
  - 根因：当前浏览器封装不支持 `inputValue`。
  - 修复：不读取密码框，改用页面正文和 API 响应确认明文未泄露。
- 批量读取 `code` 文本失败。
  - 根因：当前浏览器封装不支持 `allInnerTexts`。
  - 修复：改用一次性面板显隐和 metadata 字段形状验证。
- DOM evaluate 点击失败。
  - 根因：当前浏览器封装不支持 locator `evaluate`。
  - 修复：改用 `filter/getByRole` 精确定位指定 token 卡片中的“撤销”按钮。

部署状态：

- 本轮未改运行时代码，未重新部署。
- 生产服务在浏览器验收后通过 post-deploy 与 healthz 检查。
- 未修改公开端口。
- 未 commit、未 push。

权限隔离结果：

- UI 登录态操作使用短期 PAT，scope 包含 `memory:read`/`memory:write`。
- Secret 创建/禁用依赖 owner 项目写权限与后端 `authorizeSecretScope`。
- Adapter Token 创建/撤销依赖 owner 项目写权限与后端 `authorizeProjectScope`。
- PAT 撤销仅允许撤销调用者自己的 PAT。
- 撤销登录 PAT 后接口返回 `401`，证明 token revocation 生效。

剩余问题：

- 本轮覆盖了 Secret Vault 和 Token 管理页面主流程，但尚未覆盖这些页面的所有错误态。
- 浏览器工具部分 Playwright 方法不可用，本轮已通过替代方式完成验收并记录限制。
- 仍需继续执行 Archive、Hot Memory、Qdrant 状态、检索测试页等浏览器验收。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 继续按 Phase 10 浏览器验收清单推进剩余页面。
- 优先验收 Archive 与 Hot Memory，因为它们涉及真实写入、版本、删除、刷新持久化和检索入口。

## 2026-07-03 Phase 1.113：Archive 与 Hot Memory 登录态浏览器验收

完成事项：

- 创建短期浏览器验收主体：
  - 测试 owner 用户。
  - 测试组织。
  - 测试项目。
  - 45 分钟短期 PAT，scope 为 `memory:read` 与 `memory:write`。
- 通过真实登录页输入短期 PAT，进入管理台。
- 在 `/archive/` 页面通过 UI 创建 Archive。
- 在 Archive 详情页通过 UI 编辑正文，验证版本和索引代次递增。
- 在 Archive 详情页通过 UI 触发重建索引，验证索引代次继续递增并能读取索引状态。
- 在 Archive 详情页通过 UI 软删除 Archive，验证 active 列表不再返回，deleted 列表返回。
- 在 `/hot-memory/` 页面通过 UI 创建 Hot Memory。
- 通过 UI 编辑 Hot Memory fact。
- 通过 UI 执行 mark-used、promote、demote，验证 used_count 和状态流转。
- 通过 UI 删除 Hot Memory，验证 active/promoted/demoted 列表均不再返回，数据库状态为 `deleted`。
- 撤销短期登录 PAT。
- 验证撤销后的 PAT 再访问管理 API 返回 `401`。
- 对本轮创建的测试数据做软清理：
  - 1 个测试用户改为 `disabled`。
  - 1 个测试项目改为 `deleted`。
  - 1 个测试组织改为 `deleted`。
  - 本轮 Archive 保持 `deleted`，版本和索引记录保留。
  - 本轮 Hot Memory 保持 `deleted`，事件和计数记录保留。

修改模块：

- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

测试命令和结果：

- 服务器 `source scripts/load-prod-env.sh && docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps && curl -fsS http://127.0.0.1:18081/healthz`：通过，API/Web/Worker/MCP 均 Up，Postgres/Redis healthy，healthz 返回 db/qdrant/redis 均 `ok`。
- 服务器 `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过，包含 compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。

浏览器验收：

- 登录页：
  - 通过真实登录页填写短期 PAT。
  - 登录后进入管理台页面。
- Archive 列表页 `/archive/`：
  - 页面显示“Markdown 归档库”。
  - “创建真实 Archive”按钮可用。
  - 通过 UI 输入测试标题和正文并创建 Archive。
  - `/memory/archive/list` 返回 `200`。
  - API 返回本轮 Archive metadata，状态为 `active`，版本为 `1`，索引代次为 `1`。
  - 页面显示本轮测试标题。
- Archive 详情页 `/archive/:id`：
  - 页面显示“Archive 编辑 / 版本 / 重索引”。
  - 通过 UI 修改 Markdown 正文并点击“保存并生成新版本”。
  - `/memory/archive/detail` 返回 `200`。
  - `/memory/archive/versions` 返回 `200`。
  - 编辑后 metadata `current_version=2`，`index_generation=2`。
  - 正文包含编辑标记。
  - versions 数量为 `2`。
  - 详情响应不包含本轮检查用的 secret-like 标记。
  - 通过 UI 点击“触发重建索引”。
  - 重建后 metadata `current_version=2`，`index_generation=3`。
  - `/memory/archive/index-status` 返回 `200`，索引状态接口返回 generation、job 状态和 chunk 状态。
  - 通过 UI 点击“软删除 Archive”。
  - active 列表不再返回该 Archive。
  - deleted 列表返回该 Archive，状态为 `deleted`，版本仍为 `2`，索引代次仍为 `3`。
- Hot Memory 页 `/hot-memory/`：
  - 页面显示“Hot Memory 管理”。
  - 通过 UI 输入 fact 并点击“创建”。
  - `/memory/hot-memory/list` 返回 `200`。
  - API 返回本轮 Hot Memory，状态为 `active`，scope 为 `project`，`used_count=0`。
  - 页面显示本轮 fact。
  - 通过 UI 点击“编辑”，保存编辑后的 fact。
  - API 返回编辑后的 fact，状态仍为 `active`。
  - 通过 UI 点击“标记使用”，API 验证 `used_count=1`。
  - 通过 UI 点击“提升”，promoted 列表返回该记忆，状态为 `promoted`。
  - 通过 UI 切换到 promoted 过滤后点击“降权”，demoted 列表返回该记忆，状态为 `demoted`。
  - 通过 UI 在 demoted 过滤下点击“删除”。
  - active/promoted/demoted 列表均不再返回该记忆。
  - 数据库验证该记忆状态为 `deleted`，`used_count=1`，`access_count=1`。

安全检查：

- 短期登录 PAT 未写入代码、文档、日志或回复。
- 本轮 Archive 正文使用非敏感测试内容。
- Archive 编辑响应未出现本轮检查用 secret-like 标记。
- Hot Memory fact 使用非敏感测试内容。
- 交付日志只记录行为结果，不记录 PAT 明文、token hash 或真实凭据。
- 短期登录 PAT 已撤销，撤销后访问 `/memory/archive/list` 返回 `401`。
- 浏览器已回到登录页。
- 测试数据采用软清理，没有物理删除生产数据。

失败命令、根因和修复：

- 代码图谱 MCP `search_graph` 失败。
  - 根因：`codebase-memory-mcp` transport closed。
  - 修复：按项目规则明确降级到 `rg` 和文件读取，并继续执行服务器验收。
- `web/pages/archive/[id].vue` 第一次读取失败。
  - 根因：zsh 将方括号按 glob 展开。
  - 修复：使用引号包裹路径后读取。
- 服务器表结构 shell 查询失败。
  - 根因：直接嵌套 SQL 引号转义不稳定。
  - 修复：改用 base64 SQL 管道进入 psql。

部署状态：

- 本轮未改运行时代码，未重新部署。
- 生产服务在浏览器验收后通过 post-deploy 与 healthz 检查。
- 未修改公开端口。
- 未 commit、未 push。

权限隔离结果：

- UI 登录态操作使用短期 PAT，scope 包含 `memory:read`/`memory:write`。
- Archive 创建/编辑/重索引/删除依赖 owner 项目写权限与后端 `authorizeArchiveScope`。
- Hot Memory 创建/编辑/热度操作/删除依赖 owner 项目写权限与后端 `authorizeProjectScope`。
- 撤销登录 PAT 后接口返回 `401`，证明 token revocation 生效。

剩余问题：

- 本轮覆盖了 Archive 与 Hot Memory 页面主流程，但尚未覆盖全部错误态和跨租户隔离 UI 验收。
- Archive 索引状态中本轮可见 chunk 状态为 `pending`，后续仍需结合 worker/Qdrant 完整验收检索召回。
- 仍需继续执行 Qdrant 状态页、检索测试页、统一检索/MCP 语义一致性等验收。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 继续按 Phase 10 与 Phase 8 验收清单推进 Qdrant 状态页和检索测试页。
- 重点验证 Archive RAG/Hot Memory 是否通过统一检索入口召回，并验证 query-time filter 与跨租户隔离。

## 2026-07-03 Phase 1.114：Qdrant 状态页与统一检索浏览器验收

完成事项：

- 创建 A/B 两套短期浏览器验收主体：
  - A 用户、组织、项目与短期 PAT。
  - B 用户、组织、项目与短期 PAT。
  - scope 均为 `memory:read` 与 `memory:write`。
- 写入 A 项目唯一 Hot Memory 测试 marker。
- 写入 A 项目唯一 Archive RAG 测试 marker，并触发重建索引。
- 写入 B 项目干扰 Hot Memory marker，用于跨租户泄露反证。
- 验证 A 项目 Archive RAG point 已进入 Qdrant point 跟踪表。
- 验证 A/B Hot Memory point 已进入 Qdrant point 跟踪表。
- 通过 A 用户真实登录浏览器验收 `/qdrant/`。
- 通过 A 用户真实登录浏览器验收 `/search-test` 同时召回 Archive RAG 与 Hot Memory。
- 通过 B 用户真实登录浏览器验收同一 A marker 查询不会泄露 A 的 marker、Archive ID 或 Hot Memory ID。
- 通过 API 反证 A 查 B marker、B 查 A marker 均不泄露对方 marker。
- 撤销本轮全部短期 PAT。
- 对本轮创建的测试数据做软清理：
  - 2 个测试用户改为 `disabled`。
  - 2 个测试项目改为 `deleted`。
  - 2 个测试组织改为 `deleted`。
  - 1 个测试 Archive 改为 `deleted`。
  - 2 条测试 Hot Memory 改为 `deleted`。

修改模块：

- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

测试命令和结果：

- 服务器 `source scripts/load-prod-env.sh && docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps && curl -fsS http://127.0.0.1:18081/healthz`：通过，API/Web/Worker/MCP 均 Up，Postgres/Redis healthy，healthz 返回 db/qdrant/redis 均 `ok`。
- 服务器 `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过，包含 compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。

浏览器验收：

- Qdrant 状态页 `/qdrant/`：
  - A 用户通过真实登录页进入管理台。
  - 页面显示“Qdrant 索引状态”。
  - 页面显示 collection `memory_os`。
  - 页面显示 Query-Time Filter 为“已强制”。
  - 页面显示必需 payload 字段：
    - `doc_type`
    - `user_id`
    - `org_id`
    - `project_id`
    - `visibility`
    - `permission_labels`
    - `index_generation`
  - 页面显示“Qdrant Point 状态汇总”。
  - `/memory/qdrant/status` 返回 `200`。
  - API 返回 `query_time_filter_enforced=true`。
  - API 返回 Archive indexed points 数量大于 0。
  - API 返回 Hot Memory indexed points 数量大于 0。
- 检索测试页 `/search-test`：
  - A 用户页面显示“检索测试”。
  - 页面当前组织/项目为 A 项目。
  - 使用 A marker 执行检索。
  - 页面显示“压缩上下文”和“Source refs”。
  - 页面包含 A marker。
  - 页面不包含 B marker。
  - `/memory/search` 返回 `200`。
  - API 返回结果数为 `2`。
  - API 返回 Hot Memory 结果数为 `1`。
  - API 返回 Archive RAG 结果数为 `1`。
  - API 返回 source kind 同时包含 `archive_chunk` 与 `hot_memory`。
  - API 返回 `marked_used_count=1`。
  - API 返回 `access_log_count=2`。
  - 压缩上下文包含 A marker，不包含 B marker。
- 跨租户浏览器反证：
  - B 用户通过真实登录页进入管理台。
  - B 用户页面当前组织/项目为 B 项目。
  - 使用 A marker 执行检索。
  - 页面没有出现 A marker。
  - 页面没有出现 A Archive ID。
  - 页面没有出现 A Hot Memory ID。
  - 页面只出现 B 自己项目的干扰 marker，未泄露 A 项目内容。

安全检查：

- 本轮短期 PAT 明文未写入代码、文档、日志或回复。
- 测试 marker 不包含真实 secret、token、密码或私钥。
- Qdrant Archive payload 包含 A 的 user/org/project，不包含 B org。
- 代码层证据确认：
  - `qdrant.Client.SearchPoints` 缺少 filter 会报错。
  - `qdrant.BuildPayloadFilter` 强制 user/visibility/permission labels。
  - `retrieval.Service` 将 user/org/project/permission/index_generation 传入 Archive RAG Qdrant filter。
  - `hotmemory.BuildFilter` 将 user/org/project/scope/status/permission labels 传入 Hot Memory filter。
  - 相关测试覆盖 query-time filter 序列化、缺 filter 拒绝、跨租户不泄露。
- 本轮全部短期 PAT 已撤销，撤销后访问管理 API 返回 `401`。
- 浏览器已回到登录页。
- 测试数据采用软清理，没有物理删除生产数据。

失败命令、根因和修复：

- 代码图谱 MCP `search_graph` 失败。
  - 根因：`codebase-memory-mcp` transport closed。
  - 修复：按项目规则明确降级到 `rg` 和文件读取，并继续执行服务器验收。
- 第一次浏览器 Qdrant 验收超时并重置自动化内存。
  - 根因：浏览器自动化单步等待时间过长，Node 会话超时重置；服务本身未失败。
  - 修复：不恢复已丢失的 PAT 明文，给同一测试用户补发新的短期 PAT 后继续验收，并在最终清理时撤销原始 PAT 与补发 PAT。

部署状态：

- 本轮未改运行时代码，未重新部署。
- 生产服务在浏览器验收后通过 post-deploy 与 healthz 检查。
- 未修改公开端口。
- 未 commit、未 push。

权限隔离结果：

- A 项目检索能召回 A 自己的 Archive RAG 和 Hot Memory。
- A 项目检索不包含 B marker。
- B 项目检索不包含 A marker、A Archive ID 或 A Hot Memory ID。
- API 反证显示跨租户查询即使返回本租户相近结果，也不会把对方 marker 写入 context 或 source refs。
- Qdrant payload filter 静态实现和生产行为共同证明 query-time filter 生效，而不是仅应用层后过滤。

剩余问题：

- 本轮验证了 HTTP 统一检索入口和浏览器检索页，但尚未完成 MCP `memory_search` 与 HTTP 语义一致性验收。
- 本轮验证了跨租户 marker 不泄露，但还需要继续补充 agent_specific 与跨 agent scope 的专项验收。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 继续推进 MCP `memory_search` 与 HTTP `/memory/search` 语义一致性验收。
- 随后推进 agent_specific 隔离、跨 Agent 共享 scope、access log 页面和最终生产安全扫描。

## 2026-07-03 Phase 1.115：MCP memory_search 与 Agent Scope 生产验收

完成事项：

- 创建短期浏览器/接口验收主体：
  - 测试 owner 用户。
  - 测试组织。
  - 测试项目。
  - 45 分钟短期 PAT，scope 为 `memory:read` 与 `memory:write`。
- 写入同一生产 fixture：
  - 1 条 Archive RAG marker，并触发重建索引。
  - 1 条 `codex` 的 `agent_specific` Hot Memory。
  - 1 条 `claude` 的 `agent_specific` Hot Memory。
  - 1 条 `project` scope Hot Memory。
- 使用同一 actor/query 对比 HTTP `/memory/search` 与 MCP `/tools/call memory_search`。
- 补写独立 agent-only marker，重新验证 `agent_specific` 隔离，避免 Archive marker 干扰判断。
- 验证 project scope Hot Memory 可被 `codex` 与 `claude` 两个 agent 召回。
- 撤销短期 PAT。
- 对本轮创建的测试数据做软清理：
  - 1 个测试用户改为 `disabled`。
  - 1 个测试项目改为 `deleted`。
  - 1 个测试组织改为 `deleted`。
  - 1 个测试 Archive 改为 `deleted`。
  - 6 条测试 Hot Memory 改为 `deleted`。

修改模块：

- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

测试命令和结果：

- 服务器 `source scripts/load-prod-env.sh && docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps && curl -fsS http://127.0.0.1:18081/healthz && curl -fsS http://127.0.0.1:18082/healthz`：通过，API/Web/Worker/MCP 均 Up，Postgres/Redis healthy，API healthz 返回 db/qdrant/redis 均 `ok`，MCP healthz 返回 `ok`。
- 服务器 `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过，包含 compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。

MCP 与 HTTP 一致性验收：

- HTTP `/memory/search`：
  - 返回 `200`。
  - result count 为 `3`。
  - source kind 包含 `archive_chunk` 与 `hot_memory`。
  - context 包含本轮 MCP marker。
  - context 包含本轮 project scope shared marker。
- MCP `/tools/call` 调用 `memory_search`：
  - HTTP status 为 `200`。
  - response `code` 为 `ok`。
  - result count 为 `3`。
  - source kind 包含 `archive_chunk` 与 `hot_memory`。
  - context 包含本轮 MCP marker。
  - context 包含本轮 project scope shared marker。
  - `access_log_count=3`。
  - `marked_used_count=2`。
- 同一 fixture 下 HTTP 与 MCP 的 source IDs 一致：
  - 同一 Archive chunk ID。
  - 同一 codex agent-specific Hot Memory ID。
  - 同一 project scope Hot Memory ID。

Agent Scope 验收：

- 第一轮 agent_specific 验收发现 fixture marker 同时存在于 Archive，判断不干净，因此未把该轮作为结论证据。
- 第二轮补写只存在于 Hot Memory 的独立 marker 后重新验收：
  - `codex` 查询 `codex` agent-only marker，能召回 codex-only Hot Memory。
  - `claude` 查询 `codex` agent-only marker，不包含 codex-only marker。
  - `claude` 查询 `claude` agent-only marker，能召回 claude-only Hot Memory。
  - `codex` 查询 `claude` agent-only marker，不包含 claude-only marker。
  - `codex` 查询 project-only marker，能召回 project scope Hot Memory。
  - `claude` 查询 project-only marker，能召回同一 project scope Hot Memory。
- 额外观察：
  - project scope 检索会合并当前 agent 的 agent_specific 结果，这是当前 `retrieval.Service.collect` 的设计行为。
  - `agent_specific` scope 专项检索证明不同 agent 之间不会互相召回对方 agent-only 记忆。

安全检查：

- 本轮短期 PAT 明文未写入代码、文档、日志或回复。
- 测试 marker 不包含真实 secret、token、密码或私钥。
- MCP 调用不需要 PAT，但必须显式传入 actor 与 permission labels；本轮使用同一生产 actor/permission label 验证语义一致。
- 代码层证据确认：
  - `cmd/memory-mcp` 生产构建注入同一 `retrieval.Service`。
  - `internal/mcp` 的 `memory_search` 调用 `retrieval.Service.Search`。
  - `internal/mcp` 单测覆盖 MCP/HTTP 语义一致性。
  - `cmd/memory-smoke` 的 pipeline E2E 在 `SMOKE_MCP_URL` 存在时检查 MCP 与 HTTP 搜索同一 marker。
- 本轮短期 PAT 已撤销，撤销后访问 `/memory/search` 返回 `401`。
- 测试数据采用软清理，没有物理删除生产数据。

失败命令、根因和修复：

- 代码图谱 MCP `search_graph` 失败。
  - 根因：`codebase-memory-mcp` transport closed。
  - 修复：按项目规则明确降级到 `rg` 和文件读取，并继续执行服务器验收。
- 首次检查 MCP tools 使用 `/tools/list` 返回 `404`。
  - 根因：当前 MCP HTTP surface 使用 `/tools` 列表路径。
  - 修复：改用 `/tools`，确认 `memory_search` 等工具存在。
- 首轮 agent_specific 判断不干净。
  - 根因：codex marker 同时存在于 Archive RAG 和 agent_specific Hot Memory，跨 agent 查询可能命中 Archive 内容。
  - 修复：补写只存在于 Hot Memory 的独立 agent-only marker 后重新验证。

部署状态：

- 本轮未改运行时代码，未重新部署。
- 生产服务在验收后通过 post-deploy 与 healthz 检查。
- 未修改公开端口。
- 未 commit、未 push。

权限隔离结果：

- MCP 与 HTTP 在同一 actor/permission label 下返回同一 source IDs。
- `agent_specific` scope 下，codex 与 claude 不互相召回对方 agent-only Hot Memory。
- project scope 下，codex 与 claude 均可召回同一 project scope Hot Memory。
- 撤销 PAT 后 HTTP 管理检索返回 `401`，证明 token revocation 生效。

剩余问题：

- MCP HTTP surface 当前没有 PAT 鉴权层，安全边界依赖调用方传入 actor/permission labels 与部署网络边界；后续生产安全验收需要明确是否接受 MCP 仅内网暴露，或补 MCP 鉴权。
- 尚未完成多 Adapter fixture 导入验收。
- 尚未完成 import/export、最终安全扫描和最终交付报告。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 继续推进多 Agent Adapter fixture 验收：Codex、Claude Code、opencode、Hermes、Generic Transcript Importer。
- 同时检查 MCP 端口安全边界是否应增加鉴权或仅内网访问约束。

## 2026-07-03 Phase 1.116：MCP PAT 鉴权与 Smoke 生产验证修复

完成事项：

- 修复 MCP `/tools/call` 生产安全边界：
  - production 环境默认要求 `Authorization: Bearer <PAT>`。
  - 未带 PAT 返回 `401 {"error":"pat_required"}`。
  - 无效或撤销 PAT 返回 `401 {"error":"invalid_pat"}`。
  - PAT 缺少 `memory:read` 或等价 scope 返回 `403 {"error":"mcp_forbidden"}`。
- 修复 MCP `memory_search` actor 信任边界：
  - 调用方传入的 `actor.user_id` 不再作为可信用户来源。
  - 服务端使用 PAT subject 覆盖 `actor.user_id`。
  - 服务端通过 Tenant permission context 生成 permission labels，并覆盖请求参数中的 labels。
- 修复部署后 smoke 兼容性：
  - `cmd/memory-smoke` 的 MCP 一致性检查复用 `SMOKE_SEARCH_PAT`。
  - pipeline E2E 在 `SMOKE_MCP_URL` 存在时，会带 PAT 调用 `/tools/call memory_search`。
- 部署到 `thinkpad:/opt/memory-os` 并重建生产容器。
- 验证未授权 MCP 调用被拒绝。

修改模块：

- `cmd/memory-mcp/main.go`
- `cmd/memory-mcp/main_test.go`
- `cmd/memory-smoke/main.go`
- `cmd/memory-smoke/main_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- `/tools/call` 行为变更：
  - production 下新增 PAT 鉴权要求。
  - `memory_search` 的 actor 与 permission labels 改由服务端认证上下文裁定。
- `/tools` 列表接口未变更。

新增 migration：

- 无。

测试命令和结果：

- 本地 `GOCACHE="$PWD/.gocache" go test ./cmd/memory-smoke -run TestPipelineE2ESmokeChecksMCPMemorySearchWhenConfigured -count=1`：
  - 红灯：修改测试后失败，失败原因为 MCP 请求缺少 Authorization header。
  - 绿灯：实现后通过。
- 本地 `GOCACHE="$PWD/.gocache" go test ./cmd/memory-mcp ./internal/mcp ./cmd/memory-smoke -count=1`：通过。
- 服务器 `PATH=/usr/local/go/bin:$PATH GOCACHE=$PWD/.gocache go test ./cmd/memory-mcp ./internal/mcp ./cmd/memory-smoke -count=1`：通过。
- 服务器 `PATH=/usr/local/go/bin:$PATH make prod-up`：通过，API/Worker/MCP/Web 镜像重建并启动；dangling 镜像清理回收 `747MB`。
- 服务器 `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过，包含 compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。
- 服务器未授权 MCP 验证：
  - `POST http://127.0.0.1:18082/tools/call` 不带 Authorization。
  - 返回 `HTTP/1.1 401 Unauthorized`。
  - 响应体为 `{"error":"pat_required"}`。
- 服务器 compose 状态：
  - `memory-api` Up。
  - `memory-mcp` Up。
  - `memory-web` Up。
  - `memory-worker` Up。
  - `postgres` healthy。
  - `redis` healthy。
  - `qdrant` Up。

安全检查：

- 关闭上一轮记录的 MCP `/tools/call` 无 PAT 鉴权风险。
- PAT 明文没有写入代码、文档、日志或回复。
- smoke 使用已有短期 `SMOKE_SEARCH_PAT` 环境变量，不新增真实 secret 配置。
- MCP `memory_search` 不再信任客户端提交的 `actor.user_id` 和 `permission_labels`。
- 未修改公开端口。
- 本轮没有物理删除生产业务数据。

失败命令、根因和修复：

- 服务器首次窄范围测试命令 `go test ...` 失败。
  - 根因：非登录 SSH 命令 PATH 未包含 `/usr/local/go/bin`。
  - 修复：后续服务器验证显式使用 `PATH=/usr/local/go/bin:$PATH`。
- 首次 `rsync` 目标路径写到 `/opt/memory-os/cmd/`，产生了临时误放文件。
  - 根因：目标路径没有精确到子目录。
  - 修复：立即删除本轮误放的 `/opt/memory-os/cmd/main.go` 和 `/opt/memory-os/cmd/main_test.go`，随后按精确路径重新同步四个目标文件。

部署状态：

- 已重新部署生产 API/Worker/MCP/Web。
- 生产服务 post-deploy 通过。
- `memory-mcp` 当前运行的是包含 PAT 鉴权的新镜像。
- 未 commit、未 push。

权限隔离结果：

- 未授权调用不能进入 MCP `memory_search`。
- 授权调用由 PAT subject 和 Tenant permission context 决定 actor/permission labels。
- pipeline E2E 通过，说明授权 MCP `memory_search` 与 HTTP 搜索链路仍保持可用。

剩余问题：

- `/tools` 工具列表接口仍未要求 PAT；目前只暴露 schema，不执行检索或写操作。最终安全验收需要决定是否也收紧为认证访问。
- 多 Agent Adapter fixture 导入到 TurnEvent/Archive/RAG/Search 的生产链路验收仍未完成。
- import/export、备份恢复演练、最终安全扫描和最终交付报告仍未完成。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 继续推进多 Agent Adapter fixture 验收：Claude Code、opencode、Hermes、Generic Transcript Importer。
- 将 Adapter fixture 实际导入 TurnEvent，并验证 Archive、RAG、Search 可召回。
- 随后继续收紧 `/tools` 列表认证策略或记录为最终安全决策。

## 2026-07-03 Phase 1.117：多 Adapter Fixture 到 Archive/RAG/Search 生产验收

完成事项：

- 给 `cmd/memory-smoke` 增加可控生产验收开关：
  - `SMOKE_ENABLE_ADAPTER_FIXTURE_E2E=true`。
  - 默认关闭，避免普通 smoke 变重。
  - `post-deploy-verify.sh` 的 pipeline E2E 默认开启。
- 将以下 fixture 通过现有 Adapter SDK 转换为 TurnEvent v1：
  - Claude Code：`internal/adapter/fixtures/claude_code_sample.json`。
  - opencode：`internal/adapter/fixtures/opencode_sample.json`。
  - Hermes：`internal/adapter/fixtures/hermes_sample.json`。
  - Generic Transcript Importer：`internal/adapter/fixtures/transcript_sample.md`。
- 每个 fixture 写入生产 `/memory/turn-event`：
  - 使用本轮 pipeline 临时 Adapter Token。
  - 事件 ID、turn ID、thread ID、session ID 均带本轮唯一 marker，避免重复验收互相污染。
  - 保留 Adapter 的 `source.platform`，用于区分来源平台。
  - `actor.agent_id` 遵守 Adapter Token 绑定，不伪造跨 agent 写入。
- 每个 fixture 写入后通过生产 `/memory/search` 验证 Archive RAG 可召回：
  - 查询包含本轮唯一 marker 和 fixture 原始文本。
  - 要求响应包含查询 marker。
  - 要求 source kind 包含 `archive_chunk`。
- `post-deploy-verify` 已覆盖该链路。

修改模块：

- `cmd/memory-smoke/main.go`
- `cmd/memory-smoke/main_test.go`
- `scripts/post-deploy-verify.sh`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

测试命令和结果：

- 本地 `GOCACHE="$PWD/.gocache" go test ./cmd/memory-smoke -run TestAdapterFixtureE2ESmokeWritesAndSearchesAllFixtures -count=1`：
  - 红灯 1：新增测试后编译失败，失败原因为 `adapterFixtureE2ESmoke` 未实现。
  - 红灯 2：实现后首次失败，失败原因为 `go test` 工作目录不是仓库根，fixture 相对路径找不到。
  - 绿灯：新增 `repoPath` 后通过。
- 本地 `GOCACHE="$PWD/.gocache" go test ./cmd/memory-smoke -count=1`：通过。
- 本地 `GOCACHE="$PWD/.gocache" go test ./internal/adapter ./cmd/memory-adapter ./cmd/memory-smoke -count=1`：通过。
- 服务器 `PATH=/usr/local/go/bin:$PATH GOCACHE=$PWD/.gocache go test ./internal/adapter ./cmd/memory-adapter ./cmd/memory-smoke -count=1`：通过。
- 服务器第一次 `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：失败。
  - 失败点：`pipeline-e2e`。
  - 错误：`adapter fixture claude-code turn event failed ... 403 {"error":"adapter_token_forbidden"}`。
  - 根因：临时 Adapter Token 只绑定 `agent_id=codex`，而 fixture E2E 初版把事件 actor.agent_id 改成了 `claude-code/opencode/hermes/transcript`。
- 服务器第二次 `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过，包含 compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。

生产证据：

- 本次 pipeline E2E 日志路径：`/tmp/memory-os-post-deploy.sI0viW/pipeline-e2e.log`。
- 日志结果：`smoke ok`。
- PostgreSQL `turn_events` 中可见本轮最新 `adapter_fixture_event_*` 事件：
  - `adapter_fixture_event_pipeline_e2e_..._claude_code_1`
  - `adapter_fixture_event_pipeline_e2e_..._claude_code_2`
  - `adapter_fixture_event_pipeline_e2e_..._opencode_1`
  - `adapter_fixture_event_pipeline_e2e_..._opencode_2`
  - `adapter_fixture_event_pipeline_e2e_..._hermes_1`
  - `adapter_fixture_event_pipeline_e2e_..._hermes_2`
  - `adapter_fixture_event_pipeline_e2e_..._transcript_1`
  - `adapter_fixture_event_pipeline_e2e_..._transcript_2`
  - `adapter_fixture_event_pipeline_e2e_..._transcript_3`
- 事件类型覆盖：
  - `user_message`
  - `assistant_final`
  - `tool_call_completed`
- 生产 search 验收已经在 `pipeline-e2e` 中完成；每个 fixture 查询均要求 `archive_chunk` source。

安全检查：

- fixture 原文中的 `sk-test-redacted-example` 经 Adapter SDK 脱敏，不允许出现在 turn event response 或 search response。
- smoke 通过 `assertNoSecretLeak` 检查 TurnEvent 写入响应和搜索响应。
- 本轮没有写入真实 secret。
- 本轮没有新增公开端口。
- 初版跨 agent 写入被 API 正确拒绝，证明 Adapter Token 绑定规则生效。
- 修复后不绕过 token 绑定；多平台身份通过 `source.platform` 保留，actor.agent_id 使用临时 token 绑定的 agent。

失败命令、根因和修复：

- 本地 fixture E2E 测试首次失败。
  - 根因：`go test ./cmd/memory-smoke` 的工作目录不是仓库根。
  - 修复：新增 `repoPath`，从当前目录向上查找仓库根并定位 fixture。
- 服务器第一次 post-deploy 失败。
  - 根因：测试实现违反 Adapter Token 绑定 agent 的安全规则。
  - 修复：fixture E2E 不再重写 actor.agent_id；使用绑定 agent 写入，同时保留 `source.platform`。
- 生产证据查询 `turn_event_payloads` join 失败。
  - 根因：辅助查询猜错列名。
  - 修复：改用 `turn_events` 表验证事件 ID、事件类型和 agent_id；payload 泄露由 smoke 响应检查覆盖。

部署状态：

- 本轮未重建长期运行服务镜像，因为改动只影响 smoke 与 post-deploy 脚本。
- 已同步到 `thinkpad:/opt/memory-os`。
- 生产服务 post-deploy 通过。
- API/Web/Worker/MCP 容器继续运行。
- 未 commit、未 push。

权限隔离结果：

- Adapter Token 不允许写入非绑定 agent 的事件；初版 smoke 已被生产 API 以 `403 adapter_token_forbidden` 拒绝。
- 修复后的 fixture E2E 遵守绑定 actor 写入规则。
- 检索仍通过 PAT 和 Tenant permission context 执行。

剩余问题：

- 本轮验证了多平台 Adapter fixture 进入 TurnEvent/Archive/RAG/Search，但没有为每个平台单独创建生产 Adapter Token；最终如要证明跨 agent 写入链路，需要补“每 agent 独立 token”的专项验收。
- `/tools` 工具列表接口仍未要求 PAT；目前只暴露 schema，不执行检索或写操作。最终安全验收需要决定是否也收紧。
- import/export、备份恢复演练、最终安全扫描和最终交付报告仍未完成。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 继续推进 import/export/backup/restore 生产验收。
- 或先补 `/tools` 列表认证策略，关闭 MCP surface 最后一个安全决策点。

## 2026-07-03 Phase 1.118：MCP /tools 列表接口生产鉴权收紧

完成事项：

- 收紧 MCP `/tools` 列表接口生产安全边界：
  - production 环境默认要求 `Authorization: Bearer <PAT>`。
  - 未带 PAT 返回 `401 {"error":"pat_required"}`。
  - 无效或撤销 PAT 返回 `401 {"error":"invalid_pat"}`。
  - PAT 缺少 `memory:read` 或等价 scope 返回 `403 {"error":"mcp_forbidden"}`。
- `/tools/call` 与 `/tools` 复用同一 PAT 鉴权 helper，避免两个入口安全策略漂移。
- 保持非 production / 未启用 `RequireAuth` 的测试和开发路径兼容。
- 部署到 `thinkpad:/opt/memory-os` 并重建生产容器。
- 验证未授权 `/tools` 请求被拒绝。

修改模块：

- `cmd/memory-mcp/main.go`
- `cmd/memory-mcp/main_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- `/tools` 行为变更：
  - production 下新增 PAT 鉴权要求。
  - 只具备合法 PAT 且包含 `memory:read` 或 `memory:write` scope 时返回工具 schema。
- `/tools/call` 行为保持 Phase 1.116 的 PAT 鉴权要求不变。

新增 migration：

- 无。

测试命令和结果：

- 本地 `GOCACHE="$PWD/.gocache" go test ./cmd/memory-mcp -run 'TestToolsListRequiresPATWhenAuthConfigured|TestToolsListAcceptsPATWhenAuthConfigured' -count=1`：
  - 红灯：新增测试后失败，`/tools` 未带 PAT 仍返回 `200` 和工具 schema。
  - 绿灯：实现 `/tools` PAT 鉴权后通过。
- 本地 `GOCACHE="$PWD/.gocache" go test ./cmd/memory-mcp ./internal/mcp ./cmd/memory-smoke -count=1`：
  - 第一次失败：旧的非鉴权 `TestToolsCallRunsMemorySearch` 触发 nil pointer。
  - 根因：`authorizeToolCall` 在 `RequireAuth=false` 时没有短路，继续使用空 PATRecord 执行 Tenant permission context。
  - 修复：`authorizeToolCall` 显式在 `RequireAuth=false` 时返回 true。
  - 第二次通过。
- 服务器 `PATH=/usr/local/go/bin:$PATH GOCACHE=$PWD/.gocache go test ./cmd/memory-mcp ./internal/mcp ./cmd/memory-smoke -count=1`：通过。
- 服务器 `PATH=/usr/local/go/bin:$PATH make prod-up`：通过，API/Worker/MCP/Web 镜像重建并启动；dangling 镜像清理回收约 `750MB`。
- 服务器 `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过，包含 compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。
- 服务器未授权 `/tools` 验证：
  - `GET http://127.0.0.1:18082/tools` 不带 Authorization。
  - 返回 `HTTP/1.1 401 Unauthorized`。
  - 响应体为 `{"error":"pat_required"}`。
- 服务器 compose 状态：
  - `memory-api` Up。
  - `memory-mcp` Up。
  - `memory-web` Up。
  - `memory-worker` Up。
  - `postgres` healthy。
  - `redis` healthy。
  - `qdrant` Up。

安全检查：

- 关闭上一轮记录的 MCP `/tools` 工具列表无 PAT 鉴权风险。
- `/tools` 不再向未授权调用方暴露 MCP 工具 schema。
- `/tools` 与 `/tools/call` 使用一致的 PAT scope 判断。
- 本轮没有写入真实 secret。
- 本轮没有新增公开端口。
- 未修改 PostgreSQL、Redis、Qdrant 对外暴露策略。

失败命令、根因和修复：

- 本地 MCP 相关包测试首次失败。
  - 根因：复用 PAT helper 后，`authorizeToolCall` 少了非鉴权模式短路，导致旧测试中空 TenantService 被调用。
  - 修复：在 `authorizeToolCall` 开头恢复 `RequireAuth=false` 短路。

部署状态：

- 已重新部署生产 API/Worker/MCP/Web。
- 生产服务 post-deploy 通过。
- `memory-mcp` 当前运行的是包含 `/tools` 与 `/tools/call` 双入口 PAT 鉴权的新镜像。
- 未 commit、未 push。

权限隔离结果：

- 未授权调用不能读取 MCP 工具列表。
- 未授权调用不能进入 MCP `memory_search`。
- 授权调用仍由 PAT subject 和 Tenant permission context 决定 actor/permission labels。
- pipeline E2E 通过，说明 MCP 鉴权收紧未破坏授权检索链路。

剩余问题：

- import/export、备份恢复演练、最终安全扫描和最终交付报告仍未完成。
- 本轮仍未为每个平台单独创建生产 Adapter Token；如果最终验收要求证明跨 agent 写入，需要补专项验收。
- Docker build context 约 `111MB`，部署耗时仍偏高，后续需要用 `.dockerignore` 或构建上下文拆分优化。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 继续推进 import/export/backup/restore 生产验收。
- 或先做 Docker build context 瘦身，降低后续部署验证成本。

## 2026-07-03 Phase 1.119：生产备份与隔离恢复演练验收

完成事项：

- 在 `thinkpad:/opt/memory-os` 执行真实生产备份，生成 PostgreSQL、Markdown Archive、Qdrant snapshot 三类备份产物。
- 验证备份 manifest 包含每类产物路径、大小和 sha256 摘要。
- 执行生产备份的 restore dry-run，验证恢复脚本参数、manifest、目录和命令生成链路。
- 执行 restore rehearsal preflight，验证隔离恢复演练前置条件。
- 执行 restore rehearsal dry-run，验证隔离恢复演练不会误写生产环境。
- 执行真实隔离恢复演练：使用独立 Docker Compose project、独立 volume、独立容器恢复备份并运行 smoke。
- 演练结束后确认隔离恢复容器、volume 已清理，无残留。
- 演练后执行生产 post-deploy 验证，确认生产服务仍稳定运行。

修改模块：

- `docs/production-delivery-log.md`：记录 Phase 1.119 生产备份与隔离恢复演练证据。
- 本轮没有修改运行时代码、Docker Compose、migration 或生产配置。

新增或变更 API：

- 无。

新增 migration：

- 无。

测试命令和结果：

- 本地 `GOCACHE="$PWD/.gocache" go test ./internal/backup ./internal/restore ./internal/verify ./cmd/memory-importer ./internal/importer -count=1`：通过。
  - `ok memory-os/internal/backup 1.218s`
  - `ok memory-os/internal/restore 4.640s`
  - `ok memory-os/internal/verify 2.087s`
  - `ok memory-os/cmd/memory-importer 2.346s`
  - `ok memory-os/internal/importer 1.847s`
- 服务器 `PATH=/usr/local/go/bin:$PATH GOCACHE=$PWD/.gocache go test ./internal/backup ./internal/restore ./internal/verify ./cmd/memory-importer ./internal/importer -count=1`：通过。
  - `ok memory-os/internal/backup 0.677s`
  - `ok memory-os/internal/restore 3.807s`
  - `ok memory-os/internal/verify 3.657s`
  - `ok memory-os/cmd/memory-importer 0.005s`
  - `ok memory-os/internal/importer 0.004s`
- 服务器真实备份 `RUN_ID=phase119-20260703T064750Z PATH=/usr/local/go/bin:$PATH make backup`：通过。
  - 备份目录：`/opt/memory-os/backups/phase119-20260703T064750Z`
  - `archives/markdown-archive.tar.gz`：`105 bytes`
  - `manifest.json`：`625 bytes`
  - `postgres/memory_os.sql`：`920476 bytes`
  - `postgres/pg_dump.command`：`233 bytes`
  - `qdrant/memory_os-1184664180328560-2026-07-03-06-47-51.snapshot`：`2021888 bytes`
  - `qdrant/snapshot-response.json`：`242 bytes`
  - `qdrant/snapshot.command`：`73 bytes`
- 备份 manifest 摘要：
  - PostgreSQL sha256：`e44f5d7e0c9151aba72aff0d3a2b2dfd0078653ae78854bbf924258459aabc34`
  - Markdown Archive sha256：`881e394670be5e303ab28f09def0078d048892432dfa535f280425d451fe571f`
  - Qdrant snapshot sha256：`16d22a285a6068e914345f2b7895a0834b55716580139d39100e833eae720a3a`
- 服务器 restore dry-run `BACKUP_DIR=/opt/memory-os/backups/phase119-20260703T064750Z DRY_RUN=1 PATH=/usr/local/go/bin:$PATH make restore`：通过。
  - 输出：`restore dry-run completed: /opt/memory-os/artifacts/restore-20260703T064758Z`
- 服务器 restore rehearsal preflight `BACKUP_DIR=/opt/memory-os/backups/phase119-20260703T064750Z PATH=/usr/local/go/bin:$PATH make restore-rehearsal-preflight`：通过。
  - 输出：`restore rehearsal preflight ok: /opt/memory-os/artifacts/restore-rehearsal-preflight-20260703T064803Z`
- 服务器 restore rehearsal dry-run `BACKUP_DIR=/opt/memory-os/backups/phase119-20260703T064750Z PATH=/usr/local/go/bin:$PATH make restore-rehearsal-dry-run`：通过。
  - 输出：`restore dry-run completed: /opt/memory-os/artifacts/restore-rehearsal-20260703T064811Z/restore`
  - 输出：`restore rehearsal dry-run completed: /opt/memory-os/artifacts/restore-rehearsal-20260703T064811Z`
- 服务器真实隔离恢复演练 `BACKUP_DIR=/opt/memory-os/backups/phase119-20260703T064750Z RESTORE_REHEARSAL_MODE=real CONFIRM_RESTORE_REHEARSAL=I_UNDERSTAND_TEST_RESTORE PATH=/usr/local/go/bin:$PATH bash scripts/restore-rehearsal.sh`：通过。
  - 使用隔离 project：`memory-os-restore-rehearsal`
  - 使用隔离 volume：`memory-os-restore-rehearsal_restore_rehearsal_pg`
  - 使用隔离 volume：`memory-os-restore-rehearsal_restore_rehearsal_qdrant`
  - 使用隔离 volume：`memory-os-restore-rehearsal_restore_rehearsal_redis`
  - 使用隔离 volume：`memory-os-restore-rehearsal_restore_rehearsal_archive`
  - PostgreSQL dump 恢复成功。
  - Qdrant snapshot upload 返回 `{"result":true,"status":"ok"}`。
  - 隔离恢复环境 smoke 返回 `smoke ok`。
  - 演练完成输出：`restore rehearsal completed: /opt/memory-os/artifacts/restore-rehearsal-20260703T064818Z`
- 演练残留检查：通过。
  - `docker ps -a --filter label=com.docker.compose.project=memory-os-restore-rehearsal -q` 无输出。
  - `docker volume ls --filter label=com.docker.compose.project=memory-os-restore-rehearsal -q` 无输出。
- 演练后生产 `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过。
  - 日志：`/tmp/memory-os-post-deploy.5irhIu`
  - 输出：`post deploy verify completed`

安全检查：

- 本轮没有写入真实 secret。
- 交付日志只记录备份路径、文件大小、sha256、命令类型和验收结果，不记录备份正文内容。
- 真实恢复演练使用独立 Docker Compose project 和独立 volume，没有覆盖生产 PostgreSQL、Archive 或 Qdrant 数据。
- 演练后确认隔离容器和隔离 volume 无残留。
- 本轮没有新增公开端口。
- PostgreSQL、Redis、Qdrant 对外暴露策略未变更。

失败命令、根因和修复：

- 本轮生产备份、restore dry-run、restore rehearsal preflight、restore rehearsal dry-run、真实隔离恢复演练、post-deploy 验证均通过。
- 本轮未发现需要修复的脚本缺陷。
- 本轮未新增测试文件；原因是现有 backup/restore/verify/importer 契约测试已覆盖脚本解析、manifest、dry-run 和 importer 基础行为，本轮重点是服务器真实演练验收。

部署状态：

- 没有重新部署运行时代码。
- 生产 API/Web/Worker/MCP 容器仍运行中。
- 生产 post-deploy 验证通过。
- 未 commit、未 push。

权限隔离结果：

- 本轮不改变业务权限模型。
- 通过隔离恢复演练验证备份恢复流程不会误写生产 Compose project。
- 演练后生产 pipeline E2E 仍通过，说明恢复演练未破坏现有认证、归档、检索主链路。

剩余问题：

- importer/exporter 仍需要后续生产化：当前 importer 测试覆盖 CLI 和基础行为，但还不能声明完整满足 Phase 11 的生产级 PG apply、重复 apply 幂等和 bundle round-trip 要求。
- 最终安全扫描、最终浏览器验收和最终交付报告仍未完成。
- Docker build context 约 `111MB`，部署耗时仍偏高，后续需要瘦身。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 继续推进 importer/exporter 生产化验收。
- 或先处理 Docker build context 瘦身，降低后续部署验证成本。
- 或进入最终安全扫描前置审计，提前收敛 Secret 泄露和权限隔离证据。

## 2026-07-03 Phase 1.120：Importer CLI 持久化状态与 bundle round-trip 验收

完成事项：

- `memory-importer` 新增 `--state` 参数，可把 apply 后的 importer item 写入本地持久化 state 文件。
- 默认无 `--state` 时保持旧行为，继续使用内存 repository，避免破坏现有 dry-run 和临时 apply 用法。
- importer repository 抽象为接口，`Service.Apply` 可把持久化写入错误向上返回。
- 新增 file-backed repository：
  - 启动时从 state 文件读取已导入 item。
  - `source_type + external_id` 作为幂等 key。
  - 写入使用临时文件 + rename。
  - state 文件权限为 `0600`。
  - item 输出排序稳定，便于 bundle 和测试验收。
- CLI 支持单独执行 `--export-bundle --state <path> --batch <batch>`，从持久化 state 导出 Markdown/RAG bundle，不需要重新导入原始文件。
- `cmd/memory-smoke` 的 importer smoke 升级：
  - dry-run 验证不落状态。
  - 第一次 apply 验证 `created_count=2`。
  - 第二次 apply 验证 `deduped_count=2`。
  - 单独 export-bundle 验证可从 state 导出 bundle。
  - 全流程验证不会泄露 fixture 中的假 secret。
- 修复一次服务器同步操作瑕疵：
  - 第一次 rsync 未带 `--relative`，误把 4 个文件平铺到 `/opt/memory-os` 根目录。
  - 已确认并删除误放的 `main.go`、`main_test.go`、`repository.go`、`service.go`。
  - 之后使用 `rsync -avzR` 按目录结构重新同步。

修改模块：

- `cmd/memory-importer/main.go`
- `cmd/memory-importer/main_test.go`
- `cmd/memory-smoke/main.go`
- `internal/importer/repository.go`
- `internal/importer/service.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无 HTTP API 变更。
- CLI 新增参数：`memory-importer --state <path>`。
- CLI 新增可独立使用的导出模式：`memory-importer --batch <batch> --export-bundle --state <path>`。

新增 migration：

- 无。

测试命令和结果：

- 本地红灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./cmd/memory-importer -run 'TestRunApplyUsesStateFileForIdempotency|TestRunExportBundleReadsStateFileWithoutReimport' -count=1`
  - 初次失败符合预期：`flag provided but not defined: -state`。
- 本地绿灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./cmd/memory-importer -run 'TestRunApplyUsesStateFileForIdempotency|TestRunExportBundleReadsStateFileWithoutReimport' -count=1`：通过。
- 本地相关包测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/importer ./cmd/memory-importer -count=1`：通过。
  - `GOCACHE="$PWD/.gocache" go test ./internal/importer ./cmd/memory-importer ./cmd/memory-smoke -count=1`：通过。
- 服务器相关包测试：
  - `PATH=/usr/local/go/bin:$PATH GOCACHE=$PWD/.gocache go test ./internal/importer ./cmd/memory-importer ./cmd/memory-smoke -count=1`：通过。
- 服务器 smoke：
  - `PATH=/usr/local/go/bin:$PATH make smoke`：通过，输出 `smoke ok`。
- 服务器 post-deploy：
  - `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过。
  - 日志：`/tmp/memory-os-post-deploy.q6Dyzq`
  - 覆盖：compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。

安全检查：

- 本轮没有写入真实 secret。
- importer state 文件保存的是经 importer sanitize 后的 item；测试覆盖 fixture 假 secret 不出现在 dry-run、apply、reapply、export-bundle 输出中。
- state 文件默认权限为 `0600`。
- 本轮没有新增公开端口。
- 本轮没有改变 PostgreSQL、Redis、Qdrant 对外暴露策略。

失败命令、根因和修复：

- 红灯测试失败符合预期：
  - 根因：CLI 没有 `--state` 参数，无法跨进程保留 apply 状态。
  - 修复：新增 file-backed repository 与 CLI 注入路径。
- 服务器同步第一次出现路径错误：
  - 根因：rsync 多文件同步时未使用 `--relative`。
  - 修复：删除误放在 `/opt/memory-os` 根目录的 4 个文件，并使用 `rsync -avzR` 重新同步。

部署状态：

- 本轮没有重新构建或重启生产容器。
- 生产 post-deploy 验证通过。
- 未 commit、未 push。

权限隔离结果：

- 本轮不改变业务权限模型。
- pipeline E2E 仍通过，说明 importer smoke 升级未破坏当前认证、归档、检索主链路。

剩余问题：

- 这次只把 importer CLI 从内存演示推进到本地持久化 state；还不是最终 Phase 11 要求的生产级 PostgreSQL apply。
- importer apply 尚未直接写入 Hot Memory/Archive 主业务表，也未触发 RAG index job。
- export bundle 已可从 state 独立导出，但 bundle re-import 到主系统的端到端验收仍未完成。
- 最终安全扫描、最终浏览器验收和最终交付报告仍未完成。
- Docker build context 约 `111MB`，部署耗时仍偏高，后续需要瘦身。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 继续把 importer apply 接入生产主链路：Hot Memory / Archive metadata / Markdown Archive / RAG index queue。
- 或先补 bundle re-import 验收，证明 export bundle 可重新进入 importer。
- 或先做 Docker build context 瘦身，降低后续部署成本。

## 2026-07-03 Phase 1.121：Importer Apply 接入 Hot Memory / Archive 业务层

完成事项：

- importer service 新增可选 `ProductionSink`。
- `ProductionSink` 支持把 `hot_memory` import item 投递到现有 `hotmemory.Service.Upsert`。
- `ProductionSink` 支持把 `archive` import item 转换为 `manual_archive_request` TurnEvent，并投递到现有 `archive.Service.Create`。
- importer `Apply` 仍先写 importer repository 以保留导入幂等统计。
- importer `Apply` 对每个 item 都会调用 production sink：
  - 首次 apply 写入主链路。
  - 重复 apply 即使 importer repository 判定 dedupe，也会再次投递给主链路 service。
  - 依赖 Hot Memory fact hash 和 Archive request id 的幂等语义避免重复对象。
  - 这样可以支持前一次主链路写入失败后的重试修复。
- Hot Memory 写入复用现有业务逻辑：
  - secret sanitize。
  - fact hash。
  - hot score。
  - permission labels。
  - 可选 Qdrant index。
- Archive 写入复用现有业务逻辑：
  - Markdown 文件生成。
  - archive metadata。
  - version。
  - request id 幂等。
  - Markdown 内容不保存 fixture 假 secret 明文。

修改模块：

- `internal/importer/service.go`
- `internal/importer/service_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无 HTTP API 变更。
- 新增 Go API：
  - `NewServiceWithProductionSink(repository Repository, sink ProductionSink) Service`
  - `ProductionSink.Import(item ImportItem, scope Scope) error`

新增 migration：

- 无。

测试命令和结果：

- 本地红灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/importer -run 'TestServiceApplyMem0WritesHotMemoryThroughProductionSink|TestServiceApplyFastGPTWritesArchiveThroughProductionSink' -count=1`
  - 初次失败符合预期：
    - `undefined: NewServiceWithProductionSink`
    - `undefined: ProductionSink`
- 本地绿灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/importer -run 'TestServiceApplyMem0WritesHotMemoryThroughProductionSink|TestServiceApplyFastGPTWritesArchiveThroughProductionSink' -count=1`：通过。
- 本地相关包测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/importer ./internal/hotmemory ./internal/archive ./cmd/memory-importer ./cmd/memory-smoke -count=1`：通过。
- 服务器相关包测试：
  - `PATH=/usr/local/go/bin:$PATH GOCACHE=$PWD/.gocache go test ./internal/importer ./internal/hotmemory ./internal/archive ./cmd/memory-importer ./cmd/memory-smoke -count=1`：通过。
- 服务器 smoke：
  - `PATH=/usr/local/go/bin:$PATH make smoke`：通过，输出 `smoke ok`。
- 服务器 post-deploy：
  - `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过。
  - 日志：`/tmp/memory-os-post-deploy.BlkIjp`
  - 覆盖：compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。

安全检查：

- 本轮没有写入真实 secret。
- Hot Memory 路径复用 `hotmemory.Service.Upsert` 的 secret sanitize。
- Archive 路径使用 importer 已脱敏的 item text 生成 `manual_archive_request` payload。
- 新增测试覆盖 mem0 和 FastGPT fixture 中的假 secret 不出现在 Hot Memory fact 或 Archive Markdown 中。
- 本轮没有新增公开端口。
- 本轮没有改变 PostgreSQL、Redis、Qdrant 对外暴露策略。

失败命令、根因和修复：

- 红灯测试失败符合预期：
  - 根因：importer 尚无业务层 production sink。
  - 修复：新增 `ProductionSink`，通过现有 Hot Memory / Archive service 接入。

部署状态：

- 本轮没有重新构建或重启生产容器。
- 生产 post-deploy 验证通过。
- 未 commit、未 push。

权限隔离结果：

- importer 写入 Hot Memory 时保留 `user_id`、`org_id`、`project_id`、`agent_id`、`visibility`、`permission_labels`。
- importer 写入 Archive 时保留 `user_id`、`org_id`、`project_id` 和 importer source ref。
- pipeline E2E 仍通过，说明本轮业务层接入未破坏当前认证、归档、检索主链路。

剩余问题：

- CLI 还没有从生产配置中自动构建 PG-backed Hot Memory / Archive service。
- importer apply 还没有触发 Archive RAG index queue。
- Hot Memory 写入可通过 service 可选 Qdrant index，但当前 CLI 尚未注入生产 Qdrant index。
- bundle re-import 到主系统的端到端验收仍未完成。
- 最终安全扫描、最终浏览器验收和最终交付报告仍未完成。
- Docker build context 约 `111MB`，部署耗时仍偏高，后续需要瘦身。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 给 `cmd/memory-importer` 增加生产配置加载，使用 PostgreSQL repository 和 Archive 根目录注入 `ProductionSink`。
- 或补 Archive RAG index queue 接入，让 imported archive 能进入检索链路。
- 或补 bundle re-import 验收，证明 export bundle 可重新进入主系统。

## 2026-07-03 Phase 1.122：Importer CLI 支持 PG-backed ProductionSink 构建

完成事项：

- `memory-importer` 新增显式参数 `--production-sink`。
- 默认不启用生产写入，避免普通 dry-run/apply 意外连接生产库。
- `--production-sink` 仅在 `--apply` 时加载生产依赖：
  - `config.Load()` 读取 `POSTGRES_DSN` 和 `ARCHIVE_DIR`。
  - `db.NewPool()` 构造 pgx pool。
  - `hotmemory.NewPGRepository(pool)` 构造 Hot Memory PG repository。
  - `archive.NewPGRepository(pool)` 构造 Archive PG repository。
  - `archive.NewService(..., cfg.ArchiveDir)` 使用生产 Archive 根目录。
  - 组合为 importer `ProductionSink`。
- `--production-sink --dry-run` 不连接 PostgreSQL，仍可安全预览。
- 缺少 `POSTGRES_DSN` 时，`--production-sink --apply` 明确返回 `POSTGRES_DSN is required for production sink`。
- pgx pool 在 CLI 执行结束时关闭。

修改模块：

- `cmd/memory-importer/main.go`
- `cmd/memory-importer/main_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无 HTTP API 变更。
- CLI 新增参数：`memory-importer --production-sink`。

新增 migration：

- 无。

测试命令和结果：

- 本地红灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./cmd/memory-importer -run 'TestRunProductionSinkRequiresPostgresDSNForApply|TestRunProductionSinkDryRunDoesNotRequirePostgresDSN' -count=1`
  - 初次失败符合预期：`flag provided but not defined: -production-sink`。
- 本地绿灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./cmd/memory-importer -run 'TestRunProductionSinkRequiresPostgresDSNForApply|TestRunProductionSinkDryRunDoesNotRequirePostgresDSN' -count=1`：通过。
- 本地相关包测试：
  - `GOCACHE="$PWD/.gocache" go test ./cmd/memory-importer ./internal/importer ./internal/hotmemory ./internal/archive ./cmd/memory-smoke -count=1`：通过。
- 服务器相关包测试：
  - `PATH=/usr/local/go/bin:$PATH GOCACHE=$PWD/.gocache go test ./cmd/memory-importer ./internal/importer ./internal/hotmemory ./internal/archive ./cmd/memory-smoke -count=1`：通过。
- 服务器 production-sink dry-run：
  - `PATH=/usr/local/go/bin:$PATH go run ./cmd/memory-importer --source mem0 --batch prod_dry_verify --dry-run --production-sink --input internal/importer/fixtures/mem0_sample.jsonl`
  - 输出写入 `/tmp/memory-importer-prod-dryrun.json`。
  - 验证包含 `"dry_run":true`。
  - 结果：`production-sink-dry-run-ok`。
- 服务器 smoke：
  - `PATH=/usr/local/go/bin:$PATH make smoke`：通过，输出 `smoke ok`。
- 服务器 post-deploy：
  - `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过。
  - 日志：`/tmp/memory-os-post-deploy.7SUKcV`
  - 覆盖：compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。

安全检查：

- 本轮没有写入真实 secret。
- 本轮没有对生产数据库执行真实 importer apply，避免未经确认写入生产数据。
- dry-run 路径不连接 PostgreSQL。
- DSN 连接错误沿用 `db.NewPool` 和 `config.RedactDSN`，避免错误中泄露密码。
- 本轮没有新增公开端口。
- 本轮没有改变 PostgreSQL、Redis、Qdrant 对外暴露策略。

失败命令、根因和修复：

- 红灯测试失败符合预期：
  - 根因：CLI 尚无 `--production-sink` 参数。
  - 修复：新增显式开关和 PG-backed ProductionSink 构建路径。

部署状态：

- 本轮没有重新构建或重启生产容器。
- 生产 post-deploy 验证通过。
- 未 commit、未 push。

权限隔离结果：

- 本轮不改变业务权限模型。
- PG-backed ProductionSink 复用 Hot Memory / Archive service；业务层继续保存 user/org/project/agent/scope/permission labels。
- pipeline E2E 仍通过，说明 CLI 生产开关未破坏当前认证、归档、检索主链路。

剩余问题：

- 还没有对生产库执行真实 `--production-sink --apply` 导入；该操作会写入生产 Hot Memory / Archive，需要单独确认或在隔离恢复环境先验收。
- importer apply 还没有触发 Archive RAG index queue。
- Hot Memory 写入可通过 service 可选 Qdrant index，但当前 CLI 尚未注入生产 Qdrant index。
- bundle re-import 到主系统的端到端验收仍未完成。
- 最终安全扫描、最终浏览器验收和最终交付报告仍未完成。
- Docker build context 约 `111MB`，部署耗时仍偏高，后续需要瘦身。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 在隔离恢复环境中执行真实 `memory-importer --production-sink --apply`，证明 PG-backed importer 能写入 Hot Memory / Archive 且可重复 apply 幂等。
- 或补 Archive RAG index queue 接入，让 imported archive 能进入检索链路。
- 或补 bundle re-import 验收，证明 export bundle 可重新进入主系统。

## 2026-07-03 Phase 1.123：Importer Archive 导入触发 RAG Index Queue

完成事项：

- importer `ProductionSink` 新增可选 `ArchiveIndexQueue`。
- FastGPT / Archive import item 写入 Archive service 后，会读取生成的 Markdown 文件并执行 `archive.ChunkMarkdown`。
- chunk 后构造稳定的 archive index job：
  - `idempotency_key = rag_<archive_id>_g<index_generation>`
  - 保留 `archive_id`
  - 保留 `index_generation`
  - 保留 `user_id`
  - 保留 `org_id`
  - 保留 `project_id`
  - 固定 `visibility=project`
  - 生成 `project:<project_id>:read` permission label
  - 携带 Archive chunks
- 重复 importer apply 会重复 enqueue 同一 idempotency key，交给底层 PG queue 去重；这样支持前一次 queue 写入失败后的重试。
- `cmd/memory-importer` 新增 archive index queue adapter：
  - 把 `importer.ArchiveIndexJob` 转换为 `jobs.RAGIndexJob`。
  - `--production-sink --apply` 构建 PG-backed `jobs.NewPGArchiveIndexQueue` 并注入 importer sink。

修改模块：

- `internal/importer/service.go`
- `internal/importer/service_test.go`
- `cmd/memory-importer/main.go`
- `cmd/memory-importer/main_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无 HTTP API 变更。
- 新增 Go API：
  - `ArchiveIndexQueue`
  - `ArchiveIndexJob`
  - `ProductionSink.ArchiveIndexQueue`
- CLI 行为增强：
  - `memory-importer --production-sink --apply` 现在会为 imported archive enqueue RAG index job。

新增 migration：

- 无。

测试命令和结果：

- 本地红灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/importer -run TestServiceApplyFastGPTEnqueuesArchiveRAGIndex -count=1`
  - 初次失败符合预期：
    - `unknown field ArchiveIndexQueue in struct literal of type ProductionSink`
    - `undefined: ArchiveIndexJob`
- 本地绿灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/importer -run TestServiceApplyFastGPTEnqueuesArchiveRAGIndex -count=1`：通过。
- 本地 CLI adapter 红灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./cmd/memory-importer -run TestArchiveIndexQueueAdapterConvertsImporterJob -count=1`
  - 初次失败符合预期：`undefined: archiveIndexQueueAdapter`。
- 本地 CLI adapter 绿灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./cmd/memory-importer -run TestArchiveIndexQueueAdapterConvertsImporterJob -count=1`：通过。
- 本地相关包测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/importer ./cmd/memory-importer ./internal/jobs ./internal/archive ./internal/hotmemory ./cmd/memory-smoke -count=1`：通过。
- 服务器相关包测试：
  - `PATH=/usr/local/go/bin:$PATH GOCACHE=$PWD/.gocache go test ./internal/importer ./cmd/memory-importer ./internal/jobs ./internal/archive ./internal/hotmemory ./cmd/memory-smoke -count=1`：通过。
- 服务器 production-sink dry-run：
  - `PATH=/usr/local/go/bin:$PATH go run ./cmd/memory-importer --source mem0 --batch prod_dry_verify_index --dry-run --production-sink --input internal/importer/fixtures/mem0_sample.jsonl`
  - 输出写入 `/tmp/memory-importer-prod-dryrun-index.json`。
  - 验证包含 `"dry_run":true`。
  - 结果：`production-sink-dry-run-ok`。
- 服务器 smoke：
  - `PATH=/usr/local/go/bin:$PATH make smoke`：通过，输出 `smoke ok`。
- 服务器 post-deploy：
  - `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过。
  - 日志：`/tmp/memory-os-post-deploy.Qzw5Z8`
  - 覆盖：compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。

安全检查：

- 本轮没有写入真实 secret。
- 本轮没有执行真实生产 `--production-sink --apply`。
- Archive chunking 复用 `archive.ChunkMarkdown`，继续执行 chunk 级 secret sanitize。
- 新增测试覆盖 index job chunk 中不包含 fixture 假 secret。
- 本轮没有新增公开端口。
- 本轮没有改变 PostgreSQL、Redis、Qdrant 对外暴露策略。

失败命令、根因和修复：

- importer 红灯测试失败符合预期：
  - 根因：ProductionSink 尚无 Archive index queue。
  - 修复：新增轻量 `ArchiveIndexQueue` 接口和 `ArchiveIndexJob` 模型。
- CLI adapter 红灯测试失败符合预期：
  - 根因：`cmd/memory-importer` 尚未把 importer queue job 适配到 `jobs.RAGIndexJob`。
  - 修复：新增 `archiveIndexQueueAdapter` 并在 PG-backed ProductionSink 中注入 `jobs.NewPGArchiveIndexQueue`。

部署状态：

- 本轮没有重新构建或重启生产容器。
- 生产 post-deploy 验证通过。
- 未 commit、未 push。

权限隔离结果：

- index job 保留 user/org/project 作用域。
- index job 携带 query-time filter 所需的 permission label。
- pipeline E2E 仍通过，说明 importer index queue 接入未破坏当前认证、归档、检索主链路。

剩余问题：

- 还没有在隔离恢复环境中执行真实 `--production-sink --apply` 并验证 PG 表里写入 Hot Memory、Archive、archive_chunks、archive_index_jobs。
- Hot Memory 写入可通过 service 可选 Qdrant index，但当前 CLI 尚未注入生产 Qdrant index。
- bundle re-import 到主系统的端到端验收仍未完成。
- 最终安全扫描、最终浏览器验收和最终交付报告仍未完成。
- Docker build context 约 `111MB`，部署耗时仍偏高，后续需要瘦身。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 在隔离恢复环境中执行真实 `memory-importer --production-sink --apply`，验证 PG-backed importer 端到端写入 Hot Memory / Archive / RAG index queue 且重复 apply 幂等。
- 或补 Hot Memory Qdrant index 注入，让 imported hot memory 也能立即进入向量检索。
- 或补 bundle re-import 验收，证明 export bundle 可重新进入主系统。

## 2026-07-03 Phase 1.124：Importer PG-backed ProductionSink 隔离真实 Apply 验收

完成事项：

- 在 `thinkpad:/opt/memory-os` 执行隔离真实 importer apply 验收。
- 使用临时 PostgreSQL 容器、临时 volume、动态 localhost 端口和临时 Archive 目录。
- 临时 PostgreSQL 密码随机生成，只存在于本次脚本环境，未写入日志。
- 对空库按 `migrations/*.sql` 顺序执行全部 15 个 SQL migration。
- 执行真实 `memory-importer --production-sink --apply`：
  - mem0 fixture 首次 apply。
  - mem0 fixture 重复 apply。
  - FastGPT fixture 首次 apply。
  - FastGPT fixture 重复 apply。
- 验证 PG 表真实落库：
  - `hot_memories=2`
  - `hot_memory_sources=2`
  - `archives=1`
  - `archive_versions=1`
  - `archive_chunks=1`
  - `archive_index_jobs=1`
- 验证重复 apply 幂等：
  - mem0 首次输出 `created_count=2`
  - mem0 重复输出 `deduped_count=2`
  - FastGPT 首次输出 `created_count=1`
  - FastGPT 重复输出 `deduped_count=1`
- 验证 fixture 假 secret 未进入隔离验收产物、Hot Memory fact 或 Archive chunk。
- 验证临时 PostgreSQL 容器和 volume 已清理，无残留。
- 验收后执行生产 smoke 和 post-deploy，确认隔离验收没有影响生产服务。

修改模块：

- `docs/production-delivery-log.md`
- 本轮没有修改运行时代码、migration、Docker Compose 或生产配置。

新增或变更 API：

- 无。

新增 migration：

- 无。

测试命令和结果：

- 服务器隔离 importer apply 验收脚本：通过。
  - 工作目录：`/opt/memory-os`
  - 临时审计目录：`/opt/memory-os/artifacts/importer-production-sink-20260703T071436Z-1637514`
  - migration 数：`15`
  - 结果：`importer production-sink isolated apply ok`
  - 计数：`hot_memories:2,hot_memory_sources:2,archives:1,archive_versions:1,archive_chunks:1,archive_index_jobs:1,leaked_chunks:0,leaked_hot:0`
  - 清理结果：`residue=clean`
- 服务器生产 smoke：
  - `PATH=/usr/local/go/bin:$PATH make smoke`：通过，输出 `smoke ok`。
- 服务器 post-deploy：
  - `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过。
  - 日志：`/tmp/memory-os-post-deploy.jqKMqq`
  - 覆盖：compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。

安全检查：

- 本轮没有写入真实 secret。
- 本轮没有对生产 PostgreSQL 执行 importer apply。
- 临时 PostgreSQL 仅绑定 `127.0.0.1` 动态端口，不对外暴露。
- 临时 PostgreSQL 密码随机生成且未输出。
- fixture 假 secret 未出现在隔离验收 artifacts、Hot Memory fact 或 Archive chunk。
- 临时容器和 volume 已删除。
- 本轮没有新增公开端口。
- 本轮没有改变 PostgreSQL、Redis、Qdrant 对外暴露策略。

失败命令、根因和修复：

- 本轮隔离 importer apply、SQL 断言、secret 扫描、残留检查、生产 smoke、post-deploy 均通过。
- 未发现需要修复的运行时代码缺陷。

部署状态：

- 本轮没有重新构建或重启生产容器。
- 生产 post-deploy 验证通过。
- 未 commit、未 push。

权限隔离结果：

- 隔离落库数据使用默认 importer scope：`user_1`、`org_1`、`project_1`、`agent_id=importer`。
- Archive chunk 与 index job 保留 project scope 和 permission label。
- pipeline E2E 仍通过，说明隔离验收未破坏当前认证、归档、检索主链路。

剩余问题：

- Hot Memory 写入可通过 service 可选 Qdrant index，但当前 CLI 尚未注入生产 Qdrant index。
- bundle re-import 到主系统的端到端验收仍未完成。
- 最终安全扫描、最终浏览器验收和最终交付报告仍未完成。
- Docker build context 约 `111MB`，部署耗时仍偏高，后续需要瘦身。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 补 Hot Memory Qdrant index 注入，让 imported hot memory 也能立即进入向量检索。
- 或补 bundle re-import 验收，证明 export bundle 可重新进入主系统。
- 或启动最终安全扫描前置审计，收敛 Secret 泄露和权限隔离证据。

## 2026-07-03 Phase 1.125：Importer Hot Memory 注入生产 Qdrant Index

完成事项：

- `cmd/memory-importer` 的 PG-backed ProductionSink 不再只构建 Hot Memory PG service。
- 新增 importer 侧生产 Hot Memory 构建路径，复用 API 的生产索引模式：
  - `qdrant.NewClient(cfg.QdrantURL)`
  - `EnsureCollection(memory_os, 1024, Cosine)`
  - `llm.NewOpenAICompatible` embedding client
  - `hotmemory.NewQdrantIndex`
  - `hotmemory.NewServiceWithVectorIndex`
- `memory-importer --production-sink --apply` 导入 Hot Memory 时现在会同步：
  - 写入 `hot_memories`
  - 写入 `hot_memory_sources`
  - 调用 embedding provider
  - upsert Qdrant point
  - 写入 `hot_memory_qdrant_points`
- 保留 Archive index queue 注入，不影响 Phase 1.123。
- 新增可测试的 `newProductionSink` 和可替换 factory，避免 CLI 生产路径不可验证。

修改模块：

- `cmd/memory-importer/main.go`
- `cmd/memory-importer/main_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无 HTTP API 变更。
- CLI 行为增强：
  - `memory-importer --production-sink --apply` 现在会把 imported Hot Memory 写入生产 Qdrant index。

新增 migration：

- 无。

测试命令和结果：

- 本地红灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./cmd/memory-importer -run 'TestNewProductionSinkConfiguresHotMemoryVectorIndex|TestNewProductionSinkReturnsHotMemoryVectorIndexError' -count=1`
  - 初次失败符合预期：
    - `undefined: newProductionSink`
    - `undefined: newImporterProductionHotMemory`
    - `undefined: newImporterArchiveIndexQueue`
- 本地绿灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./cmd/memory-importer -run 'TestNewProductionSinkConfiguresHotMemoryVectorIndex|TestNewProductionSinkReturnsHotMemoryVectorIndexError' -count=1`：通过。
- 本地相关包测试：
  - `GOCACHE="$PWD/.gocache" go test ./cmd/memory-importer ./internal/importer ./internal/hotmemory ./internal/qdrant ./internal/llm ./internal/jobs ./internal/archive ./cmd/memory-smoke -count=1`：通过。
- 服务器相关包测试：
  - `PATH=/usr/local/go/bin:$PATH GOCACHE=$PWD/.gocache go test ./cmd/memory-importer ./internal/importer ./internal/hotmemory ./internal/qdrant ./internal/llm ./internal/jobs ./internal/archive ./cmd/memory-smoke -count=1`：通过。
- 服务器 production-sink dry-run：
  - `PATH=/usr/local/go/bin:$PATH go run ./cmd/memory-importer --source mem0 --batch prod_dry_verify_qdrant --dry-run --production-sink --input internal/importer/fixtures/mem0_sample.jsonl`
  - 结果：`production-sink-dry-run-ok`。
- 服务器隔离真实 Qdrant apply 验收：通过。
  - 临时 PostgreSQL 空库执行 15 个 migration。
  - 临时 Qdrant 容器启动并创建 `memory_os` collection。
  - 临时 mock embedding endpoint 返回 1024 维向量。
  - 执行 mem0 首次 apply、mem0 重复 apply、FastGPT 首次 apply、FastGPT 重复 apply。
  - 审计目录：`/opt/memory-os/artifacts/importer-qdrant-20260703T072021Z-1653616`
  - 计数：`hot_memories:2,hot_memory_qdrant_points:2,indexed_hot_memory_qdrant_points:2,archives:1,archive_chunks:1,archive_index_jobs:1,qdrant_points:2,leaked_chunks:0,leaked_hot:0`
  - 清理结果：`residue=clean`
- 服务器 smoke：
  - `PATH=/usr/local/go/bin:$PATH make smoke`：通过，输出 `smoke ok`。
- 服务器 post-deploy：
  - `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过。
  - 日志：`/tmp/memory-os-post-deploy.wQM6UO`
  - 覆盖：compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。

安全检查：

- 本轮没有写入真实 secret。
- 本轮没有对生产 PostgreSQL 或生产 Qdrant 执行 importer apply。
- 隔离 Qdrant 和隔离 PostgreSQL 仅绑定 `127.0.0.1` 动态端口，不对外暴露。
- 临时 PostgreSQL 密码随机生成且未输出。
- 临时 embedding endpoint 只服务本机回环地址。
- fixture 假 secret 未出现在隔离验收 artifacts、Hot Memory fact 或 Archive chunk。
- 临时容器、volume、mock embedding 进程已清理。
- 本轮没有新增公开端口。
- 本轮没有改变 PostgreSQL、Redis、Qdrant 对外暴露策略。

失败命令、根因和修复：

- 红灯测试失败符合预期：
  - 根因：Importer production sink 尚未有可测试的 Hot Memory vector index 构建路径。
  - 修复：新增 `newProductionSink` 和 `newImporterProductionHotMemory` factory，生产路径注入 `hotmemory.NewQdrantIndex`。

部署状态：

- 本轮没有重新构建或重启生产容器。
- 生产 post-deploy 验证通过。
- 未 commit、未 push。

权限隔离结果：

- Hot Memory Qdrant payload 保留 user/org/project/agent/scope/visibility/permission labels/status。
- Qdrant 写入仍通过单 collection `memory_os`。
- pipeline E2E 仍通过，说明 importer Hot Memory Qdrant 注入未破坏当前认证、归档、检索主链路。

剩余问题：

- bundle re-import 到主系统的端到端验收仍未完成。
- 最终安全扫描、最终浏览器验收和最终交付报告仍未完成。
- Docker build context 约 `111MB`，部署耗时仍偏高，后续需要瘦身。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 补 bundle re-import 验收，证明 export bundle 可重新进入主系统。
- 或启动最终安全扫描前置审计，收敛 Secret 泄露和权限隔离证据。
- 或做 Docker build context 瘦身，降低最终部署验证成本。

## 2026-07-03 Phase 1.126：Importer Bundle Re-import 端到端验收

完成事项：

- `memory-importer --export-bundle` 导出的 Markdown/RAG bundle 现在可以作为 `--source bundle` 重新导入。
- Bundle parser 支持当前 CLI 导出格式：
  - `# Memory OS Export Bundle`
  - `## <kind> <external_id>` item 分段。
  - `source_refs` JSON 恢复原始来源。
- Bundle re-import 复用 importer repository 幂等 key，重复导入不会产生重复 importer item。
- Hot Memory bundle item 重新导入后保持 `hot_memory` 类型。
- FastGPT Archive bundle item 重新导入后保持 `archive` 类型。
- 重新导入时再次执行 sanitize，fixture 假 secret 不会进入 re-import item。

修改模块：

- `internal/importer/model.go`
- `internal/importer/service.go`
- `internal/importer/service_test.go`
- `cmd/memory-importer/main_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无 HTTP API 变更。
- CLI 新增 source type：
  - `--source bundle`

新增 migration：

- 无。

测试命令和结果：

- 本地红灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/importer ./cmd/memory-importer -count=1`
  - 初次失败符合预期：
    - `undefined: SourceBundle`
    - `undefined: importer.SourceBundle`
- 本地绿灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/importer ./cmd/memory-importer -count=1`：通过。
- 本地相关包测试：
  - `GOCACHE="$PWD/.gocache" go test ./cmd/memory-importer ./internal/importer ./internal/hotmemory ./internal/qdrant ./internal/llm ./internal/jobs ./internal/archive ./cmd/memory-smoke -count=1`：通过。
- 服务器相关包测试：
  - `PATH=/usr/local/go/bin:$PATH GOCACHE=$PWD/.gocache go test ./cmd/memory-importer ./internal/importer ./internal/hotmemory ./internal/qdrant ./internal/llm ./internal/jobs ./internal/archive ./cmd/memory-smoke -count=1`：通过。
- 服务器真实 CLI bundle round-trip：
  - mem0 fixture 导入到临时 source state。
  - 从 source state 导出 bundle。
  - 使用 `--source bundle` 导入到临时 bundle state。
  - 第二次重复导入同一 bundle。
  - 结果：
    - 首次：`source_type=bundle,item_count=2,created_count=2,deduped_count=0`
    - 重复：`source_type=bundle,item_count=2,created_count=0,deduped_count=2`
- 服务器 smoke：
  - `PATH=/usr/local/go/bin:$PATH make smoke`：通过，输出 `smoke ok`。
- 服务器 post-deploy：
  - `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过。
  - 日志：`/tmp/memory-os-post-deploy.tNMCwQ`
  - 覆盖：compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。

安全检查：

- 本轮没有写入真实 secret。
- 本轮没有对生产 PostgreSQL 或生产 Qdrant 执行 importer apply。
- 服务器 bundle round-trip 只使用 `/tmp` 临时 state 和临时 bundle 文件，命令结束后清理。
- 重新导入时会再次执行 `secret.Sanitize`。
- fixture 假 secret 未出现在测试输出、bundle re-import result 或 importer item。
- 本轮没有新增公开端口。
- 本轮没有改变 PostgreSQL、Redis、Qdrant 对外暴露策略。

失败命令、根因和修复：

- 服务器首次测试命令失败：
  - 命令：`ssh thinkpad 'cd /opt/memory-os && GOCACHE="$PWD/.gocache" go test ...'`
  - 现象：`bash: line 1: go: command not found`
  - 根因：非交互 SSH shell 的 `PATH` 未包含 `/usr/local/go/bin`。
  - 修复：验证命令显式临时设置 `PATH=/usr/local/go/bin:$PATH`，未修改服务器 profile。
- 红灯测试失败符合预期：
  - 根因：Importer 尚无 `bundle` source type 和 bundle parser。
  - 修复：新增 `SourceBundle`、`parseBundle`、heading/source_refs 解析和 re-import 测试。

部署状态：

- 本轮没有重新构建或重启生产容器。
- 已同步源码和测试到 `thinkpad:/opt/memory-os`。
- 生产 post-deploy 验证通过。
- 未 commit、未 push。

权限隔离结果：

- Bundle re-import 本身不执行检索，不新增 Qdrant query-time filter 风险。
- 原始 `source_refs` 会被保留，后续进入 ProductionSink 时仍可追溯来源。
- 当前 pipeline E2E 仍通过，说明本轮 CLI source type 增加未破坏运行态 API/Web/Worker/MCP。

剩余问题：

- Bundle re-import 已验证 importer state 层幂等；尚未执行 production-sink bundle apply 到隔离 PostgreSQL + Qdrant 的完整 round-trip。
- 最终安全扫描、最终浏览器验收和最终交付报告仍未完成。
- Docker build context 约 `111MB`，部署耗时仍偏高，后续需要瘦身。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 做 production-sink bundle apply 隔离验收，证明 bundle 可进入 Archive/Hot Memory/Qdrant 主系统。
- 或启动最终安全扫描前置审计，收敛 Secret 泄露和权限隔离证据。
- 或做 Docker build context 瘦身，降低最终部署验证成本。

## 2026-07-03 Phase 1.127：Importer Bundle ProductionSink 隔离真实验收

完成事项：

- Bundle re-import 进入 ProductionSink 时不再丢失原始来源。
- Hot Memory ProductionSink source ref 从 `importer:bundle:<external_id>` 修正为原始来源：
  - mem0 bundle 写入后为 `importer:mem0:<external_id>`。
- Archive ProductionSink payload 新增原始来源字段：
  - `original_source_type`
  - `original_external_id`
- FastGPT bundle 写入 Archive Markdown 后可以看到原始 `fastgpt` source ref。
- 完成隔离 PostgreSQL + Qdrant + mock embedding 的真实 `--source bundle --production-sink --apply` 验收。

修改模块：

- `internal/importer/service.go`
- `internal/importer/service_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无 HTTP API 变更。
- CLI 参数无新增。
- ProductionSink 写入语义增强：
  - Bundle item 的主系统来源使用原始 `source_refs`。

新增 migration：

- 无。

测试命令和结果：

- 本地红灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/importer -run 'TestBundleApplyWritesOriginalSourceRefToHotMemoryProductionSink|TestBundleApplyWritesOriginalSourceRefToArchiveProductionSink' -count=1`
  - 初次失败符合预期：
    - Hot Memory source ref 为 `importer:bundle:mem0_1`，不满足原始 mem0 来源追溯。
    - Archive Markdown 只包含 `source type: bundle`，不包含原始 fastgpt 来源。
- 本地绿灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/importer -run 'TestBundleApplyWritesOriginalSourceRefToHotMemoryProductionSink|TestBundleApplyWritesOriginalSourceRefToArchiveProductionSink' -count=1`：通过。
- 本地相关包测试：
  - `GOCACHE="$PWD/.gocache" go test ./cmd/memory-importer ./internal/importer ./internal/hotmemory ./internal/qdrant ./internal/llm ./internal/jobs ./internal/archive ./cmd/memory-smoke -count=1`：通过。
- 服务器相关包测试：
  - `PATH=/usr/local/go/bin:$PATH GOCACHE=$PWD/.gocache go test ./cmd/memory-importer ./internal/importer ./internal/hotmemory ./internal/qdrant ./internal/llm ./internal/jobs ./internal/archive ./cmd/memory-smoke -count=1`：通过。
- 服务器隔离真实 ProductionSink bundle apply：
  - 临时 PostgreSQL 容器绑定 `127.0.0.1` 动态端口。
  - 临时 Qdrant 容器绑定 `127.0.0.1` 动态端口。
  - 临时 mock embedding endpoint 绑定 `127.0.0.1` 动态端口，返回 1024 维向量。
  - 执行 embedded migrations。
  - mem0 fixture 先导出 bundle，再以 `--source bundle --production-sink --apply` 写入 Hot Memory + Qdrant。
  - FastGPT fixture 先导出 bundle，再以 `--source bundle --production-sink --apply` 写入 Archive + index job。
  - 结果：
    - `hot=2`
    - `hot_points=2`
    - `source_refs=2`
    - `archives=1`
    - `index_jobs=1`
    - `archive_files=1`
    - `archive_original_refs=1`
    - `qdrant=2`
    - `leaked=0`
- 服务器 smoke：
  - `PATH=/usr/local/go/bin:$PATH make smoke`：通过，输出 `smoke ok`。
- 服务器 post-deploy：
  - `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过。
  - 日志：`/tmp/memory-os-post-deploy.t8Zckk`
  - 覆盖：compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。

安全检查：

- 本轮没有写入真实 secret。
- 本轮没有对生产 PostgreSQL 或生产 Qdrant 执行 importer apply。
- 隔离 PostgreSQL、Qdrant、mock embedding 均只绑定本机回环地址。
- 临时数据库密码随机生成且未输出。
- 临时 mock embedding 使用非真实测试 API key。
- fixture 假 secret 未进入 Archive Markdown、Hot Memory、Qdrant 计数链路或验收输出。
- 临时容器、临时目录、mock embedding 进程均已清理。
- 本轮没有新增公开端口。
- 本轮没有改变 PostgreSQL、Redis、Qdrant 对外暴露策略。

失败命令、根因和修复：

- 第一次隔离验收失败：
  - 现象：`use of internal package memory-os/internal/db not allowed`
  - 根因：临时 migration 程序放在 `/tmp`，不在 Go module 根目录下，违反 Go `internal/` 导入规则。
  - 修复：把临时 migration 程序放入 `/opt/memory-os/.bundle-prod.*` 仓库内临时目录。
- 第二次隔离验收失败：
  - 现象：`step=query_counts`
  - 根因：`set -euo pipefail` 下，泄露扫描 `grep` 无命中返回 1，导致脚本提前退出。
  - 修复：无命中 grep 使用安全计数。
- 第三次隔离验收失败：
  - 现象：`archive_original_refs=0`，其余计数均正确。
  - 根因：验收脚本用普通 `grep` 匹配 Markdown `**...**`，星号被正则语义影响。
  - 修复：改用 `grep -F` 固定字符串匹配。

部署状态：

- 本轮没有重新构建或重启生产容器。
- 已同步源码和测试到 `thinkpad:/opt/memory-os`。
- 生产 post-deploy 验证通过。
- 未 commit、未 push。

权限隔离结果：

- Bundle ProductionSink 写入 Hot Memory 后，Qdrant payload 仍由 Hot Memory index 生成，继续包含 user/org/project/agent/scope/visibility/permission labels/status。
- Bundle ProductionSink 写入 Archive 后，Archive index job 仍携带 user/org/project/visibility/permission labels。
- 本轮不改变 Qdrant 查询路径；query-time filter 风险未扩大。

剩余问题：

- 最终安全扫描、最终浏览器验收和最终交付报告仍未完成。
- Docker build context 约 `111MB`，部署耗时仍偏高，后续需要瘦身。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 启动最终安全扫描前置审计，收敛 Secret 泄露、日志、Archive、Hot Memory、Qdrant payload 证据。
- 或做 Docker build context 瘦身，降低最终部署验证成本。
- 或进入浏览器验收前的管理台静态假数据清理。

## 2026-07-03 Phase 1.128：最终安全扫描前置审计

完成事项：

- 完成源码 Secret scan。
- 完成生产运行态只读 Secret audit：
  - 容器日志。
  - Markdown Archive 文件。
  - PostgreSQL Hot Memory fact。
  - PostgreSQL Archive chunk content。
  - PostgreSQL Hot Memory Qdrant payload tracking。
  - PostgreSQL Archive Qdrant payload tracking。
  - Qdrant `memory_os` collection 全量 payload scroll。
- 验证 Qdrant payload 只包含治理元数据、hash、scope、permission labels 等字段，不直接保存正文或 Secret 明文。
- 验证生产容器状态：
  - PostgreSQL 未对外暴露。
  - Redis 未对外暴露。
  - Qdrant 按项目计划暴露 `18083`。

修改模块：

- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

测试命令和结果：

- 服务器源码 Secret scan：
  - `PATH=/usr/local/go/bin:$PATH make secret-scan`
  - 结果：`secret scan ok`
- 服务器安全相关测试包：
  - `PATH=/usr/local/go/bin:$PATH GOCACHE=$PWD/.gocache go test ./internal/secret ./internal/eventlog ./internal/secretscan ./internal/qdrant ./internal/rag ./internal/hotmemory ./internal/archive ./internal/importer -count=1`
  - 结果：全部通过。
- 服务器运行态 Secret audit：
  - `runtime_secret_audit log_hits=0 archive_hits=0 pg_hits=0 qdrant_hits=0 qdrant_total=206 archive_files=194`
  - 完整 Qdrant payload scroll：
    - `qdrant_full_payload_secret_audit scanned=206 pages=4 hits=0`
- PostgreSQL 分项审计：
  - `archive_chunks_rows=202`
  - `archive_chunks_secret_hits=0`
  - `hot_memories_rows=22`
  - `hot_memories_secret_hits=0`
  - `hot_memory_qdrant_points_rows=10`
  - `hot_memory_qdrant_payload_secret_hits=0`
  - `qdrant_points_rows=196`
  - `qdrant_points_payload_secret_hits=0`
- 服务器 smoke：
  - `PATH=/usr/local/go/bin:$PATH make smoke`：通过，输出 `smoke ok`。
- 服务器 post-deploy：
  - `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过。
  - 日志：`/tmp/memory-os-post-deploy.f2f35s`
  - 覆盖：compose-ps、version、healthz、openapi、openapi-validate、smoke、pipeline-e2e。

安全检查：

- 本轮没有写入真实 secret。
- 本轮没有修改生产数据。
- 本轮没有重启或重新部署生产容器。
- 本轮所有运行态审计都是只读查询或日志读取。
- 审计命令只输出命中计数，不输出任何疑似敏感原文。
- 源码、日志、Archive、Hot Memory、Archive chunk、PG payload tracking、Qdrant payload 均未发现 Secret 明文命中。

失败命令、根因和修复：

- 首次运行态审计脚本无输出失败：
  - 根因：`set -euo pipefail` 下，`grep` 无命中返回 1，导致计数管道提前退出。
  - 修复：无命中 grep 使用安全计数。
- 初始 PG 分项审计覆盖不足：
  - 现象：脚本猜测表名 `archive_qdrant_points`，生产库实际不存在。
  - 根因：真实 Archive Qdrant payload tracking 表为 `qdrant_points`。
  - 修复：先列出实际表名，再用 `qdrant_points` 重新执行分项审计。

部署状态：

- 本轮没有重新构建或重启生产容器。
- 生产 post-deploy 验证通过。
- 未 commit、未 push。

权限隔离结果：

- 本轮未改变权限逻辑。
- Qdrant full scroll 只审计 payload 是否含敏感明文，不作为检索路径。
- 当前 Qdrant payload 仍仅包含治理字段、hash、scope、visibility 和 permission labels。

剩余问题：

- 最终浏览器验收和最终交付报告仍未完成。
- 管理台仍需继续清理静态假数据和不可操作项。
- Docker build context 约 `111MB`，部署耗时仍偏高，后续需要瘦身。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 进入浏览器验收前的管理台静态假数据清理。
- 或做 Docker build context 瘦身，降低最终部署验证成本。
- 或开始整理最终交付报告框架，边补证据边收敛。

## 2026-07-03 Phase 1.129：总览页移除静态统计并接入真实 API

完成事项：

- 移除管理台首页静态假统计：
  - `归档文档 12`
  - `热记忆 38`
  - `Secret 引用 6`
  - `Adapter 5`
- 首页改为聚合真实 API：
  - `/memory/archive/list`
  - `/memory/hot-memory/list`
  - `/memory/secrets/list`
  - `/memory/tokens/adapter/list`
  - `/memory/qdrant/status`
- 首页增加未登录、未选择组织/项目、加载中、错误态展示。
- 首页统计卡片明确展示来源 API，不再展示静态假数据。
- 完成生产 Web 重新构建和部署。

修改模块：

- `web/pages/index.vue`
- `internal/webdeploy/web_dockerfile_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无后端 API 变更。
- 前端总览页新增真实 API 聚合调用。

新增 migration：

- 无。

测试命令和结果：

- 本地红灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/webdeploy -run TestDashboardPageUsesRealAPIStats -count=1`
  - 初次失败符合预期：
    - 首页仍包含 `['归档文档', '12'` 静态统计。
- 本地绿灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/webdeploy -run TestDashboardPageUsesRealAPIStats -count=1`：通过。
- 本地 webdeploy 测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/webdeploy -count=1`：通过。
- 本地 Nuxt build：
  - `cd web && npm run build`：通过。
- 服务器 webdeploy 测试：
  - `PATH=/usr/local/go/bin:$PATH GOCACHE=$PWD/.gocache go test ./internal/webdeploy -count=1`：通过。
- 服务器 Web build：
  - `make build-web`：通过。
- 生产部署：
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-web`
  - 结果：完成。
  - 注意：compose 因依赖关系同时重建并重启了 `memory-api`，不是纯 web 替换。
- 生产 smoke：
  - `PATH=/usr/local/go/bin:$PATH make smoke`：通过，输出 `smoke ok`。
- 生产 post-deploy：
  - `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过。
  - 日志：`/tmp/memory-os-post-deploy.sNglHm`
- 部署产物检查：
  - `deployed_dashboard_assets api_hits=6 old_exact_hits=0`
  - 说明：生产 Nginx 静态 JS 中包含真实 API marker，且不含旧静态固定值 marker。
- 浏览器验收：
  - Playwright 打开 `http://ddns.08121.top:18080/` 后正常跳转到 `/login`。
  - 未登录快照包含登录页文案和 PAT 输入框。
  - 截图：`memory-os-login-after-dashboard-deploy.png`
  - in-app browser 控制通道连续超时，未能完成登录后 DOM 验收。

安全检查：

- 本轮没有写入真实 secret。
- 本轮没有打印或读取真实 PAT。
- 未为了浏览器验收创建临时生产 PAT。
- Secret/PAT 明文仍只由登录页用户输入；本轮没有通过自动化向页面注入凭据。

失败命令、根因和修复：

- in-app browser `goto` 和新标签创建连续超时：
  - 根因：浏览器控制通道不可用；服务器 curl 与 Playwright 打开页面均证明 Web 服务正常。
  - 处理：改用 Playwright 浏览器工具完成未登录路由保护验收，并用生产静态产物 grep 验证首页真实 API marker。
- 首次部署产物 grep 出现旧静态数字误报：
  - 根因：minified JS 单行过长，正则 `归档文档.*12` 跨整行匹配到无关数字。
  - 修复：改用精确 marker `label:"归档文档",value:"12"` 等复查，结果 `old_exact_hits=0`。

部署状态：

- 生产 Web 已重建并运行。
- `memory-api` 被 compose 依赖关系重建并重启，post-deploy 已证明健康。
- 未 commit、未 push。

权限隔离结果：

- 首页仅在已登录且存在 org/project context 时调用管理 API。
- 未登录访问首页会跳转登录页。
- 首页统计请求全部复用现有 `useApi`，携带 PAT Bearer token。

剩余问题：

- 登录后浏览器 DOM 验收尚未完成；需要可安全使用的一次性 PAT 或修复 in-app browser 控制通道。
- 管理台仍需继续扫描其它页面是否存在静态/不可操作项。
- Docker build context 已观测到约 `117.8MB`，并且 `memory-web` 部署触发 `memory-api` 重建，部署耗时偏高，下一步应优先瘦身和拆依赖。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 优先处理 Docker build context 瘦身与 web-only 部署不应重建 API 的问题。
- 或继续清理管理台剩余页面的静态假数据。
- 或准备一次性 PAT 安全验收流程，补登录后浏览器 DOM 验收。

## 2026-07-03 Phase 1.130：解除 Web-only 部署对 API 容器的 Compose 依赖

完成事项：

- 移除 `memory-web` 服务对 `memory-api` 的 Docker Compose `depends_on`。
- 保留 `memory-web` 的静态构建参数、T480 外部 API Base 和 `18080` 端口映射。
- 新增防回归测试，确保 `memory-web` 服务块不再依赖 API 容器。
- 在本地和 `thinkpad:/opt/memory-os` 分别渲染 compose 配置，确认 `memory-web` 只包含 build、network、ports，不包含 `depends_on`。

修改模块：

- `deploy/docker-compose.yml`
- `internal/webdeploy/web_dockerfile_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

测试命令和结果：

- 本地红灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/webdeploy -run TestComposeWebServiceDoesNotDependOnAPIContainer -count=1`
  - 初次失败符合预期：`memory-web compose service must not depend on API container marker "depends_on:"`。
- 本地绿灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/webdeploy -run TestComposeWebServiceDoesNotDependOnAPIContainer -count=1`：通过。
- 本地 compose 配置渲染：
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml config memory-web`：通过。
  - 结果显示 `memory-web` 无 `depends_on`，API Base 为 `http://ddns.08121.top:18081`。
- 服务器同步：
  - `rsync -avz deploy/docker-compose.yml internal/webdeploy/web_dockerfile_test.go thinkpad:/opt/memory-os/ --relative`：完成。
- 服务器 webdeploy 窄测试：
  - `PATH=/usr/local/go/bin:$PATH GOCACHE="$PWD/.gocache" go test ./internal/webdeploy -run TestComposeWebServiceDoesNotDependOnAPIContainer -count=1`：通过。
- 服务器 compose 配置渲染：
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml config memory-web`：通过。
  - 结果显示 `memory-web` 无 `depends_on`。
- 服务器容器状态检查：
  - `deploy-memory-web-1 Up`
  - `deploy-memory-api-1 Up`
  - `deploy-memory-worker-1 Up`
  - `deploy-memory-mcp-1 Up`

部署状态：

- 本轮未执行 `docker-compose up`，未重启线上容器。
- 新 compose 配置已同步到服务器工作区，将在下一次 Web-only 部署时生效。

安全检查：

- 本轮未读取、写入或打印真实 secret。
- 本轮没有数据库 migration、数据删除、容器重启或服务端口变更。

剩余问题：

- Docker build context 仍约为百 MB 级，后续还需要继续瘦身。
- 需要在下一次实际 Web-only 部署时复验 API 容器不会被重建或重启。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 继续处理 Docker build context 瘦身，降低部署耗时和误打包风险。
- 或继续清理管理台剩余页面的静态假数据。
- 或补登录后浏览器 DOM 验收。

## 2026-07-03 Phase 1.131：Docker Build Context 瘦身与开发产物隔离

完成事项：

- 新增根目录 `.dockerignore`。
- 排除不应进入 Docker build context 的开发与运行产物：
  - `.gocache/`
  - `.codebase-memory/`
  - `.playwright-mcp/`
  - `node_modules/`
  - `web/node_modules/`
  - `web/.nuxt/`
  - `web/.output/`
  - `artifacts/`
  - `backups/`
  - `docs.zip`
  - `.DS_Store`
  - 临时日志与截图产物。
- 新增防回归测试，确保 `.dockerignore` 不会误排除 `cmd/`、`internal/`、`web/`、`migrations/`、`deploy/`、`go.mod` 等构建输入。
- 在服务器用 Docker 经典 builder 验证真实 context 传输大小。

修改模块：

- `.dockerignore`
- `internal/webdeploy/web_dockerfile_test.go`
- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

测试命令和结果：

- 本地红灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/webdeploy -run TestDockerignoreExcludesDevelopmentArtifactsFromBuildContext -count=1`
  - 初次失败符合预期：根目录缺少 `.dockerignore`。
- 本地绿灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/webdeploy -run TestDockerignoreExcludesDevelopmentArtifactsFromBuildContext -count=1`：通过。
- 本地 webdeploy 全量测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/webdeploy -count=1`：通过。
- 本地 Docker context 检查：
  - `DOCKER_BUILDKIT=1 docker build --progress=plain -f - .`：未执行成功。
  - 原因：本地 Docker daemon 未运行，`unix:///Users/kanyun/.docker/run/docker.sock` 不存在。
- 服务器同步：
  - `rsync -avz .dockerignore internal/webdeploy/web_dockerfile_test.go thinkpad:/opt/memory-os/ --relative`：完成。
- 服务器 webdeploy 全量测试：
  - `PATH=/usr/local/go/bin:$PATH GOCACHE="$PWD/.gocache" go test ./internal/webdeploy -count=1`：通过。
- 服务器体积检查：
  - `du -ah . | sort -hr | head -40` 发现主要开发产物：
    - `web/node_modules` 约 `286M`
    - `.gocache` 约 `182M`
    - `artifacts` / `backups` 为运行与验收证据目录。
- 服务器 Docker context 验证：
  - `DOCKER_BUILDKIT=0 docker build -f - .`
  - 输出包含：`Sending build context to Docker daemon  2.456MB`
  - 命令最终 exit 1，原因是临时 Dockerfile 只有 `FROM scratch`，没有生成镜像；context 传输证据已有效输出。

部署状态：

- 本轮未执行生产 `docker-compose up`。
- 本轮未重启线上容器。
- `.dockerignore` 已同步到服务器工作区，将在下一次 Docker build 时生效。

安全检查：

- 本轮未读取、写入或打印真实 secret。
- `.dockerignore` 避免把备份、artifacts、缓存、截图等开发/运行产物误送入镜像构建上下文。

剩余问题：

- 还需要在下一次实际 Web-only 部署时复验：
  - context 仍为 MB 级。
  - API 容器不会被重建或重启。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 进行一次受控 Web-only 构建/部署验收，验证 context 瘦身与 API 不重启同时成立。
- 或继续清理管理台剩余页面的静态假数据。
- 或补登录后浏览器 DOM 验收。

## 2026-07-03 Phase 1.132：受控 Web-only 构建/部署验收

完成事项：

- 在 `thinkpad:/opt/memory-os` 执行真实 Web-only 构建与部署。
- 验证 `.dockerignore` 在真实 compose build 中生效。
- 验证 `memory-web` 重新构建和重启时，`memory-api` 未被重建、未被重启。
- 部署后完成 smoke 和 post-deploy pipeline E2E 验收。

修改模块：

- `docs/production-delivery-log.md`

新增或变更 API：

- 无。

新增 migration：

- 无。

部署前证据：

- API 容器部署前指纹：
  - container id：`4d050aac73b2e4b738c54304ece4f8ab6f12abfb5a045e4a16306040d239e7c6`
  - image：`sha256:5318da307aeac4e307afbb42458d5951bb5d2ee6babd8a4a722736dbf381b12f`
  - StartedAt：`2026-07-03T07:52:49.659629546Z`
  - status：`running`
- Web 容器部署前指纹：
  - container id：`6e157281fd4bd81116a83171e2cea6fc238dee2863ec3b6c8b47160467deb90a`
  - image：`sha256:ed08043de8dddf9b0951dabfdf18f7db04450559debcf566496dffa909f601f2`
  - StartedAt：`2026-07-03T07:52:49.810444004Z`
- context probe：
  - `DOCKER_BUILDKIT=0 docker build -f - .`
  - 输出包含：`Sending build context to Docker daemon  2.459MB`
  - 命令最终 exit 1，原因是临时 Dockerfile 只有 `FROM scratch`，没有生成镜像；context 输出有效。

部署命令和结果：

- 命令：

```bash
cd /opt/memory-os
. scripts/load-prod-env.sh >/dev/null
NUXT_PUBLIC_API_BASE=${NUXT_PUBLIC_API_BASE:-http://ddns.08121.top:18081} \
DOCKER_BUILDKIT=0 \
docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-web
```

- 结果：成功。
- compose 输出只出现：
  - `Image deploy-memory-web Building`
  - `Container deploy-memory-web-1 Recreate`
  - `Container deploy-memory-web-1 Started`
- compose 输出未出现 `memory-api` build/recreate/start。
- 真实 Web build context：
  - `Sending build context to Docker daemon  474.4kB`

部署后证据：

- API 容器部署后指纹：
  - container id：`4d050aac73b2e4b738c54304ece4f8ab6f12abfb5a045e4a16306040d239e7c6`
  - image：`sha256:5318da307aeac4e307afbb42458d5951bb5d2ee6babd8a4a722736dbf381b12f`
  - StartedAt：`2026-07-03T07:52:49.659629546Z`
  - status：`running`
- 对比结果：
  - `api_container_id_unchanged=true`
  - `api_image_unchanged=true`
  - `api_started_unchanged=true`
- Web 容器部署后指纹：
  - container id：`08baaa6e862b5b5c681721349854be4bd342499848f54e80428073fba65931e5`
  - image：`sha256:baf838db49d7105c292de3d78ab5631a57170cb95ed521d52f3e911583c8f503`
  - StartedAt：`2026-07-03T08:08:12.06448838Z`
  - status：`running`
- `curl -fsSI http://127.0.0.1:18080/`：HTTP 200。
- `curl -fsS http://127.0.0.1:18081/healthz`：`db/qdrant/redis` 均为 `ok`。

测试命令和结果：

- `PATH=/usr/local/go/bin:$PATH make smoke`：通过，输出 `smoke ok`。
- `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过。
- post-deploy 步骤：
  - `compose-ps`
  - `version`
  - `healthz`
  - `openapi`
  - `openapi-validate`
  - `smoke`
  - `pipeline-e2e`
- post-deploy 日志：
  - `/tmp/memory-os-post-deploy.83fPYP`

部署状态：

- `memory-web` 已完成受控重建并运行。
- `memory-api` 未重建、未重启。
- `memory-worker`、`memory-mcp` 未重建、未重启。

安全检查：

- 本轮未读取、写入或打印真实 secret。
- `scripts/load-prod-env.sh` 仅用于向 compose 注入环境变量，命令输出未打印 secret 值。
- 本轮没有数据库 migration、数据删除、端口变更或备份恢复操作。

风险关闭：

- Phase 1.129 遗留的 `memory-web` 部署牵动 `memory-api` 重建风险：已通过真实部署证据关闭。
- Phase 1.131 遗留的 Docker build context 百 MB 级风险：已通过真实 compose build `474.4kB` 证据关闭。

剩余问题：

- 登录后浏览器 DOM 验收仍未补齐。
- 管理台仍需继续清理剩余静态/不可操作项。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 继续清理管理台剩余页面的静态假数据与不可操作项。
- 或准备安全的一次性 PAT，补登录后浏览器 DOM 验收。
- 或继续推进生产级最终交付报告与全量验收清单。

## 2026-07-03 Phase 1.133：用户管理补齐禁用 / 启用治理能力

完成事项：

- 用户管理页从只读列表 + 创建，补齐真实状态治理操作。
- 新增后端 API：`POST /memory/tenant/users/update-status`。
- 新增 Tenant service / repository 能力：
  - `UpdateUserStatus(user_id, status)`
  - 支持 `active` / `disabled`。
  - 不支持从 UI/API 执行 `deleted`，避免误导为物理删除或破坏审计归属。
- PostgreSQL repository 更新 `users.status` 和 `updated_at`，返回用户 metadata。
- Nuxt 用户页新增：
  - 活跃用户：`禁用用户`
  - 已禁用用户：`启用用户`
  - 状态更新 loading / error / notice。
- OpenAPI runtime spec 已包含 `/memory/tenant/users/update-status`。

修改模块：

- `internal/tenant/repository.go`
- `internal/tenant/service.go`
- `internal/tenant/pg_repository.go`
- `internal/tenant/service_test.go`
- `internal/tenant/pg_repository_test.go`
- `internal/http/router.go`
- `internal/http/router_test.go`
- `internal/webdeploy/web_dockerfile_test.go`
- `web/pages/users/index.vue`
- `docs/production-delivery-log.md`

新增或变更 API：

- 新增 `POST /memory/tenant/users/update-status`
  - 认证：PAT Bearer。
  - 权限：`memory:write`。
  - 请求：`user_id`、`status`。
  - 支持状态：`active`、`disabled`。
  - 返回：用户 metadata，不返回 password、credential、token、hash。

新增 migration：

- 无。
- 原有 `users.status` 和 `users.updated_at` 字段已满足本切片。

测试命令和结果：

- 本地红灯测试：
  - `go test ./internal/tenant -run 'TestServiceUpdatesUserStatus|TestPGRepositoryUpdatesUserStatus' -count=1`
  - 初次失败符合预期：service / PG repository 缺少 `UpdateUserStatus`。
  - `go test ./internal/http -run TestTenantUserUpdateStatusRequiresWritePATAndReturnsMetadata -count=1`
  - 初次失败符合预期：`/memory/tenant/users/update-status` 返回 404。
  - `go test ./internal/webdeploy -run TestUsersPageUsesRealStatusGovernanceAPI -count=1`
  - 初次失败符合预期：用户页没有真实 status governance marker，且仍有示例邮箱 marker。
- 本地绿灯测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/tenant -run 'TestServiceUpdatesUserStatus|TestPGRepositoryUpdatesUserStatus' -count=1`：通过。
  - `GOCACHE="$PWD/.gocache" go test ./internal/http -run TestTenantUserUpdateStatusRequiresWritePATAndReturnsMetadata -count=1`：通过。
  - `GOCACHE="$PWD/.gocache" go test ./internal/webdeploy -run TestUsersPageUsesRealStatusGovernanceAPI -count=1`：通过。
- 本地包级回归：
  - `GOCACHE="$PWD/.gocache" go test ./internal/tenant ./internal/http ./internal/webdeploy -count=1`：通过。
- 本地 Web build：
  - `cd web && npm run build`：通过。
- 服务器同步：
  - `rsync -avz internal/tenant/... internal/http/... internal/webdeploy/... web/pages/users/index.vue thinkpad:/opt/memory-os/ --relative`：完成。
- 服务器包级回归：
  - `PATH=/usr/local/go/bin:$PATH GOCACHE="$PWD/.gocache" go test ./internal/tenant ./internal/http ./internal/webdeploy -count=1`：通过。
- 服务器 Web build：
  - `make build-web`：通过。

部署命令和结果：

```bash
cd /opt/memory-os
. scripts/load-prod-env.sh >/dev/null
NUXT_PUBLIC_API_BASE=${NUXT_PUBLIC_API_BASE:-http://ddns.08121.top:18081} \
DOCKER_BUILDKIT=0 \
docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-api memory-web
```

- 结果：成功。
- 真实 build context：`477.1kB`。
- `memory-api` 与 `memory-web` 按预期重建并重启。
- `memory-worker` 与 `memory-mcp` 未重建、未重启。

部署后证据：

- `deploy-memory-api-1`：
  - image：`sha256:e50c9d16aeca1605a0c581a2c02787bc3b72387a4196b89d4e942d9a83aca333`
  - StartedAt：`2026-07-03T08:16:57.263895679Z`
  - status：`running`
- `deploy-memory-web-1`：
  - image：`sha256:56f856b56b326ca85289b88f9cddbb1876fc9e9bfc56a38496cc85479afa57f9`
  - StartedAt：`2026-07-03T08:16:56.762500731Z`
  - status：`running`
- `deploy-memory-worker-1`：仍为 Phase 1.132 前实例，未重启。
- `deploy-memory-mcp-1`：仍为 Phase 1.132 前实例，未重启。
- `curl http://127.0.0.1:18081/healthz`：`db/qdrant/redis` 均为 `ok`。
- `curl http://127.0.0.1:18081/openapi.json | grep /memory/tenant/users/update-status`：命中。
- `curl http://127.0.0.1:18080/`：页面包含生产 API base marker。

最终验证：

- `PATH=/usr/local/go/bin:$PATH make smoke`：通过，输出 `smoke ok`。
- `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过。
- post-deploy 日志：
  - `/tmp/memory-os-post-deploy.zmbWXi`

安全检查：

- 本轮未读取、写入或打印真实 secret。
- 用户状态 API 仅返回 metadata。
- 测试明确禁止响应包含 `password`、`credential`、`token`、`hash`。
- 本轮没有数据库 migration、数据删除、端口变更或备份恢复操作。

权限隔离结果：

- 未登录访问 `/memory/tenant/users/update-status` 返回 401。
- 只有 `memory:read` 的 PAT 访问返回 403。
- `memory:write` PAT 可更新状态。

剩余问题：

- 登录后浏览器 DOM 验收仍未补齐。
- 用户状态治理尚未做真实浏览器点击验收，当前证据为 API 单测、页面构建 marker、部署后 OpenAPI 与 smoke/post-deploy。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 继续补管理台关键页面的浏览器自动验收。
- 或继续扫描剩余页面的不可操作项。
- 或开始整理最终交付报告框架。

## 2026-07-03 Phase 1.134：用户状态治理验收残留核实

完成事项：

- 核实 Phase 1.133 后中断的临时浏览器验收残留。
- 确认一次性浏览器验收 PAT 的本地 token 文件已删除。
- 确认一次性浏览器验收 PAT 已在生产 PostgreSQL 中撤销。
- 确认生产 API 用户状态生命周期测试目标用户最终回到 `active`。
- 使用浏览器工具验证未登录访问 `/users` 会跳转 `/login`。
- 明确记录登录后浏览器 DOM 点击验收尚未通过，避免把 API 证据冒充完整 UI 验收。

验证命令和结果：

- 本地临时 token 文件检查：

```bash
test ! -f /tmp/memory-os-browser-pat-1783066908.txt && echo token_file_removed=true || echo token_file_removed=false
```

结果：`token_file_removed=true`。

- 生产 PostgreSQL PAT 撤销状态检查：

```bash
docker exec -i deploy-postgres-1 psql -U memory_os -d memory_os -Atc "
SELECT count(*)
FROM personal_access_tokens pat
JOIN users u ON u.id = pat.user_id
WHERE u.email = 'browser-acceptance-1783066908@memory.local'
  AND pat.name = 'browser-acceptance'
  AND pat.revoked_at IS NOT NULL;
"
```

结果：`1`。

- 生产 PostgreSQL 用户状态生命周期结果检查：

```bash
docker exec -i deploy-postgres-1 psql -U memory_os -d memory_os -Atc "
SELECT email || ' ' || status
FROM users
WHERE email LIKE 'browser-status-target-%@memory.local'
ORDER BY created_at DESC
LIMIT 5;
"
```

结果：`browser-status-target-1783067033@memory.local active`。

浏览器验收：

- 外部 Playwright 打开 `http://ddns.08121.top:18080/users`。
- 未登录状态按预期跳转到 `http://ddns.08121.top:18080/login`。
- 截图证据：`memory-os-users-redirect-login-phase-134.png`。

未通过 / 未完成项：

- in-app browser 登录后验收未完成。
- 失败现象：
  - `domSnapshot()` 报错：`TypeError: o.incrementalAriaSnapshot is not a function`。
  - 后续 locator click 等待 `Runtime.evaluate` 超时。
  - 截图接口返回 `Unable to capture screenshot`。
- 结论：当前只能证明未登录路由保护和生产 API 生命周期；不能声明用户页登录后真实点击验收已完成。

安全检查：

- 本轮未在日志和文档中记录 PAT 明文。
- 一次性 PAT 已撤销。
- 一次性 PAT 本地 token 文件已删除。
- 数据库检查只查询撤销状态和测试用户状态，不输出 token hash、credential 或 Secret。

剩余问题：

- 仍需补可靠的登录后浏览器自动验收工具链，或增加专用验收脚本在真实浏览器运行并留存截图 / trace。
- 用户状态治理的 UI 点击路径仍只完成代码级和构建级验证，未完成真实 DOM 点击验证。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 继续补管理台关键页面的浏览器自动验收。
- 或继续扫描剩余页面的不可操作项，优先把页面从“只读 / 静态”推进到真实 CRUD。
- 或补一个不打印 secret 的生产验收脚本，用于稳定复现登录后关键路径。

## 2026-07-03 Phase 1.135：权限页角色目录 API 生产化

完成事项：

- 新增后端角色目录模型 `tenant.RoleDefinition`。
- 新增 Tenant service 能力：`ListRoles(project_id)`。
- 新增生产 API：`POST /memory/tenant/roles/list`。
- 角色目录 API 使用 PAT 认证和项目权限上下文，未登录返回 401，非项目成员返回 403。
- 角色目录返回 `owner`、`admin`、`member` 的 metadata 和后端生成的 `permission_labels`。
- 权限管理页改为调用 `/memory/tenant/roles/list` 加载角色定义。
- 权限管理页的角色下拉和权限标签展示不再由前端硬编码函数推导。
- OpenAPI runtime spec 已包含 `/memory/tenant/roles/list`。

修改模块：

- `internal/tenant/model.go`
- `internal/tenant/service.go`
- `internal/tenant/service_test.go`
- `internal/http/router.go`
- `internal/http/router_test.go`
- `internal/webdeploy/web_dockerfile_test.go`
- `web/pages/permissions/index.vue`
- `docs/production-delivery-log.md`

新增或变更 API：

- 新增 `POST /memory/tenant/roles/list`
  - 认证：PAT Bearer。
  - 权限：`memory:read` + 项目成员权限上下文。
  - 请求：`org_id`、`project_id`。
  - 返回：`roles[]`，包含 `role`、`display_name`、`description`、`permission_labels`。
  - 不返回 credential、token、hash 或 Secret。

新增 migration：

- 无。
- 本切片复用现有固定角色语义，不新增自定义角色表或权限 DSL。

测试命令和结果：

- 本地红灯测试：
  - `go test ./internal/tenant -run TestServiceListsRoleDefinitions -count=1`
  - 初次失败符合预期：`Service.ListRoles` 缺失。
- 本地目标测试：
  - `GOCACHE="$PWD/.gocache" go test ./internal/tenant -run TestServiceListsRoleDefinitions -count=1`：通过。
  - `GOCACHE="$PWD/.gocache" go test ./internal/http -run 'TestTenantRolesListRequiresReadPermissionAndReturnsLabels|TestOpenAPIJSON|TestOpenAPICoversRegisteredProductionRoutes' -count=1`：通过。
  - `GOCACHE="$PWD/.gocache" go test ./internal/webdeploy -run TestPermissionsPageUsesRealMembershipGovernanceAPI -count=1`：通过。
- 本地包级回归：
  - `GOCACHE="$PWD/.gocache" go test ./internal/tenant ./internal/http ./internal/webdeploy -count=1`：通过。
- 本地 Web build：
  - `cd web && npm run build`：通过。
- 服务器同步：
  - `rsync -avz internal/tenant/model.go internal/tenant/service.go internal/tenant/service_test.go internal/http/router.go internal/http/router_test.go internal/webdeploy/web_dockerfile_test.go web/pages/permissions/index.vue thinkpad:/opt/memory-os/ --relative`：完成。
- 服务器包级回归：
  - `PATH=/usr/local/go/bin:$PATH GOCACHE="$PWD/.gocache" go test ./internal/tenant ./internal/http ./internal/webdeploy -count=1`：通过。
- 服务器 Web build：
  - `make build-web`：通过。
- 服务器全量测试：
  - `PATH=/usr/local/go/bin:$PATH make test`：通过。
- 服务器 secret scan：
  - `make secret-scan`：通过，输出 `secret scan ok`。

部署命令和结果：

```bash
cd /opt/memory-os
. scripts/load-prod-env.sh >/dev/null
NUXT_PUBLIC_API_BASE=${NUXT_PUBLIC_API_BASE:-http://ddns.08121.top:18081} \
DOCKER_BUILDKIT=0 \
docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-api memory-web
```

- 结果：成功。
- 真实 build context：`479.7kB`。
- `memory-api` 与 `memory-web` 按预期重建并重启。
- `memory-worker` 与 `memory-mcp` 未重建、未重启。

部署后证据：

- `deploy-memory-api-1`：
  - image：`sha256:17a10d2cccc1d624b3372b98c2ecbb0fd2600fb5d76b340e9b33f66fcbd2019e`
  - StartedAt：`2026-07-03T08:34:09.020899509Z`
  - status：`running`
- `deploy-memory-web-1`：
  - image：`sha256:25bc4475a846f0721c84bf0d4a39ebaf49303c1f2b0211f03425db5b82772f4e`
  - StartedAt：`2026-07-03T08:34:08.517993267Z`
  - status：`running`
- `deploy-memory-worker-1`：仍为 `2026-07-03T06:44:20.348990624Z` 启动的实例，未重启。
- `deploy-memory-mcp-1`：仍为 `2026-07-03T06:44:19.845302616Z` 启动的实例，未重启。
- `curl http://127.0.0.1:18081/healthz`：`db/qdrant/redis` 均为 `ok`。
- `curl http://127.0.0.1:18081/openapi.json | grep /memory/tenant/roles/list`：命中。
- `curl` 未带 PAT 访问 `/memory/tenant/roles/list`：HTTP 401，响应 `{"error":"pat_required"}`。

最终验证：

- `PATH=/usr/local/go/bin:$PATH make smoke`：通过，输出 `smoke ok`。
- `PATH=/usr/local/go/bin:$PATH make post-deploy-verify`：通过。
- post-deploy 日志：
  - `/tmp/memory-os-post-deploy.MOsyaV`

安全检查：

- 本轮未在代码、日志或文档中写入真实 secret。
- 新角色 API 只返回权限标签 metadata。
- 本轮曾尝试创建短生命周期角色 API 验收 PAT；第一次 Go 临时脚本因 `internal` 包导入限制失败，未写入数据。
- 第二次临时脚本因 shell 引号问题不可作为验收证据；随后检查 `roles-acceptance` 相关未撤销 PAT 数量为 0。
- 未带 PAT 的线上 API 验证返回 401，安全边界有效。

剩余问题：

- 本切片交付的是固定角色目录 API，不是自定义角色 CRUD。
- 生产角色 API 的 200 路径已有 handler 单测和 OpenAPI 部署证据，但登录后浏览器点击验收仍未完成。
- v0.4 生产级完全体仍未完成，不能声明 P0/P1/P2 为零。

下一步入口条件：

- 继续补可靠浏览器验收链路。
- 或推进自定义角色 / 权限标签管理的完整 CRUD。
- 或继续扫描管理台其它页面是否仍有前端硬编码治理逻辑。

## 2026-07-03 Phase 1.136：管理台角色下拉改为后端角色目录源

- 切片范围：`web/pages/permissions/index.vue`、`web/pages/projects/index.vue`。
- 风险点：页面成员角色选择仍使用静态枚举 `member/admin/owner`，会与后端角色定义偏离。
- 处置：
  - 在两个页面统一通过 `POST /memory/tenant/roles/list` 拉取角色目录。
  - 去除 `permissions` / `projects` 页面内硬编码角色列表。
  - 当角色目录未加载时，对应新增成员与角色更新动作按钮置灰，给出“先加载角色目录”提示。
- 结果：
  - 前端页面不再直接假设 `member/admin/owner`。
  - 角色标签展示继续从 `permission_labels` 派生。
 - 说明更新：
   - 继续细化“无角色目录”场景下的回退策略，所有角色回退从硬编码 `member` 改为“角色目录第一项”。

## 2026-07-03 Phase 1.137：角色回退策略改为目录第一项

- 切片范围：`web/pages/permissions/index.vue`、`web/pages/projects/index.vue`。
- 风险点：列表尚有基于 `'member'` 的兜底，角色目录缺失时可能产生脆弱默认。
- 处置：
  - 将成员角色映射默认值统一改为 `roleDefinitions[0].role`。
  - 角色目录为空时回退为 `''` 并保持新增/编辑动作禁用态。
- 结果：
  - 页面不再携带魔法字符串 `member`。
  - 与后端角色目录解耦一致性进一步提高。

说明：

- 本次变更为 UI 真实治理链路补齐切片，未扩展自定义角色 CRUD。
- 本地未执行 `npm run build` 验证（待下一次部署前一并执行）。

## 2026-07-03 Phase 1.138：Smoke 健康检查地址兼容修复

切片范围：

- `cmd/memory-smoke/main.go`
- `cmd/memory-smoke/main_test.go`

风险点：

- 自动化验收中常见误配置是把 `SMOKE_API_URL` 写成服务根地址（不含 `/healthz`）。
- 根因：此前 smoke 逻辑直接 GET `SMOKE_API_URL`，根地址会命中 API 根路由 404，阻断验收。

处置：

- 新增 `resolveSmokeHealthURL`：当 `SMOKE_API_URL` 为空时使用 `http://localhost:18081/healthz`；当地址仅为根路径时自动补齐 `/healthz`。
- 补充单测 `TestResolveSmokeHealthURL*`，覆盖空值、根地址、显式 `/healthz`、保留自定义路径。
- 同步 `thinkpad:/opt/memory-os` 后重跑 smoke 与 post-deploy 验收。

测试命令和结果：

- 本地：
  - `go test ./cmd/memory-smoke -run 'TestResolveSmokeHealthURL|TestSmokeTimeout' -count=1`：通过。
  - `go test ./cmd/memory-smoke -count=1`：通过。
- 服务器：
  - `SMOKE_API_URL=http://127.0.0.1:18081 ... make smoke`：通过（`smoke ok`）。
  - `make post-deploy-verify`：通过，日志 `/tmp/memory-os-post-deploy.6zyJrb`。

剩余风险：

- 未覆盖 `SMOKE_API_URL` 中 `http://` 外其他协议场景；当前默认仅支持标准 HTTP(S) 运行环境。

## 2026-07-03 Phase 1.139：管理台角色目录页面接入与本地缓存清理

切片范围：

- `web/components/AppShell.vue`
- `web/pages/roles/index.vue`
- `web/pages/permissions/index.vue`
- `.gitignore`
- `docs/production-delivery-log.md`

风险点：

- `web/pages/roles` 初次新增时为新增功能，缺失浏览器验收说明可能导致“页面有但未验证”。
- 本地工作区存在 `.gocache/.codebase-memory/.playwright-mcp` 与截图文件，容易干扰后续重建状态可读性。

处置：

- 在 `AppShell` 左侧导航加入“角色目录”入口，指向 `/roles`。
- 提供 `web/pages/roles/index.vue` 页面：支持角色列表、编辑回填、创建/更新，调用真实 `POST /memory/tenant/roles/list` 与 `POST /memory/tenant/roles/upsert`，不再展示静态假数据。
- 将 `permissions` 页面角色来源切换为后端角色目录。
- 将本地临时缓存目录写入 `.gitignore`，并清理当前工作区中的 `.gocache`、`.codebase-memory`、`.playwright-mcp`、历史截图两张。
- 继续保留本地构建产物，记录到交付日志确保可追溯下一步入口。

测试命令和结果：

- `go test ./...`：PASS。
- `cd web && npm run build`：PASS。
- 本次清理后 `git status` 显示上述变更仍保留，临时缓存目录与截图已移除。

剩余风险：

- `roles` 与 `permissions` 页面还未完成真实登录态浏览器验收（创建/刷新/编辑后的持久化验证）。
- 生产端口验收与跨用户隔离安全链路仍按整体 Phase 1.140+ 继续推进。

## 2026-07-03 Phase 1.140：角色目录页真实接口验收标记补齐

切片范围：

- `internal/webdeploy/web_dockerfile_test.go`

风险点：

- 页面验收记录覆盖了角色目录链路，但缺乏自动化文本级校验，后续改动可能无感回归。

处置：

- 新增 `TestRolesPageUsesRealRoleManagementAPI`：要求 `web/pages/roles/index.vue` 使用 `useAuthStore()`/`useContextStore()`，并且只调用真实 API `POST /memory/tenant/roles/list` 与 `POST /memory/tenant/roles/upsert`。
- 新增 `TestRolesPageUsesLoadedRoleDirectoryAndLocalPersistence`：要求页面保存角色目录状态并保持新增/更新链路完整。
- 继续约束静态角色片段被回写后误判。

测试命令和结果：

- `go test ./internal/webdeploy -count=1`：通过。
- `go test ./...`：通过。
- `cd web && npm run build`：通过。

说明：

- 本切片为治理面板验收链路补强，属于管理台持续性回归约束，不替代手工浏览器登录态验收。
- 下一步入口：继续补齐角色/权限/用户页面登录态写操作后的浏览器持久化验收。

## 2026-07-03 Phase 1.141：治理页持久化回写验收标记补齐

切片范围：

- `internal/webdeploy/web_dockerfile_test.go`

风险点：

- 三个关键治理页已有“真实接口”覆盖，但未要求“写操作后刷新/持久化回写”强约束。
- 未补齐该标记会导致回归时只检验 UI 文案与接口字符串，未覆盖创建/更新后列表刷新行为。

处置：

- 新增 `TestRolesPageReloadsAfterSave`：要求角色目录页在保存成功后触发 `loadRoles()` 回写持久化状态。
- 新增 `TestPermissionsPageReloadsAfterMembershipMutation`：要求权限管理页在成员关系新增/更新/移除成功后重新加载成员列表，并重置角色选择。
- 新增 `TestUsersPageReloadsAfterMutation`：要求用户页在创建与状态变更后都重新加载列表，并回写 `active` 过滤器。

测试命令和结果：

- `go test ./internal/webdeploy -count=1`：PASS。
- `go test ./...`：PASS。
- `cd web && npm run build`：PASS。

说明：

- 该切片只补强文本级验收约束，不替代最终真实浏览器登录态验收（下一步继续执行）。

## Latest Phase Pointer

- 最新完成切片：`2026-07-03 Phase 1.142：线上 smoke 与运维门禁抽样复验`。
- 详细证据位置：本文搜索 `Phase 1.142`。
- 下一步入口：执行角色/权限/用户页登录态浏览器验收（写操作->刷新->持久化回写）与跨租户隔离 UI 约束。
- 当前运维状态：生产服务 post-deploy 通过；用户管理已支持真实禁用 / 启用；Web-only 部署已证明不会重启 API；Docker build context 已降至 MB/KB 级；Importer PG-backed ProductionSink 已在隔离 PostgreSQL + Qdrant 中真实 apply 验收；生产备份已生成；隔离真实恢复演练通过且无容器/volume 残留；MCP `/tools` 与 `/tools/call` 生产环境均要求 PAT；Adapter fixture E2E 已纳入 post-deploy；API/Web/Worker/MCP 容器运行中。

## 2026-07-03 Phase 1.142：线上 smoke 与运维门禁抽样复验

切片范围：

- 生产环境线上运行时检查：`ddns.08121.top:18081` 与 `ddns.08121.top:18080`

风险点：

- 本地未起服务时，不能只依赖本地静态测试；需确认线上端点与核心门禁可用。

处置：

- 使用远端 API URL 执行 `go run ./cmd/memory-smoke`，并注入远端 endpoints：
  - `SMOKE_API_URL=http://ddns.08121.top:18081`
  - `SMOKE_WEB_URL=http://ddns.08121.top:18080`
  - `SMOKE_QDRANT_URL=http://ddns.08121.top:18083`
  - `SMOKE_TIMEOUT=60s`
  - `SMOKE_ENABLE_DEV_ENDPOINTS=false`
- 抽检：
  - `GET /healthz` 返回 `{"status":"ok"...}`
  - `GET /openapi.json` 可访问并包含 `/memory/tenant/*` 路径
  - 无 token 访问 `/memory/tenant/users/list` 返回 `401 {"error":"pat_required"}`

测试命令和结果：

- `curl -sS http://ddns.08121.top:18081/healthz`：返回健康检查 `ok`。
- `curl -sS http://ddns.08121.top:18081/openapi.json`：包含 ` /memory/tenant/*` 全量租户路由。
- `curl -sS -X POST http://ddns.08121.top:18081/memory/tenant/users/list`：返回 `HTTP 401 pat_required`。
- `SMOKE_API_URL=http://ddns.08121.top:18081 SMOKE_WEB_URL=http://ddns.08121.top:18080 SMOKE_QDRANT_URL=http://ddns.08121.top:18083 SMOKE_TIMEOUT=60s SMOKE_ENABLE_DEV_ENDPOINTS=false go run ./cmd/memory-smoke`：通过（`smoke ok`）。

说明：

- 该切片为线上运行时抽样复验，尚未覆盖带 PAT 的跨租户 UI 写入持久化完整浏览器验收。

## 2026-07-03 Phase 1.143：Tenant Governance Smoke 持久化闭环

切片范围：

- `cmd/memory-smoke/main.go`
- `cmd/memory-smoke/main_test.go`
- `Makefile`

风险点：

- 现有 `make smoke` 只覆盖 Web 首页可达、检索、TurnEvent、导入器等链路，未真实验证用户/角色/成员治理 API 的写入后 reload 持久化行为。
- 前端治理页虽然已接真实 API，但如果没有一条自动化 smoke 闭环，后续回归仍可能出现“接口存在但持久化写后读不一致”而未被及时发现。
- 服务器默认 `make smoke` 运行在宿主机；新增 PG actor provision 需要显式使用容器网络内可达的 PostgreSQL DSN。

处置：

- 新增 `smokePostgresDSN()`，优先读取 `SMOKE_POSTGRES_DSN`，为空时回退 `POSTGRES_DSN`。
- 扩展 `pipelineE2EActor`，新增 `WriteToken`，并在 `provisionPipelineE2EActorFromPostgres` 中同时 provision：
  - Adapter Token：`turn_event:write`
  - PAT：`memory:write`
  - PAT：`memory:read`
- 新增 `tenantGovernanceSmoke()`，默认在存在 PG DSN 或显式设置 `SMOKE_ENABLE_TENANT_GOVERNANCE=true` 时启用，执行以下真实 API 闭环：
  - 创建用户 -> `users/list(active)` 验证
  - 禁用用户 -> `users/list(disabled)` 验证
  - upsert 自定义角色 -> `roles/list` 验证
  - 添加成员 -> `memberships/list` 验证
  - 更新成员角色 -> `memberships/list` 验证
  - 移除成员 -> `memberships/list` 验证
- 将该闭环纳入 `run()` 主 smoke 流程，放在 importer smoke 之后、web smoke 之前。
- `Makefile` docker smoke 分支新增 `SMOKE_ENABLE_TENANT_GOVERNANCE` 透传，便于容器化服务器验证。

测试命令和结果：

- 本地 `gofmt -w cmd/memory-smoke/main.go cmd/memory-smoke/main_test.go`：完成。
- 本地 `go test ./cmd/memory-smoke -run 'TestSmokePostgresDSNFallsBackToPostgresDSN|TestTenantGovernanceSmokeUsesProvisionedTokensAndReloadsPersistedState' -count=1`：PASS。
- 本地 `go test ./cmd/memory-smoke -count=1`：PASS。
- 本地 `go test ./...`：PASS。
- 远端 `ssh thinkpad "cd /opt/memory-os && docker run --rm --network deploy_default ... go test ./cmd/memory-smoke -count=1"`：PASS。
- 远端 `ssh thinkpad "cd /opt/memory-os && . scripts/load-prod-env.sh && docker run --rm --network deploy_default ... -e SMOKE_ENABLE_TENANT_GOVERNANCE=true -e SMOKE_POSTGRES_DSN=postgres://memory_os:${POSTGRES_PASSWORD}@postgres:5432/memory_os?sslmode=disable ... go run ./cmd/memory-smoke"`：PASS，输出 `smoke ok`。

处理过的报错：

- 第一次远端治理 smoke 失败：`Get "http://memory-web": dial tcp ...:80: connect: connection refused`。
- 根因：`memory-web` 在 `deploy_default` 网络内监听 `18080`，不是假设的 80。
- 处理：改为 `SMOKE_WEB_URL=http://memory-web:18080` 后重跑，通过。

说明：

- 该切片补上了治理 API 的自动化持久化 smoke 闭环，但仍未替代最终要求的“登录态浏览器真实操作验收”。
- 下一步入口：执行用户/角色/权限页的浏览器登录态验收，记录真实写操作、刷新后的持久化结果，以及跨租户/越权场景提示。

## 2026-07-03 Phase 1.144：登录态浏览器验收推进与线上 Web 同步修正

切片范围：

- 线上 `memory-web` 部署
- 登录态浏览器验收：`/users`、`/roles`、`/permissions`

风险点：

- 线上 `memory-web` 初始版本落后于当前仓库，`/roles` 返回 `404`，`AppShell` 也缺少“角色目录”导航，无法继续按计划执行治理页浏览器验收。
- 浏览器 runtime 在部分长链路操作中会超时或重置，不能把超时后的页面状态当作成功证据。

处置：

- 核对本地与线上差异，确认本地存在：
  - `web/pages/roles/index.vue`
  - `web/components/AppShell.vue` 中 `['角色目录', '/roles']`
- 首次同步发现 `rsync` 目标路径错误，只把文件 basename 写入仓库根目录，未真实覆盖 `web/...` 路径。
- 纠正同步方式后，将以下文件按真实目录同步到 thinkpad：
  - `web/components/AppShell.vue`
  - `web/pages/users/index.vue`
  - `web/pages/permissions/index.vue`
  - `web/pages/roles/index.vue`
- 在 thinkpad 上重新构建并重启 `memory-web`。
- 构建日志确认 `/roles` 已进入 prerender：`Prerendering 17 initial routes` 包含 `/roles`。

浏览器验收结果：

- 登录：
  - 使用临时 browser acceptance PAT 成功登录 `http://ddns.08121.top:18080`
  - 组织 / 项目上下文成功从真实后端加载
- 用户页：
  - 真实创建用户：`browser-user-1783075743514@example.invalid`
  - 真实禁用后，`active` 列表消失
  - 刷新页面后切到 `disabled` 过滤，目标用户仍存在，状态为 `已禁用`
  - 结论：用户页 create / update-status / reload 持久化闭环成立
- 角色页：
  - 新建角色：`browser_role_1783076279131`
  - 刷新后角色仍存在，权限标签为 `project:dc1316c1-50b4-4a45-8487-28a02b04fdd3:read`
  - 结论：角色页 upsert / reload 持久化闭环成立
- 权限页：
  - 新增活跃用户：`browser-membership-1783076400@example.invalid`（仅用于本轮授权验收）
  - 通过页面成功添加项目授权
  - 页面上看到角色切换后的 admin 权限标签预览
  - 但后端复查 `POST /memory/tenant/memberships/list` 显示：
    - 该成员关系仍为 `status=active`
    - `role=browser_role_1783076279131`
  - 说明本轮浏览器操作中，“更新角色”与“移除授权”尚未形成可证明的持久化成功证据

测试与运行证据：

- thinkpad 前端重建：
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-web`
  - 构建日志包含 `/roles`
- API 实查：
  - `POST /memory/tenant/memberships/list`
  - 返回 `user_id=e1805de0-904f-41dd-9452-7dc3c94e3dcd`
  - 当前状态：`role=browser_role_1783076279131`, `status=active`

当前结论：

- `users` 浏览器验收：通过
- `roles` 浏览器验收：通过
- `permissions` 浏览器验收：部分通过
- 仍不能宣称权限治理页完整浏览器验收通过，需要继续补强“更新角色 / 移除授权”的可证明成功链路

下一步入口：

- 继续聚焦 `permissions` 页：
  - 缩短浏览器操作链路，避免 runtime reset
  - 必要时增加更明确的成功态提示或更稳的可见按钮交互方式
  - 复查是否存在页面按钮点击未触发请求、触发后未刷新、或状态映射错误的问题

## 2026-07-03 Phase 1.145：项目成员治理权限链路修复

切片范围：

- `TenantMembershipUpdateRoleHandler`
- `TenantMembershipRemoveHandler`
- `tenant.Service.UpdateMembershipRole`
- `tenant.Service.RemoveMembership`

根因确认：

- `/permissions` 页失败不是前端误操作，而是后端权限边界错误。
- 项目成员治理的“更新角色 / 移除授权”此前只校验 PAT，随后在 service 层统一要求 `RequireOrgWrite`。
- 这会错误拒绝“仅具备 project owner/admin、没有 org owner/admin”的合法治理者。

实现修复：

- `internal/tenant/service.go`
  - 新增 `RequireProjectWrite(userID, orgID, projectID)`。
  - `UpdateMembershipRole` 与 `RemoveMembership` 改为：
    - `project_id != ""` 时使用项目级写权限。
    - `project_id == ""` 时继续保持 org 级写权限。
- `internal/http/router.go`
  - `TenantMembershipUpdateRoleHandler`
  - `TenantMembershipRemoveHandler`
  - 对项目成员关系先走 `authorizeProjectScope(..., "memory:write", "project:<project_id>:write", ...)`，不再把项目治理退化成 PAT-only。
- 测试修正：
  - 旧测试把“成员被提升为 admin 后仍不能删除成员”当成正确行为，这和项目写权限模型冲突。
  - 已改为使用真正无项目写权限的 outsider 场景验证 403。

新增与更新的测试：

- `internal/tenant/service_test.go`
  - `TestServiceProjectOwnerCanManageProjectMembershipWithoutOrgMembership`
- `internal/http/router_test.go`
  - `TestTenantMembershipRoleAndRemoveAllowProjectOwnerWithoutOrgMembership`
- 同步修正既有成员治理测试，使其断言与项目写权限模型一致。

验证结果：

- `gofmt -w internal/tenant/service.go internal/http/router.go internal/tenant/service_test.go internal/http/router_test.go`
- 定向测试通过：
  - `go test ./internal/tenant ./internal/http -run 'TestServiceProjectOwnerCanManageProjectMembershipWithoutOrgMembership|TestTenantMembershipRoleAndRemoveAllowProjectOwnerWithoutOrgMembership' -count=1`
- 全量后端测试通过：
  - `go test ./...`

当前结论：

- 项目 owner/admin 现在可以合法治理本项目成员关系。
- org 级成员治理仍保持 org 写权限门槛。
- 该修复直接对齐了 `/permissions` 页此前暴露出的真实后端缺陷。

下一步入口：

- 同步修复到 thinkpad，重建 `memory-api`。
- 回到浏览器重新验收 `/permissions` 页：
  - 添加授权
  - 更新角色
  - 移除授权
  - 刷新验证持久化结果

## 2026-07-03 Phase 1.146：`/permissions` 页浏览器复验通过

切片范围：

- thinkpad 服务器部署
- `/permissions` 页真实浏览器验收

服务器动作：

- 同步文件到 `thinkpad:/opt/memory-os`：
  - `internal/tenant/service.go`
  - `internal/http/router.go`
  - `internal/tenant/service_test.go`
  - `internal/http/router_test.go`
  - `docs/production-delivery-log.md`
- 修正一次误传 basename 到仓库根目录的问题后，按真实目录重新同步。
- 清理误传的根目录脏文件：
  - `service.go`
  - `service_test.go`
  - `router.go`
  - `router_test.go`
  - `production-delivery-log.md`
- 重建并重启：
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-api`
- 运行时健康检查：
  - `curl http://127.0.0.1:18081/healthz`
  - 返回 `{\"status\":\"ok\",\"components\":{\"db\":{\"status\":\"ok\"},\"qdrant\":{\"status\":\"ok\"},\"redis\":{\"status\":\"ok\"}}}`

远端验证：

- 定向测试：
  - `go test ./internal/tenant ./internal/http -run TestServiceProjectOwnerCanManageProjectMembershipWithoutOrgMembership -count=1`
  - `go test ./internal/http -run TestTenantMembershipRoleAndRemoveAllowProjectOwnerWithoutOrgMembership -count=1`
  - 通过

浏览器验收：

- 使用临时 owner 验收 PAT 登录管理台，自动带出真实 org / project 上下文：
  - org：`5c6dc6aa-0227-493f-ab33-4678de37215d`
  - project：`dc1316c1-50b4-4a45-8487-28a02b04fdd3`
- 权限页初始状态显示 2 条授权。
- 通过页面“添加真实授权”为用户
  - `Browser Status Target · browser-status-target-1783067033@memory.local`
  - `user_id = cce81b46-a61b-4d83-94d2-98b911b9e9cf`
  - 新增项目成员，角色为 `member`
- 刷新页面后显示 3 条授权，新成员状态为 `active`
- 通过页面把该成员角色从 `member` 改为 `admin`
- 后端复查 `memberships/list` 返回：
  - `role = admin`
  - `status = active`
- 通过页面执行“移除授权”
- 后端复查 `memberships/list` 返回：
  - `role = admin`
  - `status = disabled`
- 页面刷新后仍能看到该成员，状态显示为 `disabled`

当前结论：

- `/permissions` 页此前阻塞的真实后端权限 bug 已修复。
- 页面上的添加授权、更新角色、移除授权三条核心治理链路均已用真实 API 和真实 PostgreSQL 持久化复验通过。
- 权限页现在可以从“部分通过”提升为“通过”。

注意事项：

- 当前 UI 对已移除授权采用“保留显示 + 状态 disabled”的治理方式，而不是立即从列表消失。
- 这与后端软删除 / 保留审计轨迹的实现一致，不视为缺陷。

下一步入口：

- 继续推进剩余治理页和生产持久化基座的 Phase 1 缺口。
- 结合已通过的 `users`、`roles`、`permissions`，继续向 Secret / Archive / Hot Memory / Retrieval 的真实浏览器验收推进。

## 2026-07-03 Phase 1.147：Qdrant 状态页改为真实证明 query-time filter

切片范围：

- `Qdrant 状态` 后端真实性修复
- 状态页暴露缺失 payload 字段

问题背景：

- `internal/qdrant/status.go` 之前把 `query_time_filter_enforced` 直接硬编码为 `true`。
- 这会把“理论上应当启用 query-time filter”和“当前 collection payload schema 已满足生产过滤前提”混在一起，不能作为生产验收证据。

本次改动：

- `internal/qdrant/status.go`
  - 新增 `missing_required_payload_fields`
  - 基于 collection `payload_schema` 真实计算缺失字段
  - 只有 `doc_type`、`user_id`、`org_id`、`project_id`、`visibility`、`permission_labels`、`index_generation` 全部存在时，才返回 `query_time_filter_enforced=true`
- `internal/http/router.go`
  - Qdrant 状态响应新增 `missing_required_payload_fields`
- `web/pages/qdrant/index.vue`
  - 当 collection payload schema 不完整时，页面直接显示缺失字段列表，避免误判为“已完成”

测试与验证：

- 红灯验证：
  - `go test ./internal/qdrant -run 'TestStatusServiceSnapshotMarksQueryTimeFilter' -count=1`
  - 初次失败，证明 `MissingRequiredPayloadFields` 与真实判定逻辑此前不存在
- 绿灯验证：
  - `go test ./internal/qdrant -run 'TestStatusServiceSnapshotMarksQueryTimeFilter|TestPGStatusStore' -count=1`
  - 通过
  - `go test ./internal/http -run 'TestQdrantStatusUsesRealServiceAndReturnsIndexStats|TestQdrantStatusRequiresPATAndConfiguredService' -count=1`
  - 通过

当前结论：

- `Qdrant 状态` 现在不再拿硬编码布尔值冒充生产证明。
- 当 payload schema 不完整时，API 与页面都会明确暴露缺口，方便后续继续完成真正的索引链路验收。

下一步入口：

- 跑更宽的本地验证。
- 同步到 thinkpad 后，做一次 `/qdrant` 页面真实浏览器验收。

## 2026-07-03 Phase 1.148：thinkpad 部署并确认 Qdrant 状态页真实暴露生产缺口

切片范围：

- thinkpad 同步与部署
- `/qdrant` 页面真实浏览器验收

服务器动作：

- 同步文件到 `thinkpad:/opt/memory-os`：
  - `internal/qdrant/status.go`
  - `internal/qdrant/status_test.go`
  - `internal/http/router.go`
  - `internal/http/router_test.go`
  - `web/pages/qdrant/index.vue`
  - `docs/production-delivery-log.md`
- 远端定向测试：
  - `/usr/local/go/bin/go test ./internal/qdrant ./internal/http -run "TestStatusServiceSnapshotMarksQueryTimeFilter|TestQdrantStatusUsesRealServiceAndReturnsIndexStats|TestQdrantStatusRequiresPATAndConfiguredService" -count=1`
  - 通过
- 重建并重启：
  - `. scripts/load-prod-env.sh && APP_ENV=production ENABLE_DEV_ENDPOINTS=false docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-api memory-web`
- 容器状态：
  - `memory-api`、`memory-web` 均成功重建并启动
- 运行时健康检查：
  - `curl http://127.0.0.1:18081/healthz`
  - 返回 `{\"status\":\"ok\",\"components\":{\"db\":{\"status\":\"ok\"},\"qdrant\":{\"status\":\"ok\"},\"redis\":{\"status\":\"ok\"}}}`
  - `curl -o /dev/null -w "%{http_code}" http://127.0.0.1:18080/`
  - 返回 `200`

浏览器验收：

- 使用临时 PAT 登录管理台并进入 `/qdrant`
- 页面正确显示：
  - `Query-Time Filter = 未确认`
  - 缺失字段提示块
  - Qdrant / PostgreSQL 实时统计数据
- 真实页面结果显示当前生产 collection 未返回 payload schema，因此页面提示缺失字段为：
  - `doc_type`
  - `index_generation`
  - `org_id`
  - `permission_labels`
  - `project_id`
  - `user_id`
  - `visibility`

当前结论：

- 这次切片已经完成“把状态页改成真实证明”的目标。
- 服务器现状证明：`/qdrant` 页面不再掩盖问题，而是把真正的生产缺口暴露出来。
- 当前未完成项已经从“状态页伪完成”切换为“Qdrant payload schema / 索引元数据仍需进一步补齐”。

下一步入口：

- 继续追查为什么线上 collection 未返回 payload schema。
- 把 Qdrant payload schema / 索引元数据补齐到能支持最终生产验收。

## 2026-07-03 Phase 1.149：补齐 Qdrant payload index 初始化并让状态页转绿

切片范围：

- Qdrant payload index 初始化
- `api / mcp / worker / importer / smoke` 启动链路统一修复
- thinkpad 真实运行时验收

根因结论：

- 线上 `memory_os` collection 之前只执行了 `EnsureCollection`。
- Qdrant `payload_schema` 不会因为 `upsert points` 自动出现；必须显式创建 payload index。
- 因此状态页此前显示 `payload_schema = {}`、`Query-Time Filter = 未确认` 是真实现状，不是前端错误。

本次改动：

- `internal/qdrant/client.go`
  - 新增 `PayloadIndexConfig`
  - 新增 `DefaultPayloadIndexConfigs()`
  - 新增 `EnsurePayloadIndexes()`
  - 新增 `EnsureCollectionSchema()`
- 默认建立以下 payload index：
  - `doc_type`
  - `user_id`
  - `org_id`
  - `project_id`
  - `visibility`
  - `permission_labels`
  - `index_generation`
  - `agent_id`
  - `scope`
  - `status`
- 启动入口统一改为建 collection 后再建 payload index：
  - `cmd/memory-api/main.go`
  - `cmd/memory-mcp/main.go`
  - `cmd/memory-worker/main.go`
  - `cmd/memory-importer/main.go`
  - `cmd/memory-smoke/main.go`

测试与验证：

- 红灯验证：
  - `go test ./internal/qdrant -run 'TestEnsureCollectionSchemaCreatesCollectionAndPayloadIndexes' -count=1`
  - 初次失败，证明此前没有 schema 初始化能力
- 本地绿灯验证：
  - `go test ./internal/qdrant -run 'TestEnsureCollectionCreatesSingleMemoryOSCollection|TestEnsureCollectionSchemaCreatesCollectionAndPayloadIndexes|TestEnsureCollectionRejectsInvalidConfig|TestCollectionInfoReadsQdrantCollectionStatus' -count=1`
  - 通过
  - `go test ./cmd/memory-api ./cmd/memory-mcp ./cmd/memory-worker ./cmd/memory-importer ./cmd/memory-smoke -count=1`
  - 通过
  - `go test ./...`
  - 通过
- thinkpad 远端验证：
  - `/usr/local/go/bin/go test ./internal/qdrant ./cmd/memory-api ./cmd/memory-mcp ./cmd/memory-worker ./cmd/memory-importer ./cmd/memory-smoke -count=1`
  - 通过
  - `. scripts/load-prod-env.sh && APP_ENV=production ENABLE_DEV_ENDPOINTS=false docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-api memory-worker memory-mcp`
  - 成功
  - `curl http://127.0.0.1:18083/collections/memory_os`
  - 返回 `payload_schema` 已包含 10 个字段
  - `curl -X POST http://127.0.0.1:18081/memory/qdrant/status -H 'Authorization: Bearer <临时 PAT>'`
  - 返回：
    - `query_time_filter_enforced = true`
    - `missing_required_payload_fields = []`

浏览器验收：

- 重新打开 `http://ddns.08121.top:18080/qdrant`
- 页面正确显示：
  - `Query-Time Filter = 已强制`
  - 不再显示缺失字段提示
  - payload schema 标签区出现：
    - `agent_id`
    - `doc_type`
    - `index_generation`
    - `org_id`
    - `permission_labels`
    - `project_id`
    - `scope`
    - `status`
    - `user_id`
    - `visibility`

当前结论：

- 这次切片已经把“Qdrant 状态页卡在未确认”这个真实生产缺口修到闭环。
- 现在不仅状态页是真的，Qdrant runtime 元数据也已经真的补齐。
- `Qdrant 状态` 页面可从“暴露缺口”提升为“通过”。

下一步入口：

- 继续推进 `检索测试` 与统一检索链路的真实浏览器验收。
- 重点确认 `/memory/search`、Hot Memory、Archive RAG、access log、mark_used 是否都已在 thinkpad 生产运行时闭环。

## 2026-07-03 Phase 1.8：统一检索浏览器验收与 Hot Memory 返回值修复

完成事项：

- 在 thinkpad 生产环境为当前浏览器验收项目补齐真实数据：
  - 创建 1 条 Hot Memory。
  - 创建 1 份 Archive。
  - 触发 Archive reindex，写入 Archive RAG chunk。
- 完成统一检索链路真实验收：
  - `/memory/search` 同时返回 Hot Memory 与 Archive RAG。
  - `marked_used_count` 正常递增。
  - `access_log_count` 正常返回。
  - `/memory/retrieval/access-log/list` 可查询 request/result 审计记录。
- 修复 Hot Memory PostgreSQL repository 的返回值缺陷：
  - `PGRepository.Upsert` 由 `Exec + 原样返回` 改为 `RETURNING + scanMemory`。
  - `/memory/hot-memory/create` 不再返回零时间戳 `created_at/updated_at`。
- 修复 `web/pages/search-test.vue` 的结果展示：
  - 改为使用后端真实字段 `text`、`score`、`source` 渲染卡片。
  - 不再依赖不存在的 `title`、`content`、`id` 字段。

修改文件：

- `internal/hotmemory/pg_repository.go`
- `internal/hotmemory/pg_repository_test.go`
- `web/pages/search-test.vue`
- `internal/webdeploy/web_dockerfile_test.go`

验证命令：

- 本地：
  - `go test ./internal/hotmemory -count=1`：通过。
  - `go test ./internal/webdeploy -run 'TestSearchTestPage' -count=1`：通过。
  - `cd web && npm run build`：通过。
- 服务器：
  - `docker run --rm --network deploy_default -e POSTGRES_TEST_DSN="postgres://memory_os:***@postgres:5432/memory_os?sslmode=disable" -v /opt/memory-os:/src -w /src golang:1.25-bookworm go test ./internal/hotmemory -run "TestPGRepository" -count=1`：通过。
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-api`：成功。
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-web`：成功。
  - `curl http://ddns.08121.top:18081/healthz`：HTTP 200，`db/qdrant/redis` 均为 `ok`。

真实接口验收：

- `POST /memory/hot-memory/create`
  - 返回 `created_at`、`updated_at` 为真实 PostgreSQL 时间，不再是零时间。
- `POST /memory/archive/create`
  - 成功创建 `archive_manual_search_test_1`。
- `POST /memory/archive/reindex`
  - 成功返回 `chunks = 1`，索引代次提升到 2。
- `POST /memory/search`
  - 返回 2 条结果：
    - `kind = hot_memory`
    - `kind = archive_chunk`
- `POST /memory/retrieval/access-log/list`
  - 能看到 `manual-search-archive-1` 的 request 与 result 记录。

浏览器验收：

- 页面：`http://ddns.08121.top:18080/search-test`
- 使用真实 PAT、真实 org/project 运行检索后，页面已确认：
  - Hot Memory 卡片显示真实文本内容。
  - Archive RAG 卡片显示真实 Markdown chunk 内容。
  - 两类卡片都显示 score。
  - 页面不再出现“后端未返回文本”兜底文案。
  - `access_log_count`、`marked_used_count`、`Source refs` 均正常展示。

当前结论：

- 统一检索链路已从“接口可用”推进到“浏览器页真实可验收”。
- `检索测试` 页面可从“功能通了但展示不完整”提升为“可用于生产验收”。
- Hot Memory 创建返回值的时间戳缺陷已修复并上线。

下一步入口：

- 继续推进剩余管理页的生产级可操作性，优先检查 `归档详情`、`Hot Memory 管理`、`Secret Vault`、`Adapter Token` 在浏览器中的完整写操作闭环。

## 2026-07-03 Phase 1.9：Archive 索引状态一致性修复

完成事项：

- 修复 Archive RAG 索引成功后的状态回写缺口：
  - `internal/rag/qdrant_store.go` 在 `qdrant_points` 写为 `indexed` 后，同步把 `archive_chunks.vector_status` 更新为 `indexed`。
  - 当 `chunk_id` 不存在时，显式返回错误，避免静默制造状态不一致。
- 为修复补充 contract 测试：
  - `internal/rag/qdrant_store_test.go` 新增断言，验证 `QdrantStore.Upsert` 后 `qdrant_points` 与 `archive_chunks` 都为 `indexed`。
- 修复服务器真实数据库下暴露出的测试脆弱性：
  - `internal/qdrant/status_test.go` 改为使用唯一 `collection_name`，避免吃到共享测试库中的历史点数据。
  - 同时为 Hot Memory 测试数据使用唯一 `fact` / `fact_hash`，避免触发 `hot_memories_scope_fact_unique` 约束误报。
- 在 thinkpad 生产环境重建 `memory-api` 与 `memory-worker`，并对真实 Archive 执行一次重建索引验收。

修改文件：

- `internal/rag/qdrant_store.go`
- `internal/rag/qdrant_store_test.go`
- `internal/qdrant/status_test.go`

验证命令：

- 本地：
  - `go test ./internal/rag ./internal/qdrant -count=1`：通过。
- 服务器：
  - `docker run --rm --network deploy_default -e POSTGRES_TEST_DSN="postgres://memory_os:***@postgres:5432/memory_os?sslmode=disable" -v /opt/memory-os:/src -w /src golang:1.25-bookworm go test ./internal/rag ./internal/qdrant -count=1`：通过。
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-api memory-worker`：成功。
  - `curl http://127.0.0.1:18081/healthz`：HTTP 200，`db/qdrant/redis` 均为 `ok`。

真实接口验收：

- `POST /memory/archive/reindex`
  - 对 `archive_manual_search_test_1` 触发重建索引后，返回 `index_generation = 3`。
- `POST /memory/archive/index-status`
  - 当前代次 `3` 返回：
    - `chunks_by_status.indexed = 1`
    - `points_by_status.indexed = 1`
    - `archive_chunks[0].vector_status = indexed`
    - `archive_chunks[0].qdrant_vector_status = indexed`

浏览器验收：

- 页面：`http://ddns.08121.top:18080/archive/archive_manual_search_test_1`
- 页面已确认：
  - Header 显示 `索引代次 3`。
  - `RAG 索引状态` 区域显示：
    - `Archive Chunk indexed 1`
    - `Qdrant Point indexed 1`
  - `Chunk 明细` 中当前 chunk `archive_manual_search_test_1_g3_c0` 显示：
    - `indexed`
    - `Qdrant ... indexed`

处理过的报错：

- 服务器第一次执行 `go test ./internal/rag ./internal/qdrant` 失败：
  - 根因 1：共享测试库已有历史 Qdrant point，旧测试用固定 collection 断言 `== 1`，误报失败。
  - 修复：测试改为唯一 `collection_name`。
  - 根因 2：共享测试库已有同 scope 的 Hot Memory，旧测试用固定 `fact_hash` 触发唯一约束。
  - 修复：测试改为唯一 `fact` / `fact_hash`。

当前结论：

- Archive 详情页此前“Qdrant point 已 indexed，但 Archive chunk 仍 pending”的生产缺口已修复并上线。
- 这不是前端假修复，后端状态源 `archive_chunks` 与 `qdrant_points` 现已保持一致。
- `归档详情` 页面可继续作为后续生产验收页使用。

下一步入口：

- 继续推进 `Hot Memory 管理`、`Secret Vault`、`Adapter Token` 三个页面的真实写操作与刷新持久化验收。

## 2026-07-03 Phase 1.10：Hot Memory 页面生产验收补完

完成事项：

- 完成 Hot Memory 页面真实浏览器写操作闭环验收：
  - 创建
  - 编辑
  - 提升
  - 降权
  - 标记使用
  - 软删除
  - 刷新后持久化验证
- 修复 Hot Memory 页面真实 UX 缺口：
  - 原行为：在 `活跃` 筛选下点击“提升/降权”后，记录因状态变化被即时过滤掉，页面没有成功提示，视觉效果接近“记录消失”。
  - 新行为：
    - 提升后自动切换到 `已提升` 筛选。
    - 降权后自动切换到 `已降权` 筛选。
    - 创建、编辑、标记使用、软删除均显示明确成功提示。
    - 软删除后明确提示“当前筛选下将不再显示”。
- 为该修复补充页面级静态验收：
  - `internal/webdeploy/web_dockerfile_test.go` 增加对成功提示文案和 `actionStatusFilter` 的断言，防止页面回退到“无提示消失”的状态。

修改文件：

- `web/pages/hot-memory/index.vue`
- `internal/webdeploy/web_dockerfile_test.go`

验证命令：

- 本地：
  - `go test ./internal/webdeploy -run 'TestHotMemoryPageUsesRealAPI' -count=1`：通过。
  - `cd web && npm run build`：通过。
- 服务器：
  - `docker run --rm -v /opt/memory-os:/src -w /src golang:1.25-bookworm go test ./internal/webdeploy -run "TestHotMemoryPageUsesRealAPI" -count=1`：通过。
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml build --no-cache memory-web`：成功。
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d memory-web`：成功。

浏览器验收：

- 页面：`http://ddns.08121.top:18080/hot-memory`
- 使用真实 PAT、真实 org/project、真实 API 完成以下验收：
  - 创建 `hot memory browser acceptance 20260703-2030 phase19b`
    - 页面显示成功提示：`Hot Memory 已创建，并切换到活跃筛选。`
  - 提升
    - 页面自动切换到 `已提升` 筛选。
    - 页面显示成功提示：`Hot Memory 已提升，已切换到已提升筛选。`
  - 降权
    - 页面自动切换到 `已降权` 筛选。
    - 页面显示成功提示：`Hot Memory 已降权，已切换到已降权筛选。`
  - 标记使用
    - 页面显示成功提示：`Hot Memory 已标记为使用。`
    - 记录显示 `used 1 · access 1`
  - 软删除
    - 页面显示成功提示：`Hot Memory 已软删除，当前筛选下将不再显示。`
    - 刷新后该记录不再显示

当前结论：

- Hot Memory 页面已从“API 接上了，但状态切换会让记录无提示消失”提升为“关键操作可理解、可持续操作、可刷新验证”。
- `Hot Memory 管理` 可以继续作为生产验收页面使用。

下一步入口：

- 继续推进 `Secret Vault` 页面真实创建/禁用/刷新持久化验收，再检查是否存在明文暴露或前端元数据展示缺口。

## 2026-07-03 Phase 1.11：Secret Vault 页面生产验收补完

完成事项：

- 完成 Secret Vault 页面真实浏览器验收：
  - 创建 Secret
  - 禁用 Secret
  - 自动切换筛选查看 disabled metadata
  - 刷新后持久化验证
- 确认页面安全边界：
  - 创建成功后页面只显示 `secret_ref` 和 metadata。
  - 提交使用的测试明文未出现在页面正文中。
  - 明文字段提交后被立即清空。
- 修复 Secret Vault 页面真实 UX 缺口：
  - 原行为：在 `active` 筛选下点击“禁用”后，记录立即消失，只剩成功提示，无法直接看到禁用后的状态。
  - 新行为：禁用后自动切换到 `disabled` 筛选，并继续展示刚禁用的 metadata。
- 新增页面级静态验收测试，防止回退到“禁用后直接消失”或误引入明文示例。

修改文件：

- `web/pages/secrets/index.vue`
- `internal/webdeploy/web_dockerfile_test.go`

验证命令：

- 本地：
  - `go test ./internal/webdeploy -run 'TestSecretsPageUsesRealAPIAndMetadataOnlyFlow' -count=1`：通过。
  - `cd web && npm run build`：通过。
- 服务器：
  - `docker run --rm -v /opt/memory-os:/src -w /src golang:1.25-bookworm go test ./internal/webdeploy -run "TestSecretsPageUsesRealAPIAndMetadataOnlyFlow" -count=1`：通过。
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml build --no-cache memory-web`：成功。
  - `docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d memory-web`：成功。

浏览器验收：

- 页面：`http://ddns.08121.top:18080/secrets`
- 使用真实 PAT、真实 org/project、真实 API 完成以下验收：
  - 创建 `SECRET_BROWSER_ACCEPTANCE_20260703_2033`
    - 页面显示成功提示：`已创建 secret_ref_...，页面只保留 metadata。`
    - 页面正文不包含测试明文 `fake-secret-browser-acceptance-20260703-2033`
    - 密码输入框在提交后被清空
  - 创建并禁用 `SECRET_BROWSER_ACCEPTANCE_20260703_2035B`
    - 页面显示成功提示：`已禁用 secret_ref_...，并切换到 disabled 筛选。`
    - 页面自动切换到 `disabled`
    - 当前页继续显示该 Secret 的 `disabled` metadata
    - 点击刷新后，该 `disabled` metadata 仍然存在

当前结论：

- Secret Vault 页面已从“真实 API 已接通”提升为“创建/禁用/刷新持久化可真实验收”。
- 页面级安全边界符合当前要求：管理台仅展示 metadata，不回显明文。

下一步入口：

- 继续推进 `Adapter Token` 页面真实创建/撤销/刷新持久化验收，检查是否存在 token 明文误展示或状态切换后不可见的问题。

## 2026-07-03 Phase 1.12：Token 页面生产验收补充

完成事项：

- 完成 Token 页面真实浏览器验收：
  - 创建 Adapter Token
  - 隐藏一次性 Adapter Token 明文
  - 撤销 Adapter Token
  - 创建 PAT
  - 隐藏一次性 PAT 明文
  - 撤销 PAT
- 确认页面安全边界：
  - 创建成功时一次性展示完整明文 token。
  - 隐藏后页面只保留 metadata。
  - 列表页只展示 `token_prefix`、scope、过期时间、状态，不展示完整 token。
- 新增 Token 页面级静态验收测试，锁定真实 API 与“一次性明文 + metadata-only”行为，防止回退。

修改文件：

- `internal/webdeploy/web_dockerfile_test.go`

验证命令：

- 本地：
  - `go test ./internal/webdeploy -run 'TestTokensPageUsesRealAPIAndOneTimeTokenFlow' -count=1`：通过。
- 服务器：
  - `docker run --rm -v /opt/memory-os:/src -w /src golang:1.25-bookworm go test ./internal/webdeploy -run "TestTokensPageUsesRealAPIAndOneTimeTokenFlow" -count=1`：通过。

浏览器验收：

- 页面：`http://ddns.08121.top:18080/tokens`
- 使用真实 PAT、真实 org/project、真实 API 完成以下验收：
  - 创建 Adapter Token
    - 页面显示 `Adapter Token 一次性明文`
    - 列表仅显示 `prefix: adapter` 和 metadata
  - 隐藏一次性 Adapter Token 明文
    - 点击 `我已保存，立即隐藏` 后，完整 token 不再显示
  - 撤销 Adapter Token
    - 当前页记录状态变为 `revoked`
    - 刷新后仍保持 `revoked`
  - 创建 `PAT_BROWSER_ACCEPTANCE_20260703_2044`
    - 页面显示 `PAT 一次性明文`
    - 列表仅显示 `prefix: pat` 和 metadata
  - 隐藏一次性 PAT 明文
    - 点击 `我已保存，立即隐藏` 后，完整 token 不再显示
  - 撤销该 PAT
    - 当前页记录状态变为 `revoked`

当前结论：

- Token 页面未发现新的运行时代码缺口。
- 当前页面已满足“创建时一次性明文、列表仅展示 metadata、撤销后状态可见”的生产验收要求。
- Token 页面现在至少有页面级静态验收，能防止退回到假数据或暴露完整 token 的实现。

下一步入口：

- 继续从剩余管理页或最终集成链路里挑下一个高风险缺口，优先检查是否还有缺少页面级验收保护或跨页面的持久化 / 权限问题。

## 2026-07-03 Phase 1.13：最终集成门禁打通

完成事项：

- 在 thinkpad 生产环境执行运行时集成门禁：
  - `scripts/validate-openapi-runtime.py`
  - `make smoke`
  - `scripts/post-deploy-verify.sh`
- 在 thinkpad 生产环境执行更完整的交付门禁：
  - `scripts/verify.sh`
- 修复两类会阻塞交付门禁的测试路径问题：
  - `cmd/memory-smoke/main_test.go`
  - `internal/webdeploy/web_dockerfile_test.go`
  - 两处都改为动态 `findRepoRoot()`，不再依赖脆弱的 `../../Makefile`
- 清理服务器工作树中的遗留脏文件：
  - 删除 `/opt/memory-os/main_test.go`
  - 根因是先前错误同步留下的测试副本，导致 `go test ./...` 命中错误包 `memory-os`

修改文件：

- `cmd/memory-smoke/main_test.go`
- `internal/webdeploy/web_dockerfile_test.go`
- `docs/production-delivery-log.md`

验证命令：

- 本地：
  - `go test ./cmd/memory-smoke -run 'TestMakeSmokeDoesNotForceDevEndpoints|TestMakeSmokePassesStrictPipelineEnvironmentToDocker' -count=1`：通过。
  - `go test ./cmd/memory-smoke -count=1`：通过。
  - `go test ./internal/webdeploy -run 'TestMakefileDevUpProvidesLocalOnlyPostgresPassword|TestMakefileProductionDeployTargetSetsBuildInfo' -count=1`：通过。
  - `go test ./internal/webdeploy -count=1`：通过。
- 服务器：
  - `python3 scripts/validate-openapi-runtime.py http://127.0.0.1:18081/openapi.json`：通过，结果 `openapi ok: 50 paths`
  - `SMOKE_TIMEOUT=3m make smoke`：通过，结果 `smoke ok`
  - `bash scripts/post-deploy-verify.sh`：通过
  - `bash scripts/verify.sh`：通过

处理过的报错：

- 第一次 `verify.sh` 失败：
  - `cmd/memory-smoke/main_test.go` 里的 `../../Makefile` 路径硬编码在服务器测试工作目录下失效
  - 修复：改为 `findRepoRoot()`
- 第二次 `verify.sh` 仍失败：
  - `internal/webdeploy/web_dockerfile_test.go` 里还有同样的 `../../Makefile` 路径硬编码
  - 修复：改为 `findRepoRoot()`
- 第三次 `verify.sh` 仍失败：
  - 服务器工作树残留 `/opt/memory-os/main_test.go`，`go test ./...` 命中错误包 `memory-os`
  - 修复：删除该遗留测试副本；确认后续 `memory-os` 根包显示 `? memory-os [no test files]`

当前结论：

- thinkpad 生产环境当前已经拿到以下系统级证据：
  - OpenAPI 运行时校验通过
  - smoke 通过
  - post-deploy runtime verify 通过
  - 完整 verify 门禁通过
  - 备份 dry-run 通过
  - 恢复 dry-run 通过
  - restore rehearsal preflight 通过
  - backup cron dry-run 通过
- 当前问题已经从“单页功能缺口”推进到“系统级交付门禁可跑通”。

下一步入口：

- 进入更高层的完成度审计：
  - 对照总计划逐项核对还未证明完成的 Phase / 能力 / 安全边界
  - 优先找“功能已存在但证据不足”与“真正还没实现”的分界线

## 2026-07-03 Phase 1.14：运行态 Build Info 闭环补强

完成事项：

- 在 thinkpad 生产环境执行正式部署路径 `make prod-up`，重建并替换：
  - `memory-api`
  - `memory-worker`
  - `memory-mcp`
  - `memory-web`
- 确认运行态 `/version` 不再是全量默认值：
  - `build_time` 已注入当前构建时间
  - `dirty` 已注入为 `false`
- 确认 `memory-api`、`memory-worker`、`memory-mcp` 均为刚重建后的容器实例。
- 再次执行生产 smoke，确认正式部署后主链路仍可通过。

验证命令：

- 服务器：
  - `make prod-up`：通过。
  - `curl -fsS http://127.0.0.1:18081/version`：通过，返回 `{\"version\":\"0.4.0-dev\",\"commit\":\"unknown\",\"build_time\":\"2026-07-03T12:54:37Z\",\"dirty\":\"false\"}`。
  - `curl -fsS http://127.0.0.1:18081/healthz`：通过，`db/qdrant/redis` 均为 `ok`。
  - `. scripts/load-prod-env.sh && docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps`：通过，`memory-api`、`memory-worker`、`memory-mcp` 为刚重建后的 `Up` 状态。
  - `SMOKE_TIMEOUT=3m make smoke`：通过，结果 `smoke ok`。

处理过的报错：

- 直接执行 `docker-compose ... ps` 初次失败：
  - 报错：`LLM_API_KEY is required`
  - 根因：生产 compose 依赖 `scripts/load-prod-env.sh` 从当前运行容器回填环境变量，裸调用 compose 不会自动注入这些值。
  - 处理：改为 `. scripts/load-prod-env.sh && docker-compose ... ps`。
- `/version` 中 `commit` 仍为 `unknown`：
  - 根因：thinkpad 上的 `/opt/memory-os` 是同步后的工作目录，不是 Git 仓库，`git rev-parse --short HEAD` 返回 `fatal: not a git repository`，因此 `Makefile` 中 `BUILD_COMMIT` 回落为 `unknown`。
  - 结论：这不是“未重建”的问题，而是服务器部署目录缺少 `.git` 元数据。

当前结论：

- 运行态 Build Info 已经从“全量 unknown”提升为“至少可证明本次构建时间和 dirty 状态”。
- thinkpad 当前运行中的核心后端容器确实来自本次 `prod-up` 重建。
- `commit` 仍未闭环，后续若要把版本可验证性提升到提交级别，需要补一条不依赖服务器 `.git` 的构建元数据注入策略。

下一步入口：

- 回到总计划完成度审计，优先区分：
  - 仅剩证据整理的问题
  - 仍需代码或验收补齐的问题
- 若继续收口“部署版本可验证性”，下一步应补服务器非 Git 工作目录下的 `BUILD_COMMIT` 注入方案。

## 2026-07-03 Phase 1.15：非 Git 工作目录 Build Commit 回退闭环

完成事项：

- 新增 `scripts/load-build-info.sh`，为生产部署补充非敏感构建元数据加载链路。
- `load-build-info.sh` 支持以下优先级：
  - 显式环境变量
  - 同步到服务器的 `.build-info.env`
  - 当前目录 Git worktree
  - 安全默认值
- `prod-up` 与 `prod-up-mock` 在 `load-prod-env.sh` 之后、`docker-compose` 之前统一 source `load-build-info.sh`。
- 新增脚本测试，证明在非 Git 目录下只要存在 `.build-info.env`，就能注入 `BUILD_COMMIT`，且不会覆盖显式环境变量。
- 在本地生成 `.build-info.env` 并同步到 thinkpad。
- 在 thinkpad 上重新执行 `make prod-up`，确认运行态 `/version` 的 `commit` 从 `unknown` 收口为真实提交号。

修改文件：

- `.gitignore`
- `Makefile`
- `scripts/load-build-info.sh`
- `internal/buildinfo/load_build_info_script_test.go`
- `internal/webdeploy/web_dockerfile_test.go`
- `internal/verify/verify_script_test.go`
- `docs/production-delivery-log.md`

验证命令：

- 本地：
  - `go test ./internal/buildinfo -count=1`：通过。
  - `go test ./internal/webdeploy -run 'TestMakefileProductionDeployTargetSetsBuildInfo' -count=1`：通过。
  - `go test ./internal/verify -run 'TestMakefileExposesPostDeployVerifyTarget' -count=1`：通过。
  - `go test ./...`：通过。
- 服务器：
  - `make test`：通过。
  - `make prod-up`：通过。
  - `curl -fsS http://127.0.0.1:18081/version`：通过，返回 `{\"version\":\"0.4.0-dev\",\"commit\":\"2b8f97d\",\"build_time\":\"2026-07-03T13:06:23Z\",\"dirty\":\"true\"}`。
  - `curl -fsS http://127.0.0.1:18081/healthz`：通过，`db/qdrant/redis` 均为 `ok`。
  - `. scripts/load-prod-env.sh && docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml ps`：通过，四个核心 Memory OS 容器均为 `Up`。
  - `SMOKE_TIMEOUT=3m make smoke`：通过，结果 `smoke ok`。

处理过的报错：

- `TestLoadBuildInfoScriptPreservesExplicitEnvironmentOverrides` 首次失败：
  - 现象：`.build-info.env` 覆盖了外部显式 `BUILD_*` 环境变量。
  - 根因：脚本直接 `source` 文件，没有恢复调用方已显式传入的值。
  - 处理：在 `load-build-info.sh` 中对已有环境变量做快照并在 source 后恢复，确保优先级为“显式环境变量 > 文件回退 > Git > 默认值”。
- 第一次向 thinkpad 同步文件时使用了拍平路径的 `rsync`：
  - 现象：少量文件被误复制到 `/opt/memory-os` 根目录。
  - 处理：删除误放副本后，改用 `rsync -R` 按原目录重新同步。

当前结论：

- `BUILD_COMMIT` 在 thinkpad 的非 Git 工作目录下已经不再依赖 `.git` 元数据。
- 运行态 `/version` 现已可证明：
  - `version`
  - `commit`
  - `build_time`
  - `dirty`
- “部署版本可验证性”已从“只能证明构建时间”提升到“能证明具体提交号”。

下一步入口：

- 回到总计划完成度审计，继续收口真正还缺的交付项。
- 优先检查：
  - 最终浏览器验收清单是否还缺页面或场景
  - 最终安全扫描与最终交付报告是否仍未形成可直接交付的证据包

## 2026-07-03 Phase 1.16：最终审计入口稳定化

完成事项：

- 修复 `scripts/preflight.sh` 在 `ALLOW_EXISTING_DEPLOYMENT=1` 场景下的生产环境加载缺口。
- 新增 `load_compose_env_for_ps()`，仅在需要用 `docker-compose ps` 判断“当前部署是否占用端口”时，懒加载 `scripts/load-prod-env.sh`。
- 保持普通 `preflight` 不对 production env 形成硬依赖；只有“识别当前部署占端口”的分支才尝试加载生产环境。
- 新增脚本级测试，锁定 `preflight.sh` 会在默认 `COMPOSE_PS_CMD` 下加载生产环境。
- 在 thinkpad 上重新执行：
  - `make verify`
  - `RUN_REAL_VERIFY=1 make audit-report`
- 确认最终 completion audit 已从 `fail` 变为 `pass`。

修改文件：

- `scripts/preflight.sh`
- `internal/preflight/preflight_script_test.go`
- `docs/production-delivery-log.md`

验证命令：

- 本地：
  - `go test ./internal/preflight -count=1`：通过。
  - `go test ./internal/verify -run 'TestMakefileExposesPostDeployVerifyTarget|TestVerifyScriptRunsDeliveryGatesInOrder' -count=1`：通过。
- 服务器：
  - `make verify`：通过。
  - `RUN_REAL_VERIFY=1 make audit-report`：通过。
  - `sed -n '1,260p' artifacts/completion-audit.md`：通过，报告 `status: pass`。

处理过的报错：

- 初次 `RUN_REAL_VERIFY=1 make audit-report` 失败：
  - 现象：`make verify` 卡在 `preflight`，报 `POSTGRES_PASSWORD is required`。
  - 根因：`preflight.sh` 在已有端口占用且 `ALLOW_EXISTING_DEPLOYMENT=1` 时会执行 `docker-compose ... ps`，但该路径没有先加载生产环境变量。
  - 处理：把环境加载逻辑收口到 `preflight.sh` 本身，而不是在 `verify.sh` 或 `audit-report.sh` 单独绕过。

当前结论：

- 最终审计入口现在已经稳定：
  - `make verify` 可在 thinkpad 生产工作区完整通过。
  - `RUN_REAL_VERIFY=1 make audit-report` 可直接生成 `status: pass` 的 completion audit。
- 这次修复让“最终门禁”从“功能本身没问题但审计入口会误报失败”提升为“可直接作为交付证据运行”。

下一步入口：

- 继续做真正的完成度审计，而不是只看门禁脚本是否通过。
- 优先收口：
  - 最终浏览器验收清单是否还缺未覆盖页面或失败场景
  - 最终安全扫描结论和最终交付报告是否已经能一键汇总

## 2026-07-03 Phase 1.17：最终交付报告入口稳定化

完成事项：

- 确认 `scripts/final-delivery-report.sh` 已生成带运行态快照、浏览器验收矩阵和安全摘要的新版本草稿报告。
- 修复 `make final-delivery-report` 的入口稳定性问题：为 `scripts/final-delivery-report.sh` 补齐执行权限，避免 Makefile 直接调用时报 `Permission denied`。
- 在 thinkpad 上重新同步脚本、测试和交付日志，并生成带真实运行态快照的 `/opt/memory-os/artifacts/final-delivery-report.md`。
- 清理一次误同步到 `/opt/memory-os` 根目录的孤立副本，避免后续排查时混淆脚本与文档来源。

修改文件：

- `scripts/final-delivery-report.sh`
- `docs/production-delivery-log.md`

验证命令：

- 本地：
  - `go test ./internal/deliveryreport -count=1`：通过。
  - `make final-delivery-report`：通过。
- 服务器：
  - `export PATH=/usr/local/go/bin:$PATH && go test ./internal/deliveryreport -count=1`：通过。
  - `RUN_RUNTIME_CHECKS=1 make final-delivery-report`：通过。
  - `sed -n '1,260p' /opt/memory-os/artifacts/final-delivery-report.md`：通过，确认包含：
    - `/version` 实时输出
    - `/healthz` 实时输出
    - OpenAPI path count
    - 浏览器验收矩阵
    - Security Summary

处理过的报错：

- `make final-delivery-report` 首次在 thinkpad 失败：
  - 现象：`scripts/final-delivery-report.sh: Permission denied`。
  - 根因：脚本缺少可执行权限，而 Makefile 使用的是直接执行而非 `bash scripts/...`。
  - 处理：为脚本补齐执行权限，并重新同步到服务器。
- 远端直接执行 `go test` 首次失败：
  - 现象：`go: command not found`。
  - 根因：SSH 非交互 shell 未带上 thinkpad 的 Go PATH。
  - 处理：显式注入 `PATH=/usr/local/go/bin:$PATH` 后验证通过；该问题属于服务器 shell 环境，不是仓库逻辑缺陷。

当前结论：

- 最终交付报告现在已经具备可重复生成的稳定入口。
- thinkpad 上的报告草稿已经带有真实运行态快照，不再只是本地静态草稿。
- 当前仍不能宣布“最终交付完成”，因为浏览器验收总包和最终安全结论仍停留在 delivery log 引用层，没有完全收束成最终手册。

下一步入口：

- 补齐一个面向交付的浏览器验收总包，至少把首页的 `partial` 项收口。
- 把安全扫描、权限隔离和运行态验证结论从 `docs/production-delivery-log.md` 提炼进最终报告，减少交付时的跳转成本。

## 2026-07-03 Phase 1.18：首页浏览器证据包与验收会话工具

完成事项：

- 新增 `cmd/memory-browser-acceptance`，提供两类正式子命令：
  - `provision`：创建一次性浏览器验收主体、组织、项目、短期只读/写入 PAT 和 Adapter Token，并写出状态文件。
  - `cleanup`：撤销本轮短期 token，并删除状态文件。
- 为该命令补齐命令级测试，覆盖：
  - `provision` 会写出状态文件。
  - `cleanup` 会调用撤销逻辑并移除状态文件。
  - 两个子命令都要求显式 `--state`。
- 使用 in-app browser 真实访问生产管理台：
  - 未登录状态位于 `/login`。
  - 使用一次性只读 PAT 登录后进入 `/` 总览页。
  - 首页显示真实组织 / 项目上下文与真实 API 统计卡片，而不是静态假数据。
  - 截图与结构化提取已写入 `artifacts/browser-acceptance/`。
- 新增浏览器证据包 `artifacts/browser-acceptance/browser-acceptance-bundle.md`，把首页截图和关键字段提取收口为单一交付入口。
- 更新 `scripts/final-delivery-report.sh`：
  - 支持 `BROWSER_ACCEPTANCE_BUNDLE_PATH`。
  - 当证据包存在时，把首页 `/` 从 `partial` 升级为 `pass`。
  - 在最终报告 `Core Evidence` 中直接列出浏览器证据包路径。
- 在 thinkpad 上重新生成 `/opt/memory-os/artifacts/final-delivery-report.md`，确认首页 `pass` 已生效。

修改文件：

- `cmd/memory-browser-acceptance/main.go`
- `cmd/memory-browser-acceptance/main_test.go`
- `scripts/final-delivery-report.sh`
- `internal/deliveryreport/final_delivery_report_script_test.go`
- `artifacts/browser-acceptance/browser-acceptance-bundle.md`
- `docs/production-delivery-log.md`

验证命令：

- 本地：
  - `go test ./cmd/memory-browser-acceptance -count=1`：通过。
  - `go test ./internal/deliveryreport -count=1`：通过。
  - `make final-delivery-report`：通过。
- 浏览器：
  - in-app browser 打开 `http://ddns.08121.top:18080/login`：确认登录页中文文案。
  - 使用一次性短期 PAT 登录后进入 `/`：确认首页显示真实上下文、健康状态 `ok` 和真实 API 统计卡片。
  - 点击“登出”后回到 `/login`。
- 服务器：
  - `go test ./cmd/memory-browser-acceptance -count=1`：通过。
  - `go test ./internal/deliveryreport -count=1 -run TestFinalDeliveryReportScriptWritesDraftReport -v`：通过。
  - `RUN_RUNTIME_CHECKS=1 make final-delivery-report`：通过。
  - `sed -n '1,220p' /opt/memory-os/artifacts/final-delivery-report.md`：通过，首页 `/` 已标记为 `pass`。

处理过的报错：

- thinkpad 上第一次 provision 失败：
  - 现象：`--dsn is required`。
  - 根因：生产环境脚本只提供 `POSTGRES_PASSWORD` 等 compose 变量，没有直接导出 `POSTGRES_DSN`。
  - 处理：复用运行中的 postgres 容器地址，并结合 `POSTGRES_PASSWORD` 组装一次性 DSN。
- 浏览器登录第一次等待失败：
  - 现象：`playwright_wait_for_load_state does not support networkidle`。
  - 根因：当前 in-app browser 封装不支持该等待模式。
  - 处理：改为兼容的 `load` / 当前页面确认路径，不重试同一失败调用。
- 浏览器会话清理后的第一次状态检查误报：
  - 现象：状态文件检查一度显示 `state_removed=false`。
  - 根因：检查命令本身不稳定，和实际文件状态不一致。
  - 处理：重新直接检查 `/tmp/memory-os-browser-acceptance-session.json`，确认文件已不存在。

当前结论：

- 最终报告中的首页验收已不再依赖“delivery log 间接引用”，而是有独立浏览器证据包支撑。
- 浏览器验收会话现在有正式的 provision / cleanup 工具，不必再依赖一次性临时程序。
- 当前仍不能宣布最终交付完成，因为安全结论、权限隔离结论和最终 handoff package 仍未完全收束。

下一步入口：

- 把 `docs/production-delivery-log.md` 中分散的安全扫描、权限隔离和运行态验证结论提炼进最终报告。
- 继续做逐项 completion audit，确认还能否继续压缩 `Remaining Audit Focus`。

## 2026-07-03 Phase 1.19：安全与权限证据包接入最终报告

完成事项：

- 为最终报告新增两类单一证据入口：
  - `artifacts/security-evidence/security-evidence-bundle.md`
  - `artifacts/security-evidence/permission-isolation-bundle.md`
- 更新 `scripts/final-delivery-report.sh`：
  - 支持 `SECURITY_EVIDENCE_BUNDLE_PATH`
  - 支持 `PERMISSION_ISOLATION_BUNDLE_PATH`
  - 在 `Core Evidence` 中直接列出安全和权限证据包
  - 新增 `Permission Isolation Summary`
  - 在 `Security Summary` 中直接引用生产安全证据包
- 更新 `internal/deliveryreport/final_delivery_report_script_test.go`，要求报告在 bundle 存在时显式输出：
  - `Security evidence bundle`
  - `Permission isolation bundle`
  - `Permission Isolation Summary`
- 本地重新生成 `artifacts/final-delivery-report.md`，确认安全与权限 bundle 已进入报告。

修改文件：

- `scripts/final-delivery-report.sh`
- `internal/deliveryreport/final_delivery_report_script_test.go`
- `artifacts/security-evidence/security-evidence-bundle.md`
- `artifacts/security-evidence/permission-isolation-bundle.md`
- `docs/production-delivery-log.md`

验证命令：

- 本地：
  - `go test ./internal/deliveryreport -count=1`：通过。
  - `make final-delivery-report`：通过。
  - `sed -n '1,260p' artifacts/final-delivery-report.md`：通过，确认包含：
    - `Security evidence bundle`
    - `Permission isolation bundle`
    - `Permission Isolation Summary`
- 服务器：
  - `go test ./internal/deliveryreport -count=1`：通过。
  - `RUN_RUNTIME_CHECKS=1 make final-delivery-report`：通过。

处理过的报错：

- 报告测试第一次失败：
  - 现象：缺少 `Security evidence bundle` 段落。
  - 根因：`final-delivery-report.sh` 还没有接入安全/权限 bundle 变量。
  - 处理：新增 bundle path 变量和对应报告段落。
- 报告测试第二次失败：
  - 现象：`production permission isolation bundle` 文案不匹配。
  - 根因：测试期望与报告实际标题大小写不一致。
  - 处理：统一为 `Production permission isolation bundle`。
- thinkpad 第一次同步后报告未出现安全 bundle：
  - 根因：同步命令把路径拍平到 `/opt/memory-os` 根目录，未落到 `artifacts/security-evidence/`。
  - 处理：改用保留目录结构的同步方式重新落盘。

当前结论：

- 最终报告已经不只包含首页浏览器证据，还开始拥有安全和权限的单一证据入口。
- 当前仍不能宣布最终交付完成，因为这些 bundle 仍是“证据汇总层”，不是对 Phase 0-12 的完整逐项 completion audit 结论。

下一步入口：

- 在 thinkpad 上修正安全/权限 bundle 的落盘路径并重生最终报告。
- 然后继续做逐项 completion audit，把 `Remaining Audit Focus` 再压缩一轮。

## 2026-07-03 Phase 1.20：密码登录、首个管理员初始化与现有用户补密码闭环

完成事项：

- 新增租户查询能力：
  - `tenant.Repository` / `tenant.Service` / `tenant.PGRepository` 补齐 `FindUserByEmail`。
- 新增真实密码登录 API：
  - `POST /memory/auth/login-password`
  - 按邮箱查用户，校验 password credential，成功后签发 12 小时短期 PAT。
  - 不引入新的 session 子系统，继续复用现有管理 API Bearer PAT 鉴权链路。
- 新增服务器运维命令：
  - `memory-bootstrap bootstrap`
    - 仅用于空库初始化首个 user / org / project / owner / password。
  - `memory-bootstrap set-password`
    - 用于非空生产库给现有用户补密码。
    - 密码只从环境变量读取，不走命令行参数，避免 shell history 泄露。
- 登录页改为“密码登录优先 + PAT 兼容入口”：
  - 默认展示邮箱 / 密码表单。
  - 保留 PAT 登录切换入口，避免当前运维链路被硬切断。
  - 登录成功后把短期 PAT 只保存在浏览器 localStorage，继续复用原有管理台请求方式。

修改文件：

- `internal/tenant/repository.go`
- `internal/tenant/service.go`
- `internal/tenant/pg_repository.go`
- `internal/tenant/service_test.go`
- `internal/tenant/pg_repository_test.go`
- `internal/http/router.go`
- `internal/http/router_test.go`
- `cmd/memory-bootstrap/main.go`
- `cmd/memory-bootstrap/main_test.go`
- `web/stores/auth.ts`
- `web/pages/login.vue`
- `internal/webdeploy/web_dockerfile_test.go`
- `artifacts/completion-checklist-audit.md`

本地验证：

- `go test ./internal/tenant ./internal/http ./cmd/memory-bootstrap ./internal/webdeploy -count=1`：通过。
- `cd web && npm run build`：通过。

thinkpad 验证：

- `PATH=/usr/local/go/bin:$PATH go test ./internal/http ./internal/webdeploy ./cmd/memory-bootstrap ./internal/tenant -count=1`：通过。
- `cd /opt/memory-os/web && npm run build`：通过。
- `APP_ENV=production ENABLE_DEV_ENDPOINTS=false docker-compose -f deploy/docker-compose.yml -f deploy/docker-compose.t480.yml up -d --build memory-api memory-web`：通过，`memory-api` / `memory-web` 容器重建成功。
- `curl http://127.0.0.1:18081/openapi.json`：通过，出现 `/memory/auth/login-password`。
- `curl http://127.0.0.1:18081/healthz`：通过。

浏览器验收：

- Playwright 打开 `http://ddns.08121.top:18080/login`。
- 页面快照确认可见：
  - `密码登录`
  - `PAT 登录`
  - `memory-bootstrap bootstrap`
  - 默认邮箱 / 密码输入框
- 截图保存：
  - `artifacts/browser-acceptance/login-password-page.png`

运行态发现：

- 生产库当前统计：
  - `users = 142`
  - `password_credentials = 1`
- 当前唯一已有 password credential 对应邮箱：
  - `alice-auth-pg@example.com`
- 这说明：
  - 仅实现 `bootstrap` 不足以覆盖现网，因为生产库已非空。
  - `set-password` 是把现有 thinkpad 环境真正切到 password login 所必需的补口。

当前仍未完成：

- 尚未完成一次浏览器内的真实密码登录验收。
- 当前已经不再缺少账号与密码本身，剩余的是浏览器侧最终验收截图与页面加载闭环。

下一步入口：

- 浏览器完成真实密码登录验收，并把该证据回写到 completion audit 与最终交付报告。

## 2026-07-03 Phase 1.22：专用验收管理员账号创建与密码登录 API 验收

完成事项：

- 使用用户指定的专用验收邮箱，在 thinkpad 生产库中创建独立管理员验收账号。
- 为该账号创建独立 org / project，并授予：
  - org owner
  - project owner
- 为该账号写入 password credential。
- 完成一次真实生产 API 密码登录验收：
  - `POST /memory/auth/login-password`
  - 登录成功后使用返回的短期 PAT 继续访问：
    - `/memory/tenant/orgs/list`
    - `/memory/tenant/projects/list`

验证结果：

- 账号创建结果：
  - `created user_id=f4158493-b0f4-4e55-8bf6-02a01f9e6a8b`
  - `org_id=77d30450-63f8-436c-87a0-74089d2813f6`
  - `project_id=6ef443e3-82c0-473a-8d13-3dda3f9df99d`
- 真实登录 API 验收：
  - `login_ok = true`
  - org list 返回 `1` 个组织
  - project list 返回 `1` 个项目
- 这证明：
  - 密码登录链路可用
  - 登录成功后的短期 PAT 可继续访问真实管理 API
  - 不是“只会返回 token 的假登录”

当前仍未完成：

- 浏览器内的最终密码登录验收和页面截图回写还没做。
- 因此 password login 闭环可以从“没有账号无法继续”推进到“只差浏览器最终验收”。

## 2026-07-03 Phase 1.21：Secret 注入运行态专项审计收口

完成事项：

- 新增 `cmd/memory-secret-audit runtime`：
  - 真实连接 PostgreSQL。
  - 真实创建临时 user / org / project / secret。
  - 真实执行一次 `secret.inject` 注入。
  - 真实校验 `secret.inject` 审计日志落库数量。
  - 真实扫描以下位置是否出现探针明文：
    - audit metadata
    - Markdown Archive 目录
    - PostgreSQL `archive_chunks`
    - PostgreSQL `hot_memories`
    - PostgreSQL `qdrant_points`
    - PostgreSQL `hot_memory_qdrant_points`
    - Qdrant live payload scroll
  - 真实执行 cleanup：
    - `vault.Disable(secret_ref)`
    - `DeleteProject`
    - `DeleteOrg`
    - `UpdateUserStatus(disabled)`
- 新增 `scripts/secret-injection-audit.sh`：
  - 生成 `artifacts/security-evidence/secret-injection-audit.md`
  - 本地测试模式支持 `RUNTIME_SECRET_INJECTION_CMD` 注入假结果
  - 生产默认模式支持直接在 thinkpad 宿主机运行：
    - 自动解析 `deploy-postgres-1` bridge IP
    - 自动解析 `/data/memory-os` 对应的 Docker volume 源路径
    - 自动使用宿主机可达的 `http://127.0.0.1:18083`
- `Makefile` 已暴露：
  - `make secret-injection-audit`

修改文件：

- `cmd/memory-secret-audit/main.go`
- `cmd/memory-secret-audit/main_test.go`
- `scripts/secret-injection-audit.sh`
- `Makefile`
- `artifacts/security-evidence/security-evidence-bundle.md`
- `artifacts/completion-checklist-audit.md`

本地验证：

- `go test ./cmd/memory-secret-audit ./internal/secretaudit -count=1`：通过。
- `bash scripts/secret-injection-audit.sh` 配合 `RUNTIME_SECRET_INJECTION_CMD=printf ...`：通过，报告可生成。

thinkpad 验证：

- `PATH=/usr/local/go/bin:$PATH go test ./cmd/memory-secret-audit ./internal/secretaudit -count=1`：通过。
- `. scripts/load-prod-env.sh >/dev/null 2>&1 && PATH=/usr/local/go/bin:$PATH make secret-injection-audit`：通过。
- 报告：
  - `artifacts/security-evidence/secret-injection-audit.md`
- 当前报告核心结果：
  - `status: pass`
  - `audit_log_count: 1`
  - `audit_metadata_hits: 0`
  - `archive_markdown_hits: 0`
  - `archive_chunk_hits: 0`
  - `hot_memory_hits: 0`
  - `archive_qdrant_payload_hits: 0`
  - `hot_memory_qdrant_payload_hits: 0`
  - `qdrant_live_payload_hits: 0`
  - cleanup 四项均为 `true`

处理中间报错：

- 第一次脚本失败：
  - 现象：`--dsn is required`
  - 根因：`load-prod-env.sh` 只补 `POSTGRES_PASSWORD`，不会直接导出 `POSTGRES_DSN`
  - 处理：脚本改为自动读取运行中 `memory-api` / `postgres` 容器的实际环境与挂载信息
- 第二次脚本失败：
  - 现象：容器网络内访问 `http://qdrant:6333` scroll 返回 `502`
  - 根因：thinkpad 临时容器到 Qdrant 的网络路径异常，与宿主机 `127.0.0.1:18083` 行为不一致
  - 处理：脚本改为宿主机模式，直接使用：
    - PostgreSQL bridge IP
    - Docker volume 源路径
    - `http://127.0.0.1:18083`

当前结论：

- Secret 注入审计已经从“阶段日志提及”升级为“独立运行态专项报告”。
- 这一项可以从 completion checklist 的 `weak` 推进到 `verified`。
- 但整套系统仍未达到最终交付完成，因为 password login 真实生产闭环、多 Agent 独立 token 专项证明等项仍未收口。
