package webdeploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const backendNoProxyDefault = "localhost,127.0.0.1,postgres,redis,qdrant,memory-api,memory-web,memory-mcp,memory-llm-mock"

func TestWebDockerfileServesGeneratedNuxtStaticSite(t *testing.T) {
	content, err := os.ReadFile("../../deploy/frontend/Dockerfile.web")
	if err != nil {
		t.Fatalf("read Dockerfile.web: %v", err)
	}
	dockerfile := string(content)

	if !strings.Contains(dockerfile, "RUN npm run generate") {
		t.Fatalf("Dockerfile.web must run nuxt generate so the SPA index.html is emitted")
	}
	if !strings.Contains(dockerfile, "ARG NUXT_PUBLIC_API_BASE=") {
		t.Fatalf("Dockerfile.web must accept optional NUXT_PUBLIC_API_BASE as a build argument")
	}
	if !strings.Contains(dockerfile, "ENV NUXT_PUBLIC_API_BASE=${NUXT_PUBLIC_API_BASE}") {
		t.Fatalf("Dockerfile.web must expose NUXT_PUBLIC_API_BASE to nuxt generate")
	}
	if !strings.Contains(dockerfile, "RUN rm -rf /usr/share/nginx/html/*") {
		t.Fatalf("Dockerfile.web must remove nginx default html before copying Nuxt output")
	}
	if !strings.Contains(dockerfile, "COPY --from=build /src/frontend/.output/public/ /usr/share/nginx/html/") {
		t.Fatalf("Dockerfile.web must copy generated Nuxt public contents into nginx html root")
	}
}

