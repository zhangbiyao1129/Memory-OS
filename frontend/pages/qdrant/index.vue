<script setup lang="ts">
const auth = useAuthStore()
const { request } = useApi()

type QdrantStatus = {
  collection_name: string
  collection_status: string
  points_count: number
  vectors_count: number
  indexed_vectors_count: number
  segments_count: number
  vector_size: number
  distance: string
  payload_schema: Record<string, boolean>
  points_by_status: Record<string, number>
  archive_points_by_status: Record<string, number>
  hot_memory_points_by_status: Record<string, number>
  index_jobs_by_status: Record<string, number>
  query_time_filter_enforced: boolean
  required_payload_fields: string[]
  missing_required_payload_fields: string[]
  latest_point_at?: string
}

const loading = ref(false)
const error = ref('')
const status = ref<QdrantStatus | null>(null)

const payloadFields = computed(() => status.value ? Object.keys(status.value.payload_schema || {}).sort() : [])
const missingPayloadFields = computed(() => status.value?.missing_required_payload_fields || [])
const pointRows = computed(() => Object.entries(status.value?.points_by_status || {}).sort(([a], [b]) => a.localeCompare(b)))
const archivePointRows = computed(() => Object.entries(status.value?.archive_points_by_status || {}).sort(([a], [b]) => a.localeCompare(b)))
const hotMemoryPointRows = computed(() => Object.entries(status.value?.hot_memory_points_by_status || {}).sort(([a], [b]) => a.localeCompare(b)))
const jobRows = computed(() => Object.entries(status.value?.index_jobs_by_status || {}).sort(([a], [b]) => a.localeCompare(b)))

async function loadStatus() {
  if (!auth.isAuthenticated) {
    status.value = null
    error.value = '请先登录后查看 Qdrant 状态'
    return
  }
  loading.value = true
  error.value = ''
  try {
    status.value = await request<QdrantStatus>('/memory/qdrant/status', { method: 'POST' })
  } catch (err: any) {
    error.value = err.message || 'Qdrant 状态加载失败'
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  auth.initFromStorage()
  await loadStatus()
})
</script>

