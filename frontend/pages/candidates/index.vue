<script setup lang="ts">
const auth = useAuthStore()
const context = useContextStore()
const { request } = useApi()

type Candidate = {
  candidate_id: string
  org_id: string
  project_id: string
  source_key: string
  user_id: string
  agent_id: string
  thread_id: string
  session_id: string
  source_event_ids: string[]
  memory_type: string
  content: string
  summary: string
  risk_level: 'low' | 'medium' | 'high'
  confidence: number
  status: string
  scores: { hot_memory_score: number; compose_score: number }
  created_at: string
  updated_at: string
}

type CandidateListResponse = { candidates: Candidate[] }

const loading = ref(false)
const error = ref('')
const success = ref('')
const candidates = ref<Candidate[]>([])
const statusFilter = ref('')
const riskFilter = ref('')
const sourceKeyFilter = ref('')
const threadIdFilter = ref('')
const composing = ref(false)
const composeForce = ref(false)
const composeSourceKey = ref('')
const composeThreadId = ref('')

const hasProjectContext = computed(() => Boolean(context.orgId && context.projectId))

function formatDate(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

function statusText(status: string) {
  const labels: Record<string, string> = {
    pending: '待处理',
    accepted: '已接受',
    discarded: '已丢弃',
    promoted_to_hot: '已提升热记忆',
    in_compose_pool: '沉淀池',
    composed: '已沉淀'
  }
  return labels[status] || status
}

function riskColor(level: string) {
  const colors: Record<string, string> = {
    low: 'bg-emerald-100 text-emerald-900',
    medium: 'bg-amber-100 text-amber-900',
    high: 'bg-red-100 text-red-900'
  }
  return colors[level] || 'bg-stone-100 text-stone-900'
}

function memoryTypeText(t: string) {
  const labels: Record<string, string> = {
    fact: '事实',
    decision: '决策',
    bugfix: '修复',
    preference: '偏好',
    risk: '风险',
    follow_up: '待办'
  }
  return labels[t] || t
}

function requestBody(extra: Record<string, unknown> = {}) {
  return {
    org_id: context.orgId,
    project_id: context.projectId,
    ...extra
  }
}

async function loadCandidates() {
  if (!auth.isAuthenticated || !hasProjectContext.value) return
  loading.value = true
  error.value = ''
  try {
    const body: Record<string, unknown> = { limit: 50 }
    if (statusFilter.value) body.status = statusFilter.value
    if (riskFilter.value) body.risk_level = riskFilter.value
    if (sourceKeyFilter.value) body.source_key = sourceKeyFilter.value
    if (threadIdFilter.value) body.thread_id = threadIdFilter.value
    const response = await request<CandidateListResponse>('/memory/candidates/list', {
      method: 'POST',
      body: requestBody(body)
    })
    candidates.value = response.candidates || []
  } catch (err: any) {
    error.value = err.message || '候选记忆加载失败'
  } finally {
    loading.value = false
  }
}

async function acceptCandidate(candidateID: string) {
  error.value = ''
  success.value = ''
  try {
    await request('/memory/candidates/accept', {
      method: 'POST',
      body: requestBody({ candidate_id: candidateID })
    })
    success.value = '候选已接受。'
    await loadCandidates()
  } catch (err: any) {
    error.value = err.message || '接受失败'
  }
}

async function discardCandidate(candidateID: string) {
  error.value = ''
  success.value = ''
  try {
    await request('/memory/candidates/discard', {
      method: 'POST',
      body: requestBody({ candidate_id: candidateID })
    })
    success.value = '候选已丢弃。'
    await loadCandidates()
  } catch (err: any) {
    error.value = err.message || '丢弃失败'
  }
}

async function composeTopic() {
  if (!composeSourceKey.value.trim()) {
    error.value = '请填写 source_key'
    return
  }
  composing.value = true
  error.value = ''
  success.value = ''
  try {
    const result = await request<{ ready: boolean; archive_id: string; composed: number }>('/memory/candidates/compose', {
      method: 'POST',
      body: requestBody({
        source_key: composeSourceKey.value.trim(),
        thread_id: composeThreadId.value.trim(),
        force: composeForce.value
      })
    })
    if (result.ready) {
      success.value = `沉淀完成，归档 ${result.archive_id}，共 ${result.composed} 条候选。`
    } else {
      success.value = '当前主题尚未满足沉淀条件（候选≥10 / 完成信号 / 24h 空闲 / 强制）。'
    }
    await loadCandidates()
  } catch (err: any) {
    error.value = err.message || '沉淀失败'
  } finally {
    composing.value = false
  }
}

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  await loadCandidates()
})