func TestComposePassesExternalAPIBaseForT480WebBuild(t *testing.T) {
	baseCompose, err := os.ReadFile("../../deploy/docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	t480Compose, err := os.ReadFile("../../deploy/docker-compose.t480.yml")
	if err != nil {
		t.Fatalf("read docker-compose.t480.yml: %v", err)
	}

	if !strings.Contains(string(baseCompose), "NUXT_PUBLIC_API_BASE: ${NUXT_PUBLIC_API_BASE:-}") {
		t.Fatalf("base compose must pass optional NUXT_PUBLIC_API_BASE to the web image build")
	}
	if strings.Contains(string(t480Compose), "your-server") {
		t.Fatalf("T480 compose must not bake placeholder API hosts into the generated web app")
	}
	if !strings.Contains(string(t480Compose), "NUXT_PUBLIC_API_BASE: ${NUXT_PUBLIC_API_BASE:-}") {
		t.Fatalf("T480 compose must pass optional NUXT_PUBLIC_API_BASE to the web image build")
	}
}

func TestComposeWebServiceDoesNotDependOnAPIContainer(t *testing.T) {
	content, err := os.ReadFile("../../deploy/docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	compose := string(content)
	webIndex := strings.Index(compose, "  memory-web:")
	volumesIndex := strings.Index(compose, "\nvolumes:")
	if webIndex < 0 || volumesIndex < 0 || volumesIndex <= webIndex {
		t.Fatal("compose must define memory-web before top-level volumes")
	}
	webService := compose[webIndex:volumesIndex]

	for _, required := range []string{
		"dockerfile: deploy/frontend/Dockerfile.web",
		"NUXT_PUBLIC_API_BASE: ${NUXT_PUBLIC_API_BASE:-}",
		`"18080:18080"`,
	} {
		if !strings.Contains(webService, required) {
			t.Fatalf("memory-web compose service missing required marker %q", required)
		}
	}
	for _, forbidden := range []string{
		"depends_on:",
		"memory-api",
	} {
		if strings.Contains(webService, forbidden) {
			t.Fatalf("memory-web compose service must not depend on API container marker %q", forbidden)
		}
	}
}

func TestFrontendApiBaseFallsBackToBrowserHost(t *testing.T) {
	useAPIContent, err := os.ReadFile("../../frontend/composables/useApi.ts")
	if err != nil {
		t.Fatalf("read useApi composable: %v", err)
	}
	nuxtConfigContent, err := os.ReadFile("../../frontend/nuxt.config.ts")
	if err != nil {
		t.Fatalf("read nuxt config: %v", err)
	}
	useAPI := string(useAPIContent)
	nuxtConfig := string(nuxtConfigContent)

	for _, forbidden := range []string{
		"your-server",
		"http://localhost:18081').replace",
	} {
		if strings.Contains(useAPI, forbidden) || strings.Contains(nuxtConfig, forbidden) {
			t.Fatalf("frontend must not ship fixed placeholder API base marker %q", forbidden)
		}
	}
	for _, required := range []string{
		"window.location.protocol",
		"window.location.hostname",
		":18081",
		"config.public.apiBase",
	} {
		if !strings.Contains(useAPI, required) {
			t.Fatalf("useApi must derive default API base from browser host, missing %q", required)
		}
	}
	if !strings.Contains(nuxtConfig, "apiBase: process.env.NUXT_PUBLIC_API_BASE || ''") {
		t.Fatal("nuxt config must leave apiBase empty by default so useApi can derive the browser host")
	}
}

func TestDockerignoreExcludesDevelopmentArtifactsFromBuildContext(t *testing.T) {
	content, err := os.ReadFile("../../.dockerignore")
	if err != nil {
		t.Fatalf("read .dockerignore: %v", err)
	}
	ignore := string(content)
	hasLine := func(marker string) bool {
		for _, line := range strings.Split(ignore, "\n") {
			if strings.TrimSpace(line) == marker {
				return true
			}
		}
		return false
	}

	for _, required := range []string{
		".gocache/",
		".codebase-memory/",
		".playwright-mcp/",
		"node_modules/",
		"frontend/.nuxt/",
		"frontend/.output/",
		"artifacts/",
		"backups/",
		"docs.zip",
		".DS_Store",
		"memory-os-login-after-dashboard-deploy.png",
	} {
		if !hasLine(required) {
			t.Fatalf(".dockerignore must exclude development artifact marker %q", required)
		}
	}
	for _, forbidden := range []string{
		"cmd/",
		"internal/",
		"frontend/",
		"migrations/",
		"deploy/",
		"go.mod",
		"package.json",
	} {
		if hasLine(forbidden) {
			t.Fatalf(".dockerignore must not exclude build input marker %q", forbidden)
		}
	}
}

func TestComposePassesProductionRetrievalEnvToMCP(t *testing.T) {
	content, err := os.ReadFile("../../deploy/docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	compose := string(content)
	mcpIndex := strings.Index(compose, "  memory-mcp:")
	webIndex := strings.Index(compose, "  memory-web:")
	if mcpIndex < 0 || webIndex < 0 || webIndex <= mcpIndex {
		t.Fatal("compose must define memory-mcp before memory-web")
	}
	mcpService := compose[mcpIndex:webIndex]
	for _, required := range []string{
		"POSTGRES_DSN: postgres://memory_os:${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}@postgres:5432/memory_os?sslmode=disable",
		"QDRANT_URL: http://qdrant:6333",
		"LLM_API_KEY: ${LLM_API_KEY:?LLM_API_KEY is required}",
		"EMBEDDING_MODEL: ${EMBEDDING_MODEL:-bge-m3}",
	} {
		if !strings.Contains(mcpService, required) {
			t.Fatalf("memory-mcp compose service missing production retrieval env %q", required)
		}
	}
	if !strings.Contains(mcpService, "LLM_BASE_URL: ${LLM_BASE_URL:?LLM_BASE_URL is required}") &&
		!strings.Contains(mcpService, "LLM_BASE_URL: ${LLM_BASE_URL:-http://memory-llm-mock:11434}") {
		t.Fatal("memory-mcp compose service missing production retrieval env for LLM_BASE_URL")
	}
}

func TestSearchTestPageUsesAuthenticatedRuntimeContext(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/search-test.vue")
	if err != nil {
		t.Fatalf("read search-test page: %v", err)
	}
	page := string(content)

	for _, forbidden := range []string{
		"user_id: 'user_1'",
		"archive_index_generation: 2",
	} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("search test page must not hardcode production search context marker %q", forbidden)
		}
	}
	for _, required := range []string{
		"useAuthStore()",
		"actor: { user_id: ''",
		"context.orgId",
		"context.projectId",
	} {
		if !strings.Contains(page, required) {
			t.Fatalf("search test page must use authenticated runtime context marker %q", required)
		}
	}
}

func TestSearchTestPageDisplaysUnifiedRetrievalEvidence(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/search-test.vue")
	if err != nil {
		t.Fatalf("read search-test page: %v", err)
	}
	page := string(content)

	for _, required := range []string{
		"hotMemoryResults",
		"archiveRAGResults",
		"Hot Memory 结果",
		"Archive RAG 结果",
		"压缩上下文",
		"marked_used_count",
		"access_log_count",
		"item.text || '后端未返回文本。'",
		"Number(item.score || 0).toFixed(4)",
		"resultKey(item, index)",
		"<SourceRefList",
	} {
		if !strings.Contains(page, required) {
			t.Fatalf("search test page must display unified retrieval evidence marker %q", required)
		}
	}
}

func TestDashboardPageUsesRealAPIStats(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/index.vue")
	if err != nil {
		t.Fatalf("read dashboard page: %v", err)
	}
	page := string(content)

	for _, forbidden := range []string{
		"['归档文档', '12'",
		"['热记忆', '38'",
		"['Secret 引用', '6'",
		"['Adapter', '5'",
		"const stats = [",
	} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("dashboard page must not keep static stats marker %q", forbidden)
		}
	}
	for _, required := range []string{
		"/memory/archive/list",
		"/memory/hot-memory/list",
		"/memory/secrets/list",
		"/memory/tokens/adapter/list",
		"/memory/qdrant/status",
		"loadDashboardStats",
		"dashboardStats",
		"真实 API",
	} {
		if !strings.Contains(page, required) {
			t.Fatalf("dashboard page must use real API stats marker %q", required)
		}
	}
}

