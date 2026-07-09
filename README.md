# Memory OS

Memory OS 是一个面向编码 Agent 的自托管记忆平台。它把 Codex、Claude Code、Cursor、opencode、Hermes 以及 MCP 客户端的工作过程采集下来，沉淀为可检索、可追溯、可治理的长期记忆。

这个项目想解决一个很具体的问题：

> Agent 能不能记住真正有用的工程上下文，同时不泄露密钥、不混淆项目、不永远相信过期事实？

Memory OS 的答案是一条可部署、可验证的生产链路：事件写入、候选提炼、Hot Memory、Archive RAG、项目权限、MCP 工具，以及用于当前可信事实治理的 Memory Kernel。

## 它能做什么

- 把 Agent 活动通过 HTTP hook 或 MCP 工具写成 `TurnEvent`。
- 从对话和工作流中提炼事实、偏好、决策和待办候选记忆。
- 将候选记忆分流到待确认、丢弃、Hot Memory 或 Markdown Archive。
- 通过统一检索接口同时查询 Hot Memory 和 Archive RAG。
- 通过 Streamable HTTP MCP 和本地 stdio 代理暴露记忆工具。
- 在 PostgreSQL 中维护租户、项目和权限边界，并在 Qdrant 查询时使用 payload filter。
- Secret 只进入 Secret Vault 或本地解密路径，不进入记忆正文。
- 基于当前可信 Memory Units 为 Agent 任务构建 Context Pack。

## 当前状态

Memory OS 当前可作为单节点服务部署使用，同时仍在持续硬化。

| 模块 | 状态 |
| --- | --- |
| HTTP API | 已实现 |
| Web 控制台 | 已实现 |
| Streamable HTTP MCP | 已实现 |
| 本地 stdio MCP 代理 | 已实现 |
| TurnEvent 写入 | 已实现 |
| 候选记忆提炼 | 已实现 |
| 候选 AI 整理 | 已实现 |
| Hot Memory | 已实现 |
| Markdown Archive | 已实现 |
| Archive RAG | 已实现 |
| 统一检索 | 已实现 |
| Secret Vault / 本地 Secret 工具 | 已实现 |
| Memory Kernel / Context Pack | 已实现，仍在硬化 |

## 架构

```text
Agent / Hook / MCP Client
        |
        v
memory-api / memory-mcp
        |
        v
TurnEvent
        |
        v
candidate_memory_jobs
        |
        v
memory-worker
        |
        v
Candidate Memory
        |
        +-------------------+-------------------+
        |                   |                   |
        v                   v                   v
   Hot Memory        AI Maintenance       Archive Composer
        |                   |                   |
        |                   v                   v
        |       discard / review / hot     Markdown Archive
        |              / archive                 |
        |                                       v
        +-------------------------------> Archive RAG
                                                |
                                                v
                                      Unified Retrieval
```

权威数据源：

- PostgreSQL：元数据、权限、任务、候选、Hot Memory、Memory Units 和 Archive 记录。
- Markdown 文件：Archive 正文权威源。
- Qdrant：只保存可重建的向量索引。
- Redis：队列、锁、缓存和限流。

## 记忆生命周期

1. Agent 发送 `TurnEvent`。
2. API 完成认证，并解析 org、project、workspace 作用域。
3. 有价值的事件类型进入候选提炼队列。
4. Worker 调用 LLM extractor，并写入候选记忆。
5. 候选维护服务按项目分组、过滤、去重并决定去向。
6. 高价值事实可以进入 Hot Memory。
7. 适合长期沉淀的内容会生成 Markdown Archive。
8. Archive chunk 被索引到 Qdrant。
9. `/memory/search` 和 MCP `memory_search` 统一检索 Hot Memory 与 Archive RAG。
10. 检索使用情况会写入反馈，用于后续排序和治理。

Memory Kernel 在历史证据之上提供“当前事实”层：

- `memory_units` 表示当前可信事实。
- governance run 用于发现过期、冲突、重复或证据不足的事实。
- Context Pack 为 Agent 当前任务组装可注入上下文。
- Memory CI 用于验证召回结果是否被旧事实污染。

## MCP 工具

远程 Streamable HTTP 入口：

```text
POST <MEMORY_OS_MCP_URL>/mcp
Authorization: Bearer <Memory OS PAT>
Accept: application/json, text/event-stream
```

兼容 HTTP bridge：

```text
GET  <MEMORY_OS_MCP_URL>/tools
POST <MEMORY_OS_MCP_URL>/tools/call
```

已实现工具：

| Tool | 用途 |
| --- | --- |
| `memory_search` | 检索 Hot Memory 和 Archive RAG，并返回可追溯来源。 |
| `memory_context_pack` | 为当前任务构建 Memory Kernel Context Pack。 |
| `memory_append_event` | 写入 Agent 事件，并在适用时排队候选提炼。 |
| `memory_archive` | 创建手动 Markdown Archive。 |
| `memory_get_archive` | 按权限读取 Archive 元数据和 Markdown 内容。 |
| `memory_mark_used` | 标记检索结果已使用，并更新反馈信号。 |
| `memory_stats` | 返回记忆生命周期统计。 |

