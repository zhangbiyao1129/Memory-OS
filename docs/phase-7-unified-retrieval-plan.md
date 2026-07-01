# Phase 7 Unified Retrieval AI 自驱动实现计划

> 面向 AI 代理的工作者：执行本计划前必须读取 spec 和 Phase 1-6 计划。所有代码改动前创建 TodoList。除非用户明确要求，不要 commit、push、deploy。

**目标：** 实现 `/memory/search` 和 MCP `memory_search`，统一权限上下文、Hot Memory、Archive RAG、rerank、压缩、可解释来源、access log 和 mark_used。

**架构：** retrieval service 是唯一检索入口；它先构造权限上下文，再调用 Hot Memory 和 Archive RAG filtered search，合并候选后 rerank，压缩上下文，返回 source refs，并写 access log。

**技术栈：** Go、Hertz、MCP、Qdrant、bge-reranker-v2-m3、PostgreSQL、zap。

---

## 1. 成功标准

- `make test` 通过。
- `make smoke` 包含 `/memory/search`。
- 同一用户跨 Agent 可召回同 project/org/user scope 记忆。
- 不同用户默认隔离。
- `agent_specific` 默认隔离。
- rerank 成功按 rerank score 排序。
- rerank 失败降级且返回 `rerank_degraded=true`。
- 返回结果可追溯到 memory/archive/chunk/thread/turn/source。
- mark_used 和 access log 生效。

## 2. 推荐文件结构

```text
internal/retrieval/
  model.go
  service.go
  permissions.go
  merge.go
  rerank.go
  compress.go
  access_log.go
  service_test.go
  permissions_test.go
cmd/memory-api/handlers/memory_search.go
cmd/memory-mcp/tools/memory_search.go
migrations/000007_retrieval.sql
```

## 3. 数据库设计

最低表：

- `memory_access_logs`
- `retrieval_requests`
- `retrieval_results`

关键要求：

- access log 不保存 Secret 明文。
- result source refs 可审计。
- request id 支持幂等或追踪。

## 4. 执行任务

### 任务 1：retrieval request model

- [ ] 定义 SearchRequest。
- [ ] 定义 SearchResult。
- [ ] 定义 SourceRef。
- [ ] 定义 RetrievalContext。
- [ ] 写单测覆盖参数校验。
- [ ] 运行 `go test ./...`。

验收：非法 scope、空 query、越权 project 被拒绝。

### 任务 2：权限上下文

- [ ] 从 auth actor 构造 PermissionContext。
- [ ] 合并 user/org/project/role/permission labels。
- [ ] 写单测覆盖跨用户、跨 project、跨 org。
- [ ] 运行 `go test ./...`。

验收：权限上下文成为唯一 filter 输入。

### 任务 3：Hot Memory search adapter

- [ ] 调用 Phase 6 Hot Memory filtered search。
- [ ] 记录 access_count。
- [ ] 标准化 candidate。
- [ ] 写单测覆盖无结果、agent_specific。
- [ ] 运行 `go test ./...`。

验收：Hot Memory candidate 有 source refs。

### 任务 4：Archive RAG search adapter

- [ ] 调用 Phase 5 Archive RAG filtered search。
- [ ] 标准化 candidate。
- [ ] 写单测覆盖 index_generation、权限 filter。
- [ ] 运行 `go test ./...`。

验收：Archive candidate 有 archive_id/chunk_id。

### 任务 5：merge 与 rerank

- [ ] 合并 Hot Memory 和 Archive candidates。
- [ ] 去重。
- [ ] 调用 rerank provider。
- [ ] rerank 失败降级排序。
- [ ] 写单测覆盖成功、失败、空 candidates。
- [ ] 运行 `go test ./...`。

验收：rerank_degraded 标记正确。

### 任务 6：context compression

- [ ] 实现 token/字符预算。
- [ ] 保留高分结果和 source refs。
- [ ] 不输出 Secret 明文。
- [ ] 写单测覆盖超预算。
- [ ] 运行 `go test ./...`。

验收：压缩结果可追溯。

### 任务 7：access log 和 mark_used

- [ ] 写 retrieval request log。
- [ ] 写 result log。
- [ ] 调用 Hot Memory mark_used。
- [ ] Archive chunk 记录 used。
- [ ] 写单测覆盖失败不影响主响应或明确事务策略。
- [ ] 运行 `go test ./...`。

验收：使用统计可见。

### 任务 8：API 与 MCP

- [ ] 实现 `POST /memory/search`。
- [ ] 实现 MCP `memory_search`。
- [ ] 统一调用 retrieval service。
- [ ] 更新 Swagger。
- [ ] 写 handler/tool 测试。
- [ ] 运行 `go test ./...`。

验收：API 和 MCP 结果一致。

### 任务 9：smoke test

- [ ] seed Hot Memory 和 Archive chunk。
- [ ] 调用 `/memory/search`。
- [ ] 验证 Alice 结果。
- [ ] 验证 Eve 隔离。
- [ ] 验证 rerank 降级可用。
- [ ] 验证 mark_used。
- [ ] 运行 `make test`。
- [ ] 运行 `make smoke`。

验收：Unified Retrieval 闭环成功。

## 5. 集成测试方案

必须覆盖：

- Hot Memory 优先召回。
- Archive RAG 补充依据。
- rerank 成功和失败。
- 同用户跨 Agent 共享。
- 不同用户隔离。
- agent_specific 默认不跨 Agent。
- access log 和 mark_used。

## 6. 不做事项

Phase 7 不做：

- 新 Adapter。
- Nuxt 管理台完整化。
- importer。

## 7. 子代理建议

Phase 7 主会话为主。可交给子代理的任务：rerank 降级测试、context compression 单测、MCP `memory_search` tool schema 测试。

必须由主会话控制的任务：Unified Retrieval service 主流程、权限上下文合并、access log + mark_used 事务策略。
