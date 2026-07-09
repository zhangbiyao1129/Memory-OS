# Memory OS Deployment Guide

本文件是 Memory OS 的唯一部署手册。执行部署、重启、生产验证或排障前，先读本文件；不要只凭记忆运行命令。

## 目标

- 日常部署要快：只同步必要源码，按变更部署受影响服务，按风险选择验证范围。
- 生产部署要稳：部署前先自检和 dry-run，部署后必须有验证日志和耗时证据。
- 远端部署目录以 `thinkpad:/opt/memory-os` 为默认目标；如需覆盖，使用 `TARGET_HOST` 和 `TARGET_DIR`。

## 硬规则

- 本地只编辑源码，不在本地跑生产容器。
- GitHub 只作为稳定代码仓库；日常部署使用 rsync 到 T480，不改成服务器 `git pull`。
- 真实 secret 只通过生产环境、Secret Vault 或本机安全文件注入，不写入代码、文档、日志或回复。
- 部署、重启、迁移数据库、删除数据、清理镜像缓存前必须确认影响范围。
- 高风险或不确定变更使用全量部署和 full 验证。

## 常用命令

| 场景 | 命令 | 是否重启服务 |
| --- | --- | --- |
| 部署脚本本地自检 | `make deploy-safety-check` | 否 |
| 只同步源码到 T480 | `make t480-sync` | 否 |
| 远端测试和前端构建检查 | `make t480-build-check` | 否 |
| 预览部署计划和同步差异 | `make t480-deploy-dry-run` | 否 |
| 日常自动分类部署 | `make t480-deploy` | 是 |
| 强制全量部署 | `make t480-deploy-full` | 是 |
| 只跑部署后验证 | `VERIFY_MODE=full make post-deploy-verify` | 否 |

## 推荐部署流程

日常部署按这个顺序执行：

```bash
make deploy-safety-check
make t480-deploy-dry-run
make t480-deploy
```

`make t480-deploy-dry-run` 会打印部署计划，预览 rsync 差异，并在远端执行 Docker Compose 配置校验；它不会执行 `up -d`，不会重启服务。

`make t480-deploy` 会重新计算部署计划，先运行 `deploy-safety-check`，再同步源码，最后在远端部署并执行部署后验证。

## 变更分类

自动分类由 `scripts/classify-deploy-changes.sh` 完成，默认基于 `git diff --name-only HEAD -- .`。

分类规则：

- `frontend/*`：部署 `memory-web`，默认 light 验证。
- `cmd/memory-api/*`：部署 `memory-api`，默认 smoke 验证。
- `cmd/memory-worker/*`：部署 `memory-worker`，默认 smoke 验证。
- `cmd/memory-mcp/*`、`internal/mcp*`、`internal/mcp/*`、`internal/mcpstdio/*`、`internal/mcpproxy/*`：部署 `memory-mcp`，默认 smoke 验证。
- `internal/retrieval/*`、`internal/rag/*`、`internal/qdrant/*`、`internal/archive/*`、`internal/candidatememory/*`、`internal/hotmemory/*`、`internal/http/*`：部署 API、Worker、MCP，并使用 full 验证。
- `migrations/*`、`deploy/*`、`scripts/load-prod-env.sh`、`scripts/preflight.sh`、`Makefile`：全量部署并使用 full 验证。
- 没有识别到变更时：部署 API、Worker、MCP、Web，并使用 smoke 验证。

分类器宁可过度部署，不能漏部署。长期 dirty 工作区可能导致部署范围偏大，这是可接受的安全取舍。

## 验证级别

`scripts/post-deploy-verify.sh` 支持三档：

- `VERIFY_MODE=light`：检查 Compose 进程、`/version`、`/healthz`。
- `VERIFY_MODE=smoke`：包含 light，再检查 OpenAPI、OpenAPI runtime validate、`make smoke`。
- `VERIFY_MODE=full`：包含 smoke，再执行 pipeline e2e。

高风险场景必须使用 full：

