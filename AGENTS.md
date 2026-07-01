# Memory OS v0.4 Agent Instructions

## Global Interaction Rules

- 所有回复必须使用中文。
- 每次回复第一行固定为：`好的傲天`。
- 除固定开头外，不再说“好的 / 明白了 / 让我来”等引导废话。
- 直接给结论，不复述问题，不做无意义夸奖。
- 简单问题一句话回答；复杂问题分点展开。
- 不确定就停下来说明哪里不清楚，不要猜。

## Core Working Principles

始终遵守：

- 不确定就问，不要猜。
- 先理解再动手；改代码前先读懂相关文件和架构。
- 只改必须改的；每一行改动都应能追溯到用户请求。
- 改完必须验证；测试通过、构建通过或明确说明无法验证后才算完成。

严格流程适用于：

- 多文件改动，通常为 3 个或更多文件。
- 重构或架构变更。
- 接手陌生代码库。
- 用户明确要求“仔细来”。

严格流程：

```text
研究（读懂代码） -> 规划（列改动清单） -> 执行（按清单改） -> 验证（测试 + Review）
```

快速流程适用于：

- 单文件小改。
- 配置修改。
- 明确的 bug 修复，且有清晰复现步骤。
- 用户明确要求“直接改”或“快”。

快速流程：

```text
理解需求 -> 改代码 -> 验证
```

## Karpathy Engineering Principles

### Think Before Coding

- 不要假设，不要隐藏困惑，必须呈现关键权衡。
- 明确陈述假设；不确定就问。
- 存在多种解释时，列出差异，不要默默选择高风险路径。
- 有更简单的方法就说出来；该推回就推回。

### Simplicity First

- 用最少代码解决当前问题。
- 不做超出要求的功能。
- 单次使用的代码不提前抽象。
- 不添加未要求的“灵活性”或“可配置性”。
- 不为极低概率场景添加复杂错误处理。

### Surgical Changes

- 只动必须动的文件和代码。
- 不顺手“改进”相邻代码、注释或格式。
- 不重构没坏的东西。
- 匹配现有风格，即使可以设计得不同。
- 发现无关死代码时只说明，不主动删除。
- 删除自己改动导致的未使用导入、变量、函数；不删除预先存在的死代码，除非用户要求。

### Goal-Driven Execution

- 把任务转成可验证目标。
- 多步任务必须有简要 TodoList。
- 每个步骤都应有可验证结果。
- 不用“让它工作”这种弱成功标准作为唯一目标。

## Codex Skill Loading Rules

Codex 以本文件作为项目级主约束入口。

- Codex 原生识别的 skills 位于全局目录：`/Users/kanyun/.codex/skills/`。
- 本项目的 `.agents/skills/` 是项目内 skill 源文件目录，不假设 Codex 会自动原生加载。
- 本项目的 `.claude/skills/` 是 Claude / Superpowers-ZH 生态使用的 skill 目录，不假设 Codex 会自动原生加载。
- 当前项目内的 20 个 skills 已同步到 Codex 全局目录 `/Users/kanyun/.codex/skills/`；Codex 重启后应从全局目录加载。
- 当任务匹配已安装的 Codex skill 时，优先使用 Codex 当前会话暴露的正式 skill；如果当前会话未暴露对应 skill，则读取项目内 `.agents/skills/<skill>/SKILL.md` 作为流程参考。
- 不要把 `.agents/skills/` 或 `.claude/skills/` 视为 Codex 自动加载目录，除非 Codex 后续明确支持项目级 skill 注册。

## Codex Subagent Rules

