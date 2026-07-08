<script setup lang="ts">
import { explainLifecycle } from '~/utils/memoryUx'

const auth = useAuthStore()
const context = useContextStore()
const { stats, loading, error, hasProjectContext, loadStats } = useMemoryLifecycleStats()

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

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  await loadStats()
})
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <p class="text-xs font-black uppercase tracking-[0.35em] text-orange-800">Diagnostics</p>
        <h2 class="mt-2 text-3xl font-black">记忆写入诊断</h2>
        <p class="mt-2 max-w-3xl text-stone-600">把采集、提炼、候选、热记忆和归档拆开看，避免把 Archive 为空误判成没有写入。</p>
      </div>
      <button class="rounded-2xl border bg-white px-4 py-2 font-bold" :disabled="loading" @click="loadStats">{{ loading ? '刷新中...' : '刷新' }}</button>
    </div>

    <p v-if="!auth.isAuthenticated" class="mt-6 rounded-2xl bg-amber-50 p-4 text-sm text-amber-800">请先登录。</p>
    <p v-else-if="!hasProjectContext" class="mt-6 rounded-2xl bg-amber-50 p-4 text-sm text-amber-800">请先选择组织和项目。</p>
    <p v-if="error" class="mt-6 rounded-2xl bg-red-50 p-4 text-sm text-red-700">{{ error }}</p>

    <section class="mt-6 grid gap-4 md:grid-cols-5">
      <article v-for="step in steps" :key="step.label" class="rounded-3xl border p-5" :class="stepClass(step.tone)">
        <h3 class="text-lg font-black">{{ step.label }}</h3>
        <p class="mt-2 text-sm leading-6">{{ step.detail }}</p>
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

    <details class="mt-6 rounded-3xl border bg-white p-5">
      <summary class="cursor-pointer text-xl font-black">技术统计</summary>
      <pre class="mt-4 overflow-auto rounded-2xl bg-stone-950 p-4 text-sm text-stone-50">{{ JSON.stringify(stats, null, 2) }}</pre>
    </details>
  </AppShell>
</template>
