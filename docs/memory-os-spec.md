# Memory OS v0.4 Native Spec

日期：2026-07-01  
状态：Draft v0.4  
目标：构建一个 Native Multi-Agent Memory Platform，把 Codex、Claude Code、opencode、Hermes 以及其它 Agent 平台中的对话、项目经验、排障记录、用户偏好、Secret 引用和长期决策沉淀为可召回、可追溯、可治理的统一记忆系统。

## 0. 当前版本结论

Memory OS v0.4 的 MVP 不依赖 mem0 或 FastGPT。

本版本固定采用：

- Go / CloudWeGo Hertz 作为后端主服务栈。
- PostgreSQL 作为权威元数据源。
- Redis 作为队列、缓存、锁和限流组件。
- Qdrant 作为向量检索后端。
- Markdown 文件作为 Archive 内容权威源。
- Nuxt 3 作为管理台。
- Docker Compose 作为单机部署方式。

Hot Memory 和 Archive RAG 都必须原生实现。mem0、FastGPT、OpenMemory、Zep、Khoj 只作为后续 importer 或迁移入口，不作为 MVP 运行依赖。

## 1. 背景与问题

多个 Agent 平台都会产生高价值上下文：

- 用户偏好和长期工作方式。
- 项目架构、部署细节、约束和历史决策。
- 命令执行、错误输出、排障路径和最终修复。
- 文件变更、代码审查、测试结果和工具调用。
- 多轮对话中被确认或否定的方案。

这些信息如果只保存在单个平台内，会产生几个问题：

- 不同 Agent 之间不能共享同一用户、项目或组织下的长期记忆。
- 简短摘要容易丢失路径、命令、报错、版本、决策原因等关键证据。
- 完整对话直接进入热记忆会造成噪声和隐私风险。
- Secret 明文如果进入日志、Archive、向量库或回答，会形成长期污染。
- 单机部署资源有限，不能依赖重型流水线或大量并发任务。

Memory OS v0.4 的核心任务是把“完整依据”和“高价值可召回事实”分层治理：

- Markdown Archive 保存可追溯的完整档案。
- Native Archive RAG 对 Archive 做 chunking、索引和权限过滤检索。
- Native Hot Memory 保存高价值、短文本、可快速召回的事实。
- Secret Vault 独立加密保存明文 Secret，记忆与 RAG 只保存 `secret_ref`。

## 2. 设计目标

### 2.1 产品目标

- 支持多用户、多组织、多项目的 Org 模式。
- 支持同一用户跨 Codex、Claude Code、opencode、Hermes 共享 user/project/org scope 下的记忆。
- 不同用户默认隔离。
- `agent_specific` 记忆默认不跨 Agent 召回。
- 支持 Markdown Archive 浏览、编辑、版本、审计和重索引。
- 支持 Hot Memory 查看、提升、降级、删除和热度统计。
- 支持 Secret Vault 管理、工具注入和审计。
- 支持 Adapter Token、PAT 和后续 MCP 集成。

### 2.2 技术目标

- PostgreSQL 是权威元数据源。
- Markdown 文件是 Archive 正文权威源。
- Qdrant 使用单 collection。
- 所有 Qdrant 查询必须使用 query-time payload filter。
- 不允许先向量召回再在应用层做租户或权限过滤。
- 后台任务必须可重试、幂等、可审计。
- 单机 T480 可运行、可备份、可恢复。

### 2.3 非目标

- MVP 不实现 mem0/FastGPT 运行时依赖。
- MVP 不要求所有 Agent 平台都实时采集完整事件流。
- MVP 不实现复杂企业 SSO。
- MVP 不实现多节点 Qdrant 或 Kubernetes 部署。
- MVP 不把 Secret 明文交给 LLM 进行总结、归档或检索。

## 3. 总体架构

