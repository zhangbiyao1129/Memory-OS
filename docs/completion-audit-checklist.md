# Memory OS v0.4 完成度自检清单

> 用途：AI 在声称阶段完成或项目完成前，必须逐项检查。本清单是验收门禁，不是实现计划。

## 1. 文档覆盖

- [ ] `AGENTS.md` 存在并包含项目规则。
- [ ] `docs/memory-os-spec.md` 是 v0.4 原生 spec。
- [ ] `docs/ai-execution-master-plan.md` 存在。
- [ ] Phase 1 计划存在。
- [ ] Phase 2 计划存在。
- [ ] Phase 3 计划存在。
- [ ] Phase 4 计划存在。
- [ ] Phase 5 计划存在。
- [ ] Phase 6 计划存在。
- [ ] Phase 7 计划存在。
- [ ] Phase 8 计划存在。
- [ ] Phase 9 计划存在。
- [ ] Phase 10 计划存在。

## 2. 基础设施验收

- [ ] `make dev-up` 可启动开发环境。
- [ ] `make dev-down` 可停止开发环境。
- [ ] `make test` 通过。
- [ ] `make build-web` 通过。
- [ ] `make smoke` 通过。
- [ ] `/healthz` 返回健康状态。
- [ ] Swagger/OpenAPI 可访问或可生成。
- [ ] zap JSON 日志可用。
- [ ] PostgreSQL migration 可执行。
- [ ] Redis health 可用。
- [ ] Qdrant health 可用。
- [ ] Qdrant 单 collection 已确保。

## 3. 安全验收

- [ ] `.env.example` 不含真实 secret。
- [ ] 代码不含真实 API key、token、密码、私钥、cookie、助记词。
- [ ] 日志不含 Secret 明文。
- [ ] API 错误不含 Secret 明文。
- [ ] Archive 不含 Secret 明文。
- [ ] Hot Memory 不含 Secret 明文。
- [ ] Qdrant payload 不含 Secret 明文。
- [ ] 测试快照不含 Secret 明文。
- [ ] Secret Vault 加密保存明文。
- [ ] Secret 注入写审计日志。

## 4. 权限验收

- [ ] user A 不能召回 user B private memory。
- [ ] org A 不能召回 org B archive chunk。
- [ ] project A 不能召回 project B project memory。
- [ ] `agent_specific` 默认不跨 Agent 召回。
- [ ] 同一用户跨 Agent 可召回 user/project/org scope 记忆。
- [ ] Qdrant 查询使用 query-time payload filter。
- [ ] 代码中不存在无 filter 的租户级 Qdrant search 路径。

## 5. Phase 1 验收

- [ ] Go/Hertz API skeleton 完成。
- [ ] worker skeleton 完成。
- [ ] MCP skeleton 完成。
- [ ] Nuxt skeleton 完成。
- [ ] Docker Compose 完成。
- [ ] T480 profile 完成。
- [ ] 模型网关 skeleton 完成。

## 6. Phase 2 验收

- [ ] users/orgs/projects/memberships/roles/permissions 完成。
- [ ] password login 完成。
- [ ] PAT 完成。
- [ ] Adapter Token 完成。
- [ ] Secret Vault 完成。
- [ ] Secret 注入审计完成。

## 7. Phase 3 验收

- [ ] TurnEvent v1 schema 完成。
- [ ] TurnEvent validator 完成。
- [ ] TurnEvent API 完成。
- [ ] Adapter SDK 完成。
- [ ] Codex Adapter 完成。
- [ ] Generic MCP Adapter 完成。
- [ ] 幂等写入完成。
- [ ] 截断和 hash 完成。

## 8. Phase 4 验收

- [ ] Markdown Archive 生成完成。
- [ ] Archive metadata 完成。
- [ ] Archive version 完成。
- [ ] Archive 编辑完成。
- [ ] Archive audit 完成。
- [ ] 编辑后新 `index_generation` 完成。

## 9. Phase 5 验收

- [ ] Markdown chunking 完成。
- [ ] Archive embedding 完成。
- [ ] Qdrant archive chunk indexing 完成。
- [ ] Archive RAG filtered search 完成。
- [ ] 旧 generation chunk 失效完成。

## 10. Phase 6 验收

- [ ] Hot Memory fact 存储完成。
- [ ] Hot Memory extraction 完成。
- [ ] Hot Memory embedding 完成。
- [ ] Hot Memory filtered search 完成。
- [ ] promote/demote 完成。
- [ ] mark_used 完成。

## 11. Phase 7 验收

- [ ] `/memory/search` 完成。
- [ ] MCP `memory_search` 完成。
- [ ] Hot Memory + Archive RAG 合并完成。
- [ ] rerank 完成。
- [ ] rerank 降级完成。
- [ ] context compression 完成。
- [ ] access log 完成。
- [ ] source refs 完成。

## 12. Phase 8 验收

- [ ] Claude Code Adapter 完成。
- [ ] opencode Adapter 完成。
- [ ] Hermes Adapter 完成。
- [ ] Generic Transcript Importer 完成。
- [ ] 所有 Adapter 输出 TurnEvent v1。

## 13. Phase 9 验收

- [ ] 用户管理页面完成。
- [ ] 组织管理页面完成。
- [ ] 项目管理页面完成。
- [ ] 权限管理页面完成。
- [ ] Archive 管理页面完成。
- [ ] Hot Memory 管理页面完成。
- [ ] Secret Vault 页面完成。
- [ ] Adapter Token 页面完成。
- [ ] Qdrant 状态页面完成。
- [ ] 检索测试页完成。

## 14. Phase 10 验收

- [ ] mem0 importer 完成。
- [ ] FastGPT importer 完成。
- [ ] OpenMemory/Zep/Khoj importer skeleton 完成。
- [ ] Markdown/RAG bundle export 完成。
- [ ] dry-run 完成。
- [ ] apply 幂等完成。

## 15. 最终声明规则

只有当以上适用项都有当前证据支撑时，AI 才能声明完成。

如果有任何项目未验证，必须说“未验证”。

如果有任何项目失败，必须说失败命令和关键错误。

如果只是文档修改，不能声称运行时能力已完成。
