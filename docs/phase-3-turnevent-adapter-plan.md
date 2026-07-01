# Phase 3 TurnEvent 与 Adapter SDK AI 自驱动实现计划

> 面向 AI 代理的工作者：执行本计划前必须读取 `README.md`、`AGENTS.md`、`docs/memory-os-spec.md`、Phase 1 和 Phase 2 计划。所有代码改动前创建 TodoList。除非用户明确要求，不要 commit、push、deploy。

**目标：** 实现标准 TurnEvent v1、事件持久化、Adapter SDK、Codex Adapter 和 Generic MCP Adapter，让所有 Agent 事件以统一协议进入 Memory OS。

**架构：** Adapter 负责平台事件到 TurnEvent 的转换、截断、secret sanitizer、`secret_ref` 替换和 POST；API 校验 Adapter Token、权限上下文和事件 schema 后写 PostgreSQL event store，并投递 Redis 后台任务。

**技术栈：** Go、Hertz、pgx/v5、Redis queue、JSON schema 或 Go validator、Secret sanitizer、Adapter Token。

---

## 1. 成功标准

- `make test` 通过。
- `make smoke` 包含 TurnEvent ingest。
- `POST /memory/turn-event` 可接收标准事件。
- 重复 `event_id` 幂等。
- 超长 tool output 被截断并保留 hash。
- Secret 明文被替换为 `secret_ref`。
- Codex Adapter 可生成标准 TurnEvent。
- Generic MCP Adapter 可生成标准 TurnEvent。

## 2. 推荐文件结构

```text
internal/eventlog/
  model.go
  validator.go
  repository.go
  service.go
  sanitizer.go
  validator_test.go
  service_test.go
internal/adapter/
  sdk.go
  codex.go
  generic_mcp.go
  transcript.go
  sdk_test.go
cmd/memory-api/handlers/turn_event.go
cmd/memory-adapter/main.go
migrations/000003_turn_events.sql
```

## 3. 数据库设计

最低表：

- `turn_events`
- `turn_event_payloads`
- `event_ingest_requests`
- `adapter_ingest_logs`

关键要求：

- `event_id` 唯一。
- `turn_id`、`thread_id`、`session_id` 建索引。
- 原始 payload 只能保存安全处理后的内容。
- request id 用于幂等。

## 4. 执行任务

### 任务 1：TurnEvent schema

- [ ] 定义 TurnEvent v1 Go struct。
- [ ] 定义事件类型枚举。
- [ ] 定义 metadata、actor、source、tool、file change 字段。
- [ ] 写 validator 单测覆盖必填字段、非法类型、时间格式。
- [ ] 运行 `go test ./...`。

验收：非法事件不会进入 repository。

### 任务 2：event store migration

- [ ] 新增 TurnEvent migration。
- [ ] 添加唯一约束和索引。
- [ ] 添加 event payload 存储表。
- [ ] 写 migration/repository 测试。
- [ ] 运行 `go test ./...`。

验收：重复 event_id 不重复写入。

### 任务 3：sanitizer 与截断

- [ ] 接入 Phase 2 Secret detector。
- [ ] 实现 `MAX_TURN_EVENT_BYTES=256KB`。
- [ ] 实现 `MAX_TOOL_OUTPUT_BYTES=64KB`。
- [ ] 保存 hash、原始长度、截断标记。
- [ ] 写单测覆盖超长输入、fake secret、命令输出 hash。
- [ ] 运行 `go test ./...`。

验收：安全处理后的 payload 不含 fake secret 明文。

### 任务 4：TurnEvent service

- [ ] 校验 Adapter Token。
- [ ] 构造权限上下文。
- [ ] 调用 sanitizer。
- [ ] 幂等写入 event store。
- [ ] 投递 archive/index candidate job。
- [ ] 写单测覆盖成功、重复、无权限、非法 payload。
- [ ] 运行 `go test ./...`。

验收：API 重试安全。

### 任务 5：TurnEvent API

- [ ] 实现 `POST /memory/turn-event`。
- [ ] 返回 event_id、status、deduped、warnings。
- [ ] 更新 Swagger。
- [ ] 写 handler 测试。
- [ ] 运行 `go test ./...`。

验收：错误响应不含 secret。

### 任务 6：Adapter SDK

- [ ] 实现 SDK 配置。
- [ ] 实现事件构造 helper。
- [ ] 实现本地 sanitizer 预处理。
- [ ] 实现 POST client、重试、幂等 request id。
- [ ] 写单测覆盖 retry、401、413、网络失败。
- [ ] 运行 `go test ./...`。

验收：SDK 可被 Codex/Generic Adapter 复用。

### 任务 7：Codex Adapter

- [ ] 定义 Codex 输入格式。
- [ ] 支持 transcript 或 hook payload 转 TurnEvent。
- [ ] 标准化路径和命令。
- [ ] 写样例 fixture。
- [ ] 写转换测试。
- [ ] 运行 `go test ./...`。

验收：fixture 转换输出稳定。

### 任务 8：Generic MCP Adapter

- [ ] 定义 Generic MCP 输入格式。
- [ ] 支持 append user/assistant/tool/file events。
- [ ] 输出 TurnEvent v1。
- [ ] 写转换测试。
- [ ] 运行 `go test ./...`。

验收：MCP Adapter 与 Codex Adapter 输出同一协议。

### 任务 9：smoke test

- [ ] 更新 `make smoke` 写入一组 TurnEvent。
- [ ] 验证重复提交 deduped。
- [ ] 验证 fake secret 已替换。
- [ ] 验证 archive candidate job 入队。
- [ ] 运行 `make test`。
- [ ] 运行 `make smoke`。

验收：TurnEvent 端到端 ingest 成功。

## 5. 集成测试方案

必须覆盖：

- Adapter Token 绑定 project，不能写入其他 project。
- 同一 event_id 重试不会重复写。
- command output 超长被截断。
- fake secret 被替换为 `secret_ref`。
- Redis job 至少投递一次，但 worker 重试幂等。

## 6. 不做事项

Phase 3 不做：

- Archive Markdown 正式生成。
- Qdrant indexing。
- Hot Memory 派生。
- Claude/opencode/Hermes Adapter。

## 7. 子代理建议

Phase 3 适合混合执行。可交给子代理的任务：Codex Adapter fixture 转换、Generic MCP Adapter fixture 转换、SDK retry 测试、TurnEvent validator 边界测试。

必须由主会话控制的任务：TurnEvent v1 schema 初版、event store 幂等策略、sanitizer 与 Secret Vault 集成主流程。