```text
Agent Platforms
  Codex / Claude Code / opencode / Hermes / Custom Agents
          |
          v
Adapter Layer
  Codex Adapter / Generic MCP Adapter / Transcript Importer
          |
          v
TurnEvent API
          |
          v
PostgreSQL Event Store  ---- Redis Queue
          |                     |
          |                     v
          |              Archive Worker
          |                     |
          v                     v
Secret Sanitizer        Markdown Archive Files
          |                     |
          |                     v
          |              Archive Chunker
          |                     |
          v                     v
Hot Memory Deriver      Qdrant Single Collection
          |                     |
          v                     v
PostgreSQL Hot Memory   Unified Retrieval
          |                     |
          +-----------> API / MCP / Nuxt Admin
```

查询链路：

```text
User Query
  |
  v
Permission Context
  |
  v
Hot Memory Search
  |
  v
Archive RAG Search
  |
  v
Rerank
  |
  v
Context Compression
  |
  v
Explainable Results
  |
  v
Access Log + mark_used
```

## 4. 固定技术栈

### 4.1 后端

- Go >= 1.25。
- CloudWeGo Hertz。
- pgx/v5。
- zap JSON structured logging。
- swaggo/swag。
- PostgreSQL migrations。

如果本机 Go 版本低于要求，必须通过 `go.mod` 的 `toolchain` 或项目文档明确处理。

### 4.2 前端

- Nuxt 3。
- Vue 3。
- Tailwind CSS。
- Pinia。
- Nuxt UI 或 shadcn-vue 二选一，并保持一致。
- Nuxt 静态构建后由 Nginx 托管。

### 4.3 基础设施

- PostgreSQL。
- Redis。
- Qdrant。
- Docker Compose。
- 本地 Markdown archive volume。

## 5. 仓库结构

优先采用以下结构：

```text
memory-os/
  cmd/
    memory-api/          # Hertz API 服务
    memory-worker/       # 归档、索引、热度治理、备份 worker
    memory-mcp/          # MCP server
    memory-adapter/      # 本地 adapter runner，可选
  internal/
    config/              # 配置加载与校验
    logger/              # zap 初始化
    db/                  # pgx pool、migration、repository
    redis/               # Redis queue、lock、cache
    qdrant/              # Qdrant collection、index、search client
    auth/                # password login、PAT、Adapter Token
    tenant/              # users、orgs、projects、memberships、roles、permissions
    secret/              # Secret Vault、加密、注入、审计
    adapter/             # Codex、Claude Code、opencode、Hermes、Generic adapter
    eventlog/            # TurnEvent v1 校验和持久化
    archive/             # Markdown Archive、版本、编辑、chunking
    hotmemory/           # Hot Memory fact、scope、热度
    retrieval/           # 统一检索、rerank、压缩、mark_used
    audit/               # 审计日志
    jobs/                # 后台任务和幂等重试
  web/                   # Nuxt 管理台
  migrations/            # PostgreSQL migrations
  deploy/                # Dockerfile、compose、nginx、T480 profile
  docs/                  # 规格、运维、部署、API 文档
```

## 6. 核心领域模型

### 6.1 Tenant 与权限

最低模型：

- `users`：用户。
- `orgs`：组织。
- `projects`：项目。
- `memberships`：用户在组织或项目中的成员关系。
- `roles`：角色。
- `resource_permissions`：资源级权限标签。

权限判断必须先于检索。检索服务根据当前用户、组织、项目、角色、资源标签构造权限上下文，再传给 Hot Memory 和 Archive RAG。

### 6.2 Scope

记忆 scope 至少包含：

- `user`：用户级记忆，仅该用户可见。
- `project`：项目级记忆，项目授权成员可见。
- `org`：组织级记忆，组织授权成员可见。
- `agent_specific`：特定 Agent 记忆，默认只在同 Agent 召回。

### 6.3 TurnEvent v1

所有 Agent 平台必须转换成统一 TurnEvent。事件类型包括：

- `session_start`
- `user_message`
- `assistant_message`
- `assistant_final`
- `tool_call_started`
- `tool_call_completed`
- `file_changed`
- `command_started`
- `command_completed`
- `turn_completed`
- `turn_failed`
- `compact`
- `manual_archive_request`

