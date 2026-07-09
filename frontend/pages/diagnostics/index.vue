<script setup lang="ts">
import { explainLifecycle } from '~/utils/memoryUx'

const auth = useAuthStore()
const context = useContextStore()
const { request } = useApi()
const { stats, loading, error, hasProjectContext, loadStats } = useMemoryLifecycleStats({ userScoped: true })

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
  archive_material: number
  promoted_hot: number
  needs_review: number
  hot_memory_demoted: number
  archive_id: string
  summary: string
  last_error: string
  started_at: string
  completed_at: string | null
}

const maintenanceStatus = ref<MaintenanceStatus | null>(null)
const maintenanceError = ref('')

const steps = computed(() => explainLifecycle({
  candidateJobsDone: stats.value?.candidate_jobs?.done || 0,
  candidateJobsFailed: stats.value?.candidate_jobs?.failed || 0,
  candidatesActionable: stats.value?.candidates?.actionable_total || 0,
  hotMemories: stats.value?.hot_memories?.total || 0,
  archives: stats.value?.archives?.total || 0
}))

function stepClass(tone: string) {
  if (tone === 'ok') return 'border-emerald-200 bg-emerald-50 text-emerald-950'
  if (tone === 'error') return 'border-red-200 bg-red-50 text-red-950'
  if (tone === 'warning') return 'border-amber-200 bg-amber-50 text-amber-950'
  return 'border-stone-200 bg-stone-50 text-stone-700'
}

function formatDate(value?: string | null) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

function stageLabel(stage?: string): string {
  const labels: Record<string, string> = {
    queued: '排队中',
    loading_candidates: '加载候选',
    calling_llm: '模型处理中',
    validating: '校验结果',
    applying: '应用整理',
    composing: '生成归档',
    done: '已完成',
    failed: '失败'
  }
  return labels[stage || ''] || stage || '-'
}

async function loadMaintenanceStatus() {
  maintenanceError.value = ''
  maintenanceStatus.value = null
  if (!auth.isAuthenticated || !context.orgId) return
  try {
    maintenanceStatus.value = await request<MaintenanceStatus>('/memory/organize/workspace/status', {
      method: 'POST',
      body: { org_id: context.orgId }
    })
  } catch (err: any) {
    maintenanceError.value = err.message || 'AI 整理任务状态加载失败'
  }
}

async function loadDiagnostics() {
  await loadStats()
  await loadMaintenanceStatus()
}

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  await loadDiagnostics()
})

