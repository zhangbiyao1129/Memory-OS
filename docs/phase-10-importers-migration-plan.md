# Phase 10 Importers 与迁移 AI 自驱动实现计划

> 面向 AI 代理的工作者：执行本计划前必须读取 spec 和 Phase 1-9 计划。所有代码改动前创建 TodoList。除非用户明确要求，不要 commit、push、deploy。

**目标：** 提供 mem0、FastGPT、OpenMemory、Zep、Khoj 等系统的迁移入口，并支持导出为 Markdown/RAG bundle。

**架构：** importer 将外部数据转换为 Memory OS 标准对象：TurnEvent、Archive、Hot Memory、Secret metadata 或 source refs；不引入外部系统作为运行时依赖。导入过程必须可预览、可幂等、可回滚或可标记批次。

**技术栈：** Go importer CLI/API、PostgreSQL import batches、Markdown exporter、JSONL/CSV parser、Secret sanitizer。

---

## 1. 成功标准

- `make test` 通过。
- importer 支持 dry-run。
- importer 支持 batch id。
- 导入过程幂等。
- 导入数据执行 Secret sanitizer。
- mem0 importer 可导入 memory facts。
- FastGPT importer 可导入 document/chunk 为 Archive。
- 导出 bundle 包含 Markdown、metadata、source refs。
- 导入后可通过 `/memory/search` 召回。

## 2. 推荐文件结构

```text
internal/importer/
  model.go
  service.go
  mem0.go
  fastgpt.go
  openmemory.go
  zep.go
  khoj.go
  bundle_export.go
  service_test.go
cmd/memory-importer/main.go
migrations/000010_import_batches.sql
docs/importers.md
```

## 3. 数据库设计

最低表：

- `import_batches`
- `import_items`
- `import_errors`
- `external_source_refs`

关键要求：

- batch id 可追踪。
- external id + source type 幂等。
- 错误可重试。
- 导入数据不绕过权限和 Secret 规则。

## 4. 执行任务

### 任务 1：import batch 基座

- [ ] 创建 migration。
- [ ] 实现 import batch service。
- [ ] 实现 dry-run 结果模型。
- [ ] 写单测覆盖重复 batch、失败 item。
- [ ] 运行 `go test ./...`。

验收：导入可追踪、可重试。

### 任务 2：mem0 importer

- [ ] 定义 mem0 export 输入格式。
- [ ] 转换为 Hot Memory。
- [ ] 保留 source refs。
- [ ] 执行 Secret sanitizer。
- [ ] 写 fixture 测试。
- [ ] 运行 `go test ./...`。

验收：mem0 memory fact 可导入 Hot Memory。

### 任务 3：FastGPT importer

- [ ] 定义 FastGPT document/chunk 输入格式。
- [ ] 转换为 Markdown Archive。
- [ ] 保留 external document id。
- [ ] 投递 Archive RAG reindex。
- [ ] 写 fixture 测试。
- [ ] 运行 `go test ./...`。

验收：FastGPT 文档可导入 Archive 并检索。

### 任务 4：OpenMemory/Zep/Khoj importer skeleton

- [ ] 定义接口和输入格式占位。
- [ ] 支持 dry-run schema validation。
- [ ] 不实现外部 API 深度集成。
- [ ] 写单测覆盖 unsupported field。
- [ ] 运行 `go test ./...`。

验收：后续扩展不影响现有 importer。

### 任务 5：Markdown/RAG bundle export

- [ ] 导出 Markdown Archive。
- [ ] 导出 metadata JSON。
- [ ] 导出 source refs。
- [ ] 可选导出 chunk manifest，不导出 vector 原始数据除非用户要求。
- [ ] 写单测覆盖权限和 secret。
- [ ] 运行 `go test ./...`。

验收：bundle 不包含 Secret 明文。

### 任务 6：CLI 和 API

- [ ] 实现 `cmd/memory-importer`。
- [ ] 支持 dry-run。
- [ ] 支持 apply。
- [ ] 支持 export bundle。
- [ ] 写 CLI smoke。
- [ ] 运行 `go test ./...`。

验收：迁移可由 AI 或用户以命令方式执行。

### 任务 7：smoke test

- [ ] 使用 mem0 fixture dry-run。
- [ ] apply 导入。
- [ ] 执行 `/memory/search` 验证召回。
- [ ] export bundle。
- [ ] 验证 bundle 无 fake secret。
- [ ] 运行 `make test`。
- [ ] 运行 `make smoke`。

验收：迁移闭环成功。

## 5. 集成测试方案

必须覆盖：

- dry-run 不写数据库。
- apply 幂等。
- 导入后可检索。
- external refs 可追溯。
- fake secret 不进入 Archive/Hot Memory/Qdrant/export bundle。

## 6. 不做事项

Phase 10 不做：

- 把 mem0/FastGPT 作为运行时依赖。
- 在线双写外部系统。
- 大规模分布式迁移调度。

## 7. 子代理建议

Phase 10 适合多子代理并行。可交给子代理的任务：mem0 importer fixture、FastGPT importer fixture、OpenMemory/Zep/Khoj skeleton、bundle export 测试。

主会话负责 importer batch schema、dry-run/apply 幂等策略、Secret sanitizer 接入、最终导入后检索 smoke。