func TestLoginPageSupportsPasswordLoginAndPATFallback(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/login.vue")
	if err != nil {
		t.Fatalf("read login page: %v", err)
	}
	page := string(content)

	for _, required := range []string{
		"/memory/auth/login-password",
		"密码登录",
		"PAT 登录",
		"memory-bootstrap bootstrap",
		"auth.loginWithPassword",
		"auth.loginWithPAT",
	} {
		if !strings.Contains(page, required) {
			t.Fatalf("login page missing production login marker %q", required)
		}
	}
}

func TestHotMemoryPageUsesRealAPI(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/hot-memory/index.vue")
	if err != nil {
		t.Fatalf("read hot memory page: %v", err)
	}
	page := string(content)

	for _, forbidden := range []string{
		"hm_1",
		"hm_2",
		"项目使用 Docker Compose 在 T480 上部署 API",
		"Embedding 默认使用 bge-m3",
	} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("hot memory page must not keep static demo marker %q", forbidden)
		}
	}
	for _, required := range []string{
		"/memory/hot-memory/list",
		"/memory/hot-memory/create",
		"/memory/hot-memory/edit",
		"/memory/hot-memory/promote",
		"/memory/hot-memory/demote",
		"/memory/hot-memory/mark-used",
		"/memory/hot-memory/delete",
		"保存编辑",
		"Hot Memory 已提升，已切换到已提升筛选。",
		"Hot Memory 已降权，已切换到已降权筛选。",
		"actionStatusFilter",
	} {
		if !strings.Contains(page, required) {
			t.Fatalf("hot memory page must call real API marker %q", required)
		}
	}
}

func TestSecretsPageUsesRealAPIAndMetadataOnlyFlow(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/secrets/index.vue")
	if err != nil {
		t.Fatalf("read secrets page: %v", err)
	}
	page := string(content)

	for _, forbidden := range []string{
		"sk-live-",
		"fake-secret-value",
		"api-key-plaintext-demo",
	} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("secrets page must not keep static or plaintext secret marker %q", forbidden)
		}
	}
	for _, required := range []string{
		"/memory/secrets/list",
		"/memory/secrets/create",
		"/memory/secrets/disable",
		"SecretValueGuard",
		"页面只保留 metadata",
		"已禁用 ${metadata.secret_ref}，并切换到 disabled 筛选。",
	} {
		if !strings.Contains(page, required) {
			t.Fatalf("secrets page must use real API and safe metadata marker %q", required)
		}
	}
}

func TestTokensPageUsesRealAPIAndOneTimeTokenFlow(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/tokens/index.vue")
	if err != nil {
		t.Fatalf("read tokens page: %v", err)
	}
	page := string(content)

	for _, forbidden := range []string{
		"pat_demo_",
		"adapter_demo_",
		"token-plaintext-demo",
		`v-model="patName"`,
		`v-model="patScopes"`,
		`v-model="ttlSeconds"`,
		"Scopes，用逗号分隔",
		"TTL 秒数",
		"memory:read,memory:write",
		"2592000",
	} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("tokens page must not keep static token marker %q", forbidden)
		}
	}
	for _, required := range []string{
		"/memory/tokens/pat/create",
		"/memory/tokens/pat/list",
		"/memory/tokens/pat/revoke",
		"通用 MCP Token",
		"一键创建通用 MCP Token",
		"系统会自动使用推荐权限和默认有效期",
		"配到 Codex、Claude Code、opencode、Hermes",
		"项目由工作目录 / Git 自动识别",
		"明文 token 只在创建响应中显示一次",
		"我已保存，立即隐藏",
		"token_prefix",
		"manualPATTokens",
		"sessionPATTokens",
		"登录会话 PAT",
		"/logs",
	} {
		if !strings.Contains(page, required) {
			t.Fatalf("tokens page must use real API and metadata-only marker %q", required)
		}
	}
}

