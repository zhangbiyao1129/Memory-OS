# Phase 8 多 Agent Adapter AI 自驱动实现计划

> 面向 AI 代理的工作者：执行本计划前必须读取 spec 和 Phase 1-7 计划。所有代码改动前创建 TodoList。除非用户明确要求，不要 commit、push、deploy。

**目标：** 扩展 Claude Code、opencode、Hermes 和 Generic Transcript Importer，确保所有平台输出同一种 TurnEvent v1，并能共享同一 Memory OS 检索链路。

**架构：** 每个平台 Adapter 只负责平台格式解析、字段标准化、安全预处理和 TurnEvent 输出；Memory Kernel 不关心事件来源。Transcript Importer 用于没有实时 hook 的平台。

**技术栈：** Go Adapter SDK、TurnEvent v1、Secret sanitizer、Adapter Token、fixture-based tests。

---

## 1. 成功标准

- `make test` 通过。
- `make smoke` 包含至少一个非 Codex adapter fixture。
- Claude Code Adapter 输出 TurnEvent v1。
- opencode Adapter 输出 TurnEvent v1。
- Hermes Adapter 输出 TurnEvent v1。
- Generic Transcript Importer 输出 TurnEvent v1。
- 所有 Adapter 都执行 secret sanitize 和截断。
- Adapter 输出通过 Phase 3 validator。

## 2. 推荐文件结构

```text
internal/adapter/
  claude_code.go
  opencode.go
  hermes.go
  transcript_importer.go
  fixtures/
    claude_code_sample.json
    opencode_sample.json
    hermes_sample.json
    transcript_sample.md
  claude_code_test.go
  opencode_test.go
  hermes_test.go
  transcript_importer_test.go
cmd/memory-adapter/main.go
```

## 3. 执行任务

### 任务 1：Adapter interface 稳定化

- [ ] 定义统一 Adapter interface。
- [ ] 定义 input metadata。
- [ ] 定义 output TurnEvent batch。
- [ ] 写单测确保现有 Codex/Generic MCP Adapter 兼容。
- [ ] 运行 `go test ./...`。

验收：新增 Adapter 不影响旧 Adapter。

### 任务 2：Claude Code Adapter

- [ ] 定义 Claude Code fixture。
- [ ] 解析用户消息、助手消息、工具调用、文件变化。
- [ ] 输出 TurnEvent v1。
- [ ] 写转换测试。
- [ ] 运行 `go test ./...`。

验收：fixture 输出通过 validator。

### 任务 3：opencode Adapter

- [ ] 定义 opencode fixture。
- [ ] 解析 transcript/hook payload。
- [ ] 标准化路径、命令和工具结果。
- [ ] 写转换测试。
- [ ] 运行 `go test ./...`。

验收：opencode 输出和 Codex 输出语义一致。

### 任务 4：Hermes Adapter

- [ ] 定义 Hermes fixture。
- [ ] 解析平台事件。
- [ ] 输出 TurnEvent v1。
- [ ] 写转换测试。
- [ ] 运行 `go test ./...`。

验收：Hermes 输出通过 validator。

### 任务 5：Generic Transcript Importer

- [ ] 支持 Markdown/text transcript 输入。
- [ ] 识别 user/assistant/tool/code block。
- [ ] 不能识别的内容保存为 transcript segment。
- [ ] 写转换测试。
- [ ] 运行 `go test ./...`。

验收：非实时平台可延迟导入。

### 任务 6：统一 CLI

- [ ] 扩展 `cmd/memory-adapter`。
- [ ] 支持 `--adapter codex|claude-code|opencode|hermes|transcript`。
- [ ] 支持 dry-run 输出 TurnEvent。
- [ ] 支持 POST 到 Memory OS。
- [ ] 写 CLI 参数测试或最小 smoke。
- [ ] 运行 `go test ./...`。

验收：dry-run 可用于本地调试。

### 任务 7：smoke test

- [ ] 使用 Claude Code 或 transcript fixture。
- [ ] 生成 TurnEvent。
- [ ] POST Memory OS。
- [ ] 执行 `/memory/search` 验证可召回。
- [ ] 运行 `make test`。
- [ ] 运行 `make smoke`。

验收：多 Agent 数据进入同一检索链路。

## 4. 集成测试方案

必须覆盖：

- 不同 Adapter 输出 schema 一致。
- 同一用户跨 Agent 共享 project memory。
- agent_specific 默认隔离。
- fake secret 统一脱敏。
- 超长 transcript 截断并保留 hash。

## 5. 不做事项

Phase 8 不做：

- 平台插件市场发布。
- 各平台复杂 UI。
- importer 历史大规模迁移优化。

## 6. 子代理建议

Phase 8 适合多子代理并行。可交给子代理的任务：Claude Code Adapter、opencode Adapter、Hermes Adapter、Generic Transcript Importer。

主会话必须先稳定 Adapter interface，并在所有子代理完成后审查输出一致性、集成 smoke test。
