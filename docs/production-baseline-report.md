# Memory OS v0.4 生产级基线报告

生成时间：2026-07-02

## 结论

当前系统不能声明为生产级完全体。服务器部署可启动，`make test`、`make build-web`、`make smoke` 基线通过，但真实生产能力仍停留在“基础骨架 + 部分 service + dev smoke + 静态管理台”阶段。

必须先修复 P0/P1/P2 缺口，再进入最终交付声明。

## 环境基线

- 服务器：`thinkpad`
- 项目目录：`/opt/memory-os`
- Web：`http://ddns.08121.top:18080`
- API：`http://ddns.08121.top:18081`
- MCP：`18082`
- Qdrant：`18083`
- 服务器目录状态：`/opt/memory-os` 不是 git 仓库，不能在服务器直接做 branch、diff、commit 级追踪。
- 本地仓库分支：`main`
- 本地仓库状态：已有多项未提交改动，后续实现不得回滚未知改动。

## 新鲜验证证据

服务器 `/opt/memory-os` 下执行：

```bash
make test
```

结果：通过。所有 Go package 测试返回 `ok`。

```bash
make build-web
```

结果：通过。Nuxt 3 production build 完成，Nitro server build 完成。

```bash
make smoke
```

结果：通过，输出 `smoke ok`。

```bash
docker-compose -f deploy/docker-compose.yml ps
```

结果：`memory-api`、`memory-web`、`memory-mcp`、`memory-worker`、`postgres`、`redis`、`qdrant` 均为 Up。

```bash
curl http://127.0.0.1:18081/healthz
```

结果：HTTP 200，`db`、`qdrant`、`redis` 均为 `ok`。

```bash
curl http://127.0.0.1:18081/openapi.json
```

结果：HTTP 200，但 OpenAPI 只包含 `/healthz`、`/openapi.json`、`/memory/search`。

## 代码审计发现

### API 路由

当前真实公开路由只有：

- `GET /healthz`
- `GET /openapi.json`
- `POST /memory/turn-event`
- `POST /memory/search`
- development + dev endpoints 下的 `/dev/smoke/*`

缺失生产级管理 API：

- Auth / PAT / Adapter Token API
- 用户 / 组织 / 项目 / 成员 / 角色 / 权限 API
- Secret Vault API
- Archive CRUD / version / reindex API
- Hot Memory CRUD / promote / demote / mark_used API
- Qdrant status / index jobs API
- Retrieval access log API

### 生产仓库实现

当前生产启动仍存在内存实现：

- `cmd/memory-api/main.go` 中 Auth 使用 `auth.NewMemoryRepository()`。
- `cmd/memory-api/main.go` 中 Tenant 使用 `tenant.NewMemoryRepository()`。
- `cmd/memory-api/main.go` 中 Archive RAG 使用 `rag.NewMemoryStore()`。
- `internal/http/router.go` 中 `TurnEventHandler` 使用全局 `devEventLogService`，底层为 `eventlog.NewMemoryRepository()`。

已有但不完整：

- Hot Memory 已存在 PG repository。
- migrations 已覆盖 Auth/Tenant/Secret、TurnEvent、Archive、RAG、Hot Memory、Retrieval、Importer 的部分表。
- 多数 service 层具备初始业务能力，但缺 HTTP 管理闭环和生产 repository。

### 前端页面

浏览器验收确认：

- 首页：health 显示 `offline`，控制台有错误；侧边栏组织 / 项目 / Agent 选择器为静态选项。
- 组织页：静态 Users / Orgs / Permission Labels，无创建、编辑、删除、成员管理。
- 项目页：静态项目卡片，无 CRUD。
- 归档库：静态 archive 列表，无真实创建、删除、版本列表、重建索引。
- 热记忆：本地 `ref` 数组，`降权` 只改本地状态。
- Secret Vault：静态 metadata，有输入框但无真实保存 API。
- Adapter Token：前端本地生成随机 token，不是后端真实 token。
- Qdrant 状态：静态说明，不读取真实 collection 或索引任务。
- 检索测试：点击 `运行检索` 后浏览器报 `Failed to fetch`。

### 浏览器 API 链路

Phase 0 审计时，浏览器调用 `POST http://ddns.08121.top:18081/memory/search` 失败。

服务器验证：

```bash
curl -i -X OPTIONS http://127.0.0.1:18081/memory/search \
  -H "Origin: http://ddns.08121.top:18080" \
  -H "Access-Control-Request-Method: POST"
```

结果：HTTP 404。

判断：API 未处理 CORS preflight，导致 Web 页面跨端口调用失败。

Phase 1.1 已修复该网络层问题：

```bash
curl -i -X OPTIONS http://127.0.0.1:18081/memory/search \
  -H "Origin: http://ddns.08121.top:18080" \
  -H "Access-Control-Request-Method: POST" \
  -H "Access-Control-Request-Headers: content-type, authorization"
```

修复后结果：HTTP 204，并返回 `Access-Control-Allow-Origin`、`Access-Control-Allow-Methods`、`Access-Control-Allow-Headers`。

浏览器复测：检索页点击 `运行检索` 后不再报 `<no response> Failed to fetch`，当前进入后端业务错误 `503 Service Unavailable`。这说明 CORS 已解除，剩余问题是生产 retrieval 尚未配置完成。

## P0/P1/P2 缺口

### P0

- 暂未发现已验证的 Secret 泄露或数据破坏。
- 但由于管理 API 未实现、权限未生产化，不能证明生产权限安全。

### P1

- 生产 API 仍使用内存 Auth/Tenant/RAG/TurnEvent 仓库，重启后核心数据会丢。
- 管理台核心页面不可真实操作。
- 浏览器端 API 预检问题已在 Phase 1.1 修复；检索链路仍因 `retrieval_not_configured` 不可用。
- 当前部署启用 development dev smoke endpoints，不能作为生产配置。
- OpenAPI 与目标生产 API 严重不一致。

### P2

- 部分页面仍有英文标题或演示文案，例如 `Users`、`Orgs`、`Permission Labels`。
- Secret 页有输入但无提交动作，容易误导用户。
- Qdrant 状态页不显示真实状态。
- Token 页 token 行为是本地演示，不具备撤销和持久化。

## Phase 1 入口条件

进入 Phase 1 前应接受以下事实：

- 当前 smoke 通过只证明基础容器和 dev smoke 路径可用。
- Phase 1 必须优先完成 PG repository 和 API 启动生产依赖替换。
- 不能先做漂亮前端，否则仍会接到静态假数据或内存仓库。

## Phase 1 推荐任务

1. 为 Tenant/Auth/Secret/Archive/Audit/AccessLog 编写 repository contract tests。
2. 实现对应 PostgreSQL repository。
3. 修改 `cmd/memory-api/main.go`，生产模式接 PG repository，不再接 memory repository。
4. 将 TurnEvent service 从全局 dev memory service 改为注入式生产 service。
5. CORS 支持已完成；继续完成生产 Retrieval 配置，恢复检索业务链路。
6. 更新 OpenAPI，至少保持当前实际路由准确。
7. 运行 `go test ./...`、`make build-web`、`make smoke`。

## 阶段声明

Phase 0 已完成基线审计和证据记录，但 Memory OS v0.4 生产级完全体尚未完成。下一阶段应从 Phase 1：生产持久化基座开始。