只支持 stdio MCP 的客户端可以使用本地代理：

```bash
go build -o ~/bin/memory-mcp-local ./cmd/memory-mcp-local
```

MCP 客户端配置示例：

```json
{
  "mcpServers": {
    "memory-os": {
      "command": "/Users/you/bin/memory-mcp-local",
      "env": {
        "MEMORY_OS_MCP_URL": "https://memory.example.com",
        "MEMORY_OS_API_URL": "https://memory-api.example.com",
        "MEMORY_OS_TOKEN": "${MEMORY_OS_TOKEN}"
      }
    }
  }
}
```

不要把真实 token 写进已提交配置。请从本地 secret manager 或环境变量加载。

## HTTP 接口

常用生产接口：

| Endpoint | 用途 |
| --- | --- |
| `GET /healthz` | 检查数据库、Redis 和 Qdrant 健康状态。 |
| `GET /version` | 返回构建版本、commit、构建时间和 dirty 状态。 |
| `GET /openapi.json` | 返回运行时 API schema。 |
| `POST /memory/turn-event` | Agent 事件写入。 |
| `POST /memory/search` | 统一检索。 |
| `POST /memory/candidates/maintenance/run` | 手动触发候选维护。 |
| `POST /memory/kernel/governance/run` | 触发 Memory Kernel 治理。 |
| `POST /memory/kernel/context-pack` | 构建 Context Pack。 |

## Web 控制台

Nuxt 控制台提供：

- 工作区级总览
- 候选记忆审查
- Hot Memory 管理
- Archive 和检索检查
- 写入与维护诊断
- Secret Vault 接入指引
- 审计与访问日志
- 租户、Token 和项目配置

总览页按用户/工作区聚合展示。更深层的工作流页面仍保留项目作用域和权限边界。

## 部署

参考技术栈：

- Go / Hertz API
- PostgreSQL
- Redis
- Qdrant
- Nuxt 3 前端
- Docker Compose

生产服务：

| Service | 作用 |
| --- | --- |
| `memory-api` | HTTP API 和 Web 控制台后端。 |
| `memory-worker` | 后台任务、提炼、维护、归档索引。 |
| `memory-mcp` | Streamable HTTP MCP 服务。 |
| `memory-web` | Web 控制台。 |
| `postgres` | 元数据权威源。 |
| `redis` | 队列、锁、缓存、限流。 |
| `qdrant` | 可重建向量索引。 |

仓库包含维护者单节点主机的部署自动化。执行部署、重启或生产验证前，请先阅读 [DEPLOYMENT.md](DEPLOYMENT.md)。

常用验证命令：

```bash
go test ./...
go vet ./...
npm --prefix frontend run build
git diff --check
```

部署后验证：

```bash
VERIFY_MODE=full make post-deploy-verify
```

运行时检查：

```bash
curl "$MEMORY_OS_API_URL/healthz"
curl "$MEMORY_OS_API_URL/version"
curl "$MEMORY_OS_API_URL/openapi.json"
```

## 安全模型

Memory OS 默认按保守记忆安全边界设计：

- 真实 API key、PAT、密码、cookie、私钥和 token 不得进入代码、日志、Markdown、Qdrant、Hot Memory、README 示例或测试快照。
- PAT 明文只在创建时展示一次。
- Secret 明文只允许进入 Secret Vault 加密存储或本地解密注入路径。
- Qdrant 查询必须使用 query-time payload filter。
- HTTP Handler 只做协议、认证、校验和错误映射。
- Service 负责业务行为。
- Repository 负责 SQL 和事务。
- Adapter 只转换事件，不直接写 Markdown、Hot Memory 或 Qdrant。

## 目录结构

```text
cmd/
  memory-api          HTTP API
  memory-worker       后台 worker
  memory-mcp          Streamable HTTP MCP 服务
  memory-mcp-local    stdio MCP 本地代理
  memory-bootstrap    首个管理员 bootstrap 工具

internal/
  eventlog            TurnEvent 写入和 payload 处理
  candidatememory     候选提炼、triage、维护和归档素材
  hotmemory           高价值短期记忆
  memorykernel        当前事实、治理和 Context Pack
  archive             Markdown Archive 元数据和正文
  retrieval           Hot Memory + Archive RAG 统一检索
  qdrant              向量索引和 payload filter
  tenant              用户、组织、项目和权限
  secret              加密 Secret Vault
  secretlocal         本地 Secret 解密桥接
  mcp                 MCP schema 和工具处理
  http                API 路由

frontend/
  Nuxt 3 Web 控制台

deploy/
  Docker Compose 和生产容器文件

scripts/
  验证、部署、备份、恢复和安全脚本

migrations/
  PostgreSQL schema migrations
```

## 项目原则

- 每条记忆都应能追溯到事件、归档、chunk、项目、线程和 actor。
- 优先使用显式权限上下文，不依赖隐式信任。
- Archive 是历史证据，Memory Units 是当前上下文。
- 生产健康必须能通过日志、表、诊断页面和验证脚本解释。
- MCP 工具出现在 schema 里不代表完成，必须确认 handler 和下游链路可用。

## License

当前仓库尚未包含 license 文件。添加 license 前，请按私有/专有项目处理。
