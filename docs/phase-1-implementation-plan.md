# Phase 1 AI 自驱动实现计划

> 面向 AI 代理的工作者：执行本计划前必须读取 `README.md`、`AGENTS.md`、`docs/memory-os-spec.md`。所有代码改动前创建 TodoList。除非用户明确要求，不要 commit、push、deploy。

**目标：** 构建 Memory OS v0.4 Phase 1 基础设施骨架，让项目可以通过固定命令启动、测试、构建和 smoke test。

**架构：** Go/Hertz API、worker skeleton、MCP skeleton、Nuxt 管理台、PostgreSQL、Redis、Qdrant、Docker Compose。Phase 1 不实现完整业务闭环，但必须为 Auth、Secret Vault、TurnEvent、Archive、RAG、Hot Memory 和 Unified Retrieval 保留接口边界。

**技术栈：** Go >= 1.25、CloudWeGo Hertz、pgx/v5、zap、swaggo/swag、PostgreSQL、Redis、Qdrant、Nuxt 3、Vue 3、Tailwind CSS、Pinia、Docker Compose。

---

## 1. 成功标准

Phase 1 完成时必须满足：

- `make test` 可执行，后端至少运行 `go test ./...`。
- `make build-web` 可执行，前端完成 Nuxt build。
- `make smoke` 可执行，覆盖 API、PostgreSQL、Redis、Qdrant 和基础 filtered search。
- `make dev-up` 能启动基础服务。
- `GET /healthz` 返回健康状态。
- Swagger/OpenAPI 可访问或可生成。
- 日志为 zap JSON 格式。
- `.env.example` 存在且不包含真实 secret。
- Docker Compose 不对外暴露 PostgreSQL 和 Redis。
- Qdrant 默认端口映射到 `18083`。

## 2. 推荐文件结构

创建或维护：

```text
Makefile
.env.example
go.mod
cmd/memory-api/main.go
cmd/memory-worker/main.go
cmd/memory-mcp/main.go
internal/config/config.go
internal/logger/logger.go
internal/db/db.go
internal/redis/client.go
internal/qdrant/client.go
internal/llm/client.go
internal/llm/openai_compatible.go
internal/health/service.go
internal/http/router.go
migrations/000001_init.sql
deploy/docker-compose.yml
deploy/docker-compose.t480.yml
deploy/nginx.conf
web/
docs/memory-os-spec.md
docs/phase-1-implementation-plan.md
```

## 3. 执行任务

### 任务 1：项目骨架与配置

- [ ] 创建 Go module。
- [ ] 设置 Go toolchain 要求，处理 Go >= 1.25。
- [ ] 创建 `.env.example`，只写占位值。
- [ ] 创建 `internal/config`，读取端口、数据库、Redis、Qdrant、模型 provider 配置。
- [ ] 为配置解析写单测，覆盖默认值、缺失值、非法端口。
- [ ] 运行 `go test ./...`。

验收：配置不打印 secret，测试通过。

### 任务 2：日志与 API health

- [ ] 创建 `internal/logger`，初始化 zap JSON logger。
- [ ] 创建 Hertz API skeleton。
- [ ] 实现 `GET /healthz`。
- [ ] health response 包含 API、db、redis、qdrant 的状态字段；依赖未配置时返回 degraded 而不是 panic。
- [ ] 为 health service 写单测。
- [ ] 运行 `go test ./...`。

验收：API 可启动，`/healthz` 可返回 JSON。

### 任务 3：PostgreSQL migration 与 pgx

- [ ] 创建 pgx pool 初始化。
- [ ] 创建 migrations 目录。
- [ ] 添加最小 migration 表，例如 `schema_migrations` 或基础 health 表。
- [ ] 创建 migration runner 或文档化迁移命令。
- [ ] 为 DSN 脱敏日志写单测。
- [ ] 运行 `go test ./...`。

验收：数据库连接失败不会泄露密码。

### 任务 4：Redis client

- [ ] 创建 Redis client wrapper。
- [ ] 实现 Ping health。
- [ ] 预留 queue、lock、cache 的包边界。
- [ ] 为配置和错误映射写单测。
- [ ] 运行 `go test ./...`。

验收：Redis 不可用时 health 标记 degraded。

### 任务 5：Qdrant client 与 collection ensure

- [ ] 创建 Qdrant client wrapper。
- [ ] 实现 health probe。
- [ ] 实现 ensure single collection 的接口骨架。
- [ ] 定义 payload filter builder，必须支持 user/org/project/visibility/permission_labels/doc_type。
- [ ] 为 filter builder 写单测，验证不会生成空权限 filter。
- [ ] 运行 `go test ./...`。

验收：query-time filter 构造有测试覆盖。

### 任务 6：模型网关 skeleton

- [ ] 创建 `internal/llm`。
- [ ] 定义 Chat、Embedding、Rerank 接口。
- [ ] 实现 OpenAI-compatible client skeleton。
- [ ] 支持 `LLM_BASE_URL`、`LLM_API_KEY`、`LLM_MODEL`、`EMBEDDING_MODEL`、`RERANK_MODEL`。
- [ ] 日志和错误不得包含 API key。
- [ ] 为配置缺失、认证错误脱敏写单测。
- [ ] 运行 `go test ./...`。

