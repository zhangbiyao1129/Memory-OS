# Memory OS v0.4 Agent Instructions

> 本文件只记 Memory OS 项目硬约束。通用规则以全局 `~/ai-rules/AGENTS.md` 为准。能从代码、配置、docs 读出的内容不在此重复。

## Architecture（固定）

- Go/Hertz + pgx/v5 + zap + PostgreSQL + Redis + Qdrant + Nuxt 3 + Docker Compose，单机部署到 ThinkPad T480。
- PostgreSQL 是权威元数据源；Markdown 是 Archive 内容权威源；Qdrant 只作可重建索引；Redis 只作队列/锁/缓存/限流。
- MVP 不依赖 mem0/FastGPT；mem0/FastGPT 只作后续 importer 入口。

## Hard Rules（不可违反）

- Qdrant 单 collection；所有查询必须 query-time payload filter，禁止先召回后应用层过滤。Qdrant client 不得含租户权限判断，filter 由 retrieval/service 构造传入。
- Handler 只做协议/认证/校验/错误映射；Service 拥有业务；Repository 拥有 SQL 和事务。
- Adapter 只转 TurnEvent，不写 Markdown/Hot Memory/Qdrant。
- 关键 ID（event_id/turn_id/archive_id/secret_ref/index_generation）必须有唯一约束或幂等保护。Redis 任务必须幂等重试。
- Archive 每次编辑后生成新 `index_generation`，旧 chunk 失效但保留审计历史。
- 单文件接近 700 行必须拆分。注释用简体中文。

## Security（补充全局红线）

- Secret 明文不进日志/Markdown/Qdrant/Hot Memory/Archive chunk/回复/测试快照；Secret Vault 加密存明文，其余只存 `secret_ref`。
- 工具用 Secret 时后端自动解密注入并写审计。
- 工具/命令输出截断 `MAX_TOOL_OUTPUT_BYTES=64KB`；单轮事件 `MAX_TURN_EVENT_BYTES=256KB`。

## Unified Retrieval

`/memory/search` 和 MCP `memory_search` 统一流程：权限上下文 → Hot Memory → Archive RAG → rerank → 压缩 → 可解释来源 → access log → mark_used。结果必须可追溯到 memory/archive/chunk/thread/turn/source。

## Ports

web 18080 / api 18081 / mcp 18082 / qdrant 18083；PostgreSQL、Redis 不对外。

## Codex Subagent

- 主会话控 schema/权限/安全/Secret Vault/Qdrant filter/Unified Retrieval/跨 Phase 接口；子代理做 fixture/测试/前端/文档/importer 等边界任务。
- 子代理不得 commit/push/deploy、不得写真实 secret、不得同时改同一文件。
- 高风险（schema/安全/权限/Secret/Qdrant filter/检索主流程）用 `gpt-5.5` 或主会话；普通实现 `gpt-5.4`；简单文档/fixture 可用 `gpt-5.4-mini`/`spark`。轻量模型不得碰 schema/安全/权限/Secret/Qdrant filter/检索主流程。
- 详细策略见 `docs/subagent-execution-strategy.md`。

## Workflow（项目特例）

- 本地只编辑源码，不跑容器；部署、重启或部署排障前必须先读 `DEPLOYMENT.md`，按其中 SOP 执行。
- GitHub 只作为稳定改动仓库；不要把日常部署流程改回依赖服务器 `git pull`。
- `docs/`、`specs/`、`artifacts/` 是开发过程文件，不提交、不作为日常同步内容；提交只包含真实源码、迁移、部署脚本和必要配置。
- 默认 TDD：写测试 → 确认失败 → 实现 → 通过 → 重构 → 再验证。bug 修复先补复现测试。
- 后端验证 `gofmt` + `go test ./...`（+ `go vet`）；前端 `nuxt build`；纯文档改动说明未验证运行时。
- 不自动 commit/push/deploy，除非明确要求。
- 实现计划、phase 拆解、TurnEvent 类型、目录结构、T480 调优参数、接口清单见 `docs/` 和代码。