最低要求：

- 必须能生成 `user_message`。
- 必须能生成 `assistant_final` 或 `turn_completed`。
- 如果不能拿工具调用明细，至少生成 `tool_output_summary` 或空数组。
- 如果不能实时采集，允许通过 transcript importer 延迟生成。

Adapter 写入前必须完成：

- 统一时间格式。
- 统一 user/org/project/thread/turn/session 标识。
- Secret 识别、脱敏和 `secret_ref` 替换。
- 工具输出摘要与截断。
- 文件路径标准化。
- 命令输出 hash 化。

## 7. Secret Vault

Secret Vault 必须在 TurnEvent 大规模写入前上线。

### 7.1 核心规则

- Secret 明文只允许加密保存于 Secret Vault。
- Archive、Hot Memory、Qdrant、日志、审计、聊天回答和测试快照都不得保存 Secret 明文。
- 记忆、RAG、事件只保存 `secret_ref`。
- 工具使用 Secret 时由后端自动解密注入。
- 每次解密和注入都必须写审计日志。

### 7.2 Secret 生命周期

```text
输入内容
  |
  v
Secret Detector
  |
  v
Vault Encrypt + Store
  |
  v
Replace with secret_ref
  |
  v
TurnEvent / Archive / Hot Memory / RAG
```

### 7.3 审计字段

审计日志至少记录：

- `actor_user_id`
- `org_id`
- `project_id`
- `secret_ref`
- `tool_name`
- `purpose`
- `request_id`
- `created_at`
- `result`

不得记录 Secret 明文。

## 8. Markdown Archive

Markdown Archive 是完整档案层，也是 Archive 正文权威源。

### 8.1 目录结构

建议默认结构：

```text
archives/
  org_<org_id>/
    project_<project_id>/
      user_<user_id>/
        yyyy/
          mm/
            archive_<archive_id>.md
```

PostgreSQL 保存：

- `archive_id`
- `org_id`
- `project_id`
- `user_id`
- `path`
- `content_hash`
- `version`
- `index_generation`
- `status`
- `created_at`
- `updated_at`

### 8.2 Archive 生成

Archive Worker 从已安全处理的 TurnEvent 批量生成 Markdown。

Markdown 应包含：

- 标题。
- 时间范围。
- 来源 Agent。
- 用户、项目、组织上下文。
- 摘要。
- 关键决策。
- 关键命令与结果摘要。
- 文件变更摘要。
- 错误与修复路径。
- 后续行动。
- source refs。

不得包含 Secret 明文。

### 8.3 编辑与版本

- Nuxt 管理台支持在线编辑 Archive。
- 每次编辑生成新版本。
- 每次编辑生成新的 `index_generation`。
- 旧 chunk 失效但保留审计历史。
- 重索引任务必须幂等。

## 9. Native Archive RAG

### 9.1 Chunking

Archive chunking 输入是 Markdown 文件正文，输出是可索引 chunk。

chunk 元数据至少包含：

- `chunk_id`
- `archive_id`
- `org_id`
- `project_id`
- `user_id`
- `visibility`
- `permission_labels`
- `index_generation`
- `chunk_index`
- `content_hash`

### 9.2 Qdrant 单 collection

Qdrant 使用单 collection，Archive chunk 与 Hot Memory 共用，通过 payload 区分。

payload 至少包含：

- `doc_type`: `archive_chunk` 或 `hot_memory`
- `user_id`
- `org_id`
- `project_id`
- `archive_id`
- `memory_id`
- `visibility`
- `permission_labels`
- `agent_id`
- `scope`
- `index_generation`
- `created_at`

### 9.3 查询强约束

所有 Qdrant 查询必须带 query-time payload filter。

禁止流程：

```text
向量召回大量结果 -> 应用层过滤租户和权限
```

必须流程：

```text
构造权限上下文 -> 构造 Qdrant filter -> query-time filtered search
```

## 10. Native Hot Memory

Hot Memory 是短事实、偏好、项目状态、决策和高频知识的快速召回层。