func TestAppShellHidesGlobalContextSelectors(t *testing.T) {
	content, err := os.ReadFile("../../frontend/components/AppShell.vue")
	if err != nil {
		t.Fatalf("read AppShell: %v", err)
	}
	component := string(content)
	for _, forbidden := range []string{
		"<select",
		"组织 / 项目",
		"context.setAgent",
		"codex</option>",
		"claude</option>",
		"opencode</option>",
		"hermes</option>",
		"['权限', '/permissions']",
		"['角色目录', '/roles']",
	} {
		if strings.Contains(component, forbidden) {
			t.Fatalf("AppShell must hide global workspace selector marker %q", forbidden)
		}
	}
	for _, required := range []string{
		"工作空间自动识别",
		"项目会在 Agent 写入时按目录 / Git 自动归类",
		"loadTenants",
		"['高级设置', '/settings']",
	} {
		if !strings.Contains(component, required) {
			t.Fatalf("AppShell must explain automatic workspace context marker %q", required)
		}
	}
}

func TestSettingsPageCollectsAdvancedGovernanceMenus(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/settings/index.vue")
	if err != nil {
		t.Fatalf("read settings page: %v", err)
	}
	page := string(content)
	for _, required := range []string{
		"高级设置",
		"/permissions",
		"/roles",
		"成员权限",
		"角色目录",
		"日常使用可以忽略",
	} {
		if !strings.Contains(page, required) {
			t.Fatalf("settings page must collect advanced governance marker %q", required)
		}
	}
}

func TestLogsPageUsesRealAuditAndSecurityLogAPIs(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/logs/index.vue")
	if err != nil {
		t.Fatalf("read logs page: %v", err)
	}
	page := string(content)
	for _, forbidden := range []string{
		"login-log-demo",
		"pat_demo_",
		"token-plaintext-demo",
	} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("logs page must not keep static log marker %q", forbidden)
		}
	}
	for _, required := range []string{
		"/memory/security/logs/list",
		"/memory/audit/list",
		"/memory/retrieval/access-log/list",
		"安全日志",
		"项目审计日志",
		"检索访问日志",
		"auth.password_login",
		"token.pat.revoke",
		"metadata",
		"context.orgId",
		"context.projectId",
		"useAuthStore()",
	} {
		if !strings.Contains(page, required) {
			t.Fatalf("logs page must use real API marker %q", required)
		}
	}
}

func TestAppShellLogoutNavigatesToLogin(t *testing.T) {
	content, err := os.ReadFile("../../frontend/components/AppShell.vue")
	if err != nil {
		t.Fatalf("read AppShell: %v", err)
	}
	component := string(content)

	if !strings.Contains(component, "async function handleLogout()") {
		t.Fatalf("AppShell must centralize logout in handleLogout")
	}
	if !strings.Contains(component, "auth.logout()") {
		t.Fatalf("AppShell logout must clear auth token")
	}
	if !strings.Contains(component, "router.push('/login')") {
		t.Fatalf("AppShell logout must navigate to login page")
	}
	if !strings.Contains(component, `@click="handleLogout"`) {
		t.Fatalf("logout button must call handleLogout")
	}
	if !strings.Contains(component, "['日志', '/logs']") {
		t.Fatalf("AppShell must expose logs page navigation")
	}
}

func TestArchivePagesUseRealAPI(t *testing.T) {
	listContent, err := os.ReadFile("../../frontend/pages/archive/index.vue")
	if err != nil {
		t.Fatalf("read archive list page: %v", err)
	}
	detailContent, err := os.ReadFile("../../frontend/pages/archive/[id].vue")
	if err != nil {
		t.Fatalf("read archive detail page: %v", err)
	}
	listPage := string(listContent)
	detailPage := string(detailContent)

	for _, forbidden := range []string{
		"archive_1",
		"archive_2",
		"部署记录",
		"Adapter 对话转写",
		"通过 Docker Compose 在 T480 上部署 API",
		"turn_event_1",
		"chunk_1",
	} {
		if strings.Contains(listPage, forbidden) || strings.Contains(detailPage, forbidden) {
			t.Fatalf("archive pages must not keep static demo marker %q", forbidden)
		}
	}
	for _, required := range []string{
		"/memory/archive/list",
		"/memory/archive/create",
		"manual_archive_request",
		"useAuthStore()",
		"useContextStore()",
		"context.orgId",
		"context.projectId",
	} {
		if !strings.Contains(listPage, required) {
			t.Fatalf("archive list page must use real API/runtime context marker %q", required)
		}
	}
	for _, required := range []string{
		"/memory/archive/detail",
		"/memory/archive/edit",
		"/memory/archive/versions",
		"/memory/archive/delete",
		"/memory/archive/reindex",
		"/memory/archive/index-status",
		"/memory/archive/index-retry",
		"/memory/audit/list",
		"RAG 索引状态",
		"indexStatusRows",
		"retryIndexJobs",
		"indexStatus.index_jobs",
		"indexStatus.archive_chunks",
		"重试失败索引任务",
		"失败原因",
		"Chunk 明细",
		"resource_type: 'archive'",
		"useAuthStore()",
	} {
		if !strings.Contains(detailPage, required) {
			t.Fatalf("archive detail page must use real API marker %q", required)
		}
	}
}

