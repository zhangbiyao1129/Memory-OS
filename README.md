# Memory OS v0.4

Memory OS 是一个 Native Multi-Agent Memory Platform，用于把 Codex、Claude Code、opencode、Hermes 以及其它 Agent 平台中的对话、项目经验、排障记录、用户偏好、Secret 引用和长期决策沉淀为可召回、可追溯、可治理的统一记忆系统。

## 当前交付状态

当前仓库处于 v0.4 生产级完全体开发中，不能仅凭 smoke 通过声明完成。

生产级完成标准见：

- `docs/ai-execution-master-plan.md`
- `docs/completion-audit-checklist.md`
- `docs/production-baseline-report.md`

## 默认运行环境

Memory OS 项目的开发默认在本地编辑源码，测试、构建、部署默认在 T480 的 `/opt/memory-os` 执行。本地不运行 Memory OS 容器。

日常开发流程:

```bash
# 本地同步到 T480,不经过 GitHub
make t480-sync

# 同步后在 T480 跑测试和前端构建
make t480-build-check

# 同步后在 T480 构建镜像、部署并做部署后验证
make t480-deploy
```

GitHub 只作为稳定改动的远端仓库使用。日常构建和部署不依赖 `git pull`;只有当你明确决定保存一个稳定节点时,再手动 `git commit` 和 `git push`。

默认端口（部署到自己的服务器时按需替换）：

- Web：`18080`
- API：`18081`
- MCP：`18082`
- Qdrant：`18083`

## 常用验证命令

在项目根目录下运行：

```bash
make test
make build-web
make smoke
docker-compose -f deploy/docker-compose.yml ps
curl http://127.0.0.1:18081/healthz
curl http://127.0.0.1:18081/openapi.json
```

## Agent MCP 接入

日常使用只需要一个“通用 MCP Token”，不需要按项目或按 Agent 创建多个 Token。

正式接入优先使用远程 MCP 服务：Agent 客户端配置 Memory OS 的服务地址和 Token 后，直接调用 MCP 暴露的工具能力。Agent 名称不要求用户手动填写；服务端会优先读取 `X-Memory-Agent-ID`，没有时从 `User-Agent` 自动识别 Claude Code、Codex、Cursor、opencode、Cline、Roo、Hermes 等常见客户端，仍无法识别时使用 `mcp` 作为兜底来源 metadata。

远程 HTTP 入口：

```text
POST http://your-server:18082/mcp
Authorization: Bearer <后台 Token 页面创建的一次性明文 Token>
Accept: application/json, text/event-stream
```

`/mcp` 是标准 MCP Streamable HTTP JSON-RPC 入口，支持 `initialize`、`tools/list`、`tools/call` 和 `ping`。`/tools`、`/tools/call` 仍保留为旧 bridge 兼容接口。

`agent_id` 只作为来源 metadata，不决定项目归属。项目归属默认由 Git remote / workspace identity 自动判定；同一个 Token 可以配置给 Codex、Claude Code、opencode、Hermes 等 Agent。

对于只支持 stdio MCP 的客户端，可以使用 `memory-mcp-local` 作为兼容入口。它会在每次 `memory_search` 时读取当前工作目录的 Git 信息，并把去除凭据后的 `git_remote`、`git_root`、branch、commit 传给服务器。服务器再按同一个 Git 仓库自动创建或复用项目空间。

构建本地入口：

```bash
go build -o ~/bin/memory-mcp-local ./cmd/memory-mcp-local
```

MCP 配置示例：

```json
{
  "mcpServers": {
    "memory-os": {
      "command": "/Users/你的用户名/bin/memory-mcp-local",
      "env": {
        "MEMORY_OS_MCP_URL": "http://your-server:18082",
        "MEMORY_OS_TOKEN": "<后台 Token 页面创建的一次性明文 Token>"
      }
    }
  }
}
```

## 生产级开发原则

- PostgreSQL 是权威元数据源。
- Markdown 文件是 Archive 正文权威源。
- Qdrant 只作为可重建索引。
- Redis 只作为队列、锁、缓存和限流组件。
- Secret 明文只允许进入 Secret Vault 加密存储。
- 管理 API 默认必须认证。
- Qdrant 检索必须使用 query-time payload filter。
- 不允许使用 dev smoke endpoint 冒充生产能力。
- 不允许把静态页面或内存仓库当作生产实现。

## 当前已知基线缺口

详见 `docs/production-baseline-report.md`。截至 Phase 0 审计，当前系统仍存在：

- 生产 API 仍部分使用内存仓库。
- 管理台多页面仍为静态假数据或本地状态。
- 浏览器端 API 预检已在 Phase 1.1 修复，但检索仍返回后端 `retrieval_not_configured`。
- OpenAPI 只覆盖少量路由。
- dev smoke endpoint 仍在当前服务器部署中启用。

这些缺口修复前，不能声明 Memory OS v0.4 生产级完全体完成。