验收：缺少 key 时服务仍可启动，模型功能返回明确错误。

### 任务 7：worker skeleton

- [ ] 创建 `cmd/memory-worker`。
- [ ] 加载同一份 config。
- [ ] 初始化 logger、db、redis、qdrant。
- [ ] 预留 archive/index/hotmemory job runner 接口。
- [ ] 支持 graceful shutdown。
- [ ] 写最小启动单测或包级单测。
- [ ] 运行 `go test ./...`。

验收：worker 可启动并在缺少外部依赖时给出明确错误。

### 任务 8：MCP server skeleton

- [ ] 创建 `cmd/memory-mcp`。
- [ ] 预留 `memory_search`、`memory_archive`、`memory_append_event`、`memory_get_archive`、`memory_mark_used`、`memory_stats`。
- [ ] Phase 1 可以返回 not implemented，但 tool schema 必须稳定。
- [ ] 写 schema 或 handler 单测。
- [ ] 运行 `go test ./...`。

验收：MCP skeleton 不阻塞 API 和 worker。

### 任务 9：Nuxt 管理台 skeleton

- [ ] 创建 `web/` Nuxt 3 项目。
- [ ] 配置 Tailwind CSS、Pinia、选择一种 UI 组件库。
- [ ] 创建首页、health 页面或基础 dashboard。
- [ ] 读取 API base URL 配置。
- [ ] 添加 `npm run build`。
- [ ] 运行 `npm run build` 或 `npx nuxt build`。

验收：前端可静态构建。

### 任务 10：Docker Compose 与 T480 profile

- [ ] 创建 `deploy/docker-compose.yml`。
- [ ] 包含 memory-api、memory-worker、memory-mcp、memory-web、postgres、redis、qdrant。
- [ ] PostgreSQL 和 Redis 不对外暴露。
- [ ] Qdrant 映射 `18083`。
- [ ] API 映射 `18081`。
- [ ] MCP 映射 `18082`。
- [ ] Web 映射 `18080`。
- [ ] 创建 T480 override，设置低并发和资源参数。
- [ ] 创建 Nginx 配置托管 Nuxt 静态产物。

验收：`make dev-up` 使用 compose 启动基础服务。

### 任务 11：Makefile 与 smoke test

- [ ] 创建 `Makefile`。
- [ ] 实现 `make dev-up`。
- [ ] 实现 `make dev-down`。
- [ ] 实现 `make test`。
- [ ] 实现 `make build-web`。
- [ ] 实现 `make smoke`。
- [ ] smoke test 覆盖 `/healthz`、PostgreSQL、Redis、Qdrant collection ensure、filtered search builder。
- [ ] 运行完整顺序：`make test`、`make build-web`、`make smoke`。

验收：失败时返回非零退出码，成功时输出关键检查项。

## 4. 集成测试方案

### 4.1 最小集成链路

集成测试应自动创建：

```text
org_alpha/project_alpha/user_alice
org_alpha/project_alpha/user_bob
org_beta/project_beta/user_eve
```

测试数据：

- Alice 的项目级 Archive chunk。
- Bob 的用户级 Hot Memory。
- Eve 的跨 org Archive chunk。
- 一个包含假 secret 的 TurnEvent。

### 4.2 必须断言

- Alice 可以搜到 Alice project scope 数据。
- Alice 搜不到 Eve org 数据。
- Alice 搜不到 Bob private user scope 数据。
- Bob 可以搜到 Bob private user scope 数据。
- `agent_specific` 数据默认不被其他 Agent 搜到。
- Qdrant search 请求包含 query-time filter。
- 假 secret 在 Archive、Hot Memory、Qdrant payload、日志中均不出现明文。

### 4.3 模型 provider 集成

如果环境变量存在：

```text
LLM_BASE_URL
LLM_API_KEY
EMBEDDING_MODEL
RERANK_MODEL
```

则 smoke test 额外执行：

- embedding probe：输入短文本，返回非空向量。
- rerank probe：输入 query + documents，返回排序结果。

如果环境变量不存在：

- smoke test 跳过模型 probe。
- 输出 `model provider skipped: not configured`。
- 不得失败。

## 5. AI 自驱动失败处理规则

AI 执行本计划时，如果命令失败：

1. 不要继续下一个阶段。
2. 记录失败命令。
3. 摘要关键错误。
4. 定位最小必要修改。
5. 修改后重新运行失败命令。
6. 当前失败命令通过后，再运行完整验收顺序。

同一命令失败 3 次后，必须停下来总结原因和可选方案，不要机械重试。

## 6. 不做事项

Phase 1 不做：

- 完整 Auth。
- 完整 Secret Vault。
- 完整 TurnEvent ingestion。
- 完整 Archive 生成。
- 完整 Hot Memory 派生。
- 完整 RAG 检索。
- 线上部署。
- 自动 commit。

这些能力在 Phase 2 之后逐步实现。

## 7. 子代理建议

Phase 1 主会话为主。可交给子代理的任务：Nuxt skeleton、smoke script 初版、Swagger 文档补充、config/logger 单测补齐。

必须由主会话控制的任务：Go module 初始架构、Makefile 最终整合、Docker Compose 最终整合、Qdrant filter builder。
