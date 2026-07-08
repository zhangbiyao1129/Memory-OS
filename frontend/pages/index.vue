<script setup lang="ts">
const { request, baseURL } = useApi()
const health = ref('checking')

const { stats, loading, error: statsError, loadStats } = useMemoryLifecycleStats({ userScoped: true })

const assetRows = computed(() => [
  { label: '归档库', value: stats.value?.archives.total || 0, tone: 'bg-sky-600' },
  { label: '热记忆', value: stats.value?.hot_memories.total || 0, tone: 'bg-orange-600' },
  { label: '待确认', value: stats.value?.candidates.actionable_total ?? 0, tone: 'bg-amber-500' },
  { label: '归档任务', value: stats.value?.topics.total || 0, tone: 'bg-emerald-600' }
])

const candidateStatusSegments = computed(() => Object.entries(stats.value?.candidates.by_status || {}).map(([label, value]) => ({
  label,
  value,
  className: label === 'discarded' ? 'bg-red-300' : label === 'composed' ? 'bg-emerald-500' : 'bg-amber-400'
})))

async function loadHealth() {
  try {
    const data = await request<{ status: string }>('/healthz')
    health.value = data.status || 'unknown'
  } catch {
    health.value = 'offline'
  }
}

onMounted(async () => {
  await loadHealth()
  await loadStats()
})
</script>

<template>
  <AppShell>
    <section class="grid gap-6">
      <div class="flex flex-col gap-4 rounded-[2rem] bg-gradient-to-br from-orange-100 to-lime-100 p-6 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <p class="text-xs font-bold uppercase tracking-[0.35em] text-orange-900">原生多 Agent 记忆平台</p>
          <h2 class="mt-3 text-4xl font-black">Memory OS 记忆与检索总览</h2>
          <p class="mt-2 max-w-2xl text-stone-700">统一管理 Archive RAG、Hot Memory、候选记忆和归档任务。</p>
        </div>
        <HealthBadge :status="health" />
      </div>
      <div class="rounded-3xl border border-stone-200 bg-white p-4 text-sm text-stone-600">
        <span>总览口径：当前用户全部记忆</span>
        <span class="mx-2 text-stone-300">/</span>
        <span>API 地址：<code>{{ baseURL }}</code></span>
      </div>
      <p v-if="loading" class="rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">正在汇总当前用户的全部记忆统计...</p>
      <p v-if="statsError" class="rounded-2xl bg-red-50 p-4 text-sm text-red-700">{{ statsError }}</p>
      <section class="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <MetricTile label="归档库" :value="stats?.archives.total || 0" detail="长期沉淀" />
        <MetricTile label="热记忆" :value="stats?.hot_memories.total || 0" detail="工作记忆" />
        <MetricTile label="待确认" :value="stats?.candidates.actionable_total ?? 0" detail="需人工确认" />
        <MetricTile label="归档任务" :value="stats?.topics.total || 0" detail="归档进度" />
      </section>
      <section class="grid gap-4 lg:grid-cols-2">
        <BarChart title="生命周期资产" :rows="assetRows" />
        <StackedBar title="候选状态" :segments="candidateStatusSegments" />
        <RingMeter title="归档完成率" :value="stats?.topics.composed || 0" :total="stats?.topics.total || 0" label="已归档" />
      </section>
    </section>
  </AppShell>
</template>