<template>
  <AppShell>
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div>
        <h2 class="text-3xl font-black">索引诊断</h2>
        <p class="mt-2 text-stone-600">读取真实 Qdrant collection 与 PostgreSQL 索引任务统计，不再展示静态假数据。</p>
      </div>
      <button class="rounded-2xl border px-4 py-2 font-bold" :disabled="loading" @click="loadStatus">
        {{ loading ? '刷新中...' : '刷新' }}
      </button>
    </div>

    <p v-if="error" class="mt-4 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <p v-if="loading && !status" class="mt-6 rounded-3xl border bg-white p-5 text-stone-600">正在读取 Qdrant 状态...</p>

    <div v-if="status" class="mt-6 grid gap-4 md:grid-cols-3">
      <section class="rounded-3xl border bg-white p-5">
        <p class="text-sm text-stone-500">Collection</p>
        <h3 class="mt-2 break-all font-black">{{ status.collection_name }}</h3>
        <p class="mt-1 text-sm">状态：{{ status.collection_status || 'unknown' }}</p>
      </section>
      <section class="rounded-3xl border bg-white p-5">
        <p class="text-sm text-stone-500">Point / Vector</p>
        <h3 class="mt-2 font-black">{{ status.points_count }} points</h3>
        <p class="mt-1 text-sm">{{ status.indexed_vectors_count }} / {{ status.vectors_count }} vectors indexed</p>
      </section>
      <section class="rounded-3xl border bg-white p-5">
        <p class="text-sm text-stone-500">Vector Config</p>
        <h3 class="mt-2 font-black">{{ status.vector_size || 'unknown' }} · {{ status.distance || 'unknown' }}</h3>
        <p class="mt-1 text-sm">segments: {{ status.segments_count }}</p>
      </section>
    </div>

    <div v-if="status" class="mt-4 grid gap-4 lg:grid-cols-2">
      <section class="rounded-3xl border bg-white p-5">
        <div class="flex items-center justify-between gap-3">
          <h3 class="font-black">Query-Time Filter</h3>
          <span class="rounded-full px-3 py-1 text-xs font-bold" :class="status.query_time_filter_enforced ? 'bg-emerald-100 text-emerald-800' : 'bg-red-100 text-red-800'">
            {{ status.query_time_filter_enforced ? '已强制' : '未确认' }}
          </span>
        </div>
        <p class="mt-3 text-sm text-stone-600">必需 payload 字段：</p>
        <div class="mt-3 flex flex-wrap gap-2">
          <span v-for="field in status.required_payload_fields" :key="field" class="rounded-full bg-stone-100 px-3 py-1 text-xs font-bold text-stone-700">{{ field }}</span>
        </div>
        <div v-if="missingPayloadFields.length" class="mt-4 rounded-2xl bg-red-50 p-4 text-sm text-red-700">
          缺失字段：{{ missingPayloadFields.join('、') }}
        </div>
        <p class="mt-4 text-sm text-stone-600">当前 collection payload schema：</p>
        <div v-if="payloadFields.length" class="mt-3 flex flex-wrap gap-2">
          <span v-for="field in payloadFields" :key="field" class="rounded-full bg-lime-100 px-3 py-1 text-xs font-bold text-lime-800">{{ field }}</span>
        </div>
        <p v-else class="mt-3 rounded-2xl bg-amber-50 p-3 text-sm text-amber-800">Qdrant 尚未返回 payload schema，需在最终验收中确认索引已写入。</p>
      </section>

      <section class="rounded-3xl border bg-white p-5">
        <h3 class="font-black">索引任务</h3>
        <div v-if="jobRows.length" class="mt-3 space-y-2">
          <div v-for="[name, count] in jobRows" :key="name" class="flex items-center justify-between rounded-2xl bg-stone-50 px-4 py-3 text-sm">
            <span class="font-bold">{{ name }}</span>
            <span>{{ count }}</span>
          </div>
        </div>
        <p v-else class="mt-3 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">暂无 Archive index job 记录。</p>
        <p v-if="status.latest_point_at" class="mt-4 text-xs text-stone-500">最近 point 更新时间：{{ status.latest_point_at }}</p>
      </section>

      <section class="rounded-3xl border bg-white p-5 lg:col-span-2">
        <h3 class="font-black">Qdrant Point 状态汇总</h3>
        <div v-if="pointRows.length" class="mt-3 grid gap-2 md:grid-cols-3">
          <div v-for="[name, count] in pointRows" :key="name" class="rounded-2xl bg-stone-50 px-4 py-3 text-sm">
            <p class="font-bold">{{ name }}</p>
            <p class="mt-1 text-2xl font-black">{{ count }}</p>
          </div>
        </div>
        <p v-else class="mt-3 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">暂无向量点跟踪记录。写入 Archive 或 Hot Memory 并完成索引后这里会显示 indexed/pending 等状态。</p>
      </section>

      <section class="rounded-3xl border bg-white p-5">
        <h3 class="font-black">Archive Points</h3>
        <p class="mt-2 text-sm text-stone-600">来自 PostgreSQL `qdrant_points`，对应 Archive RAG chunk。</p>
        <div v-if="archivePointRows.length" class="mt-3 space-y-2">
          <div v-for="[name, count] in archivePointRows" :key="name" class="flex items-center justify-between rounded-2xl bg-stone-50 px-4 py-3 text-sm">
            <span class="font-bold">{{ name }}</span>
            <span>{{ count }}</span>
          </div>
        </div>
        <p v-else class="mt-3 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">暂无 Archive RAG point 跟踪记录。</p>
      </section>

      <section class="rounded-3xl border bg-white p-5">
        <h3 class="font-black">Hot Memory Points</h3>
        <p class="mt-2 text-sm text-stone-600">来自 PostgreSQL `hot_memory_qdrant_points`，对应 Hot Memory 向量索引。</p>
        <div v-if="hotMemoryPointRows.length" class="mt-3 space-y-2">
          <div v-for="[name, count] in hotMemoryPointRows" :key="name" class="flex items-center justify-between rounded-2xl bg-stone-50 px-4 py-3 text-sm">
            <span class="font-bold">{{ name }}</span>
            <span>{{ count }}</span>
          </div>
        </div>
        <p v-else class="mt-3 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">暂无 Hot Memory point 跟踪记录。</p>
      </section>
    </div>
  </AppShell>
</template>
