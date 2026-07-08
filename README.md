# Memory OS v0.9

Memory OS 是一个原生多 Agent 记忆平台，用于把 Codex、Claude Code、Cursor、opencode、Hermes 等 Agent 的工作过程沉淀为可检索、可追溯、可治理的长期记忆。

v0.9 的重点是把记忆生命周期跑成闭环：事件写入、候选提炼、Hot Memory、AI 整理、归档任务、Archive RAG、统一检索、MCP 接入和管理台总览已经形成一条可部署、可验证的生产路径。

## 核心能力

- **统一记忆入口**：HTTP API 和 MCP 都走同一套 Memory OS 后端能力。
- **用户级总览**：管理台展示当前用户的全部记忆，不按项目或 Agent 分开展示总数。
- **项目级治理**：记忆写入、整理、归档和检索仍保留项目维度，用于隔离上下文和权限边界。
- **Agent 来源标记**：Agent ID 只作为来源 metadata，不作为统计或存储隔离维度。
- **候选记忆闭环**：TurnEvent 进入候选队列，worker 提炼候选；AI 整理把候选分流为待确认、丢弃、Hot Memory 或归档素材，归档任务再写成 Markdown Archive。
- **Hot Memory**：高价值短期工作记忆可以进入 Hot Memory，并在检索命中后记录使用反馈。
- **Archive RAG**：长期沉淀以 Markdown 为正文权威源，Qdrant 只保存可重建索引。
- **Secret 安全边界**：Secret 明文只进入加密存储和本地解密路径，不进入日志、Markdown、Qdrant、Hot Memory 或模型回复。
- **生产部署脚本**：通过 Docker Compose 管理 API、worker、MCP、Web、PostgreSQL、Redis、Qdrant。

## 当前架构

```text
Agent / MCP Client
        |
        v
memory-mcp / memory-api
        |
        v
TurnEvent -> candidate_memory_jobs -> memory-worker
        |                                |
        |                                v
        |                         Candidate Memory
        |                                |
        |              +-----------------+-----------------+
        |              |                                   |
        v              v                                   v
   Hot Memory      AI Organizer                      Archive Composer
        |              |                                   |
        |              v                                   v
        |   discard / review / hot/archive         Markdown Archive
        |                                                  |
        +------------------------+-------------------------+
                                 v
                       Unified Retrieval
                    Hot Memory + Archive RAG
```

权威数据源：

- PostgreSQL：元数据、权限、任务、候选、Hot Memory、Archive 元信息。
- Markdown 文件：Archive 正文权威源。
- Qdrant：向量索引，可从 Archive/Hot Memory 重建。
- Redis：队列、锁、缓存和限流。

## 记忆生命周期

1. Agent 通过 MCP 或 HTTP 写入 TurnEvent。
2. API 根据事件类型和价值判断创建 candidate job。
3. `memory-worker` 消费 job，调用 LLM 提炼候选记忆。
4. 候选按规则进入 Hot Memory、待整理候选或归档素材池。
5. worker 按项目串行触发 AI 整理，避免模型 provider 并发过高。
6. AI 整理调用 LLM 做统一去向决策，执行丢弃、保留、待确认、写入 Hot Memory 或进入归档素材。
7. Archive Composer 把满足条件的归档素材写成 Markdown Archive。
8. Archive 进入索引队列，生成 Qdrant chunk 索引。
9. `/memory/search` 和 MCP `memory_search` 统一检索 Hot Memory + Archive RAG。
10. 检索使用情况写入反馈，用于后续排序和治理。

## MCP 接入

远程 MCP Streamable HTTP 入口：

```text
POST <memory-os-mcp-url>/mcp
Authorization: Bearer <Memory OS PAT>
Accept: application/json, text/event-stream
```

兼容 HTTP bridge：

```text
GET  <memory-os-mcp-url>/tools
POST <memory-os-mcp-url>/tools/call
```

当前 MCP 工具状态：

| Tool | 状态 | 说明 |
| --- | --- | --- |
| `memory_search` | 已实现 | 统一检索 Hot Memory 和 Archive RAG |
| `memory_mark_used` | 已实现 | 标记检索结果已使用 |
| `memory_stats` | 占位 | 当前仍返回 phase 1 not implemented |
| `memory_archive` | 占位 | 后续接入手动归档 |
| `memory_append_event` | 占位 | 后续接入完整事件写入工具 |
| `memory_get_archive` | 占位 | 后续接入 Archive 读取 |

对于只支持 stdio MCP 的客户端，可以使用本地代理：

```bash
go build -o ~/bin/memory-mcp-local ./cmd/memory-mcp-local
```

配置示例：

