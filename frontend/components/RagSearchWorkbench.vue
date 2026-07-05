<script setup lang="ts">
const { request } = useApi()
const auth = useAuthStore()
const context = useContextStore()
const query = ref('deploy API')
const loading = ref(false)
const error = ref('')
const result = ref<any>(null)
const hasContext = computed(() => Boolean(context.orgId && context.projectId))
const allResults = computed(() => result.value?.results || [])
const hotMemoryResults = computed(() => allResults.value.filter((item: any) => item?.source?.kind === 'hot_memory'))
const archiveRAGResults = computed(() => allResults.value.filter((item: any) => item?.source?.kind === 'archive_chunk'))
const sourceRefs = computed(() => allResults.value.map((item: any) => item.source).filter(Boolean))

function resultKey(item: any, index: number) {
  const source = item?.source || {}
  return source.memory_id || source.chunk_id || source.archive_id || item?.text || `result_${index}`
}

function resultTitle(item: any) {
  if (item?.source?.kind === 'hot_memory') return 'Hot Memory'
  if (item?.source?.kind === 'archive_chunk') return 'Archive Chunk'
  return item?.source?.kind || '检索结果'
}

onMounted(() => {
  auth.initFromStorage()
  context.initFromStorage()
})

async function runSearch() {
  auth.initFromStorage()
  context.initFromStorage()
  if (!auth.isAuthenticated) {
    error.value = '请先登录，再执行检索测试。'
    return
  }
  if (!hasContext.value) {
    error.value = '请先在顶部选择真实组织和项目。'
    return
  }

  loading.value = true
  error.value = ''
  result.value = null
  try {
    result.value = await request('/memory/search', {
      method: 'POST',
      body: {
        request_id: `web-search-${Date.now()}`,
        query: query.value,
        actor: { user_id: '', org_id: context.orgId, project_id: context.projectId, agent_id: context.agentId },
        scope: 'project',
        visibility: 'project',
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
    <h2 class="text-3xl font-black">检索验证</h2>
    <p class="mt-2 text-stone-600">调用 <code>/memory/search</code>，后端会使用当前 PAT 主体覆盖请求里的 user_id，并按当前组织 / 项目执行权限过滤。</p>
    <div class="mt-4 grid gap-3 md:grid-cols-3">
      <div class="rounded-2xl border bg-white p-4 text-sm">
        <div class="font-bold text-stone-500">组织</div>
        <div class="mt-1 break-all text-stone-950">{{ context.orgId || '未选择' }}</div>
      </div>
      <div class="rounded-2xl border bg-white p-4 text-sm">
        <div class="font-bold text-stone-500">项目</div>
        <div class="mt-1 break-all text-stone-950">{{ context.projectId || '未选择' }}</div>
      </div>
      <div class="rounded-2xl border bg-white p-4 text-sm">
        <div class="font-bold text-stone-500">Agent</div>
        <div class="mt-1 break-all text-stone-950">{{ context.agentId }}</div>
      </div>
    </div>
    <div class="mt-6 rounded-3xl border bg-white p-5">
      <textarea v-model="query" class="min-h-28 w-full rounded-2xl border p-4" />
      <button class="mt-3 rounded-2xl bg-stone-950 px-4 py-2 font-bold text-white disabled:cursor-not-allowed disabled:bg-stone-400" :disabled="loading || !auth.isAuthenticated || !hasContext" @click="runSearch">{{ loading ? '检索中...' : '运行检索' }}</button>
      <p v-if="!auth.isAuthenticated" class="mt-3 rounded-2xl bg-amber-50 p-3 text-sm text-amber-800">请先登录后再测试检索。</p>
      <p v-else-if="!hasContext" class="mt-3 rounded-2xl bg-amber-50 p-3 text-sm text-amber-800">请先选择组织和项目。</p>
      <p v-if="error" class="mt-3 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    </div>
    <section v-if="result" class="mt-6 grid gap-4">
      <div class="grid gap-3 md:grid-cols-4">
        <div class="rounded-3xl border bg-lime-50 p-5">
          <div class="text-xs font-bold uppercase tracking-wide text-lime-700">rerank_degraded</div>
          <div class="mt-2 text-2xl font-black text-stone-950">{{ result.rerank_degraded }}</div>
        </div>
        <div class="rounded-3xl border bg-white p-5">
          <div class="text-xs font-bold uppercase tracking-wide text-stone-500">results</div>
          <div class="mt-2 text-2xl font-black text-stone-950">{{ allResults.length }}</div>
        </div>
        <div class="rounded-3xl border bg-white p-5">
          <div class="text-xs font-bold uppercase tracking-wide text-stone-500">marked_used_count</div>
          <div class="mt-2 text-2xl font-black text-stone-950">{{ result.marked_used_count || 0 }}</div>
        </div>
        <div class="rounded-3xl border bg-white p-5">
          <div class="text-xs font-bold uppercase tracking-wide text-stone-500">access_log_count</div>
          <div class="mt-2 text-2xl font-black text-stone-950">{{ result.access_log_count || 0 }}</div>
        </div>
      </div>
      <div class="rounded-3xl border bg-white p-5">
        <div class="flex items-center justify-between gap-3">
          <h3 class="text-lg font-black text-stone-950">压缩上下文</h3>
          <span class="rounded-full bg-stone-100 px-3 py-1 text-xs font-bold text-stone-600">context</span>
        </div>
        <pre class="mt-3 max-h-80 overflow-auto whitespace-pre-wrap rounded-2xl bg-stone-950 p-4 text-sm text-stone-50">{{ result.context || '后端未返回压缩上下文。' }}</pre>
      </div>
      <div v-if="allResults.length === 0" class="rounded-3xl border bg-white p-5 text-stone-600">没有返回结果，请确认当前项目已有归档或热记忆索引。</div>
      <div class="grid gap-4 lg:grid-cols-2">
        <div class="rounded-3xl border bg-white p-5">
          <div class="flex items-center justify-between gap-3">
            <h3 class="text-lg font-black text-stone-950">Hot Memory 结果</h3>
            <span class="rounded-full bg-amber-100 px-3 py-1 text-xs font-bold text-amber-800">{{ hotMemoryResults.length }}</span>
          </div>
          <div v-if="hotMemoryResults.length === 0" class="mt-3 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">本次检索没有命中 Hot Memory。</div>
          <article v-for="(item, index) in hotMemoryResults" :key="resultKey(item, index)" class="mt-3 rounded-2xl border border-amber-100 bg-amber-50 p-4 text-sm">
            <div class="font-bold text-stone-950">{{ resultTitle(item) }}</div>
            <p class="mt-1 text-xs font-bold uppercase tracking-wide text-stone-500">score {{ Number(item.score || 0).toFixed(4) }}</p>
            <p class="mt-2 whitespace-pre-wrap text-stone-700">{{ item.text || '后端未返回文本。' }}</p>
            <pre class="mt-3 overflow-auto rounded-xl bg-white/80 p-3 text-xs text-stone-600">{{ JSON.stringify(item.source, null, 2) }}</pre>
          </article>
        </div>
        <div class="rounded-3xl border bg-white p-5">
          <div class="flex items-center justify-between gap-3">
            <h3 class="text-lg font-black text-stone-950">Archive RAG 结果</h3>
            <span class="rounded-full bg-sky-100 px-3 py-1 text-xs font-bold text-sky-800">{{ archiveRAGResults.length }}</span>
          </div>
          <div v-if="archiveRAGResults.length === 0" class="mt-3 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">本次检索没有命中 Archive RAG。</div>
          <article v-for="(item, index) in archiveRAGResults" :key="resultKey(item, index)" class="mt-3 rounded-2xl border border-sky-100 bg-sky-50 p-4 text-sm">
            <div class="font-bold text-stone-950">{{ resultTitle(item) }}</div>
            <p class="mt-1 text-xs font-bold uppercase tracking-wide text-stone-500">score {{ Number(item.score || 0).toFixed(4) }}</p>
            <p class="mt-2 whitespace-pre-wrap text-stone-700">{{ item.text || '后端未返回文本。' }}</p>
            <pre class="mt-3 overflow-auto rounded-xl bg-white/80 p-3 text-xs text-stone-600">{{ JSON.stringify(item.source, null, 2) }}</pre>
          </article>
        </div>
      </div>
      <div class="rounded-3xl border bg-white p-5">
        <h3 class="text-lg font-black text-stone-950">Source refs</h3>
        <SourceRefList class="mt-3" :refs="sourceRefs" />
      </div>
      <pre class="overflow-auto rounded-3xl border bg-stone-950 p-5 text-sm text-stone-50">{{ JSON.stringify(result, null, 2) }}</pre>
    </section>
  </AppShell>
</template>
