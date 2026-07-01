<script setup lang="ts">
const { request, baseURL } = useApi()
const health = ref('checking')
const stats = [
  ['Archives', '12', 'Markdown authoritative source'],
  ['Hot Memories', '38', 'Project/user scoped facts'],
  ['Secrets', '6', 'metadata only, plaintext hidden'],
  ['Adapters', '5', 'Codex, Claude, opencode, Hermes, Transcript']
]
onMounted(async () => {
  try {
    const data = await request<{ status: string }>('/healthz')
    health.value = data.status || 'unknown'
  } catch {
    health.value = 'offline'
  }
})
</script>

<template>
  <AppShell>
    <section class="grid gap-6">
      <div class="flex flex-col gap-4 rounded-[2rem] bg-gradient-to-br from-orange-100 to-lime-100 p-6 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <p class="text-xs font-bold uppercase tracking-[0.35em] text-orange-900">Native Multi-Agent Memory Platform</p>
          <h2 class="mt-3 text-4xl font-black">Memory OS Dashboard</h2>
          <p class="mt-2 max-w-2xl text-stone-700">统一管理组织、项目、Archive RAG、Hot Memory、Secret Vault、Adapter Token 和检索测试。</p>
        </div>
        <HealthBadge :status="health" />
      </div>
      <div class="rounded-3xl border border-stone-200 bg-white p-4 text-sm text-stone-600">API Base: <code>{{ baseURL }}</code></div>
      <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <article v-for="item in stats" :key="item[0]" class="rounded-3xl border border-stone-200 bg-white p-5">
          <p class="text-sm font-bold text-stone-500">{{ item[0] }}</p>
          <p class="mt-3 text-4xl font-black">{{ item[1] }}</p>
          <p class="mt-2 text-sm text-stone-600">{{ item[2] }}</p>
        </article>
      </div>
    </section>
  </AppShell>
</template>
