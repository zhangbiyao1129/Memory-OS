# Phase 6 Native Hot Memory AI 自驱动实现计划

> 面向 AI 代理的工作者：执行本计划前必须读取 spec 和 Phase 1-5 计划。所有代码改动前创建 TodoList。除非用户明确要求，不要 commit、push、deploy。

**目标：** 实现原生 Hot Memory，从安全处理后的 TurnEvent 和 Archive 中派生高价值短事实，并支持热度、权限、向量索引、promote/demote 和 mark_used。

**架构：** PostgreSQL 保存 Hot Memory 正文权威、scope、权限、来源、热度和状态；Qdrant 单 collection 保存 `doc_type=hot_memory` vector；worker 负责候选提取和索引。

**技术栈：** Go、PostgreSQL、Qdrant、bge-m3 embedding、Redis worker、LLM fact extraction、Secret sanitizer。

---

## 1. 成功标准

- `make test` 通过。
- `make smoke` 包含 Hot Memory create/index/search/use。
- Hot Memory 可从安全输入派生。
- Hot Memory 不含 Secret 明文。
- Qdrant payload 使用 `doc_type=hot_memory`。
- `agent_specific` 默认不跨 Agent 召回。
- promote/demote 更新状态和 hot_score。
- mark_used 增加 used_count。

## 2. 推荐文件结构

```text
internal/hotmemory/
  model.go
  repository.go
  service.go
  extractor.go
  scorer.go
  indexer.go
  filter.go
  service_test.go
  extractor_test.go
internal/jobs/hotmemory_worker.go
cmd/memory-api/handlers/hotmemory.go
migrations/000006_hot_memory.sql
```

## 3. 数据库设计

最低表：

- `hot_memories`
- `hot_memory_sources`
- `hot_memory_events`
- `hot_memory_index_jobs`

关键字段：

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

## 4. 执行任务

### 任务 1：migration 与 repository

- [ ] 创建 Hot Memory 表。
- [ ] 添加 scope、status、hot_score、source 索引。
- [ ] 写 repository CRUD 测试。
- [ ] 运行 `go test ./...`。

验收：memory_id 唯一，soft delete 后不参与查询。

### 任务 2：fact extractor

- [ ] 从安全 TurnEvent/Archive 中提取候选 fact。
- [ ] 支持规则提取和 LLM 提取接口。
- [ ] 输出 confidence、source_ref、scope 建议。
- [ ] 写单测覆盖 fake secret、低价值文本、重复事实。
- [ ] 运行 `go test ./...`。

验收：输出不包含 Secret 明文。

### 任务 3：dedupe 与 upsert

- [ ] 实现相同 scope 下 fact 去重。
- [ ] 支持更新来源和 confidence。
- [ ] 使用唯一约束或 hash 幂等。
- [ ] 写单测覆盖重复 job。
- [ ] 运行 `go test ./...`。

验收：重复提取不会产生重复 memory。

### 任务 4：Hot Memory indexing

- [ ] 调用 embedding client。
- [ ] upsert Qdrant point。
- [ ] payload 包含 doc_type、scope、user/org/project、agent_id、permission_labels。
- [ ] 写单测覆盖 embedding 失败 pending。
- [ ] 运行 `go test ./...`。

验收：PostgreSQL fact 是权威，vector 可重建。

### 任务 5：Hot Memory filter

- [ ] 构造 Hot Memory Qdrant filter。
- [ ] 支持 user/project/org scope。
- [ ] `agent_specific` 默认包含 agent_id filter。
- [ ] 禁止空权限 filter。
- [ ] 写单测覆盖跨 Agent、跨用户、跨 org。
- [ ] 运行 `go test ./...`。

验收：agent_specific 默认隔离。

### 任务 6：promote/demote/mark_used

- [ ] 实现 promote。
- [ ] 实现 demote。
- [ ] 实现 mark_used。
- [ ] 更新 hot_score 计算。
- [ ] 写单测覆盖状态变化和计数。
- [ ] 运行 `go test ./...`。

验收：热度可解释、可审计。

### 任务 7：API 与 smoke

- [ ] 实现 Hot Memory 列表、详情、创建、编辑、删除。
- [ ] 实现 promote/demote API。
- [ ] 实现 mark_used API。
- [ ] 更新 Swagger。
- [ ] 更新 `make smoke`。
- [ ] 运行 `make test`。
- [ ] 运行 `make smoke`。

验收：Hot Memory 最小闭环成功。

## 5. 集成测试方案

必须覆盖：

- 从 Archive 派生 Hot Memory。
- fake secret 不进入 fact。
- user/project/org scope 权限正确。
- agent_specific 隔离正确。
- Qdrant query-time filter 正确。
- promote/demote/mark_used 审计正确。

## 6. 不做事项

Phase 6 不做：

- Unified Retrieval 合并压缩。
- 多 Agent Adapter 扩展。
- mem0 迁移。

## 7. 子代理建议

Phase 6 适合混合执行。可交给子代理的任务：Hot Memory extractor fixtures、promote/demote 单测、hot_score 测试矩阵。

必须由主会话控制的任务：Hot Memory schema、scope/agent_specific 权限规则、Qdrant hot memory filter。