### 10.1 来源

Hot Memory 可从以下来源派生：

- 安全处理后的 TurnEvent。
- Markdown Archive。
- 用户手动提升。
- 管理台编辑。

### 10.2 PostgreSQL 字段

Hot Memory 元数据和正文权威存 PostgreSQL。最低字段：

- `memory_id`
- `org_id`
- `project_id`
- `user_id`
- `agent_id`
- `scope`
- `visibility`
- `permission_labels`
- `fact`
- `source_type`
- `source_ref`
- `confidence`
- `access_count`
- `used_count`
- `hot_score`
- `status`
- `created_at`
- `updated_at`

Hot Memory 不保存 Secret 明文，只保存 `secret_ref`。

### 10.3 热度治理

支持：

- `access_count`
- `used_count`
- `hot_score`
- promote
- demote
- archive-only
- soft delete

`agent_specific` Hot Memory 默认只在同 Agent 召回，除非显式提升为跨 Agent scope。

## 11. Unified Retrieval

`/memory/search` 和 MCP `memory_search` 使用同一检索流程。

步骤：

1. 构造权限上下文。
2. 检索 Hot Memory。
3. 检索 Archive RAG。
4. 合并候选。
5. rerank。
6. 上下文压缩。
7. 返回可解释来源。
8. 写 access log。
9. 使用后 mark_used。

返回结果必须可追溯到：

- `memory_id`
- `archive_id`
- `chunk_id`
- `thread_id`
- `turn_id`
- `source_ref`

## 12. API 与 MCP

### 12.1 Phase 1 API

Phase 1 至少提供：

- `GET /healthz`
- Swagger/OpenAPI 页面或 JSON。
- JSON zap 日志。
- pgx migrations。

### 12.2 后续 API

保留以下接口演进空间：

- `POST /memory/turn-event`
- `POST /memory/search`
- `POST /memory/archive/run`
- `POST /memory/promote`
- `POST /memory/demote`
- `GET /memory/stats`
- Auth、Tenant、Secret Vault、Archive、Hot Memory、Adapter Token 管理接口。

### 12.3 MCP Tools

MCP server 至少规划：

- `memory_search`
- `memory_archive`
- `memory_append_event`
- `memory_get_archive`
- `memory_mark_used`
- `memory_stats`

## 13. Nuxt 管理台

管理台最终支持：

- 用户管理。
- 组织管理。
- 项目管理。
- 权限管理。
- Archive 浏览、编辑、版本、重索引。
- Hot Memory 管理。
- Secret Vault 管理。
- Adapter Token 管理。
- Qdrant 索引状态。
- 检索测试页。
- 热度统计。

Phase 1 只要求管理台骨架可构建、可由 Nginx 托管。

## 14. Docker Compose 与端口

默认端口：

- `memory-web`: `18080`
- `memory-api`: `18081`
- `memory-mcp`: `18082`
- `qdrant`: `18083`
- PostgreSQL 不对外暴露。
- Redis 不对外暴露。

Compose 必须支持 T480 单机 profile，并能通过 docker-compose 命令启动。

## 15. T480 资源 profile

目标机器：ThinkPad T480，16GB RAM，约 133GB 可用磁盘。

默认调优：

- `worker concurrency = 1`
- `ARCHIVE_BATCH_INTERVAL=30m`
- `EMBED_BATCH_SIZE=8`
- `RERANK_BATCH_SIZE=8`
- `MAX_PARALLEL_INDEX_JOBS=1`
- `MAX_TURN_EVENT_BYTES=256KB`
- `MAX_TOOL_OUTPUT_BYTES=64KB`
- PostgreSQL `shared_buffers=512MB`
- PostgreSQL `work_mem=8MB`
- PostgreSQL `maintenance_work_mem=128MB`
- PostgreSQL `max_connections=50`
- Redis `maxmemory=256mb`
- Redis `maxmemory-policy=allkeys-lru`
- Qdrant 单节点、单 collection、`max_search_threads=2`

备份：

