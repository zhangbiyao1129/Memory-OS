<script setup lang="ts">
const auth = useAuthStore()
const context = useContextStore()
const { hasWorkspaceContext, requestWorkspaceProjects } = useWorkspaceProjectScopes()

type TriageProjectLink = {
  linked_project_id: string
  linked_source_key: string
  confidence: number
  evidence: string
  status: string
  promoted_hot_memory_id: string
}

type TriageResult = {
  candidate_id: string
  org_id: string
  source_project_id: string
  source_key: string
  triage_scope: string
  confidence: number
  review_state: string
  reason: string
  project_links: TriageProjectLink[]
  promoted_hot_memory_ids: string[]
  last_error: string
  updated_at: string
}

type TriageListResponse = { triage_results: TriageResult[] }

const loading = ref(false)
const error = ref('')
const results = ref<TriageResult[]>([])
const sourceKeyFilter = ref('')

const stats = computed(() => {
  const all = results.value
  return {
    total: all.length,
    autoApplied: all.filter((item) => item.review_state === 'auto_applied').length,
    weak: all.filter((item) => item.review_state === 'weak').length,
    needsReview: all.filter((item) => item.review_state === 'needs_review').length,
    promoted: all.reduce((sum, item) => sum + (item.promoted_hot_memory_ids?.length || 0) + (item.project_links || []).filter((link) => link.promoted_hot_memory_id).length, 0)
  }
})

