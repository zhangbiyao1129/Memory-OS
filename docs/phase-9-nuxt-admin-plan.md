# Phase 9 Nuxt 管理台完整化 AI 自驱动实现计划

> 面向 AI 代理的工作者：执行本计划前必须读取 spec 和 Phase 1-8 计划。所有代码改动前创建 TodoList。除非用户明确要求，不要 commit、push、deploy。

**目标：** 完整化 Nuxt 管理台，支持用户、组织、项目、权限、Archive、Hot Memory、Secret Vault、Adapter Token、Qdrant 索引状态、检索测试和热度统计。

**架构：** Nuxt 3 前端通过 API client 访问 Memory OS；Pinia 管理 auth/session/project context；页面按领域模块组织；所有敏感值默认隐藏，Secret 明文永不展示。

**技术栈：** Nuxt 3、Vue 3、Tailwind CSS、Pinia、Nuxt UI 或 shadcn-vue、TypeScript、Nginx static hosting。

---

## 1. 成功标准

- `make build-web` 通过。
- `make smoke` 包含前端静态资源或 health 页面检查。
- 登录页可用。
- Dashboard 可显示 health 和统计。
- 用户/组织/项目/权限可管理。
- Archive 可浏览、编辑、查看版本、触发重索引。
- Hot Memory 可管理。
- Secret Vault 可管理 metadata，不显示明文。
- Adapter Token 可创建，明文只显示一次。
- 检索测试页可调用 `/memory/search`。

## 2. 推荐文件结构

```text
web/
  app.vue
  nuxt.config.ts
  package.json
  assets/css/tailwind.css
  composables/useApi.ts
  composables/useAuth.ts
  stores/auth.ts
  stores/context.ts
  pages/login.vue
  pages/index.vue
  pages/orgs/index.vue
  pages/projects/index.vue
  pages/archive/index.vue
  pages/archive/[id].vue
  pages/hot-memory/index.vue
  pages/secrets/index.vue
  pages/tokens/index.vue
  pages/qdrant/index.vue
  pages/search-test.vue
  components/
    AppShell.vue
    SecretValueGuard.vue
    SourceRefList.vue
    HealthBadge.vue
```

## 3. 执行任务

### 任务 1：API client 与 auth store

- [ ] 实现 `useApi`。
- [ ] 实现 auth store。
- [ ] 处理 401、403、网络错误。
- [ ] 写前端单测，如果项目测试框架已存在。
- [ ] 运行 `npm run build`。

验收：API base URL 可配置。

### 任务 2：布局和导航

- [ ] 实现 AppShell。
- [ ] 实现导航菜单。
- [ ] 实现 org/project context switcher。
- [ ] 移动端可用。
- [ ] 运行 `npm run build`。

验收：核心页面可导航。

### 任务 3：Auth 页面

- [ ] 登录页。
- [ ] 登出。
- [ ] token 保存策略。
- [ ] 错误展示不泄露敏感信息。
- [ ] 运行 `npm run build`。

验收：登录成功进入 dashboard。

### 任务 4：Tenant 管理

- [ ] 用户列表。
- [ ] 组织列表。
- [ ] 项目列表。
- [ ] membership 管理。
- [ ] role/permission label 管理。
- [ ] 运行 `npm run build`。

验收：权限变化可反映到检索上下文。

### 任务 5：Archive 管理

- [ ] Archive 列表。
- [ ] Archive 详情。
- [ ] Markdown 编辑。
- [ ] 版本列表。
- [ ] 重索引按钮。
- [ ] source refs 展示。
- [ ] 运行 `npm run build`。

验收：编辑后能看到新 version/index_generation。

### 任务 6：Hot Memory 管理

- [ ] Hot Memory 列表。
- [ ] 搜索和过滤。
- [ ] promote/demote。
- [ ] soft delete。
- [ ] source refs 展示。
- [ ] 运行 `npm run build`。

验收：热度和状态可见。

### 任务 7：Secret Vault 管理

- [ ] Secret metadata 列表。
- [ ] 创建 Secret。
- [ ] 更新 Secret version。
- [ ] 禁用 Secret。
- [ ] 使用 `SecretValueGuard` 防止明文长期显示。
- [ ] 运行 `npm run build`。

验收：Secret 明文只在必要输入框短暂存在，不进入日志。

### 任务 8：Adapter Token 管理

- [ ] Token 列表。
- [ ] 创建 Token。
- [ ] 明文 token 只显示一次。
- [ ] 吊销 Token。
- [ ] 运行 `npm run build`。

验收：刷新页面后看不到 token 明文。

### 任务 9：Qdrant 和检索测试页

- [ ] Qdrant collection 状态页。
- [ ] index job 状态页。
- [ ] `/memory/search` 测试页。
- [ ] 展示 rerank_degraded、source refs、filters 摘要。
- [ ] 运行 `npm run build`。

验收：用户可手工测试检索。

### 任务 10：前端 smoke

- [ ] 更新 `make build-web`。
- [ ] 更新 `make smoke` 检查 web health 或静态首页。
- [ ] 运行 `make build-web`。
- [ ] 运行 `make smoke`。

验收：web 可构建和托管。

## 4. 集成测试方案

必须覆盖：

- 登录后访问 dashboard。
- 未登录跳转 login。
- Archive 编辑触发 version 更新。
- Secret 明文不出现在页面持久状态。
- Token 明文只显示一次。
- Search test 显示 source refs。

## 5. 不做事项

Phase 9 不做：

- 移动 App。
- 多语言。
- 复杂可视化大屏。
- 企业 SSO UI。

## 6. 子代理建议

Phase 9 适合多子代理并行。可交给子代理的任务：Archive 页面、Hot Memory 页面、Secret Vault 页面、Adapter Token 页面、Qdrant 状态页、Search test 页面。

主会话负责 app shell、auth/session/context store、API client 统一错误处理、最终 build 和交互一致性。
