<script setup lang="ts">
const auth = useAuthStore()
const context = useContextStore()
const { hasWorkspaceContext, requestWorkspaceProjects } = useWorkspaceProjectScopes()

type TopicState = {
  id: number
  org_id: string
  project_id: string
  source_key: string
  thread_id: string
  candidate_count: number
  completion_score: number
  last_event_at?: string
  ready_to_compose: boolean
  composed_archive_id: string
  created_at: string
  updated_at: string
}

type TopicListResponse = { topics: TopicState[] }

const loading = ref(false)
const error = ref('')
const topics = ref<TopicState[]>([])
const sourceKeyFilter = ref('')

function formatDate(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

async function loadTopics() {
  if (!auth.isAuthenticated || !hasWorkspaceContext.value) return
  loading.value = true
  error.value = ''
  try {
    const body: Record<string, unknown> = { limit: 50 }
    if (sourceKeyFilter.value) body.source_key = sourceKeyFilter.value
    const items = await requestWorkspaceProjects<TopicListResponse, TopicState>({
      path: '/memory/topics/list',
      buildBody: (project) => ({
        org_id: project.org_id,
        project_id: project.project_id,
        ...body
      }),
      pick: (response) => response.topics || []
    })
    topics.value = items.sort((a, b) => Date.parse(b.updated_at || '') - Date.parse(a.updated_at || ''))
  } catch (err: any) {
    error.value = err.message || '主题列表加载失败'
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  await loadTopics()
})

watch(() => [context.orgId, context.projectId], () => {
  void loadTopics()
})
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <h2 class="text-3xl font-black">主题沉淀</h2>
        <p class="mt-2 text-stone-600">候选记忆按 source_key + thread_id 聚合的主题沉淀状态。</p>
      </div>
      <button class="rounded-2xl border bg-white px-4 py-2 font-bold" :disabled="loading" @click="loadTopics">{{ loading ? '刷新中...' : '刷新' }}</button>
    </div>

    <!-- 筛选 -->
    <section class="mt-6 rounded-3xl border bg-white p-5">
      <div class="grid gap-3 sm:grid-cols-[1fr_auto]">
        <label class="text-sm font-bold text-stone-600">
          source_key
          <input v-model="sourceKeyFilter" class="mt-2 w-full rounded-2xl border p-3 text-sm" placeholder="筛选 source_key" @keyup.enter="loadTopics">
        </label>
        <button class="self-end rounded-2xl bg-stone-950 px-4 py-3 font-bold text-white disabled:cursor-not-allowed disabled:bg-stone-400" :disabled="loading || !hasWorkspaceContext" @click="loadTopics">查询</button>
      </div>
    </section>

    <!-- 主题列表 -->
    <section class="mt-6 rounded-3xl border bg-white p-5">
      <h3 class="text-xl font-black">主题列表（{{ topics.length }}）</h3>
      <div v-if="loading" class="mt-4 rounded-2xl bg-stone-50 p-4 text-stone-600">正在加载主题...</div>
      <div v-else-if="topics.length === 0" class="mt-4 rounded-2xl bg-stone-50 p-4 text-stone-600">当前工作区暂无主题。</div>
      <div v-else class="mt-4 grid gap-4">
        <article v-for="ts in topics" :key="ts.id" class="rounded-3xl border bg-stone-50 p-5">
          <div class="flex flex-col gap-2 xl:flex-row xl:items-start xl:justify-between">
            <div class="flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <span v-if="ts.ready_to_compose" class="rounded-full bg-emerald-100 px-2 py-0.5 text-xs font-bold text-emerald-900">可沉淀</span>
                <span v-if="ts.composed_archive_id" class="rounded-full bg-blue-100 px-2 py-0.5 text-xs font-bold text-blue-900">已沉淀</span>
                <span class="text-sm font-bold text-stone-950">{{ ts.source_key }}</span>
                <span v-if="ts.thread_id" class="text-xs text-stone-500">· {{ ts.thread_id }}</span>
              </div>
              <p class="mt-2 text-sm text-stone-600">
                候选 {{ ts.candidate_count }} 条 · 完成分 {{ ts.completion_score.toFixed(2) }}
              </p>
              <p class="mt-1 text-xs text-stone-500">
                最后事件 {{ formatDate(ts.last_event_at) }} · 更新于 {{ formatDate(ts.updated_at) }}
              </p>
              <p v-if="ts.composed_archive_id" class="mt-1 break-all text-xs text-stone-500">
                归档: {{ ts.composed_archive_id }}
              </p>
            </div>
          </div>
        </article>
      </div>
    </section>
  </AppShell>
</template>