func TestTenantPagesUseRealDeleteAPI(t *testing.T) {
	orgContent, err := os.ReadFile("../../frontend/pages/orgs/index.vue")
	if err != nil {
		t.Fatalf("read orgs page: %v", err)
	}
	projectContent, err := os.ReadFile("../../frontend/pages/projects/index.vue")
	if err != nil {
		t.Fatalf("read projects page: %v", err)
	}
	orgPage := string(orgContent)
	projectPage := string(projectContent)

	for _, required := range []string{
		"/memory/tenant/orgs/list",
		"/memory/tenant/orgs/create",
		"/memory/tenant/orgs/delete",
		"deleteOrg",
		"删除组织",
	} {
		if !strings.Contains(orgPage, required) {
			t.Fatalf("org page must use real tenant operation marker %q", required)
		}
	}
	for _, required := range []string{
		"/memory/tenant/projects/list",
		"/memory/tenant/projects/create",
		"/memory/tenant/projects/delete",
		"deleteProject",
		"删除项目",
	} {
		if !strings.Contains(projectPage, required) {
			t.Fatalf("project page must use real tenant operation marker %q", required)
		}
	}
}

func TestTenantPagesUseRealEditAPI(t *testing.T) {
	orgContent, err := os.ReadFile("../../frontend/pages/orgs/index.vue")
	if err != nil {
		t.Fatalf("read orgs page: %v", err)
	}
	projectContent, err := os.ReadFile("../../frontend/pages/projects/index.vue")
	if err != nil {
		t.Fatalf("read projects page: %v", err)
	}
	orgPage := string(orgContent)
	projectPage := string(projectContent)

	for _, required := range []string{
		"/memory/tenant/orgs/edit",
		"editOrg",
		"保存组织",
	} {
		if !strings.Contains(orgPage, required) {
			t.Fatalf("org page must use real tenant edit marker %q", required)
		}
	}
	for _, required := range []string{
		"/memory/tenant/projects/edit",
		"editProject",
		"保存项目",
	} {
		if !strings.Contains(projectPage, required) {
			t.Fatalf("project page must use real tenant edit marker %q", required)
		}
	}
}

func TestProjectPageKeepsMemberGovernanceInAdvancedSettings(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/projects/index.vue")
	if err != nil {
		t.Fatalf("read projects page: %v", err)
	}
	settingsContent, err := os.ReadFile("../../frontend/pages/settings/index.vue")
	if err != nil {
		t.Fatalf("read settings page: %v", err)
	}
	page := string(content)
	settings := string(settingsContent)

	for _, forbidden := range []string{
		"/memory/tenant/memberships/list",
		"/memory/tenant/memberships/add",
		"/memory/tenant/memberships/update-role",
		"/memory/tenant/memberships/remove",
		"loadMemberships",
		"updateMembershipRole",
		"removeMembership",
		"当前项目成员",
		"添加项目成员",
		"用户 ID",
	} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("project page must not expose member governance marker %q", forbidden)
		}
	}
	if !strings.Contains(settings, "/permissions") || !strings.Contains(settings, "成员权限") {
		t.Fatalf("settings page must keep member governance entry")
	}
}

func TestProjectPageShowsAutoGitWorkspaceMetadata(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/projects/index.vue")
	if err != nil {
		t.Fatalf("read projects page: %v", err)
	}
	page := string(content)

	for _, forbidden := range []string{
		"项目管理",
		"真实项目列表",
		"当前工作区：",
		"手动项目：暂未绑定 Git source_key。",
		"创建真实项目",
	} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("project page must not keep engineering-heavy marker %q", forbidden)
		}
	}
	for _, required := range []string{
		"项目空间",
		"已识别项目",
		"Git 自动识别",
		"source_key",
		"source_type",
		"技术信息",
		"高级操作",
		"手动创建项目",
		"项目会由 MCP 根据 Git 仓库自动创建",
	} {
		if !strings.Contains(page, required) {
			t.Fatalf("project page must show workspace identity marker %q", required)
		}
	}
}