- 数据库迁移。
- Compose、Dockerfile、生产环境加载、preflight 改动。
- Qdrant、Archive、Retrieval、Candidate、Hot Memory、HTTP 主流程改动。
- Secret、权限、安全、索引生成、跨服务协议改动。
- 不确定影响范围的改动。

## 部署后验收清单

涉及 MCP、候选记忆、AI 整理、归档、检索或诊断页的部署，除 `post-deploy-verify` 外还要确认：

- MCP `/tools` 暴露 `memory_search`、`memory_append_event`、`memory_archive`、`memory_get_archive`、`memory_mark_used`、`memory_stats`，且 schema 中 `memory_append_event.workspace` 为可选。
- MCP `memory_append_event` 可在 Git workspace 下写入 TurnEvent，并生成候选提炼任务。
- MCP `memory_append_event` 在无 workspace 参数时可写入 TurnEvent，并落到 inbox 项目，不应返回 `workspace is required`。
- MCP `memory_archive` 可以创建 Markdown Archive；内容中的测试 secret 形态应被替换为 `secret_ref`。
- MCP `memory_stats` 可以返回账号级生命周期统计；`memory_get_archive` 可以按权限读取归档内容。
- `/diagnostics` 能显示账号级生命周期、candidate job 队列、AI 整理任务状态和最近错误。
- `/logs` 能看到 MCP 写入类审计记录，至少包括 `turn_event.append`、`archive.create` 或 `hot_memory.mark_used` 中的一类，且 metadata 中 `source=mcp`。
- `/memory/kernel/governance/run` 可触发项目级记忆体检。
- `/memory/kernel/context-pack` 对"部署"类问题返回 DEPLOYMENT.md 规则。
- MCP `/tools` 暴露 `memory_context_pack`。
- Memory CI 用例 `memory_archive 现在实现了吗？` 通过，返回不含"尚未实现"。

## 部署提速策略

- API、Worker、MCP 共用 `deploy/backend/Dockerfile.backend` 生成的 `deploy-memory-backend` 镜像。
- `prod-up-services` 只构建受影响的 backend 或 web 镜像，然后用 `--no-build` 启动服务。
- 默认保留 Docker 构建缓存，不自动清理镜像。
- 需要主动释放磁盘时，显式执行：

```bash
CLEANUP_IMAGES=1 make prod-up
```

或：

```bash
CLEANUP_IMAGES=1 make prod-up-services
```

## 部署证据

部署完成后，最终汇报至少包含：

- 执行的部署命令。
- 部署计划中的 `services` 和 `verify_mode`。
- `post deploy verify logs: <LOG_DIR>` 输出。
- 远端 `artifacts/deploy-timing-* / timing.env` 中的耗时。
- 如执行了备份，记录备份命令和备份目录摘要。

部署耗时由 `scripts/measure-deploy-step.sh` 写入远端 `artifacts/deploy-timing-* / timing.env`。

## 失败处理

如果 `deploy-safety-check` 失败：

1. 先修复脚本语法、分类器测试或 Makefile 目标问题。
2. 重新运行 `make deploy-safety-check`。
3. 不要跳过自检直接部署。

如果 `t480-deploy-dry-run` 失败：

1. 先看 rsync 或远端 Compose 配置错误。
2. 不要执行 `make t480-deploy`。
3. 修复后重新 dry-run。

如果 `make t480-deploy` 已经重启服务但验证失败：

1. 保留 `post deploy verify logs` 输出目录。
2. 查看失败步骤对应日志，例如 `version.log`、`healthz.log`、`smoke.log`、`pipeline-e2e.log`。
3. 用最小修复重新部署；不要用 light 或 smoke 代替原本需要的 full 验证。

## 目标覆盖

默认目标：

```bash
TARGET_HOST=thinkpad
TARGET_DIR=/opt/memory-os
```

临时覆盖示例：

```bash
TARGET_HOST=thinkpad TARGET_DIR=/opt/memory-os make t480-deploy-dry-run
```

不要把私有主机名、真实域名、token、密码或生产 secret 写入仓库文档。