- 多文件、多模块、可并行任务时，主会话应先评估是否使用子代理；但不要为了并行而并行。
- 默认采用“主会话控架构 + 子代理做边界清晰任务”的模式。
- 主会话负责 schema、权限、安全、Secret Vault、Qdrant query-time filter、Unified Retrieval、跨 Phase 接口一致性、最终审查和验证。
- 子代理适合处理 fixture、普通测试、前端页面、文档/API 补充、importer/exporter 等边界清楚且写入范围独立的任务。
- 同一时间不得让多个子代理修改同一个文件。
- 子代理不得 commit、push、deploy，不得写入真实 secret。
- 子代理默认继承主会话模型；没有明确理由时不要指定 `model`。
- 架构、安全、权限、Secret、Qdrant filter、Unified Retrieval 等高风险任务使用主会话或继承 `gpt-5.5`。
- 普通实现、普通单测、Adapter/importer fixture 可在明确低风险时使用 `gpt-5.4`。
- 简单文档检查、重复性页面骨架、低风险 fixture 生成可在明确低风险时使用 `gpt-5.4-mini` 或 `gpt-5.3-codex-spark`。
- 轻量模型不得用于 schema 初版、安全边界、权限规则、Secret 处理、Qdrant query-time filter 或 Unified Retrieval 主流程。
- 详细策略见 `docs/subagent-execution-strategy.md`。

## Project Context

Memory OS 是一个 Native Multi-Agent Memory Platform，用于把 Codex、Claude Code、opencode、Hermes 以及其它 Agent 平台中的对话、项目经验、排障记录、用户偏好、Secret 引用和长期决策沉淀为可召回、可追溯、可治理的统一记忆系统。

当前目标版本是 Memory OS v0.4。架构固定为：

- Go / CloudWeGo Hertz 后端。
- PostgreSQL 作为权威元数据源。
- Redis 作为队列、缓存、锁和限流组件。
- Qdrant 作为向量检索后端。
- Markdown 文件作为 Archive 内容权威源。
- Nuxt 3 管理台。
- Docker Compose 单机部署，目标机器为 ThinkPad T480。

MVP 不依赖 mem0 或 FastGPT。Hot Memory 和 Archive RAG 都必须原生实现。

## Product Decisions

- 支持多用户、多组织、多项目的 Org 模式。
- 支持多 Agent 共享记忆：同一用户跨 Codex、Claude Code、opencode、Hermes 可共享 user/project/org scope 下的记忆。
- 不同用户默认隔离。
- `agent_specific` 记忆默认不跨 Agent 召回。
- PostgreSQL 是权威元数据源；任何不可丢失的数据必须先落 PostgreSQL。
- Markdown Archive 文件是 Archive 内容权威源；数据库保存元数据、版本、索引状态和审计信息。
- Qdrant 使用单 collection；所有查询必须使用 query-time payload filter，不允许先召回后在应用层过滤。
- Archive RAG、Hot Memory、检索审计、热度统计全部原生实现。
- mem0、FastGPT、OpenMemory、Zep、Khoj 只作为后续 importer 或迁移入口，不作为 MVP 运行依赖。

## Security And Secret Rules

- Secret 明文不得进入日志、Markdown、Qdrant、Hot Memory、Archive chunk、聊天回答或测试快照。
- Secret Vault 加密保存明文；记忆、Archive、RAG、事件和审计日志只能保存 `secret_ref`。
- Adapter SDK 必须在写入 Memory OS 前执行 secret 识别、脱敏、`secret_ref` 替换和截断。
- 工具使用 Secret 时由后端自动解密注入，并写审计日志。
- 不得在代码、配置样例、日志、memory、Qdrant payload、Markdown 或回复中明文记录 API key、token、cookie、私钥、助记词、密码。
- 命令输出和工具输出必须截断并保留 hash；默认 `MAX_TOOL_OUTPUT_BYTES=64KB`。
- 单轮事件最大默认 `MAX_TURN_EVENT_BYTES=256KB`。
- API key、token、密码、私钥、cookie、助记词不得明文写入代码、配置样例、日志、memory、mem0、Qdrant、Markdown、测试快照或回复。
- 对外发布、删除、覆盖、部署、重启线上服务等难以回滚操作前，必须先确认。
- 如果测试失败、验证跳过、权限被拒绝，要如实说明。

## Required Stack

Backend:

- Go >= 1.25。若本机 Go 版本低于要求，必须在 `go.mod` 的 `toolchain` 或文档中明确处理。
- CloudWeGo Hertz。
- `pgx/v5`。
- `zap` JSON structured logging。
- `swaggo/swag` for Swagger/OpenAPI。
- PostgreSQL migrations。

Frontend:

- Nuxt 3。
- Vue 3。
- Tailwind CSS。
- Pinia。
- Nuxt UI 或 shadcn-vue 二选一，并保持一致。

Infrastructure:

- PostgreSQL。
- Redis。
- Qdrant。
- Docker Compose。
- Nuxt 静态构建 + Nginx 托管。

## Repository Shape

优先使用以下目录结构，除非已有实现形成了清晰替代结构：

- `cmd/memory-api`：Hertz API 服务。
- `cmd/memory-worker`：归档、索引、热度治理、备份等后台 worker。
- `cmd/memory-mcp`：通用 MCP server。
- `cmd/memory-adapter`：本地 adapter runner，可选。
- `internal/config`：配置加载与校验。
- `internal/logger`：zap 初始化。
- `internal/db`：pgx pool、migration、repository。
- `internal/redis`：Redis queue、lock、cache。
- `internal/qdrant`：Qdrant collection、index、search client。
- `internal/auth`：登录、PAT、Adapter Token。
- `internal/tenant`：users、orgs、projects、memberships、roles、permissions。
- `internal/secret`：Secret Vault、加密、注入、审计。
- `internal/adapter`：Codex、Claude Code、opencode、Hermes、Generic adapter。
- `internal/eventlog`：TurnEvent v1 校验和持久化。
- `internal/archive`：Markdown Archive、版本、编辑、chunking。
- `internal/hotmemory`：原生 Hot Memory fact、scope、热度。
- `internal/retrieval`：统一检索、rerank、压缩、mark_used。
- `internal/audit`：审计日志。
- `internal/jobs`：后台任务和幂等重试。
- `web/`：Nuxt 管理台。
- `migrations/`：PostgreSQL migrations。
- `deploy/`：Dockerfile、compose、nginx、T480 profile。
- `docs/`：规格、运维、部署、API 文档。

## Backend Architecture Rules

- Handler 只做协议适配、认证、参数校验、调用 service、错误映射。
- Service 拥有业务流程。
- Repository 拥有 SQL 和事务边界。
- Qdrant client 不得包含租户权限判断；权限过滤条件由 retrieval/service 层构造并强制传入。
- Adapter 只负责把平台事件转换成标准 `TurnEvent`，不负责生成 Markdown、写 Hot Memory 或直接写 Qdrant。
- Redis 任务必须支持幂等重试；所有后台任务必须有 idempotency key。
- `event_id`、`turn_id`、`archive_id`、`secret_ref`、`index_generation` 等关键 ID 必须有唯一约束或幂等保护。
- 单文件接近 700 行前必须拆分。
- 代码注释使用简体中文；外部工具要求固定英文短语时除外。

## Standard TurnEvent v1

所有 Agent 平台必须被 Adapter 转换成统一事件协议。最低要求：

- 必须能生成 `user_message`。
- 必须能生成 `assistant_final` 或 `turn_completed`。
- 如果不能拿工具调用明细，至少生成 `tool_output_summary` 或空数组。
- 如果不能实时采集，允许通过 transcript importer 延迟生成。

事件类型包括：

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

Adapter 写入前必须完成：

- 统一时间格式。
- 统一 user/org/project/thread/turn/session 标识。
- 敏感字段脱敏和 `secret_ref` 替换。
- 工具输出摘要与截断。
- 文件路径标准化。
- 命令输出 hash 化。

## Archive And RAG Rules

