<script setup lang="ts">
const { stats, loading, error, loadStats } = useMemoryLifecycleStats({ userScoped: true })

const lifecycleRows = computed(() => [
  { label: '归档库', value: stats.value?.archives.total || 0, tone: 'bg-sky-600' },
  { label: '热记忆', value: stats.value?.hot_memories.total || 0, tone: 'bg-orange-600' },
  { label: '待处理候选', value: stats.value?.candidates.actionable_total ?? 0, tone: 'bg-amber-500' },
  { label: '主题沉淀', value: stats.value?.topics.total || 0, tone: 'bg-emerald-600' }
])

const riskRows = computed(() => Object.entries(stats.value?.candidates.by_risk || {}).map(([label, value]) => ({
  label,
  value,
  tone: label === 'high' ? 'bg-red-500' : label === 'medium' ? 'bg-amber-500' : 'bg-emerald-500'
})))

const entries = [
  { title: '归档库', to: '/archive', description: '长期沉淀的 Markdown 记忆资产。' },
  { title: '热记忆', to: '/hot-memory', description: '高价值、可直接命中的工作记忆。' },
  { title: '候选记忆', to: '/candidates', description: '待确认、待分流、待沉淀的提炼结果。' },
  { title: '主题沉淀', to: '/topics', description: '按 source_key 和 thread 聚合的沉淀进度。' }
]

onMounted(loadStats)
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <h2 class="text-3xl font-black">记忆</h2>
        <p class="mt-2 text-stone-600">围绕候选、热记忆、主题沉淀和归档库查看当前用户全部记忆生命周期。</p>
      </div>
      <button class="rounded-2xl border bg-white px-4 py-2 font-bold" :disabled="loading" @click="loadStats">{{ loading ? '刷新中...' : '刷新' }}</button>
    </div>

    <p v-if="error" class="mt-6 rounded-2xl bg-red-50 p-4 text-sm text-red-700">{{ error }}</p>

    <section class="mt-6 grid gap-4 lg:grid-cols-2">
      <BarChart title="生命周期" :rows="lifecycleRows" />
      <BarChart title="候选风险" :rows="riskRows" />
      <RingMeter title="主题沉淀" :value="stats?.topics.composed || 0" :total="stats?.topics.total || 0" label="已沉淀" />
      <StackedBar title="候选状态" :segments="Object.entries(stats?.candidates.by_status || {}).map(([label, value]) => ({ label, value, className: 'bg-amber-400' }))" />
    </section>

    <!-- 候选提炼健康 -->
    <section class="mt-6 rounded-3xl border border-stone-200 bg-white p-6">
      <h3 class="text-xl font-black">候选提炼健康</h3>
      <div class="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div class="rounded-2xl bg-stone-50 p-4">
          <p class="text-sm text-stone-500">Pending</p>
          <p class="mt-1 text-2xl font-bold">{{ stats?.candidate_jobs?.pending ?? 0 }}</p>
        </div>
        <div class="rounded-2xl bg-stone-50 p-4">
          <p class="text-sm text-stone-500">Running</p>
          <p class="mt-1 text-2xl font-bold">{{ stats?.candidate_jobs?.running ?? 0 }}</p>
        </div>
        <div class="rounded-2xl bg-stone-50 p-4">
          <p class="text-sm text-stone-500">Failed</p>
          <p class="mt-1 text-2xl font-bold" :class="stats?.candidate_jobs?.failed ? 'text-red-600' : ''">{{ stats?.candidate_jobs?.failed ?? 0 }}</p>
        </div>
        <div class="rounded-2xl bg-stone-50 p-4">
          <p class="text-sm text-stone-500">Done</p>
          <p class="mt-1 text-2xl font-bold">{{ stats?.candidate_jobs?.done ?? 0 }}</p>
        </div>
      </div>
      <div class="mt-4 grid gap-4 sm:grid-cols-3">
        <div>
          <p class="text-sm text-stone-500">最近完成时间</p>
          <p class="mt-1 text-sm">{{ stats?.candidate_jobs?.last_completed_at || '暂无' }}</p>
        </div>
        <div>
          <p class="text-sm text-stone-500">最早积压时间</p>
          <p class="mt-1 text-sm">{{ stats?.candidate_jobs?.oldest_pending_at || '暂无' }}</p>
        </div>
        <div>
          <p class="text-sm text-stone-500">最近错误</p>
          <p class="mt-1 text-sm" :class="stats?.candidate_jobs?.latest_error ? 'text-red-600' : 'text-stone-400'">
            {{ stats?.candidate_jobs?.latest_error || '暂无错误' }}
          </p>
        </div>
      </div>
    </section>

    <section class="mt-6 grid gap-4 md:grid-cols-2">
      <NuxtLink v-for="entry in entries" :key="entry.to" :to="entry.to" class="rounded-3xl border bg-white p-5 hover:border-orange-300">
        <h3 class="text-xl font-black">{{ entry.title }}</h3>
        <p class="mt-2 text-sm leading-6 text-stone-600">{{ entry.description }}</p>
      </NuxtLink>
    </section>
  </AppShell>
</template>