watch(() => [auth.token, context.orgId], () => {
  void loadDiagnostics()
})
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <p class="text-xs font-black uppercase tracking-[0.35em] text-orange-800">Diagnostics</p>
        <h2 class="mt-2 text-3xl font-black">记忆写入诊断</h2>
        <p class="mt-2 max-w-3xl text-stone-600">全部记忆生命周期：把采集、提炼、候选、热记忆和归档拆开看，避免把 Archive 为空误判成没有写入。</p>
      </div>
      <button class="rounded-2xl border bg-white px-4 py-2 font-bold" :disabled="loading" @click="loadDiagnostics">{{ loading ? '刷新中...' : '刷新' }}</button>
    </div>

    <p v-if="!auth.isAuthenticated" class="mt-6 rounded-2xl bg-amber-50 p-4 text-sm text-amber-800">请先登录。</p>
    <p v-else-if="!hasProjectContext" class="mt-6 rounded-2xl bg-amber-50 p-4 text-sm text-amber-800">请先登录后查看账号级诊断。</p>
    <p v-if="error" class="mt-6 rounded-2xl bg-red-50 p-4 text-sm text-red-700">{{ error }}</p>

    <section class="mt-6 grid gap-4 md:grid-cols-5">
      <article v-for="step in steps" :key="step.label" class="rounded-3xl border p-5" :class="stepClass(step.tone)">
        <h3 class="text-lg font-black">{{ step.label }}</h3>
        <p class="mt-2 text-sm leading-6">{{ step.detail }}</p>
      </article>
    </section>

    <section class="mt-6 grid gap-4 lg:grid-cols-2">
      <article class="rounded-3xl border bg-white p-5">
        <h3 class="text-xl font-black">候选任务队列</h3>
        <div class="mt-4 grid gap-3 text-sm sm:grid-cols-2">
          <p class="rounded-2xl bg-stone-50 p-4">总任务<br><span class="text-2xl font-black">{{ stats?.candidate_jobs?.total || 0 }}</span></p>
          <p class="rounded-2xl bg-stone-50 p-4">失败<br><span class="text-2xl font-black text-red-700">{{ stats?.candidate_jobs?.failed || 0 }}</span></p>
          <p class="rounded-2xl bg-stone-50 p-4">最早待处理<br><span class="break-all font-bold">{{ formatDate(stats?.candidate_jobs?.oldest_pending_at) }}</span></p>
          <p class="rounded-2xl bg-stone-50 p-4">最近完成<br><span class="break-all font-bold">{{ formatDate(stats?.candidate_jobs?.last_completed_at) }}</span></p>
        </div>
        <p v-if="stats?.candidate_jobs?.latest_error" class="mt-4 rounded-2xl bg-red-50 p-4 text-sm text-red-800">最近错误：{{ stats.candidate_jobs.latest_error }}</p>
      </article>

      <article class="rounded-3xl border bg-white p-5">
        <h3 class="text-xl font-black">AI 整理任务</h3>
        <p v-if="maintenanceError" class="mt-4 rounded-2xl bg-red-50 p-4 text-sm text-red-800">{{ maintenanceError }}</p>
        <p v-else-if="!context.orgId" class="mt-4 rounded-2xl bg-amber-50 p-4 text-sm text-amber-800">选择组织后展示 workspace 级 AI 整理状态。</p>
        <div v-else class="mt-4 grid gap-3 text-sm">
          <p class="rounded-2xl bg-stone-50 p-4">状态：<span class="font-black">{{ maintenanceStatus?.active ? '运行中' : (maintenanceStatus?.status || '空闲') }}</span> · 阶段：{{ stageLabel(maintenanceStatus?.stage) }}</p>
          <p class="rounded-2xl bg-stone-50 p-4">进度：{{ maintenanceStatus?.progress_percent || 0 }}% · 处理 {{ maintenanceStatus?.processed || 0 }} / {{ maintenanceStatus?.total_candidates || 0 }}</p>
          <p class="rounded-2xl bg-stone-50 p-4">归档素材 {{ maintenanceStatus?.archive_material || 0 }} · 热记忆 {{ maintenanceStatus?.promoted_hot || 0 }} · 待确认 {{ maintenanceStatus?.needs_review || 0 }}</p>
          <p v-if="maintenanceStatus?.last_error" class="rounded-2xl bg-red-50 p-4 text-red-800">最近错误：{{ maintenanceStatus.last_error }}</p>
        </div>
      </article>
    </section>

    <section class="mt-6 rounded-3xl border bg-white p-5">
      <h3 class="text-xl font-black">当前判断</h3>
      <p v-if="(stats?.archives.total || 0) === 0" class="mt-3 rounded-2xl bg-amber-50 p-4 text-sm text-amber-900">
        Archive 为空不代表没写入，可能停在候选或热记忆层。先看候选和热记忆，再决定是否执行 AI 整理和归档。
      </p>
      <p v-else class="mt-3 rounded-2xl bg-emerald-50 p-4 text-sm text-emerald-900">当前项目已有长期归档，可以继续用检索验证召回效果。</p>
      <div class="mt-4 flex flex-wrap gap-3">
        <NuxtLink class="rounded-2xl bg-stone-950 px-4 py-2 font-black text-white" to="/candidates">处理候选</NuxtLink>
        <NuxtLink class="rounded-2xl border bg-white px-4 py-2 font-black text-stone-950" to="/hot-memory">查看热记忆</NuxtLink>
        <NuxtLink class="rounded-2xl border bg-white px-4 py-2 font-black text-stone-950" to="/archive">查看归档</NuxtLink>
        <NuxtLink class="rounded-2xl border bg-white px-4 py-2 font-black text-stone-950" to="/search">检索验证</NuxtLink>
      </div>
    </section>

    <!-- Memory Kernel 统计 -->
    <section v-if="stats?.memory_kernel" class="mt-6 rounded-3xl border bg-white p-5">
      <h3 class="text-xl font-black">Memory Kernel</h3>
      <div class="mt-4 grid gap-3 text-sm sm:grid-cols-4">
        <p class="rounded-2xl bg-stone-50 p-4">可信事实<br><span class="text-2xl font-black">{{ stats.memory_kernel.units_current || 0 }}</span></p>
        <p class="rounded-2xl bg-stone-50 p-4">待确认<br><span class="text-2xl font-black text-yellow-600">{{ stats.memory_kernel.units_needs_review || 0 }}</span></p>
        <p class="rounded-2xl bg-stone-50 p-4">治理运行<br><span class="text-2xl font-black">{{ stats.memory_kernel.governance_runs_total || 0 }}</span></p>
        <p class="rounded-2xl bg-stone-50 p-4">CI 用例<br><span class="text-2xl font-black">{{ stats.memory_kernel.ci_cases_active || 0 }}</span></p>
      </div>
      <div v-if="stats.memory_kernel.last_run_at" class="mt-3 text-sm text-stone-600">
        最近治理: {{ stats.memory_kernel.last_run_at }} — {{ stats.memory_kernel.last_run_summary || '无摘要' }}
      </div>
    </section>

    <details class="mt-6 rounded-3xl border bg-white p-5">
      <summary class="cursor-pointer text-xl font-black">技术统计</summary>
      <pre class="mt-4 overflow-auto rounded-2xl bg-stone-950 p-4 text-sm text-stone-50">{{ JSON.stringify(stats, null, 2) }}</pre>
    </details>
  </AppShell>
</template>
