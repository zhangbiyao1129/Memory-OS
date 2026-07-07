<script setup lang="ts">
import { formatCandidateMaintenanceSummary } from '~/utils/memoryUx'

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

type MaintenanceStatus = {
  active: boolean
  run_id: string
  status: string
  stage: string
  progress_percent: number
  total_candidates: number
  processed: number
  discarded: number
  kept: number
  composed: number
  archive_id: string
  summary: string
  last_error: string
  started_at: string
  completed_at: string | null
}

/** 数字兜底,杜绝 undefined */
function n(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0
}

function stageLabel(stage: string): string {
  const labels: Record<string, string> = {
    queued: '排队中',
    loading_candidates: '加载候选',
    calling_llm: '模型处理中',
    validating: '校验结果',
    applying: '执行清洗',
    composing: '沉淀归档',
    done: '已完成',
    failed: '失败'
  }
  return labels[stage] || stage
}

const loading = ref(false)
const error = ref('')
const success = ref('')
const candidates = ref<Candidate[]>([])
const cleaning = ref(false)
const { stats: lifecycleStats, loadStats: loadLifecycleStats } = useMemoryLifecycleStats()

// 维护任务状态
const maintenanceStatus = ref<MaintenanceStatus | null>(null)
let pollTimer: ReturnType<typeof setInterval> | null = null

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

// localStorage key
function maintenanceKey(): string {
  return `maintenance_run:${context.orgId}:${context.projectId}`
}

function saveRunID(runID: string) {
  try {
    localStorage.setItem(maintenanceKey(), runID)
  } catch {}
}

function clearRunID() {
  try {
    localStorage.removeItem(maintenanceKey())
  } catch {}
}

function loadSavedRunID(): string | null {
  try {
    return localStorage.getItem(maintenanceKey())
  } catch {
    return null
  }
}

