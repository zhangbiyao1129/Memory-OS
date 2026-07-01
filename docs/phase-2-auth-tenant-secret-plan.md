# Phase 2 Auth、Tenant、Permission、Secret Vault AI 自驱动实现计划

> 面向 AI 代理的工作者：执行本计划前必须读取 `README.md`、`AGENTS.md`、`docs/memory-os-spec.md`、`docs/phase-1-implementation-plan.md`。所有代码改动前创建 TodoList。除非用户明确要求，不要 commit、push、deploy。

**目标：** 建立多用户、多组织、多项目权限基础，并在 TurnEvent、Archive、RAG、Hot Memory 上线前完成 Secret Vault 和审计基座。

**架构：** PostgreSQL 保存用户、组织、项目、成员、角色、权限、Token、Secret 元数据和审计日志；Secret 明文仅加密保存；API、worker、MCP 统一使用权限上下文和 Secret 注入接口。

**技术栈：** Go、Hertz、pgx/v5、zap、PostgreSQL、Redis、AES-GCM 或 envelope encryption、bcrypt/argon2id、PAT、Adapter Token。

---

## 1. 成功标准

- `make test` 通过。
- `make smoke` 通过并包含 auth/tenant/secret smoke 子项。
- 可以创建 user、org、project、membership。
- 可以 password login 并获取 session 或 token。
- 可以创建、校验、吊销 PAT。
- 可以创建、校验、吊销 Adapter Token。
- Secret 明文加密保存，API 响应永不返回明文。
- Secret 使用时自动解密注入，并写审计日志。
- 日志、错误、测试快照不包含 Secret 明文。

## 2. 推荐文件结构

```text
internal/auth/
  password.go
  token.go
  middleware.go
  repository.go
  service.go
  service_test.go
internal/tenant/
  model.go
  repository.go
  service.go
  permissions.go
  permissions_test.go
internal/secret/
  vault.go
  crypto.go
  detector.go
  injector.go
  repository.go
  service.go
  crypto_test.go
  detector_test.go
internal/audit/
  repository.go
  service.go
  model.go
migrations/000002_auth_tenant_secret.sql
cmd/memory-api/handlers/auth.go
cmd/memory-api/handlers/tenant.go
cmd/memory-api/handlers/secret.go
```

## 3. 数据库设计

最低表：

- `users`
- `orgs`
- `projects`
- `memberships`
- `roles`
- `resource_permissions`
- `password_credentials`
- `personal_access_tokens`
- `adapter_tokens`
- `secrets`
- `secret_versions`
- `audit_logs`

关键要求：

- Token 只存 hash，不存明文。
- Secret 明文必须加密后存储。
- `secret_ref` 必须唯一、不可猜测。
- membership 和 permission 查询必须有索引。
- audit log 不得保存 Secret 明文。

## 4. 执行任务

### 任务 1：迁移和模型

- [ ] 新增 migration，创建 auth、tenant、secret、audit 表。
- [ ] 为关键唯一约束和索引写 SQL。
- [ ] 添加 repository model。
- [ ] 写 repository 单测或 migration smoke。
- [ ] 运行 `go test ./...`。

验收：重复 user email、重复 token id、重复 secret_ref 会被唯一约束阻止。

### 任务 2：Tenant service

- [ ] 实现创建 user、org、project。
- [ ] 实现添加 membership。
- [ ] 实现角色和 permission label 查询。
- [ ] 实现权限上下文 `PermissionContext`。
- [ ] 写单测覆盖跨 org、跨 project、无 membership。
- [ ] 运行 `go test ./...`。

验收：权限上下文可被 retrieval 和 Qdrant filter builder 直接使用。

### 任务 3：Password login

- [ ] 实现密码 hash。
- [ ] 实现 password login。
- [ ] 实现 auth middleware。
- [ ] 写单测覆盖正确密码、错误密码、禁用用户。
- [ ] 运行 `go test ./...`。

验收：密码明文不进入日志和数据库。

### 任务 4：PAT

- [ ] 实现 PAT 创建。
- [ ] 明文 token 只在创建时返回一次。
- [ ] 数据库存 hash。
- [ ] 实现 PAT 校验和吊销。
- [ ] 写单测覆盖过期、吊销、错误 token。
- [ ] 运行 `go test ./...`。

验收：PAT 可用于 API 认证，日志只显示 token 前缀或 token id。

### 任务 5：Adapter Token

- [ ] 实现 Adapter Token 创建。
- [ ] 绑定 user、org、project、agent_id、scope。
- [ ] 实现校验、吊销、过期。
- [ ] 写单测覆盖 agent/project 不匹配。
- [ ] 运行 `go test ./...`。

验收：Adapter Token 可被 Phase 3 TurnEvent API 使用。

### 任务 6：Secret crypto

- [ ] 实现加密和解密接口。
- [ ] 使用 master key 环境变量或开发默认 key guard。
- [ ] 实现 key id、version、nonce、ciphertext 存储。
- [ ] 写单测覆盖 round trip、错误 key、篡改 ciphertext。
- [ ] 运行 `go test ./...`。

验收：加密失败不会写入 secret 记录。

### 任务 7：Secret detector 和 sanitizer

- [ ] 实现常见 token、API key、password、private key 检测。
- [ ] 实现 `secret_ref` 替换。
- [ ] 写单测覆盖正常文本、多个 secret、假阳性可接受范围。
- [ ] 运行 `go test ./...`。

验收：输入 fake secret 后输出不包含原文。

### 任务 8：Secret Vault service

- [ ] 实现创建 secret。
- [ ] 实现按权限读取 secret metadata。
- [ ] 实现更新 secret version。
- [ ] 实现禁用 secret。
- [ ] API 响应只返回 metadata 和 `secret_ref`。
- [ ] 写单测覆盖无权限读取和禁用 secret。
- [ ] 运行 `go test ./...`。

验收：API 永不返回 Secret 明文。

### 任务 9：Secret injector 与审计

- [ ] 实现工具调用时根据 `secret_ref` 解密注入。
- [ ] 每次注入写 audit log。
- [ ] 审计记录 actor、tool、purpose、request_id、result。
- [ ] 写单测覆盖成功、无权限、secret disabled。
- [ ] 运行 `go test ./...`。

验收：审计日志不含 Secret 明文。

### 任务 10：API 和 smoke

- [ ] 增加 auth、tenant、secret API handler。
- [ ] 更新 Swagger。
- [ ] 更新 `make smoke`，加入 tenant/auth/secret 测试。
- [ ] 运行 `make test`。
- [ ] 运行 `make smoke`。

验收：端到端创建用户、登录、创建项目、创建 token、保存 fake secret、注入审计成功。

## 5. 集成测试方案

测试必须覆盖：

- Alice 是 org_alpha/project_alpha 成员。
- Eve 是 org_beta/project_beta 成员。
- Alice 不能访问 Eve 的 project。
- Adapter Token 只能写入绑定 project。
- Secret 创建后只能看到 metadata。
- Secret 注入需要权限并写审计。
- fake secret 不出现在日志、API 响应、audit payload。

## 6. 不做事项

Phase 2 不做：

- 完整 TurnEvent ingest。
- Archive 生成。
- Qdrant 索引。
- Hot Memory 派生。
- 企业 SSO。
- 多租户计费。

## 7. 子代理建议

Phase 2 主会话为主。可交给子代理的任务：password/PAT 测试矩阵、Secret detector 规则测试、audit repository 测试。

必须由主会话控制的任务：tenant 权限模型、Secret crypto、Secret injector、auth middleware 主流程。