watch(() => [context.orgId, context.projectId, statusFilter.value, riskFilter.value], () => {
  void loadCandidates()
})
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <h2 class="text-3xl font-black">候选记忆</h2>
        <p class="mt-2 text-stone-600">候选提炼结果，支持接受/丢弃/主题沉淀。</p>
      </div>
      <button class="rounded-2xl border bg-white px-4 py-2 font-bold" :disabled="loading" @click="loadCandidates">{{ loading ? '刷新中...' : '刷新' }}</button>
    </div>

    <!-- 筛选 -->
    <section class="mt-6 rounded-3xl border bg-white p-5">
      <div class="grid gap-3 sm:grid-cols-4">
        <label class="text-sm font-bold text-stone-600">
          状态
          <select v-model="statusFilter" class="mt-2 w-full rounded-2xl border p-3 text-sm">
            <option value="">全部</option>
            <option value="pending">待处理</option>
            <option value="accepted">已接受</option>
            <option value="discarded">已丢弃</option>
            <option value="in_compose_pool">沉淀池</option>
            <option value="composed">已沉淀</option>
          </select>
        </label>
        <label class="text-sm font-bold text-stone-600">
          风险
          <select v-model="riskFilter" class="mt-2 w-full rounded-2xl border p-3 text-sm">
            <option value="">全部</option>
            <option value="low">低</option>
            <option value="medium">中</option>
            <option value="high">高</option>
          </select>
        </label>
        <label class="text-sm font-bold text-stone-600">
          source_key
          <input v-model="sourceKeyFilter" class="mt-2 w-full rounded-2xl border p-3 text-sm" placeholder="筛选 source_key" @keyup.enter="loadCandidates">
        </label>
        <label class="text-sm font-bold text-stone-600">
          thread_id
          <input v-model="threadIdFilter" class="mt-2 w-full rounded-2xl border p-3 text-sm" placeholder="筛选 thread_id" @keyup.enter="loadCandidates">
        </label>
      </div>
    </section>

    <!-- 主题沉淀 -->
    <section class="mt-4 rounded-3xl border bg-white p-5">
      <h3 class="text-lg font-black">主题沉淀</h3>
      <div class="mt-3 grid gap-3 sm:grid-cols-[1fr_1fr_auto_auto]">
        <label class="text-sm font-bold text-stone-600">
          source_key
          <input v-model="composeSourceKey" class="mt-2 w-full rounded-2xl border p-3 text-sm" placeholder="主题 source_key">
        </label>
        <label class="text-sm font-bold text-stone-600">
          thread_id（可选）
          <input v-model="composeThreadId" class="mt-2 w-full rounded-2xl border p-3 text-sm" placeholder="主题 thread_id">
        </label>
        <label class="flex items-end gap-2 text-sm font-bold text-stone-600">
          <input v-model="composeForce" type="checkbox" class="h-5 w-5 rounded border-stone-300">
          强制沉淀
        </label>
        <button class="self-end rounded-2xl bg-stone-950 px-4 py-3 font-bold text-white disabled:cursor-not-allowed disabled:bg-stone-400" :disabled="composing || !hasProjectContext" @click="composeTopic">
          {{ composing ? '沉淀中...' : '沉淀' }}
        </button>
      </div>
      <p v-if="!hasProjectContext" class="mt-3 rounded-2xl bg-amber-50 p-3 text-sm text-amber-800">请先选择组织和项目。</p>
      <p v-if="error" class="mt-3 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
      <p v-if="success" class="mt-3 rounded-2xl bg-emerald-50 p-3 text-sm text-emerald-700">{{ success }}</p>
    </section>

    <!-- 候选列表 -->
    <section class="mt-6 rounded-3xl border bg-white p-5">
      <h3 class="text-xl font-black">候选列表（{{ candidates.length }}）</h3>
      <div v-if="loading" class="mt-4 rounded-2xl bg-stone-50 p-4 text-stone-600">正在加载候选记忆...</div>
      <div v-else-if="candidates.length === 0" class="mt-4 rounded-2xl bg-stone-50 p-4 text-stone-600">当前过滤条件下暂无候选记忆。</div>
      <div v-else class="mt-4 grid gap-4">
        <article v-for="c in candidates" :key="c.candidate_id" class="rounded-3xl border bg-stone-50 p-5">
          <div class="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
            <div class="flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <span class="rounded-full px-2 py-0.5 text-xs font-bold" :class="riskColor(c.risk_level)">{{ c.risk_level }}</span>
                <span class="rounded-full bg-blue-100 px-2 py-0.5 text-xs font-bold text-blue-900">{{ memoryTypeText(c.memory_type) }}</span>
                <span class="rounded-full bg-stone-200 px-2 py-0.5 text-xs font-bold text-stone-700">{{ statusText(c.status) }}</span>
                <span class="text-xs text-stone-500">conf {{ c.confidence.toFixed(2) }}</span>
              </div>
              <p class="mt-2 text-sm font-bold text-stone-950">{{ c.content }}</p>
              <p v-if="c.summary" class="mt-1 text-sm text-stone-600">{{ c.summary }}</p>
              <p class="mt-2 break-all text-xs text-stone-500">
                {{ c.candidate_id }}
                <template v-if="c.source_key"> · {{ c.source_key }}</template>
                <template v-if="c.thread_id"> · {{ c.thread_id }}</template>
              </p>
              <p class="mt-1 text-xs text-stone-500">
                事件 {{ c.source_event_ids.length }} 条 · hot_score {{ c.scores.hot_memory_score.toFixed(2) }} · compose {{ c.scores.compose_score.toFixed(2) }}
              </p>
              <p class="mt-1 text-xs text-stone-400">创建于 {{ formatDate(c.created_at) }}</p>
            </div>
            <div class="flex flex-wrap gap-2">
              <button v-if="c.status === 'pending' || c.status === 'in_compose_pool'" class="rounded-2xl bg-emerald-100 px-3 py-2 text-sm font-bold text-emerald-900" @click="acceptCandidate(c.candidate_id)">接受</button>
              <button v-if="c.status === 'pending' || c.status === 'in_compose_pool'" class="rounded-2xl bg-red-100 px-3 py-2 text-sm font-bold text-red-900" @click="discardCandidate(c.candidate_id)">丢弃</button>
            </div>
          </div>
        </article>
      </div>
    </section>
  </AppShell>
</template>