- Markdown Archive 是一等公民，是 Archive 内容权威源。
- 每次在线编辑 Archive 后必须生成新的 `index_generation`，旧 chunk 失效但保留可审计历史。
- Archive chunking 后写入 Qdrant；payload 必须包含 `user_id`、`org_id`、`project_id`、`archive_id`、`visibility`、permission labels、`index_generation`。
- Qdrant 查询必须带 query-time payload filter，按 user/org/project/visibility/permission labels 过滤。
- 不允许先向量召回再在应用层做租户或权限过滤。
- Archive 浏览、编辑、版本、重索引和审计必须在 Nuxt 管理台可治理。

## Hot Memory Rules

- Hot Memory 从安全处理后的 archive/turn event 派生。
- PostgreSQL 保存 memory fact、scope、权限、热度、来源和生命周期。
- Qdrant 保存 hot memory vector；payload 同样必须支持 query-time filter。
- 支持 `access_count`、`used_count`、`hot_score`、promote、demote。
- Hot Memory 不保存 Secret 明文，只保存 `secret_ref`。
- `agent_specific` Hot Memory 默认只在同 Agent 召回，除非显式提升为跨 Agent scope。

## Unified Retrieval Rules

`/memory/search` 和 MCP `memory_search` 必须使用统一流程：

1. 构造权限上下文。
2. 检索 Hot Memory。
3. 检索 Archive RAG。
4. rerank。
5. 上下文压缩。
6. 返回可解释来源。
7. 记录 access log。
8. 最终使用后 mark_used。

检索结果必须可追溯到 memory/archive/chunk/thread/turn/source。

## API And MCP Requirements

Phase 1 至少提供：

- `GET /healthz`
- Swagger/OpenAPI 页面或 JSON。
- JSON zap 日志。
- pgx migrations。

后续接口应保留演进空间：

- `POST /memory/turn-event`
- `POST /memory/search`
- `POST /memory/archive/run`
- `POST /memory/promote`
- `POST /memory/demote`
- `GET /memory/stats`
- Secret Vault、Auth、Tenant、Archive、Hot Memory、Adapter Token 管理接口。

MCP server 至少规划：

- `memory_search`
- `memory_archive`
- `memory_append_event`
- `memory_get_archive`
- `memory_mark_used`
- `memory_stats`

## Docker Compose And Ports

默认端口：

- `memory-web`: `18080`
- `memory-api`: `18081`
- `memory-mcp`: `18082`
- `qdrant`: `18083`
- PostgreSQL 和 Redis 默认不对外暴露。

Compose 必须支持 T480 单机 profile，并能通过 docker-compose 命令启动。

## T480 Resource Profile

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

备份要求：

- 每日备份 PostgreSQL。
- 每日备份 Markdown Archive。
- 每日创建 Qdrant snapshot。
- 保留 30 天。
- 总容量建议预留 40GB。

## Implementation Phases

Phase 1：基础设施骨架。

- Go/Hertz API、worker、MCP server、Nuxt 管理台、PostgreSQL、Redis、Qdrant、Docker Compose。
- `/healthz`、Swagger、zap JSON 日志、pgx migrations。
- Compose 支持 T480 profile 和 `docker-compose up -d`。

Phase 2：Auth、Tenant、Permission、Secret Vault。

- users、orgs、projects、memberships、roles、resource permissions。
- password login、PAT、Adapter Token。
- Secret Vault 前置上线。
- 工具自动解密注入时写审计日志。

Phase 3：TurnEvent 与 Adapter SDK。

- 标准 TurnEvent v1。
- Adapter SDK：校验、截断、secret 识别、`secret_ref` 替换、POST Memory OS。
- 先做 Codex Adapter + Generic MCP Adapter。

Phase 4：Markdown Archive。

- 批量归档 turn events。
- 生成 Markdown archive。
- 支持在线编辑、版本记录、审计日志。
- 编辑后生成新的 `index_generation`，旧 chunk 失效。

Phase 5：Native Archive RAG。

- Markdown chunking。
- Qdrant indexing。
- 所有 Qdrant 查询必须 query-time payload filter。
- payload 包含 user/org/project/archive/visibility/permission labels。

Phase 6：Native Hot Memory。