```json
{
  "mcpServers": {
    "memory-os": {
      "command": "/Users/your-name/bin/memory-mcp-local",
      "env": {
        "MEMORY_OS_MCP_URL": "<memory-os-mcp-url>",
        "MEMORY_OS_TOKEN": "<Memory OS PAT>"
      }
    }
  }
}
```

`memory-mcp-local` 会读取当前工作目录的 Git 信息，把去除凭据后的 `git_remote`、`git_root`、branch、commit 传给服务器。服务器按 Git 仓库自动创建或复用项目空间。

## 管理台

Web 控制台入口：

```text
<memory-os-web-url>
```

主要页面：

- 总览：用户级记忆生命周期统计。
- 记忆：Hot Memory 和候选状态。
- 检索：统一检索调试。
- 写入诊断：事件写入与候选链路检查。
- Secret：Secret Vault 和本地解密接入向导。
- 日志：运行与审计信息。
- 高级设置：Qdrant、Token、权限和项目配置。

展示口径：

- 总览以用户为准，显示当前用户全部记忆。
- 存储、整理、归档和检索仍按项目隔离。
- Agent 只标记来源，不作为展示统计维度。

## 部署

Memory OS 可以部署到任意支持 Docker 和 Docker Compose 的 Linux 主机。仓库内提供 Compose 文件作为参考部署拓扑，具体端口、域名、TLS、反向代理、防火墙和数据目录应由部署方按自己的环境配置。

服务拓扑：

| 服务 | 说明 |
| --- | --- |
| Web | 管理台前端 |
| API | Memory OS HTTP API |
| MCP | Streamable HTTP MCP 服务 |
| Worker | 后台队列、候选提炼、AI 整理和归档 |
| PostgreSQL | 权威元数据源 |
| Redis | 队列、锁、缓存和限流 |
| Qdrant | 可重建向量索引 |

部署、重启、验证和 T480 日常发布统一按 [DEPLOYMENT.md](DEPLOYMENT.md) 执行。不要把本地私有路径、主机名或密钥写入仓库。

## 本地验证

后端：

```bash
go test ./...
go vet ./...
```

前端：

```bash
npm --prefix frontend run build
```

安全和格式：

```bash
make secret-scan
git diff --check
```

运行时检查：

```bash
curl "$MEMORY_OS_API_URL/version"
curl "$MEMORY_OS_API_URL/healthz"
curl "$MEMORY_OS_API_URL/openapi.json"
curl -X POST "$MEMORY_OS_MCP_URL/mcp" \
  -H "Authorization: Bearer $MEMORY_OS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

部署后：

```bash
VERIFY_MODE=full make post-deploy-verify
```

## 版本

当前版本：`v0.9`

构建默认版本：`0.9.0-dev`

运行时版本接口：

```bash
curl "$MEMORY_OS_API_URL/version"
```

返回字段：

```json
{
  "version": "0.9.0-dev",
  "commit": "<git short sha>",
  "build_time": "<utc timestamp>",
  "dirty": "false"
}
```

## 安全原则

- 不把真实密钥写入代码、日志、README、测试快照或 MCP 配置示例。
- PAT 明文只在创建时展示一次。
- Secret 明文只允许进入 Secret Vault 加密存储或本地解密注入路径。
- Qdrant 查询必须使用 query-time payload filter。
- Handler 只做协议、认证、校验和错误映射；业务逻辑在 Service；SQL 和事务在 Repository。
- Adapter 只转换 TurnEvent，不直接写 Markdown、Hot Memory 或 Qdrant。

## 目录速览

```text
cmd/
  memory-api           HTTP API 和管理台后端
  memory-worker        后台队列、候选提炼、AI 整理和归档
  memory-mcp           远程 MCP Streamable HTTP 服务
  memory-mcp-local     stdio MCP 本地代理

internal/
  eventlog             TurnEvent 写入和脱敏
  candidatememory      候选记忆、AI 整理、归档素材
  hotmemory            热记忆
  archive              Markdown Archive
  retrieval            统一检索
  qdrant               向量索引和 payload filter
  secret / secretlocal Secret Vault 和本地解密
  memorystats          生命周期统计

frontend/
  Nuxt 3 管理台

deploy/
  Docker Compose、Dockerfile、nginx 配置

scripts/
  同步、部署、验证、安全扫描脚本
```

## 生产判断标准

不能只看单个 smoke 结果判断系统健康。至少需要同时确认：

- `/version` 指向预期 commit，且 `dirty=false`。
- `/healthz` 中 db、redis、qdrant 都为 ok。
- `memory-worker` 正在运行。
- candidate job 可以进入 done。
- 自动 maintenance 可以产生 `auto|done|done` run。
- Archive 可以生成 active 记录。
- archive index job 可以 completed。
- `memory_search` 可以通过 MCP 或 HTTP 返回可解释来源。
