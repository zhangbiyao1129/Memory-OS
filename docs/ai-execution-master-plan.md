# Memory OS v0.4 AI 执行总控计划

> 面向 AI 代理的工作者：这是整个项目的入口文档。开始任何实现前必须先读 `AGENTS.md`、`docs/memory-os-spec.md` 和本文件。所有代码改动前创建 TodoList。除非用户明确要求，不要 commit、push、deploy。

## 1. 总目标

构建 Memory OS v0.4：一个 Native Multi-Agent Memory Platform。

固定架构：

- Go/Hertz API。
- PostgreSQL 权威元数据源。
- Redis 队列、缓存、锁、限流。
- Qdrant 单 collection 向量检索。
- Markdown Archive 正文权威源。
- Nuxt 3 管理台。
- Docker Compose 单机部署，目标 ThinkPad T480。

核心约束：

- MVP 不依赖 mem0/FastGPT。
- Hot Memory 原生实现。
- Archive RAG 原生实现。
- Qdrant 查询必须 query-time payload filter。
- Secret 明文只进 Secret Vault，其他地方只存 `secret_ref`。
- 不同用户默认隔离。
- 同一用户跨 Agent 可共享 user/project/org scope 记忆。
- `agent_specific` 默认不跨 Agent 召回。

## 2. 执行顺序

必须按顺序执行：

1. `docs/phase-1-implementation-plan.md`
2. `docs/phase-2-auth-tenant-secret-plan.md`
3. `docs/phase-3-turnevent-adapter-plan.md`
4. `docs/phase-4-markdown-archive-plan.md`
5. `docs/phase-5-native-archive-rag-plan.md`
6. `docs/phase-6-native-hot-memory-plan.md`
7. `docs/phase-7-unified-retrieval-plan.md`
8. `docs/phase-8-multi-agent-adapters-plan.md`
9. `docs/phase-9-nuxt-admin-plan.md`
10. `docs/phase-10-importers-migration-plan.md`

不要跳阶段。后续 Phase 可以预留接口，但不能依赖尚未完成的功能作为已存在能力。

## 3. 每个 Phase 的固定流程

每个 Phase 都执行：

```text
读取上下文 -> 创建 TodoList -> 写/补测试 -> 实现 -> 运行阶段测试 -> 运行完整验证 -> 更新文档缺口
```

固定命令：

```bash
make test
make build-web
make smoke
```

如果某阶段没有前端改动，也仍应确认 `make build-web` 不被破坏。

## 4. 失败处理规则

任何验证失败：

1. 停止继续下一个任务。
2. 记录失败命令。
3. 摘要关键错误。
4. 定位最小必要修改。
5. 修复后重新运行失败命令。
6. 当前失败命令通过后，再运行完整验证顺序。

同一命令失败 3 次后，必须停下来总结原因和可选方案，不要机械重试。

## 5. 安全门禁

任何阶段都不得违反：

- API key、token、密码、私钥、cookie、助记词不得写入代码、文档、日志、memory、Qdrant、Markdown、测试快照或回复。
- `.env.example` 只能使用占位值。
- 真实 provider key 只能通过环境变量、部署 secret 或 Secret Vault 注入。
- Secret 明文不得进入 Archive、Hot Memory、Qdrant、日志、审计或聊天回答。
- Secret 注入工具必须写审计日志。

## 6. 权限门禁

任何检索相关阶段都必须证明：

- user A 不能召回 user B private memory。
- org A 不能召回 org B archive chunk。
- project A 不能召回 project B project scope memory。
- `agent_specific` 默认不能被其他 Agent 召回。
- Qdrant 查询必须 query-time payload filter。
- 不允许先召回后应用层过滤。

## 7. 数据权威边界

实现时必须保持：

- PostgreSQL 是 metadata 和 Hot Memory 正文权威源。
- Markdown 文件是 Archive 正文权威源。
- Qdrant 可重建，不是权威源。
- Redis 可丢弃，不是权威源。
- Secret Vault 是 Secret 明文唯一存储位置。

## 8. 模型配置

默认模型：

- LLM：`MiniMax-M2.7`
- Embedding：`bge-m3`
- Rerank：`bge-reranker-v2-m3`

环境变量：

```env
LLM_BASE_URL=http://example.local:8000
LLM_API_KEY=replace-me
LLM_MODEL=MiniMax-M2.7
EMBEDDING_MODEL=bge-m3
RERANK_MODEL=bge-reranker-v2-m3
```

不要把真实 key 写入任何文件。

## 9. 最终项目完成标准

全部 Phase 完成后，必须满足：

- `make test` 通过。
- `make build-web` 通过。
- `make smoke` 通过。
- Docker Compose 可启动完整系统。
- `/healthz` 可用。
- Swagger/OpenAPI 可用。
- TurnEvent 可写入。
- Archive 可生成、编辑、版本化、重索引。
- Archive RAG 可 filtered search。
- Hot Memory 可派生、检索、promote/demote、mark_used。
- `/memory/search` 和 MCP `memory_search` 可用。
- 多 Agent Adapter 输出同一种 TurnEvent。
- Nuxt 管理台可管理核心资源。
- importer 可 dry-run 和导入 fixture。
- Secret 明文不出现在禁止位置。
- 跨用户、跨 org、跨 project、跨 Agent 隔离测试通过。

## 10. 禁止事项

- 不要自动 commit、push、deploy。
- 不要引入 mem0/FastGPT 作为 MVP 运行依赖。
- 不要把 PostgreSQL/Redis 暴露到公网端口。
- 不要用没有权限 filter 的 Qdrant search。
- 不要为了让测试通过而删除安全或权限断言。
- 不要跳过失败验证并声称完成。

## 11. 子代理执行策略

执行本项目时优先采用“主会话控架构 + 子代理做边界清晰任务”的模式。详细规则见 `docs/subagent-execution-strategy.md`。

默认原则：

- 主会话负责 schema、权限、安全、Qdrant filter、Unified Retrieval 和最终集成。
- 子代理负责 fixture、测试、前端页面、importer/exporter 等边界清楚的任务。
- 同一时间不得让多个子代理修改同一个文件。
- 子代理不得 commit、push、deploy。
- 子代理完成后，主会话必须审查、集成并运行验证。