- 每日备份 PostgreSQL。
- 每日备份 Markdown Archive。
- 每日创建 Qdrant snapshot。
- 保留 30 天。
- 总容量建议预留 40GB。

## 16. Phase Roadmap

### Phase 1：基础设施骨架

目标：做到可运行、可测试、可迭代。

范围：

- Go/Hertz API。
- worker skeleton。
- MCP server skeleton。
- Nuxt 管理台 skeleton。
- PostgreSQL、Redis、Qdrant、Docker Compose。
- `/healthz`。
- Swagger。
- zap JSON 日志。
- pgx migrations。
- T480 profile。

验收：

- `go test ./...` 通过。
- 前端 `npm run build` 或对应 Nuxt build 通过。
- `docker-compose up -d` 可启动基础服务。
- `/healthz` 返回健康状态。

### Phase 2：Auth、Tenant、Permission、Secret Vault

范围：

- users、orgs、projects、memberships、roles、resource permissions。
- password login。
- PAT。
- Adapter Token。
- Secret Vault。
- 工具自动解密注入审计。

### Phase 3：TurnEvent 与 Adapter SDK

范围：

- 标准 TurnEvent v1。
- Adapter SDK：校验、截断、secret 识别、`secret_ref` 替换、POST Memory OS。
- Codex Adapter。
- Generic MCP Adapter。

### Phase 4：Markdown Archive

范围：

- 批量归档 TurnEvent。
- 生成 Markdown archive。
- 在线编辑。
- 版本记录。
- 审计日志。
- 编辑后生成新的 `index_generation`，旧 chunk 失效。

### Phase 5：Native Archive RAG

范围：

- Markdown chunking。
- Qdrant indexing。
- query-time payload filter。
- Archive RAG search。

### Phase 6：Native Hot Memory

范围：

- 从安全处理后的 archive/turn event 派生 hot memory。
- PostgreSQL 存 memory fact、scope、权限、热度、来源。
- Qdrant 存 hot memory vector。
- access_count、used_count、hot_score、promote/demote。

### Phase 7：Unified Retrieval

范围：

- `/memory/search`。
- MCP `memory_search`。
- 权限上下文 -> Hot Memory -> RAG -> rerank -> 压缩 -> mark_used。
- 同一用户跨 Agent 可访问同一 project/org/user scope 下的记忆。

### Phase 8：多 Agent 扩展

范围：

- Claude Code Adapter。
- opencode Adapter。
- Hermes Adapter。
- Generic Transcript Importer。
- 所有 Adapter 输出同一种 TurnEvent。

### Phase 9：Nuxt 管理台完整化

范围：

- 用户、组织、项目、权限。
- Archive 浏览、编辑、版本、重索引。
- Hot Memory 管理。
- Secret Vault 管理。
- Adapter Token 管理。
- Qdrant 索引状态。
- 检索测试页和热度统计。

### Phase 10：兼容与迁移

范围：

- mem0 importer。
- FastGPT importer。
- OpenMemory/Zep/Khoj 后续迁移入口。
- 可选导出为 Markdown/RAG bundle。

## 17. 开发与验证规则

- 改代码前必须读 README、AGENTS 和本 spec。
- 所有代码改动前必须创建 TodoList。
- 代码改动必须补测试。
- 后端测试使用 `go test ./...`。
- 前端至少保证 `npm run build` 或对应 Nuxt build 通过。
- 不提交、不部署，除非用户明确要求。
- 纯文档修改可以不跑测试，但必须说明没有验证运行时行为。

## 18. 关键风险

### 18.1 Secret 污染风险

风险：Secret 明文进入 Archive、Qdrant、Hot Memory 或日志后很难清理。

处理：Secret Vault 和 sanitizer 必须在 TurnEvent 大规模写入前上线。

### 18.2 权限过滤风险

风险：先召回后过滤可能泄露跨用户、跨组织数据。

处理：Qdrant 查询必须 query-time payload filter，并在测试中覆盖跨租户隔离。

### 18.3 单机资源风险