- 从安全处理后的 archive/turn event 派生 hot memory。
- PostgreSQL 存 memory fact、scope、权限、热度、来源。
- Qdrant 存 hot memory vector。
- 支持 access_count、used_count、hot_score、promote/demote。

Phase 7：Unified Retrieval。

- `/memory/search` 与 MCP `memory_search`。
- 检索顺序：权限上下文、Hot Memory、RAG、rerank、压缩、mark_used。
- 同一用户跨 Agent 可访问同一 project/org/user scope 下的记忆。

Phase 8：多 Agent 扩展。

- Claude Code Adapter。
- opencode Adapter。
- Hermes Adapter。
- Generic Transcript Importer。
- 所有 Adapter 输出同一种 TurnEvent。

Phase 9：Nuxt 管理台完整化。

- 用户、组织、项目、权限。
- Archive 浏览、编辑、版本、重索引。
- Hot Memory 管理。
- Secret Vault 管理。
- Adapter Token 管理。
- Qdrant 索引状态。
- 检索测试页和热度统计。

Phase 10：兼容与迁移。

- mem0 importer。
- FastGPT importer。
- OpenMemory、Zep、Khoj 后续迁移入口。
- 可选导出为 Markdown/RAG bundle。

## Development Workflow

- 处理代码任务时，优先读取 `README.md`；不存在则跳过并说明一次，不要反复尝试。
- 改代码前必须先理解相关文件、架构和现有风格。
- 所有代码改动前必须创建 TodoList。
- 复杂任务，包括多文件、新功能、重构，拆成 3 到 7 个具体步骤。
- 每个步骤必须可执行、可验证；完成一项立即标记。
- 代码改动必须补测试；bug 修复必须补充能覆盖该 bug 的测试用例。
- 后端验证默认使用 `go test ./...`。
- 前端改动至少验证 `npm run build` 或对应 Nuxt build，例如 `npx nuxt build`。
- 大范围修改，包括多文件重构、接口变更、数据模型调整，修改前后都应跑全量测试；如果无法执行，必须说明原因。
- 纯文档修改可以不跑测试，但必须说明这是文档改动且未验证运行时行为。
- 不提交、不合并、不推送、不部署，除非用户明确要求。
- 测试通过只是允许提交或部署的前置条件，不代表自动提交或自动部署。
- 难以回滚的操作前必须确认。
- spec 与用户当前明确决策冲突时，以用户当前明确决策为准。

## TDD Requirements

默认代码改动遵循 TDD：

```text
写或补测试 -> 确认测试失败或覆盖缺口 -> 写实现 -> 跑测试通过 -> 必要时重构 -> 再验证
```

强制要求：

- 改业务代码前优先写或补测试。
- 测试必须覆盖正常路径和异常路径。
- bug 修复必须先补复现测试，或说明现有测试已覆盖。
- 无测试框架、骨架阶段或纯配置/文档修改时，可以不补测试，但必须说明原因。

## Idempotency Rules

- API 接口设计应支持幂等；重复调用不能造成不一致状态。
- 数据库写操作优先使用唯一索引、幂等 key、`ON CONFLICT DO UPDATE` 或等价机制。
- 消息消费必须处理重复消息。
- 关键操作记录 request ID、event ID 或 idempotency key 去重。

## Tool And MCP Preferences

- 需要新能力时，优先使用当前会话可用的 ToolSearch 搜索可用工具，再决定方案。
- 处理不确定的技术栈或开源项目时，优先查官方文档；如果当前会话暴露 searxng MCP，可优先使用 searxng 搜索。
- 浏览器操作优先使用当前会话可用的 Chrome 或 in-app browser 工具；未暴露 chrome-bridge 时使用可用替代方案并说明。
- 同一命令失败 3 次后，先分析原因再继续，不要机械重试。

## Memory Rules