func TestPermissionsPageUsesRealMembershipGovernanceAPI(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/permissions/index.vue")
	if err != nil {
		t.Fatalf("read permissions page: %v", err)
	}
	shellContent, err := os.ReadFile("../../frontend/components/AppShell.vue")
	if err != nil {
		t.Fatalf("read AppShell: %v", err)
	}
	page := string(content)
	shell := string(shellContent)

	for _, required := range []string{
		"/memory/tenant/users/list",
		"/memory/tenant/roles/list",
		"/memory/tenant/memberships/list",
		"/memory/tenant/memberships/add",
		"/memory/tenant/memberships/update-role",
		"/memory/tenant/memberships/remove",
		"roleDefinitions",
		"permission_labels",
		"成员权限",
		"高级权限标签",
		"保存权限等级",
		"移除成员",
	} {
		if !strings.Contains(page, required) {
			t.Fatalf("permissions page must use real membership governance marker %q", required)
		}
	}
	if strings.Contains(page, "function permissionLabelsForRole") {
		t.Fatal("permissions page must not derive role permission labels from a frontend hardcoded function")
	}
	if strings.Contains(shell, "['权限', '/permissions']") {
		t.Fatalf("AppShell must not expose permissions page as top-level navigation")
	}
}

func TestPermissionsPageReloadsAfterMembershipMutation(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/permissions/index.vue")
	if err != nil {
		t.Fatalf("read permissions page: %v", err)
	}
	page := string(content)

	if !strings.Contains(page, "await loadMemberships()") {
		t.Fatalf("permissions page must reload membership list after add/update/remove")
	}
	if !strings.Contains(page, "selectedRole.value = defaultRole.value") {
		t.Fatalf("permissions page should reset role selection after successful add")
	}
}

func TestRolesPageUsesRealRoleManagementAPI(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/roles/index.vue")
	if err != nil {
		t.Fatalf("read roles page: %v", err)
	}
	page := string(content)

	for _, forbidden := range []string{
		"member",
		"admin",
		"owner",
		"const roles = [",
		"mock",
		"role_1",
	} {
		if strings.Contains(page, forbidden) {
			if forbidden == "member" || forbidden == "admin" || forbidden == "owner" || forbidden == "role_1" {
				t.Fatalf("roles page must not keep static role hints marker %q", forbidden)
			}
		}
	}

	for _, required := range []string{
		"useAuthStore()",
		"useContextStore()",
		"/memory/tenant/roles/list",
		"/memory/tenant/roles/upsert",
		"RoleDefinition",
		"permission_labels",
		"保存",
		"请先登录后管理角色",
	} {
		if !strings.Contains(page, required) {
			t.Fatalf("roles page must use real role management marker %q", required)
		}
	}
}

func TestRolesPageUsesLoadedRoleDirectoryAndLocalPersistence(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/roles/index.vue")
	if err != nil {
		t.Fatalf("read roles page: %v", err)
	}
	page := string(content)

	for _, required := range []string{
		"loadingRoles",
		"roles",
		"loadRoles()",
		"upsertRole()",
		"parsedPermissions",
		"auth.isAuthenticated",
	} {
		if !strings.Contains(page, required) {
			t.Fatalf("roles page must keep stateful role directory behavior marker %q", required)
		}
	}

	if strings.Contains(page, "permissionLabelsForRole") {
		t.Fatal("roles page must not derive permission labels from hardcoded frontend helper")
	}
}

func TestRolesPageReloadsAfterSave(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/roles/index.vue")
	if err != nil {
		t.Fatalf("read roles page: %v", err)
	}
	page := string(content)

	if !strings.Contains(page, "await loadRoles()") {
		t.Fatalf("roles page must reload role directory after upsert to prove persistence")
	}
	if strings.Count(page, "await loadRoles()") < 2 {
		t.Fatalf("roles page must ensure loadRoles can be called on mount and after successful upsert")
	}
}

func TestUsersPageUsesRealStatusGovernanceAPI(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/users/index.vue")
	if err != nil {
		t.Fatalf("read users page: %v", err)
	}
	shellContent, err := os.ReadFile("../../frontend/components/AppShell.vue")
	if err != nil {
		t.Fatalf("read AppShell: %v", err)
	}
	page := string(content)
	shell := string(shellContent)

	for _, forbidden := range []string{
		"user_1",
		"alice@example.com",
		"bob@example.com",
		"const users = [",
	} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("users page must not keep static demo marker %q", forbidden)
		}
	}
	for _, required := range []string{
		"/memory/tenant/users/list",
		"/memory/tenant/users/create",
		"/memory/tenant/users/update-status",
		"updateUserStatus",
		"禁用用户",
		"启用用户",
		"真实 PostgreSQL 用户",
	} {
		if !strings.Contains(page, required) {
			t.Fatalf("users page must use real user governance marker %q", required)
		}
	}
	if !strings.Contains(shell, "['用户', '/users']") {
		t.Fatalf("AppShell must expose users page navigation")
	}
}

