<script setup lang="ts">
const auth = useAuthStore()
const context = useContextStore()
const { request } = useApi()

type AuditLog = {
  id: string
  actor_user_id: string
  org_id?: string
  project_id?: string
  action: string
  resource_type: string
  resource_id: string
  request_id: string
  result: string
  metadata?: Record<string, string>
  created_at: string
}

type RetrievalAccessLog = {
  request_id: string
  actor_user_id: string
  org_id: string
  project_id: string
  agent_id: string
  query_hash: string
  rerank_degraded: boolean
  created_at: string
  results?: Array<Record<string, unknown>>
}

type AuditListResponse = { audit_logs: AuditLog[] }
type RetrievalAccessLogResponse = { requests: RetrievalAccessLog[]; results: Array<Record<string, unknown> & { request_id?: string }> }

const loading = ref(false)
const error = ref('')
const securityLogs = ref<AuditLog[]>([])
const projectAuditLogs = ref<AuditLog[]>([])
const retrievalLogs = ref<RetrievalAccessLog[]>([])
const resourceType = ref('')

const hasProjectContext = computed(() => Boolean(context.orgId && context.projectId))

function formatDate(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

function metadataPairs(metadata?: Record<string, string>) {
  return Object.entries(metadata || {})
}

async function loadLogs() {
  if (!auth.isAuthenticated) return
  loading.value = true
  error.value = ''
  try {
    const securityResponse = await request<AuditListResponse>('/memory/security/logs/list', {
      method: 'POST',
      body: { resource_type: resourceType.value, limit: 50 }
    })
    securityLogs.value = securityResponse.audit_logs || []

    if (hasProjectContext.value) {
      const [auditResponse, accessResponse] = await Promise.all([
        request<AuditListResponse>('/memory/audit/list', {
          method: 'POST',
          body: { org_id: context.orgId, project_id: context.projectId, limit: 50 }
        }),
        request<RetrievalAccessLogResponse>('/memory/retrieval/access-log/list', {
          method: 'POST',
          body: { org_id: context.orgId, project_id: context.projectId, limit: 50 }
        })
      ])
      projectAuditLogs.value = auditResponse.audit_logs || []
      const resultsByRequest = new Map<string, Array<Record<string, unknown>>>()
      for (const result of accessResponse.results || []) {
        const requestID = String(result.request_id || '')
        if (!requestID) continue
        resultsByRequest.set(requestID, [...(resultsByRequest.get(requestID) || []), result])
      }
      retrievalLogs.value = (accessResponse.requests || []).map((item) => ({
        ...item,
        results: resultsByRequest.get(item.request_id) || []
      }))
    } else {
      projectAuditLogs.value = []
      retrievalLogs.value = []
    }
  } catch (err: any) {
    error.value = err.message || '日志加载失败'
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  await loadLogs()
})

watch(() => [context.orgId, context.projectId, resourceType.value], () => loadLogs())
</script>

<template>
  <AppShell>
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div>
        <h2 class="text-3xl font-black">日志中心</h2>
        <p class="mt-2 text-stone-600">集中查看安全日志、项目审计日志和检索访问日志。安全日志包含 <code>auth.password_login</code>、<code>token.pat.revoke</code> 等账号级事件。</p>
      </div>
      <button class="rounded-2xl border px-4 py-2 font-bold" :disabled="loading" @click="loadLogs">{{ loading ? '刷新中...' : '刷新日志' }}</button>
    </div>

    <p v-if="error" class="mt-4 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>

    <section class="mt-6 rounded-3xl border bg-white p-5">
      <div class="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h3 class="text-xl font-black">安全日志</h3>
          <p class="mt-2 text-sm text-stone-600">调用 <code>/memory/security/logs/list</code>，只返回当前登录用户自己的登录和 token 审计事件。</p>
        </div>
        <select v-model="resourceType" class="rounded-2xl border px-4 py-2 text-sm">
          <option value="">全部安全事件</option>
          <option value="user">登录事件</option>
          <option value="pat">PAT 事件</option>
          <option value="adapter_token">Adapter Token 事件</option>
        </select>
      </div>
      <p v-if="loading" class="mt-4 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">正在加载安全日志...</p>
      <p v-else-if="securityLogs.length === 0" class="mt-4 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">当前账号暂无安全日志。</p>
      <div v-else class="mt-4 grid gap-3">
        <article v-for="log in securityLogs" :key="log.id || log.request_id" class="rounded-2xl bg-stone-50 p-4 text-sm">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div>
              <p class="font-black">{{ log.action }} · {{ log.result }}</p>
              <p class="mt-1 break-all text-stone-600">{{ log.resource_type }} / {{ log.resource_id }}</p>
            </div>
            <p class="text-xs text-stone-500">{{ formatDate(log.created_at) }}</p>
          </div>
          <p class="mt-2 break-all text-stone-600">request: {{ log.request_id }}</p>
          <div v-if="metadataPairs(log.metadata).length" class="mt-3 flex flex-wrap gap-2">
            <span v-for="[key, value] in metadataPairs(log.metadata)" :key="key" class="rounded-full bg-white px-3 py-1 text-xs text-stone-600">{{ key }}: {{ value }}</span>
          </div>
        </article>
      </div>
    </section>

    <section class="mt-6 rounded-3xl border bg-white p-5">
      <h3 class="text-xl font-black">项目审计日志</h3>
      <p class="mt-2 text-sm text-stone-600">调用 <code>/memory/audit/list</code>，按当前组织 / 项目读取 Archive、Secret、Token 等项目级审计事件。</p>
      <p v-if="!hasProjectContext" class="mt-4 rounded-2xl bg-amber-50 p-4 text-sm text-amber-800">请先选择组织和项目。</p>
      <p v-else-if="loading" class="mt-4 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">正在加载项目审计日志...</p>
      <p v-else-if="projectAuditLogs.length === 0" class="mt-4 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">当前项目暂无审计日志。</p>
      <div v-else class="mt-4 grid gap-3">
        <article v-for="log in projectAuditLogs" :key="log.id || log.request_id" class="rounded-2xl bg-stone-50 p-4 text-sm">
          <p class="font-black">{{ log.action }} · {{ log.result }}</p>
          <p class="mt-1 break-all text-stone-600">{{ log.resource_type }} / {{ log.resource_id }}</p>
          <p class="mt-1 text-stone-500">{{ formatDate(log.created_at) }}</p>
        </article>
      </div>
    </section>

    <section class="mt-6 rounded-3xl border bg-white p-5">
      <h3 class="text-xl font-black">检索访问日志</h3>
      <p class="mt-2 text-sm text-stone-600">调用 <code>/memory/retrieval/access-log/list</code>，只展示 query hash 和来源摘要，不展示原始检索文本。</p>
      <p v-if="!hasProjectContext" class="mt-4 rounded-2xl bg-amber-50 p-4 text-sm text-amber-800">请先选择组织和项目。</p>
      <p v-else-if="loading" class="mt-4 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">正在加载检索访问日志...</p>
      <p v-else-if="retrievalLogs.length === 0" class="mt-4 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">当前项目暂无检索访问日志。</p>
      <div v-else class="mt-4 grid gap-3">
        <article v-for="log in retrievalLogs" :key="log.request_id" class="rounded-2xl bg-stone-50 p-4 text-sm">
          <p class="font-black">{{ log.agent_id }} · {{ log.request_id }}</p>
          <p class="mt-1 break-all text-stone-600">query_hash: {{ log.query_hash }}</p>
          <p class="mt-1 text-stone-500">{{ formatDate(log.created_at) }} · results: {{ log.results?.length || 0 }}</p>
        </article>
      </div>
    </section>
  </AppShell>
</template>
