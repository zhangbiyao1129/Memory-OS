# Phase 5 Native Archive RAG AI 自驱动实现计划

> 面向 AI 代理的工作者：执行本计划前必须读取 spec 和 Phase 1-4 计划。所有代码改动前创建 TodoList。除非用户明确要求，不要 commit、push、deploy。

**目标：** 对 Markdown Archive 进行 chunking、embedding、Qdrant indexing，并实现严格 query-time payload filter 的 Archive RAG 检索。

**架构：** PostgreSQL 保存 chunk metadata 和 index_generation；Markdown 文件是 chunking 输入权威源；Qdrant 单 collection 保存 vector 与 payload；retrieval 构造权限 filter 后进行向量检索。

**技术栈：** Go、Qdrant、bge-m3 embedding、pgx/v5、Redis jobs、Markdown chunker、query-time payload filter。

---

## 1. 成功标准

- `make test` 通过。
- `make smoke` 包含 Archive RAG indexing 和 filtered search。
- Archive 可 chunking。
- chunk 可 embedding。
- Qdrant 单 collection 可写入 archive chunk。
- 查询必须带 query-time payload filter。
- 跨 user/org/project 数据无法召回。
- 编辑 Archive 后旧 `index_generation` chunk 不参与查询。

## 2. 推荐文件结构

```text
internal/archive/chunker.go
internal/archive/chunker_test.go
internal/rag/
  indexer.go
  repository.go
  search.go
  filter.go
  service.go
  service_test.go
internal/qdrant/filter.go
internal/qdrant/search.go
internal/jobs/rag_index_worker.go
migrations/000005_archive_rag.sql
cmd/memory-api/handlers/rag.go
```

## 3. 数据库设计

最低表：

- `archive_chunks`
- `archive_index_jobs`
- `qdrant_points`

关键字段：

- `chunk_id`
- `archive_id`
- `org_id`
- `project_id`
- `user_id`
- `visibility`
- `permission_labels`
- `index_generation`
- `content_hash`
- `vector_status`

## 4. 执行任务

### 任务 1：chunk schema 与 migration

- [ ] 创建 chunk metadata 表。
- [ ] 创建 index job 表。
- [ ] 添加 archive_id + index_generation 索引。
- [ ] 写 repository 测试。
- [ ] 运行 `go test ./...`。

验收：同一 generation 的 chunk 可批量失效。

### 任务 2：Markdown chunker

- [ ] 实现 Markdown chunker。
- [ ] 保留标题层级和 source refs。
- [ ] 控制 chunk 大小。
- [ ] 不输出 Secret 明文。
- [ ] 写 golden 测试。
- [ ] 运行 `go test ./...`。

验收：chunk 稳定、可追溯。

### 任务 3：Qdrant collection schema

- [ ] 确保单 collection。
- [ ] 定义 `doc_type=archive_chunk` payload。
- [ ] 建立 payload index，如果 Qdrant 支持。
- [ ] 写 collection ensure 测试或 smoke。
- [ ] 运行 `go test ./...`。

验收：Archive chunk 和 Hot Memory 可共用 collection。

### 任务 4：embedding indexer

- [ ] 读取 Markdown Archive。
- [ ] chunking。
- [ ] 调用 embedding client。
- [ ] 写 PostgreSQL chunk metadata。
- [ ] upsert Qdrant points。
- [ ] 标记 index status。
- [ ] 写单测覆盖 embedding 失败和重试。
- [ ] 运行 `go test ./...`。

验收：embedding 失败不会标记 indexed。

### 任务 5：query-time filter builder

- [ ] 根据 PermissionContext 构造 Qdrant filter。
- [ ] filter 包含 doc_type、user_id/org_id/project_id、visibility、permission_labels、index_generation。
- [ ] 禁止空权限 filter。
- [ ] 写单测覆盖跨租户隔离。
- [ ] 运行 `go test ./...`。

验收：没有 filter 的 search 在代码层不可调用。

### 任务 6：Archive RAG search

- [ ] 实现 Archive RAG search service。
- [ ] 调用 embedding query。
- [ ] 调用 Qdrant filtered search。
- [ ] 组装 source refs。
- [ ] 写单测覆盖无结果、provider failure、权限 filter。
- [ ] 运行 `go test ./...`。

验收：返回结果可追溯到 archive/chunk。

### 任务 7：index worker

- [ ] 消费 reindex job。
- [ ] 幂等处理 index_generation。
- [ ] 旧 generation 标记 stale。
- [ ] 写 worker 单测覆盖重复 job。
- [ ] 运行 `go test ./...`。

验收：Archive 编辑后旧 chunk 不参与查询。

### 任务 8：smoke test

- [ ] seed Archive。
- [ ] 执行 indexing。
- [ ] 执行 Alice filtered search。
- [ ] 执行 Eve 隔离 search。
- [ ] 编辑 Archive 并 reindex。
- [ ] 验证旧 generation 不召回。
- [ ] 运行 `make test`。
- [ ] 运行 `make smoke`。

验收：Archive RAG 端到端成功。

## 5. 集成测试方案

必须覆盖：

- Qdrant 请求包含 filter。
- 应用层不允许先召回后过滤。
- 跨 org/project/user 隔离。
- index_generation 生效。
- rerank 不在本 Phase 强制要求，但结果 source refs 必须完整。

## 6. 不做事项

Phase 5 不做：

- Hot Memory。
- Unified Retrieval 合并排序。
- 复杂 rerank 压缩。

## 7. 子代理建议

Phase 5 主会话偏多。可交给子代理的任务：Markdown chunker golden tests、Qdrant fake client 测试、Archive RAG smoke fixture。

必须由主会话控制的任务：Qdrant filter builder、单 collection payload schema、index_generation 失效策略。
