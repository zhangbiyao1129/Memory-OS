<script setup lang="ts">
import { explainSearchHit } from '~/utils/memoryUx'

const { request } = useApi()
const auth = useAuthStore()
const context = useContextStore()
const query = ref('deploy API')
const loading = ref(false)
const markingUsed = ref('')
const error = ref('')
const success = ref('')
const result = ref<any>(null)

const hasContext = computed(() => Boolean(context.orgId && context.projectId))
const allResults = computed(() => result.value?.results || [])
const sourceRefs = computed(() => allResults.value.map((item: any) => item.source).filter(Boolean))

function resultKey(item: any, index: number) {
  const source = item?.source || {}
  return source.memory_id || source.chunk_id || source.archive_id || item?.text || `result_${index}`
}

function resultTitle(item: any) {
  return explainSearchHit(item?.source)
}

function canMarkUsed(item: any) {
  return item?.source?.kind === 'hot_memory' && Boolean(item?.source?.memory_id)
}

onMounted(() => {
  auth.initFromStorage()
  context.initFromStorage()
})

async function runSearch() {
  auth.initFromStorage()
  context.initFromStorage()
  if (!auth.isAuthenticated) {
    error.value = '请先登录，再执行检索。'
    return
  }
  if (!hasContext.value) {
    error.value = '请先选择组织和项目。'
    return
  }

  loading.value = true
  error.value = ''
  success.value = ''
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

async function markUsed(item: any) {
  const memoryID = item?.source?.memory_id
  if (!memoryID) return
  markingUsed.value = memoryID
  error.value = ''
  success.value = ''
  try {
    await request('/memory/hot-memory/mark-used', {
      method: 'POST',
      body: { memory_id: memoryID }
    })
    success.value = '已标记为有用。'
  } catch (err: any) {
    error.value = err.message || '标记有用失败'
  } finally {
    markingUsed.value = ''
  }
}
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <p class="text-xs font-black uppercase tracking-[0.35em] text-orange-800">Search</p>
        <h2 class="mt-2 text-3xl font-black">检索记忆</h2>
        <p class="mt-2 max-w-3xl text-stone-600">输入问题，查看 Memory OS 找到的相关记忆和可注入上下文。</p>
      </div>
      <NuxtLink class="rounded-2xl border bg-white px-4 py-2 font-bold" to="/diagnostics">写入诊断</NuxtLink>
    </div>

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
      <textarea v-model="query" class="min-h-28 w-full rounded-2xl border p-4" placeholder="输入你想找回的项目事实、决策或上下文" />
      <button class="mt-3 rounded-2xl bg-stone-950 px-4 py-2 font-bold text-white disabled:cursor-not-allowed disabled:bg-stone-400" :disabled="loading || !auth.isAuthenticated || !hasContext" @click="runSearch">{{ loading ? '检索中...' : '运行检索' }}</button>
      <p v-if="!auth.isAuthenticated" class="mt-3 rounded-2xl bg-amber-50 p-3 text-sm text-amber-800">请先登录后再检索。</p>
      <p v-else-if="!hasContext" class="mt-3 rounded-2xl bg-amber-50 p-3 text-sm text-amber-800">请先选择组织和项目。</p>
      <p v-if="error" class="mt-3 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
      <p v-if="success" class="mt-3 rounded-2xl bg-emerald-50 p-3 text-sm text-emerald-800">{{ success }}</p>
    </div>

    <section v-if="result" class="mt-6 grid gap-4">
      <div class="rounded-3xl border bg-white p-5">
        <div class="flex items-center justify-between gap-3">
          <h3 class="text-lg font-black text-stone-950">可注入上下文</h3>
          <span class="rounded-full bg-stone-100 px-3 py-1 text-xs font-bold text-stone-600">{{ allResults.length }} 条命中</span>
        </div>
        <pre class="mt-3 max-h-80 overflow-auto whitespace-pre-wrap rounded-2xl bg-stone-950 p-4 text-sm text-stone-50">{{ result.context || '后端未返回压缩上下文。' }}</pre>
      </div>

      <div v-if="allResults.length === 0" class="rounded-3xl border bg-white p-5 text-stone-600">
        没有返回结果。请到写入诊断确认当前项目是否已有候选、热记忆或归档。
      </div>

      <div v-else class="grid gap-4">
        <article v-for="(item, index) in allResults" :key="resultKey(item, index)" class="rounded-3xl border bg-white p-5 text-sm">
          <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <div class="flex flex-wrap items-center gap-2">
                <span class="rounded-full bg-orange-100 px-3 py-1 text-xs font-black text-orange-900">{{ resultTitle(item) }}</span>
                <span class="rounded-full bg-stone-100 px-3 py-1 text-xs font-bold text-stone-600">score {{ Number(item.score || 0).toFixed(4) }}</span>
              </div>
              <p class="mt-3 whitespace-pre-wrap text-stone-800">{{ item.text || '后端未返回文本。' }}</p>
            </div>
            <button v-if="canMarkUsed(item)" class="rounded-2xl bg-emerald-100 px-3 py-2 text-xs font-black text-emerald-900 disabled:cursor-not-allowed disabled:bg-stone-100 disabled:text-stone-500" :disabled="markingUsed === item.source.memory_id" @click="markUsed(item)">
              {{ markingUsed === item.source.memory_id ? '标记中...' : '标记有用' }}
            </button>
          </div>
        </article>
      </div>

      <details class="rounded-3xl border bg-white p-5">
        <summary class="cursor-pointer text-lg font-black text-stone-950">调试详情</summary>
        <div class="mt-4 grid gap-3 md:grid-cols-4">
          <div class="rounded-2xl bg-stone-50 p-4 text-sm">
            <div class="font-bold text-stone-500">rerank_degraded</div>
            <div class="mt-1 text-xl font-black">{{ result.rerank_degraded }}</div>
          </div>
          <div class="rounded-2xl bg-stone-50 p-4 text-sm">
            <div class="font-bold text-stone-500">results</div>
            <div class="mt-1 text-xl font-black">{{ allResults.length }}</div>
          </div>
          <div class="rounded-2xl bg-stone-50 p-4 text-sm">
            <div class="font-bold text-stone-500">marked_used_count</div>
            <div class="mt-1 text-xl font-black">{{ result.marked_used_count || 0 }}</div>
          </div>
          <div class="rounded-2xl bg-stone-50 p-4 text-sm">
            <div class="font-bold text-stone-500">access_log_count</div>
            <div class="mt-1 text-xl font-black">{{ result.access_log_count || 0 }}</div>
          </div>
        </div>
        <h4 class="mt-5 font-black">Source refs</h4>
        <SourceRefList class="mt-3" :refs="sourceRefs" />
        <pre class="mt-4 overflow-auto rounded-2xl bg-stone-950 p-5 text-sm text-stone-50">{{ JSON.stringify(result, null, 2) }}</pre>
      </details>
    </section>
  </AppShell>
</template>