风险：T480 内存和磁盘有限，索引、rerank、备份可能影响可用性。

处理：默认低并发、小 batch、保留期限制和每日备份容量预算。

### 18.4 规格漂移风险

风险：旧 mem0/FastGPT 方案影响 v0.4 原生实现。

处理：当前 spec 和 AGENTS.md 明确 v0.4 原生路线，旧方案只作为 importer/迁移参考。

## 19. Model Gateway And Provider Configuration

Memory OS v0.4 需要统一模型网关，业务代码不得直接耦合具体模型供应商。

### 19.1 Provider 配置

当前默认模型配置：

- LLM base URL：通过 `LLM_BASE_URL` 注入。
- LLM model：`MiniMax-M2.7`。
- Embedding model：`bge-m3`。
- Rerank model：`bge-reranker-v2-m3`。

API key、token、密码不得写入代码、文档、`.env.example`、日志、测试快照或回复。真实值只能通过本机环境变量、部署环境 secret 或 Secret Vault 注入。

`.env.example` 只能包含占位值：

```env
LLM_BASE_URL=http://example.local:8000
LLM_API_KEY=replace-me
LLM_MODEL=MiniMax-M2.7
EMBEDDING_MODEL=bge-m3
RERANK_MODEL=bge-reranker-v2-m3
```

如果真实 API key 已经出现在聊天、日志或提交记录中，正式部署前必须轮换。

### 19.2 模型网关目录

建议目录：

```text
internal/llm/
  client.go              # 统一接口
  openai_compatible.go   # OpenAI-compatible provider
  embedding.go           # embedding request/response
  rerank.go              # rerank request/response
  errors.go              # 错误分类
```

最低接口：

```go
type ChatClient interface {
    Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

type EmbeddingClient interface {
    Embed(ctx context.Context, texts []string) (EmbeddingResponse, error)
}

type RerankClient interface {
    Rerank(ctx context.Context, query string, documents []string) (RerankResponse, error)
}
```

### 19.3 Embedding 契约

输入：

- `[]string` 文本数组。
- 每个文本必须已完成 Secret 脱敏。
- 单批默认不超过 `EMBED_BATCH_SIZE=8`。

输出：

- 向量数组。
- 向量维度。
- provider trace ID，如果有。
- token 或字符估算，如果 provider 返回。

失败策略：

- Archive indexing 失败：任务重试，不写入“已索引”状态。
- Hot Memory indexing 失败：PostgreSQL 事实仍可保存，但 vector 状态标记为 pending。
- provider 认证失败：返回明确配置错误，不打印 key。

### 19.4 Rerank 契约

输入：

- query。
- candidate documents。
- candidate source refs。
- 单批默认不超过 `RERANK_BATCH_SIZE=8`。

输出：

- candidate index。
- rerank score。
- provider trace ID，如果有。

失败策略：

- rerank 失败时，不中断检索。
- 降级使用 Qdrant vector score + Hot Memory hot_score 排序。
- 响应 metadata 标记 `rerank_degraded=true`。

## 20. AI Self-Driven Implementation Contract

为了让 AI 实现者尽量一次性跑通，项目必须提供固定命令、固定验收口径和固定 smoke test。

### 20.1 标准命令

Phase 1 必须提供以下命令或等价 Makefile target：

```bash
make dev-up       # 启动 PostgreSQL、Redis、Qdrant、API、worker、MCP、web
make dev-down     # 停止开发环境
make test         # 后端 go test ./...，如有前端单测也一并执行
make build-web    # 前端 Nuxt build
make smoke        # 端到端 smoke test
make lint         # 可选，执行 gofmt/go vet/npm lint 等
```

如果某个 target 在当前阶段还不能完整实现，必须返回清晰错误，不能静默成功。

### 20.2 配置文件

必须提供：

- `.env.example`：所有环境变量名和安全占位值。
- `deploy/docker-compose.yml`：基础 compose。
- `deploy/docker-compose.t480.yml`：T480 profile 或 override。
- `deploy/nginx.conf`：Nuxt 静态托管配置。