- 当用户明确说“记住 / 记下来 / 帮我记 / 保存到记忆 / 以后记得 / 这点记一下”时，才执行记忆写入。
- 当用户提到“记得 / 回忆 / 之前 / 以前 / 上次 / 之前配置过 / 之前怎么做的”等关键词时，才执行记忆搜索。
- 记忆内容优先归当前项目桶；只要出现项目名、仓库、路径、服务名、具体配置、具体报错、部署细节，就优先归项目，不进通用桶。
- 通用记忆只保留跨目录成立的抽象规则、通用流程和通用方法。
- 不确定时默认归项目，不归通用。
- 如果当前环境暴露 mem0 MCP 且用户明确要求写入或检索记忆，可按本地项目 memory + mem0 双写或双查；如果未暴露 mem0 MCP，不要声称 mem0 已写入或已验证。
- Memory OS v0.4 自身的 MVP 仍不依赖 mem0 或 FastGPT；mem0/FastGPT 只作为后续 importer 或迁移入口。

## Server Operations

- 操作服务器前先读取 `~/.codex/servers.md` 获取连接信息。
- 部署新服务前必须检查端口冲突，优先在目标服务器执行 `ss -tlnp`。
- 若新增长期服务端口，更新对应项目 memory；如果 mem0 MCP 可用且用户要求同步，再同步 mem0。
- 内网服务器访问外网可使用 Mihomo 代理：`http://admin:mihomo8121@ddns.08121.top:9999`。

## Git Rules

- 除非用户明确要求，不要 commit、merge、push、rebase、tag 或创建 PR。
- 除非用户明确要求使用特定分支，Git 操作默认基于 `main` 分支。
- 测试通过后才允许提交，但不能自动提交。
- 不要 amend commit，除非用户明确要求。
- 不要使用破坏性命令，例如 `git reset --hard` 或 `git checkout --`，除非用户明确批准。

## SSH And GitLab Credentials

- `gitlab.zhenguanyu.com` 默认使用 `~/.ssh/id_ed25519_gitlab`；认证检查命令：`ssh -T gitlab.zhenguanyu.com`。
- `gitlab-ee.zhenguanyu.com` 默认复用当前 SSH 配置中的 GitLab 凭据；认证检查命令：`ssh -T git@gitlab-ee.zhenguanyu.com`。
- 两个 GitLab 域名都应已在 `~/.ssh/known_hosts` 中登记；克隆仓库时优先使用 SSH 地址。
- 如需排查凭据，优先检查 `~/.ssh/config` 中对应 `Host` 段与 `IdentityFile` 配置。
- 不要在日志、memory、mem0 或回复中明文记录私钥内容。

## MCP Project Rules

- MCP 注册信息在 `~/.codex.json`，使用 Codex MCP 相关命令管理。
- 项目级 MCP 可放项目根目录 `.mcp.json` 或 `~/.codex/.mcp.json`。

Phase 10：兼容与迁移。

- mem0 importer。
- FastGPT importer。
- OpenMemory、Zep、Khoj 后续迁移入口。
- 可选导出为 Markdown/RAG bundle。

## Development Workflow

- 改代码前必须先读取当前仓库 README、AGENTS、spec 或相关设计文档。
- 多文件改动、新功能、重构必须先创建 TodoList。
- 先理解项目结构再动手。
- 所有代码改动必须补测试。
- 后端测试用 `go test ./...`。
- 前端至少保证 `npm run build` 或对应 Nuxt build 通过。
- 不要提交，除非用户明确要求。
- 纯文档修改可不跑测试，但必须说明未运行测试及原因。
- 发现 spec 与用户当前明确决策冲突时，以用户当前明确决策为准，并在回复中说明。

## Validation Expectations

完成代码变更前必须运行最窄但有意义的验证：

- 后端：`gofmt`、`go test ./...`，必要时加 `go vet`。
- 前端：`npm run build` 或 Nuxt 对应 build 命令。
- Docker/部署相关：能静态检查 compose 配置时执行配置校验。
- API：至少验证 `/healthz` 和 Swagger/OpenAPI 生成或访问路径。
- 无法运行验证时必须如实说明原因，不得声称通过。