func TestUsersPageReloadsAfterMutation(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/users/index.vue")
	if err != nil {
		t.Fatalf("read users page: %v", err)
	}
	page := string(content)

	if strings.Count(page, "await loadUsers()") < 2 {
		t.Fatalf("users page must reload after create and status update")
	}
	if !strings.Contains(page, "statusFilter.value = 'active'") {
		t.Fatalf("users page should reset status filter to active after create")
	}
}

func TestComposePersistsArchiveDirectory(t *testing.T) {
	content, err := os.ReadFile("../../deploy/docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	compose := string(content)

	if !strings.Contains(compose, "archive_data:/data/memory-os") {
		t.Fatalf("compose must mount archive_data at /data/memory-os for archive persistence")
	}
	if !strings.Contains(compose, "archive_data:") {
		t.Fatalf("compose must declare archive_data volume")
	}
}

func TestRestoreRehearsalComposeIsIsolated(t *testing.T) {
	content, err := os.ReadFile("../../deploy/docker-compose.restore-rehearsal.yml")
	if err != nil {
		t.Fatalf("read restore rehearsal compose: %v", err)
	}
	compose := string(content)
	for _, forbidden := range []string{
		"ports:",
		"postgres_data:",
		"redis_data:",
		"qdrant_data:",
		"archive_data:",
	} {
		if strings.Contains(compose, forbidden) {
			t.Fatalf("restore rehearsal compose must not expose production ports or reuse production volumes, found %q", forbidden)
		}
	}
	for _, required := range []string{
		"name: memory-os-restore-rehearsal",
		"restore_rehearsal_pg:",
		"restore_rehearsal_qdrant:",
		"restore_rehearsal_archive:",
		"POSTGRES_DSN: postgres://memory_os:${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}@postgres:5432/memory_os?sslmode=disable",
		"QDRANT_URL: http://qdrant:6333",
		"ARCHIVE_DIR: /data/memory-os",
	} {
		if !strings.Contains(compose, required) {
			t.Fatalf("restore rehearsal compose missing isolation marker %q", required)
		}
	}
}

func TestBackendDockerfilesInjectBuildInfo(t *testing.T) {
	for _, path := range []string{"../../deploy/backend/Dockerfile.api", "../../deploy/backend/Dockerfile.worker", "../../deploy/backend/Dockerfile.mcp"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		dockerfile := string(content)
		for _, marker := range []string{
			"ARG BUILD_VERSION",
			"ARG BUILD_COMMIT",
			"ARG BUILD_TIME",
			"ARG BUILD_DIRTY",
			"-X memory-os/internal/buildinfo.Version=${BUILD_VERSION}",
			"-X memory-os/internal/buildinfo.Commit=${BUILD_COMMIT}",
			"-X memory-os/internal/buildinfo.BuildTime=${BUILD_TIME}",
			"-X memory-os/internal/buildinfo.Dirty=${BUILD_DIRTY}",
		} {
			if !strings.Contains(dockerfile, marker) {
				t.Fatalf("%s missing build metadata marker %q", path, marker)
			}
		}
	}
}

func TestBackendDockerfilesConfigureGoModuleProxy(t *testing.T) {
	for _, path := range []string{"../../deploy/backend/Dockerfile.api", "../../deploy/backend/Dockerfile.worker", "../../deploy/backend/Dockerfile.mcp"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		dockerfile := string(content)
		for _, marker := range []string{
			"ARG GOPROXY=https://goproxy.cn,direct",
			"ARG NO_PROXY=" + backendNoProxyDefault,
			"ENV GOPROXY=${GOPROXY}",
			"ENV NO_PROXY=${NO_PROXY}",
			"ENV no_proxy=${NO_PROXY}",
		} {
			if !strings.Contains(dockerfile, marker) {
				t.Fatalf("%s missing go module proxy marker %q", path, marker)
			}
		}
	}
}

func TestComposePassesBackendBuildInfoArgs(t *testing.T) {
	content, err := os.ReadFile("../../deploy/docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	compose := string(content)
	for _, marker := range []string{
		"BUILD_VERSION: ${BUILD_VERSION:-0.4.0-dev}",
		"BUILD_COMMIT: ${BUILD_COMMIT:-unknown}",
		"BUILD_TIME: ${BUILD_TIME:-unknown}",
		"BUILD_DIRTY: ${BUILD_DIRTY:-unknown}",
	} {
		if strings.Count(compose, marker) < 3 {
			t.Fatalf("compose must pass backend build arg %q to api/worker/mcp", marker)
		}
	}
}

func TestComposePassesBackendBuildProxyArgs(t *testing.T) {
	content, err := os.ReadFile("../../deploy/docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	compose := string(content)
	for _, marker := range []string{
		"GOPROXY: ${GOPROXY:-https://goproxy.cn,direct}",
		"NO_PROXY: ${NO_PROXY:-" + backendNoProxyDefault + "}",
	} {
		if strings.Count(compose, marker) < 3 {
			t.Fatalf("compose must pass backend build proxy arg %q to api/worker/mcp", marker)
		}
	}
}

func TestComposeDoesNotInjectExampleEnvFileInProduction(t *testing.T) {
	content, err := os.ReadFile("../../deploy/docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	compose := string(content)
	if strings.Contains(compose, "env_file:") || strings.Contains(compose, "../.env.example") {
		t.Fatalf("production compose must not inject .env.example into running containers")
	}
}

func TestComposeRequiresLLMConfigForAPIAndWorker(t *testing.T) {
	content, err := os.ReadFile("../../deploy/docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	compose := string(content)
	for _, marker := range []string{
		"LLM_API_KEY: ${LLM_API_KEY:?LLM_API_KEY is required}",
		"EMBEDDING_MODEL: ${EMBEDDING_MODEL:-bge-m3}",
	} {
		if strings.Count(compose, marker) < 2 {
			t.Fatalf("compose must pass LLM marker %q to api and worker", marker)
		}
	}
	if strings.Count(compose, "LLM_BASE_URL: ${LLM_BASE_URL:?LLM_BASE_URL is required}")+
		strings.Count(compose, "LLM_BASE_URL: ${LLM_BASE_URL:-http://memory-llm-mock:11434}") < 2 {
		t.Fatal("compose must pass LLM_BASE_URL marker to api and worker")
	}
}

func TestComposeRequiresSecretVaultConfigForAPI(t *testing.T) {
	content, err := os.ReadFile("../../deploy/docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	compose := string(content)
	for _, marker := range []string{
		"SECRET_VAULT_KEY_ID: ${SECRET_VAULT_KEY_ID:?SECRET_VAULT_KEY_ID is required}",
		"SECRET_VAULT_KEY_B64: ${SECRET_VAULT_KEY_B64:?SECRET_VAULT_KEY_B64 is required}",
	} {
		if !strings.Contains(compose, marker) {
			t.Fatalf("compose must require explicit secret vault marker %q for api", marker)
		}
	}
}

func TestComposeRuntimeNoProxyCanBeExtended(t *testing.T) {
	content, err := os.ReadFile("../../deploy/docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	compose := string(content)
	if strings.Count(compose, "no_proxy: ${NO_PROXY:-"+backendNoProxyDefault+"}") < 3 {
		t.Fatalf("compose runtime no_proxy must be controlled by NO_PROXY for api/worker/mcp")
	}
}

func TestComposeRequiresExplicitPostgresPassword(t *testing.T) {
	content, err := os.ReadFile("../../deploy/docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	compose := string(content)
	if strings.Contains(compose, "replace-me-local-only") {
		t.Fatal("production compose must not default POSTGRES_PASSWORD to replace-me-local-only")
	}
	for _, marker := range []string{
		"POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}",
		"POSTGRES_DSN: postgres://memory_os:${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}@postgres:5432/memory_os?sslmode=disable",
	} {
		if !strings.Contains(compose, marker) {
			t.Fatalf("compose must require explicit postgres password marker %q", marker)
		}
	}
}

func TestMakefileDevUpProvidesLocalOnlyPostgresPassword(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(findRepoRoot(t), "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(content)
	if !strings.Contains(makefile, "dev-up:\n\tPOSTGRES_PASSWORD=$${POSTGRES_PASSWORD:-replace-me-local-only}") {
		t.Fatalf("Makefile dev-up target must provide a local-only POSTGRES_PASSWORD default")
	}
	if strings.Contains(makefile, "prod-up:\n\tPOSTGRES_PASSWORD=$${POSTGRES_PASSWORD:-replace-me-local-only}") {
		t.Fatalf("Makefile prod-up target must not provide a local-only POSTGRES_PASSWORD default")
	}
}

func TestMakefileProductionDeployTargetSetsBuildInfo(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(findRepoRoot(t), "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(content)
	for _, marker := range []string{
		"prod-up:",
		". scripts/load-build-info.sh && \\",
		"up -d --build memory-api memory-worker memory-mcp memory-web",
	} {
		if !strings.Contains(makefile, marker) {
			t.Fatalf("Makefile prod-up target missing %q", marker)
		}
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root")
		}
		dir = parent
	}
}