`.env` 不得提交。

### 20.3 启动降级

Phase 1 的 API 应在缺少 LLM provider 配置时仍能启动 `/healthz`。

需要模型能力的接口应返回明确错误：

```json
{
  "error": "model_provider_not_configured",
  "message": "LLM_API_KEY or LLM_BASE_URL is not configured"
}
```

错误信息不得包含真实 secret。

### 20.4 Smoke Test 最小链路

`make smoke` 至少覆盖：

1. API `/healthz` 返回 ok。
2. PostgreSQL health ok。
3. Redis health ok。
4. Qdrant health ok。
5. Qdrant collection 已创建或可创建。
6. 模型 provider 配置存在时，embedding probe 成功。
7. 模型 provider 配置存在时，rerank probe 成功。
8. 写入测试 TurnEvent。
9. 生成或模拟 Archive chunk。
10. 写入 Qdrant。
11. 执行 filtered search。
12. 验证跨用户数据不会被召回。

### 20.5 开发用 seed

开发环境可以提供 seed 命令或 dev-only endpoint：

```bash
make seed-dev
```

或：

```text
POST /dev/smoke/seed
POST /dev/smoke/search
```

要求：

- 默认仅在 `APP_ENV=development` 时启用。
- 生产环境返回 404 或 disabled。
- seed 数据不得包含真实 secret。
- seed 必须可重复执行且幂等。

## 21. Testing Strategy

### 21.1 测试分层

测试分为：

- Unit tests：纯函数、配置解析、权限 filter 构造、Secret 脱敏、TurnEvent 校验。
- Repository tests：PostgreSQL migration、repository CRUD、事务和唯一约束。
- Integration tests：PostgreSQL、Redis、Qdrant、模型 provider 的真实或容器化集成。
- E2E smoke tests：从 TurnEvent 到 search 的最小闭环。
- Frontend build tests：Nuxt build。

### 21.2 必测场景

权限与隔离：

- user A 不能召回 user B 的 private memory。
- org A 不能召回 org B 的 archive chunk。
- project A 不能召回 project B 的 project scope memory。
- `agent_specific` 默认不能被其他 Agent 召回。
- Qdrant 查询 filter 必须在 query-time 生效。

Secret：

- 输入中的 token/API key/password 被替换为 `secret_ref`。
- Archive 不包含 Secret 明文。
- Hot Memory 不包含 Secret 明文。
- Qdrant payload 和 vector text 不包含 Secret 明文。
- 日志和错误响应不包含 Secret 明文。

Archive：

- TurnEvent 可批量归档为 Markdown。
- Archive 编辑后 `index_generation` 增加。
- 旧 chunk 失效，不参与新查询。
- Archive content hash 可检测变更。

Hot Memory：

- 可从安全处理后的输入派生 fact。
- promote/demote 更新状态和 hot_score。
- mark_used 增加 used_count。
- soft delete 后不参与召回。

Retrieval：

- Hot Memory 和 Archive RAG 结果可合并。
- rerank 成功时按 rerank score 排序。
- rerank 失败时降级排序且标记 `rerank_degraded=true`。
- 返回结果带 source refs。

### 21.3 集成测试数据

最小测试租户：

```text
org_alpha
  project_alpha
    user_alice
    user_bob
org_beta
  project_beta
    user_eve
```

最小测试内容：

- Alice 的项目部署记忆。
- Bob 的私有偏好记忆。
- Eve 的跨 org 隔离记忆。
- 一个包含假 secret 的 TurnEvent，用于验证脱敏。

测试用 secret 必须是明显假值，例如：

```text
sk-test-redacted-example
password-test-redacted
```

不得使用真实 key。

### 21.4 AI 实现者验证顺序

AI 实现者每完成一个阶段必须按顺序运行：

```bash
make test
make build-web
make smoke
```

如果失败：

1. 记录失败命令。
2. 摘要关键错误。
3. 修复最小必要代码。
4. 重新运行失败命令。
5. 全部通过后再运行完整顺序。

不得在失败时声称完成。