function formatDate(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

function scopeText(scope: string) {
  const labels: Record<string, string> = {
    project: '项目',
    global: '全局工具记忆',
    tooling: '全局工具记忆',
    personal_pref: '个人偏好',
    inbox: '待确认',
    discard: '已忽略'
  }
  return labels[scope] || scope
}

function reviewText(state: string) {
  const labels: Record<string, string> = {
    auto_applied: '自动应用',
    weak: '弱召回',
    needs_review: '待确认',
    rejected: '已拒绝'
  }
  return labels[state] || state
}

async function loadTriage() {
  if (!auth.isAuthenticated || !hasWorkspaceContext.value) return
  loading.value = true
  error.value = ''
  try {
    const items = await requestWorkspaceProjects<TriageListResponse, TriageResult>({
      path: '/memory/triage/list',
      buildBody: (project) => ({
        org_id: project.org_id,
        project_id: project.project_id,
        source_key: sourceKeyFilter.value || undefined,
        limit: 100
      }),
      pick: (response) => response.triage_results || []
    })
    results.value = items.sort((a, b) => Date.parse(b.updated_at || '') - Date.parse(a.updated_at || ''))
  } catch (err: any) {
    error.value = err.message || '自动整理结果加载失败'
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  await loadTriage()
})

watch(() => [context.orgId, context.projectId], () => {
  void loadTriage()
})
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <h2 class="text-3xl font-black">自动整理</h2>
        <p class="mt-2 text-stone-600">这里是系统自动整理结果；无需手动归档。</p>
      </div>
      <button class="rounded-xl border bg-white px-4 py-2 font-bold" :disabled="loading" @click="loadTriage">{{ loading ? '刷新中...' : '刷新' }}</button>
    </div>

    <section class="mt-6 grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
      <div class="rounded-xl border bg-white p-4">
        <p class="text-xs font-bold text-stone-500">总整理</p>
        <p class="mt-2 text-3xl font-black">{{ stats.total }}</p>
      </div>
      <div class="rounded-xl border bg-white p-4">
        <p class="text-xs font-bold text-stone-500">自动应用</p>
        <p class="mt-2 text-3xl font-black">{{ stats.autoApplied }}</p>
      </div>
      <div class="rounded-xl border bg-white p-4">
        <p class="text-xs font-bold text-stone-500">弱召回</p>
        <p class="mt-2 text-3xl font-black">{{ stats.weak }}</p>
      </div>
      <div class="rounded-xl border bg-white p-4">
        <p class="text-xs font-bold text-stone-500">待确认</p>
        <p class="mt-2 text-3xl font-black">{{ stats.needsReview }}</p>
      </div>
      <div class="rounded-xl border bg-white p-4">
        <p class="text-xs font-bold text-stone-500">已投递 Hot Memory</p>
        <p class="mt-2 text-3xl font-black">{{ stats.promoted }}</p>
      </div>
    </section>

    <section class="mt-6 rounded-xl border bg-white p-5">
      <div class="grid gap-3 sm:grid-cols-[1fr_auto]">
        <label class="text-sm font-bold text-stone-600">
          source_key
          <input v-model="sourceKeyFilter" class="mt-2 w-full rounded-xl border p-3 text-sm" placeholder="筛选 source_key" @keyup.enter="loadTriage">
        </label>
        <button class="self-end rounded-xl bg-stone-950 px-4 py-3 font-bold text-white disabled:cursor-not-allowed disabled:bg-stone-400" :disabled="loading || !hasWorkspaceContext" @click="loadTriage">查询</button>
      </div>
    </section>

    <p v-if="error" class="mt-4 rounded-xl bg-red-50 p-3 text-sm font-semibold text-red-700">{{ error }}</p>

    <section class="mt-6 rounded-xl border bg-white p-5">
      <h3 class="text-xl font-black">自动整理结果（{{ results.length }}）</h3>
      <div v-if="loading" class="mt-4 rounded-xl bg-stone-50 p-4 text-stone-600">正在加载自动整理结果...</div>
      <div v-else-if="results.length === 0" class="mt-4 rounded-xl bg-stone-50 p-4 text-stone-600">当前工作区暂无自动整理结果。</div>
      <div v-else class="mt-4 grid gap-4">
        <article v-for="item in results" :key="item.candidate_id" class="rounded-xl border bg-stone-50 p-5">
          <div class="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <span class="rounded-full bg-stone-950 px-2 py-0.5 text-xs font-bold text-white">{{ scopeText(item.triage_scope) }}</span>
                <span class="rounded-full bg-white px-2 py-0.5 text-xs font-bold text-stone-700">{{ reviewText(item.review_state) }}</span>
                <span class="text-xs text-stone-500">置信度 {{ item.confidence.toFixed(2) }}</span>
              </div>
              <p class="mt-3 break-all text-sm font-bold text-stone-950">{{ item.source_key }}</p>
              <p class="mt-2 text-sm text-stone-700">{{ item.reason || '-' }}</p>
              <p class="mt-2 text-xs text-stone-500">候选 {{ item.candidate_id }} · 更新于 {{ formatDate(item.updated_at) }}</p>
              <p v-if="item.last_error" class="mt-2 text-xs font-semibold text-red-700">{{ item.last_error }}</p>
            </div>
            <div class="grid min-w-64 gap-2 text-xs text-stone-600">
              <div v-if="item.promoted_hot_memory_ids?.length" class="rounded-lg bg-white p-3">
                <p class="font-bold text-stone-900">全局工具记忆</p>
                <p v-for="id in item.promoted_hot_memory_ids" :key="id" class="mt-1 break-all">{{ id }}</p>
              </div>
              <div v-if="item.project_links?.length" class="rounded-lg bg-white p-3">
                <p class="font-bold text-stone-900">跨项目关联</p>
                <div v-for="link in item.project_links" :key="link.linked_project_id" class="mt-2 border-t pt-2 first:border-t-0 first:pt-0">
                  <p class="break-all font-semibold">{{ link.linked_project_id }}</p>
                  <p class="break-all">{{ link.linked_source_key || '-' }}</p>
                  <p>置信度 {{ link.confidence.toFixed(2) }}</p>
                  <p v-if="link.promoted_hot_memory_id" class="break-all">Hot Memory {{ link.promoted_hot_memory_id }}</p>
                </div>
              </div>
            </div>
          </div>
        </article>
      </div>
    </section>
  </AppShell>
</template>
