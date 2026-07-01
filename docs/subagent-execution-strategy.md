# Memory OS v0.4 子代理执行策略

> 用途：指导 Codex 在执行 Memory OS v0.4 计划时，什么时候应该留在主会话，什么时候可以启动子代理。目标是降低主会话上下文压力，同时避免架构、安全和权限规则被拆散。

## 1. 总原则

采用“主会话控架构 + 子代理做边界清晰任务”的模式。

主会话负责：

- 架构边界。
- 数据库 schema。
- 权限模型。
- Secret Vault。
- Qdrant query-time filter。
- Unified Retrieval 主流程。
- 跨 Phase 接口一致性。
- 最终 diff 审查、集成和验证。

子代理适合：

- 独立测试补齐。
- fixture 转换测试。
- 前端页面模块。
- importer/exporter fixture。
- 文档/API 补充。
- 明确边界的小包实现。
- 不共享写入文件的并行任务。

## 2. 子代理启动条件

满足以下条件时优先考虑子代理：

- 任务可以用 1 到 3 个文件完成。
- 任务输入、输出、验收标准明确。
- 不需要修改核心 schema 或公共接口。
- 不需要跨多个模块反复协调。
- 不会和其他并行任务写同一个文件。
- 失败不会污染数据模型、安全模型或权限模型。

## 3. 禁止或不建议子代理的任务

以下任务默认由主会话执行：

- 数据库 schema 初版设计和 migration 关键字段。
- Auth/Tenant/Permission 核心模型。
- Secret Vault crypto、injector、audit 核心流程。
- TurnEvent v1 schema 定义。
- Qdrant payload filter builder。
- Archive/Hot Memory 权威边界定义。
- Unified Retrieval 主流程。
- Makefile、Docker Compose、全局配置的最终整合。
- 跨 Phase 的接口重命名或重构。

## 4. 并行安全规则

- 同一时间不得让多个子代理修改同一个文件。
- 子代理开始前，主会话必须给出明确文件范围。
- 子代理不得扩大范围修改未授权文件。
- 子代理不得删除未知文件。
- 子代理不得 commit、push、deploy。
- 子代理不得写入真实 API key、token、密码、私钥、cookie、助记词。
- 子代理完成后，主会话必须审查输出并负责最终验证。

## 5. 推荐使用方式

主会话发给子代理的任务应包含：

```text
目标：一句话说明要完成什么。
允许修改文件：列出精确路径。
必须读取文件：列出上下文文件。
禁止事项：列出不能改的文件或规则。
验收命令：列出需要运行或由主会话运行的命令。
输出要求：总结改动、测试结果、风险。
```

## 6. 子代理模型选择策略

Codex 子代理默认继承主会话模型。除非用户明确要求，或任务有清晰的低风险理由，不要主动指定子代理模型。

推荐策略：

- 架构、安全、权限、Secret Vault、Qdrant filter、Unified Retrieval、跨 Phase 集成：使用主会话处理，或让子代理继承 `gpt-5.5`。
- 普通实现、普通单测、Adapter fixture、importer fixture：可使用继承模型；如需节省资源，可指定 `gpt-5.4`。
- 简单文档检查、重复性页面骨架、低风险 fixture 生成：可指定 `gpt-5.4-mini` 或 `gpt-5.3-codex-spark`。
- 不要把轻量模型用于 schema 初版、安全边界、权限规则、Secret 处理、Qdrant query-time filter、Unified Retrieval 主流程。

可用模型：

- `gpt-5.5`：复杂编码、架构、安全和真实项目集成。
- `gpt-5.4`：日常编码、普通测试、明确边界模块。
- `gpt-5.4-mini`：小型、快速、低风险任务。
- `gpt-5.3-codex-spark`：超快速编码、简单重复性任务。

reasoning effort：

- 默认继承主会话。
- 复杂设计或高风险审查可用 `high` 或 `xhigh`。
- 简单页面、fixture、文档检查可用 `low` 或 `medium`。

硬性约束：

- 没有明确理由时，不指定 `model`，让子代理继承主会话。
- 指定轻量模型时，任务必须边界清楚、文件范围明确、失败可局部回滚。
- 子代理模型选择不能弱化安全、权限、Secret 和检索隔离要求。

## 7. 各 Phase 子代理建议

### Phase 1

主会话为主。可用子代理：

- Nuxt skeleton。
- smoke script 初版。
- Swagger 文档补充。
- config/logger 单测补齐。

不建议子代理：

- Go module 初始架构。
- Makefile 最终整合。
- Docker Compose 最终整合。
- Qdrant filter builder。

### Phase 2

主会话为主。可用子代理：

- password/PAT 测试矩阵。
- Secret detector 规则测试。
- audit repository 测试。

不建议子代理：

- tenant 权限模型。
- Secret crypto。
- Secret injector。
- auth middleware 主流程。

### Phase 3

混合执行。可用子代理：

- Codex Adapter fixture 转换。
- Generic MCP Adapter fixture 转换。
- SDK retry 测试。
- TurnEvent validator 边界测试。

不建议子代理：

- TurnEvent v1 schema 初版。
- event store 幂等策略。
- sanitizer 与 Secret Vault 集成主流程。

### Phase 4

混合执行。可用子代理：

- Markdown renderer golden tests。
- archive path traversal 测试。
- Archive API Swagger 文档。
- Archive UI 初步页面，如果 Phase 9 前需要。

不建议子代理：

- Archive metadata/version schema。
- index_generation 策略。
- worker 幂等策略。

### Phase 5

主会话偏多。可用子代理：

- Markdown chunker golden tests。
- Qdrant fake client 测试。
- Archive RAG smoke fixture。

不建议子代理：

- Qdrant filter builder。
- 单 collection payload schema。
- index_generation 失效策略。

### Phase 6

混合执行。可用子代理：

- Hot Memory extractor fixtures。
- promote/demote 单测。
- hot_score 测试矩阵。

不建议子代理：

- Hot Memory schema。
- scope/agent_specific 权限规则。
- Qdrant hot memory filter。

### Phase 7

主会话为主。可用子代理：

- rerank 降级测试。
- context compression 单测。
- MCP `memory_search` tool schema 测试。

不建议子代理：

- Unified Retrieval service 主流程。
- 权限上下文合并。
- access log + mark_used 事务策略。

### Phase 8

适合多子代理。可用子代理：

- Claude Code Adapter。
- opencode Adapter。
- Hermes Adapter。
- Generic Transcript Importer。

主会话负责：

- Adapter interface 稳定化。
- 所有 Adapter 输出一致性审查。
- 最终 smoke 集成。

### Phase 9

适合多子代理。可用子代理：

- Archive 页面。
- Hot Memory 页面。
- Secret Vault 页面。
- Adapter Token 页面。
- Qdrant 状态页。
- Search test 页面。

主会话负责：

- 前端 app shell。
- auth/session/context store。
- API client 统一错误处理。
- 最终 build 和交互一致性。

### Phase 10

适合多子代理。可用子代理：

- mem0 importer fixture。
- FastGPT importer fixture。
- OpenMemory/Zep/Khoj skeleton。
- bundle export 测试。

主会话负责：

- importer batch schema。
- dry-run/apply 幂等策略。
- Secret sanitizer 接入。
- 最终导入后检索 smoke。
