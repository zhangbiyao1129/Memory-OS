# Phase 4 Markdown Archive AI 自驱动实现计划

> 面向 AI 代理的工作者：执行本计划前必须读取 spec 和 Phase 1-3 计划。所有代码改动前创建 TodoList。除非用户明确要求，不要 commit、push、deploy。

**目标：** 将安全处理后的 TurnEvent 批量归档为 Markdown 文件，并支持 Archive 元数据、版本、在线编辑、审计和重索引状态。

**架构：** PostgreSQL 保存 Archive 元数据、版本、状态、审计；Markdown 文件保存正文权威内容；worker 从 Redis 队列消费 TurnEvent 批次生成 Archive；编辑 Archive 后生成新 version 和新 `index_generation`。

**技术栈：** Go、pgx/v5、Redis worker、本地文件系统、Markdown renderer/parser、zap、Nuxt API。

---

## 1. 成功标准

- `make test` 通过。
- `make smoke` 包含 Archive 生成和编辑。
- TurnEvent 批次可生成 Markdown 文件。
- PostgreSQL 中有 archive metadata 和 version。
- Markdown 文件路径符合 spec。
- 编辑 Archive 后 `index_generation` 增加。
- 旧版本保留审计记录。
- Archive 正文不包含 Secret 明文。

## 2. 推荐文件结构

```text
internal/archive/
  model.go
  repository.go
  service.go
  renderer.go
  filesystem.go
  versioning.go
  chunk_manifest.go
  renderer_test.go
  service_test.go
internal/jobs/archive_worker.go
cmd/memory-api/handlers/archive.go
migrations/000004_archive.sql
archives/.gitkeep
```

## 3. 数据库设计

最低表：

- `archives`
- `archive_versions`
- `archive_events`
- `archive_edit_audit_logs`
- `archive_index_generations`

关键要求：

- `archive_id` 唯一。
- `index_generation` 单调递增。
- `content_hash` 可检测文件变更。
- version 不能覆盖旧版本。

## 4. 执行任务

### 任务 1：Archive migration

- [ ] 创建 archive 相关表。
- [ ] 添加 archive_id、project、user、status、index_generation 索引。
- [ ] 写 migration/repository 测试。
- [ ] 运行 `go test ./...`。

验收：元数据和版本可持久化。

### 任务 2：文件系统布局

- [ ] 实现 archive path builder。
- [ ] 路径格式使用 `archives/org_<org_id>/project_<project_id>/user_<user_id>/yyyy/mm/archive_<archive_id>.md`。
- [ ] 防止 path traversal。
- [ ] 写单测覆盖非法 id 和路径逃逸。
- [ ] 运行 `go test ./...`。

验收：Archive 文件只能写入 archive root。

### 任务 3：Markdown renderer

- [ ] 从 TurnEvent 批次生成 Markdown。
- [ ] 包含摘要、关键决策、命令摘要、文件变更、错误修复、source refs。
- [ ] 不包含 Secret 明文。
- [ ] 写 snapshot 或 golden 测试，使用 fake secret 验证脱敏。
- [ ] 运行 `go test ./...`。

验收：Markdown 可读且安全。

### 任务 4：Archive service

- [ ] 实现 create archive。
- [ ] 写文件。
- [ ] 写 metadata。
- [ ] 写 version。
- [ ] 写 audit log。
- [ ] 写单测覆盖文件写失败、数据库失败、幂等重试。
- [ ] 运行 `go test ./...`。

验收：失败不会产生孤立元数据或孤立文件，或有可恢复状态。

### 任务 5：Archive worker

- [ ] 从 Redis 消费 archive job。
- [ ] 聚合 TurnEvent。
- [ ] 调用 Archive service。
- [ ] job 使用 idempotency key。
- [ ] 写 worker 单测覆盖重复 job。
- [ ] 运行 `go test ./...`。

验收：重复消费不会生成重复 Archive。

### 任务 6：Archive API

- [ ] 实现 Archive 列表。
- [ ] 实现 Archive 详情。
- [ ] 实现 Archive 编辑。
- [ ] 实现 Archive 版本列表。
- [ ] 实现 Archive 重索引请求。
- [ ] 更新 Swagger。
- [ ] 写 handler 测试。
- [ ] 运行 `go test ./...`。

验收：编辑后 `index_generation` 增加并投递重索引任务。

### 任务 7：smoke test

- [ ] TurnEvent seed 后触发 Archive 生成。
- [ ] 验证 Markdown 文件存在。
- [ ] 验证 metadata 存在。
- [ ] 编辑 Archive。
- [ ] 验证 version 和 index_generation 更新。
- [ ] 运行 `make test`。
- [ ] 运行 `make smoke`。

验收：Archive 最小闭环成功。

## 5. 集成测试方案

必须覆盖：

- 同一批 TurnEvent 重复归档幂等。
- Archive 文件 hash 与数据库一致。
- 编辑创建新 version。
- 旧 version 可审计。
- fake secret 不在 Markdown 中出现。
- 无权限用户不能读写 Archive。

## 6. 不做事项

Phase 4 不做：

- Qdrant chunk indexing。
- RAG search。
- Hot Memory 派生。
- 复杂 Markdown 富文本编辑器。

## 7. 子代理建议

Phase 4 适合混合执行。可交给子代理的任务：Markdown renderer golden tests、archive path traversal 测试、Archive API Swagger 文档、必要的 Archive UI 初步页面。

必须由主会话控制的任务：Archive metadata/version schema、index_generation 策略、worker 幂等策略。