// 轮询维护状态
function startPolling(runID?: string) {
  stopPolling()
  pollTimer = setInterval(async () => {
    try {
      const body: Record<string, unknown> = {
        org_id: context.orgId,
        project_id: context.projectId
      }
      if (runID) body.run_id = runID
      const status = await request<MaintenanceStatus>('/memory/candidates/maintenance/status', {
        method: 'POST',
        body: requestBody(body)
      })
      maintenanceStatus.value = status
      if (status.status === 'done') {
        stopPolling()
        clearRunID()
        success.value = ''
        cleaning.value = false
        await loadCandidates()
        await loadLifecycleStats()
      } else if (status.status === 'failed') {
        stopPolling()
        clearRunID()
        error.value = ''
        cleaning.value = false
      }
    } catch {
      // 轮询失败不停止,等待下次
    }
  }, 2000)
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

// 恢复已有任务
async function restoreMaintenanceTask() {
  const savedRunID = loadSavedRunID()
  try {
    const body: Record<string, unknown> = {
      org_id: context.orgId,
      project_id: context.projectId
    }
    if (savedRunID) body.run_id = savedRunID
    const status = await request<MaintenanceStatus>('/memory/candidates/maintenance/status', {
      method: 'POST',
      body: requestBody(body)
    })
    if (status && status.active) {
      maintenanceStatus.value = status
      cleaning.value = true
      startPolling(status.run_id)
    } else if (status && status.status === 'done') {
      // 任务已完成,清理
      clearRunID()
      if (savedRunID) {
        maintenanceStatus.value = status
        success.value = ''
      }
    } else {
      clearRunID()
    }
  } catch {
    clearRunID()
  }
}

async function loadCandidates() {
  if (!auth.isAuthenticated || !hasProjectContext.value) return
  loading.value = true
  error.value = ''
  try {
    const body: Record<string, unknown> = { limit: 50 }
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

async function runMaintenance() {
  if (!hasProjectContext.value) {
    error.value = '请先选择组织和项目'
    return
  }
  cleaning.value = true
  error.value = ''
  success.value = ''
  maintenanceStatus.value = null
  try {
    const result = await request<MaintenanceStatus>('/memory/candidates/maintenance/run', {
      method: 'POST',
      body: requestBody()
    })
    if (result && result.active) {
      // 任务已启动或已有运行中任务
      maintenanceStatus.value = result
      saveRunID(result.run_id)
      startPolling(result.run_id)
    } else if (result && result.status === 'done') {
      // 已有完成的任务直接返回
      cleaning.value = false
      maintenanceStatus.value = result
      await loadCandidates()
    } else {
      cleaning.value = false
    }
  } catch (err: any) {
    error.value = err.message || '清洗失败'
    cleaning.value = false
  }
}

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  await Promise.all([loadCandidates(), loadLifecycleStats(), restoreMaintenanceTask()])
})

watch(() => [context.orgId, context.projectId], () => {
  stopPolling()
  maintenanceStatus.value = null
  void loadCandidates()
  void restoreMaintenanceTask()
})

onBeforeUnmount(() => {
  stopPolling()
})
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <h2 class="text-3xl font-black">候选记忆</h2>
        <p class="mt-2 text-stone-600">候选提炼结果，支持接受/丢弃；清洗和沉淀由系统自动完成。</p>
      </div>
      <button class="rounded-2xl border bg-white px-4 py-2 font-bold" :disabled="loading" @click="loadCandidates">{{ loading ? '刷新中...' : '刷新' }}</button>
    </div>

    <p v-if="error" class="mt-6 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <p v-if="success" class="mt-6 rounded-2xl bg-emerald-50 p-3 text-sm text-emerald-700">{{ success }}</p>

    <!-- 维护工具 -->
    <section class="mt-6 rounded-3xl border bg-white p-5">
      <div class="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
        <div>
          <h3 class="text-lg font-black">候选维护</h3>
          <p class="mt-1 text-sm text-stone-600">系统按当前记忆上下文自动清洗、合并，并触发主题沉淀。</p>
        </div>
        <button class="rounded-2xl bg-orange-600 px-4 py-3 text-sm font-bold text-white disabled:cursor-not-allowed disabled:bg-stone-400" :disabled="cleaning || !hasProjectContext" @click="runMaintenance">
          {{ cleaning ? '清洗中...' : 'AI 清洗' }}
        </button>
      </div>

      <p v-if="!hasProjectContext" class="mt-3 rounded-2xl bg-amber-50 p-3 text-sm text-amber-800">请先选择组织和项目。</p>

      <!-- 进度条 -->
      <div v-if="maintenanceStatus && maintenanceStatus.active" class="mt-4 rounded-2xl bg-orange-50 p-4">
        <div class="flex items-center justify-between text-sm font-bold text-orange-900">
          <span>{{ stageLabel(maintenanceStatus.stage) }}</span>
          <span>{{ n(maintenanceStatus.progress_percent) }}%</span>
        </div>
        <div class="mt-2 h-3 w-full overflow-hidden rounded-full bg-orange-200">
          <div class="h-full rounded-full bg-orange-500 transition-all duration-500" :style="{ width: n(maintenanceStatus.progress_percent) + '%' }"></div>
        </div>
        <p class="mt-2 text-xs text-orange-700">
          <template v-if="maintenanceStatus.stage === 'calling_llm'">模型处理中，请稍候...</template>
          <template v-else>已加载 {{ n(maintenanceStatus.total_candidates) }} 条候选</template>
        </p>
      </div>

      <!-- 失败原因 -->
      <div v-if="maintenanceStatus && maintenanceStatus.status === 'failed'" class="mt-4 rounded-2xl bg-red-50 p-4">
        <p class="text-sm font-bold text-red-900">清洗失败：{{ maintenanceStatus.last_error || '未知错误' }}</p>
      </div>

      <!-- 完成统计 -->
      <div v-if="maintenanceStatus && maintenanceStatus.status === 'done' && !cleaning" class="mt-4 rounded-2xl bg-emerald-50 p-4">
        <p class="text-sm font-bold text-emerald-900">
          {{ formatCandidateMaintenanceSummary(maintenanceStatus) }}
        </p>
      </div>
    </section>

    <!-- 候选列表 -->
    <section class="mt-6 rounded-3xl border bg-white p-5">
      <h3 class="text-xl font-black">候选列表（当前页 {{ candidates.length }} 条）</h3>
      <p class="mt-2 text-sm text-stone-500">
        当前页显示 {{ candidates.length }} 条，当前项目待处理候选 {{ n(lifecycleStats?.candidates?.actionable_total) }} 条。
      </p>
      <div v-if="loading" class="mt-4 rounded-2xl bg-stone-50 p-4 text-stone-600">正在加载候选记忆...</div>
      <div v-else-if="candidates.length === 0" class="mt-4 rounded-2xl bg-stone-50 p-4 text-stone-600">当前暂无候选记忆。</div>
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
