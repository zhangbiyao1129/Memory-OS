<script setup lang="ts">
const auth = useAuthStore()
const context = useContextStore()
const { request, baseURL } = useApi()
const health = ref('checking')
const loadingStats = ref(false)
const statsError = ref('')

type ArchiveListResponse = { archives?: unknown[] }
type HotMemoryListResponse = { memories?: unknown[] }
type SecretListResponse = { secrets?: unknown[] }
type TokenListResponse = { tokens?: unknown[] }
type QdrantStatusResponse = { points_count?: number; index_jobs_by_status?: Record<string, number> }

const dashboardStats = ref([
  { label: '归档文档', value: '-', description: '等待真实 Archive API 返回', endpoint: '/memory/archive/list' },
  { label: '热记忆', value: '-', description: '等待真实 Hot Memory API 返回', endpoint: '/memory/hot-memory/list' },
  { label: 'Secret 引用', value: '-', description: '等待真实 Secret Vault metadata 返回', endpoint: '/memory/secrets/list' },
  { label: 'Adapter Token', value: '-', description: '等待真实 Adapter Token metadata 返回', endpoint: '/memory/tokens/adapter/list' },
  { label: 'Qdrant Points', value: '-', description: '等待真实 Qdrant 状态返回', endpoint: '/memory/qdrant/status' }
])

const hasProjectContext = computed(() => Boolean(context.orgId && context.projectId))

function setDashboardStats(values: Partial<Record<string, number | string>>) {
  dashboardStats.value = [
    { label: '归档文档', value: String(values.archives ?? '-'), description: 'Markdown 正文权威源，来自真实 Archive API', endpoint: '/memory/archive/list' },
    { label: '热记忆', value: String(values.hotMemories ?? '-'), description: '按项目和用户隔离的高价值事实，来自真实 Hot Memory API', endpoint: '/memory/hot-memory/list' },
    { label: 'Secret 引用', value: String(values.secrets ?? '-'), description: '仅统计 metadata，明文不返回管理台', endpoint: '/memory/secrets/list' },
    { label: 'Adapter Token', value: String(values.adapterTokens ?? '-'), description: '绑定当前组织 / 项目 / Agent 的真实 token metadata', endpoint: '/memory/tokens/adapter/list' },
    { label: 'Qdrant Points', value: String(values.qdrantPoints ?? '-'), description: `真实向量点统计${values.indexJobs ? `，索引任务 ${values.indexJobs}` : ''}`, endpoint: '/memory/qdrant/status' }
  ]
}

async function loadHealth() {
  try {
    const data = await request<{ status: string }>('/healthz')
    health.value = data.status || 'unknown'
  } catch {
    health.value = 'offline'
  }
}

async function loadDashboardStats() {
  if (!auth.isAuthenticated || !hasProjectContext.value) {
    setDashboardStats({})
    return
  }
  loadingStats.value = true
  statsError.value = ''
  try {
    const [archives, hotMemories, secrets, adapterTokens, qdrantStatus] = await Promise.all([
      request<ArchiveListResponse>('/memory/archive/list', {
        method: 'POST',
        body: { org_id: context.orgId, project_id: context.projectId, status: 'active', limit: 100 }
      }),
      request<HotMemoryListResponse>('/memory/hot-memory/list', {
        method: 'POST',
        body: { org_id: context.orgId, project_id: context.projectId, agent_id: context.agentId, scope: 'project', visibility: 'project', status: 'active', limit: 100 }
      }),
      request<SecretListResponse>('/memory/secrets/list', {
        method: 'POST',
        body: { org_id: context.orgId, project_id: context.projectId, status: 'active', limit: 100 }
      }),
      request<TokenListResponse>('/memory/tokens/adapter/list', {
        method: 'POST',
        body: { org_id: context.orgId, project_id: context.projectId, agent_id: context.agentId, status: 'active', limit: 100 }
      }),
      request<QdrantStatusResponse>('/memory/qdrant/status', { method: 'POST' })
    ])
    const indexJobCount = Object.values(qdrantStatus.index_jobs_by_status || {}).reduce((sum, count) => sum + Number(count || 0), 0)
    setDashboardStats({
      archives: archives.archives?.length || 0,
      hotMemories: hotMemories.memories?.length || 0,
      secrets: secrets.secrets?.length || 0,
      adapterTokens: adapterTokens.tokens?.length || 0,
      qdrantPoints: qdrantStatus.points_count || 0,
      indexJobs: indexJobCount
    })
  } catch (err: any) {
    statsError.value = err.message || '总览统计加载失败'
    setDashboardStats({})
  } finally {
    loadingStats.value = false
  }
}

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  await loadHealth()
  await loadDashboardStats()
})

watch(() => [auth.token, context.orgId, context.projectId, context.agentId], () => {
  void loadDashboardStats()
})
</script>

<template>
  <AppShell>
    <section class="grid gap-6">
      <div class="flex flex-col gap-4 rounded-[2rem] bg-gradient-to-br from-orange-100 to-lime-100 p-6 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <p class="text-xs font-bold uppercase tracking-[0.35em] text-orange-900">原生多 Agent 记忆平台</p>
          <h2 class="mt-3 text-4xl font-black">Memory OS 总览</h2>
          <p class="mt-2 max-w-2xl text-stone-700">统一管理组织、项目、Archive RAG、Hot Memory、Secret Vault、Adapter Token 和检索测试。</p>
        </div>
        <HealthBadge :status="health" />
      </div>
      <div class="rounded-3xl border border-stone-200 bg-white p-4 text-sm text-stone-600">API 地址：<code>{{ baseURL }}</code></div>
      <p v-if="!auth.isAuthenticated" class="rounded-2xl bg-amber-50 p-4 text-sm text-amber-800">请先登录后查看真实总览统计。</p>
      <p v-else-if="!hasProjectContext" class="rounded-2xl bg-amber-50 p-4 text-sm text-amber-800">请先选择组织和项目，总览页不会展示静态假数据。</p>
      <p v-if="statsError" class="rounded-2xl bg-red-50 p-4 text-sm text-red-700">{{ statsError }}</p>
      <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <article v-for="item in dashboardStats" :key="item.label" class="rounded-3xl border border-stone-200 bg-white p-5">
          <p class="text-sm font-bold text-stone-500">{{ item.label }}</p>
          <p class="mt-3 text-4xl font-black">{{ loadingStats ? '...' : item.value }}</p>
          <p class="mt-2 text-sm text-stone-600">{{ item.description }}</p>
          <p class="mt-3 break-all text-xs font-bold text-stone-400">真实 API：{{ item.endpoint }}</p>
        </article>
      </div>
    </section>
  </AppShell>
</template>
