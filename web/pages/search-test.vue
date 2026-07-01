<script setup lang="ts">
const { request } = useApi()
const context = useContextStore()
const query = ref('deploy API')
const loading = ref(false)
const error = ref('')
const result = ref<any>(null)
async function runSearch() {
  loading.value = true
  error.value = ''
  result.value = null
  try {
    result.value = await request('/memory/search', {
      method: 'POST',
      body: {
        request_id: `web-search-${Date.now()}`,
        query: query.value,
        actor: { user_id: 'user_1', org_id: context.orgId, project_id: context.projectId, agent_id: context.agentId },
        scope: 'project',
        visibility: 'project',
        permission_labels: context.permissionLabels,
        archive_index_generation: 2,
        max_context_bytes: 512
      }
    })
  } catch (err: any) {
    error.value = err.message || '检索失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <AppShell>
    <h2 class="text-3xl font-black">检索测试</h2>
    <p class="mt-2 text-stone-600">调用 <code>/memory/search</code>，展示 rerank_degraded、source refs 和 filter 摘要。</p>
    <div class="mt-6 rounded-3xl border bg-white p-5">
      <textarea v-model="query" class="min-h-28 w-full rounded-2xl border p-4" />
      <button class="mt-3 rounded-2xl bg-stone-950 px-4 py-2 font-bold text-white" :disabled="loading" @click="runSearch">{{ loading ? '检索中...' : '运行检索' }}</button>
      <p v-if="error" class="mt-3 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    </div>
    <section v-if="result" class="mt-6 grid gap-4">
      <div class="rounded-3xl border bg-lime-50 p-5"><b>rerank_degraded:</b> {{ result.rerank_degraded }}</div>
      <pre class="overflow-auto rounded-3xl border bg-stone-950 p-5 text-sm text-stone-50">{{ JSON.stringify(result, null, 2) }}</pre>
      <SourceRefList :refs="(result.results || []).map((item: any) => item.source)" />
    </section>
  </AppShell>
</template>
