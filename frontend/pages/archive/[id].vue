<script setup lang="ts">
const route = useRoute()
const auth = useAuthStore()
const { request } = useApi()

type ArchiveMetadata = {
  archive_id: string
  user_id: string
  org_id: string
  project_id: string
  title: string
  file_path: string
  status: string
  index_generation: number
  current_version: number
  content_hash: string
  created_at: string
  updated_at: string
}

type ArchiveVersion = {
  archive_id: string
  version: number
  file_path: string
  content_hash: string
  editor_user_id: string
  reason: string
  created_at: string
}

type ArchiveDetailResponse = { metadata: ArchiveMetadata, content: string }
type ArchiveVersionsResponse = { archive_id: string, versions: ArchiveVersion[] }
type ArchiveIndexRetryResponse = { archive_id: string, index_generation: number, retried_jobs: number }
type ArchiveIndexStatusResponse = {
  archive_id: string
  index_generation: number
  jobs_by_status: Record<string, number>
  chunks_by_status: Record<string, number>
  points_by_status: Record<string, number>
  index_jobs: ArchiveIndexJobStatus[]
  archive_chunks: ArchiveChunkStatus[]
  latest_point_at?: string
}
type ArchiveIndexJobStatus = {
  idempotency_key: string
  status: string
  error_message: string
  attempts: number
  max_attempts: number
  locked_by?: string
  locked_until?: string
  completed_at?: string
  created_at: string
  updated_at: string
}
type ArchiveChunkStatus = {
  chunk_id: string
  chunk_index: number
  vector_status: string
  content_hash: string
  heading_path: string[]
  source_event_ids: string[]
  qdrant_point_id?: string
  qdrant_vector_status?: string
  qdrant_updated_at?: string
}
type AuditLog = {
  id: string
  actor_user_id: string
  org_id: string
  project_id: string
  action: string
  resource_type: string
  resource_id: string
  request_id: string
  result: string
  metadata?: Record<string, unknown>
  created_at: string
}
type AuditListResponse = { audit_logs: AuditLog[] }

const archiveID = computed(() => String(route.params.id || ''))
const loading = ref(false)
const saving = ref(false)
const reindexing = ref(false)
const retryingIndexJobs = ref(false)
const deleting = ref(false)
const auditLoading = ref(false)
const indexStatusLoading = ref(false)
const error = ref('')
const auditError = ref('')
const indexStatusError = ref('')
const notice = ref('')
const metadata = ref<ArchiveMetadata | null>(null)
const content = ref('')
const versions = ref<ArchiveVersion[]>([])
const auditLogs = ref<AuditLog[]>([])
const indexStatus = ref<ArchiveIndexStatusResponse | null>(null)
const editReason = ref('管理台手动修订')
const deleteReason = ref('管理台软删除')
const reindexReason = ref('管理台手动重建索引')

const failedIndexJobs = computed(() => indexStatus.value?.index_jobs?.filter((job) => job.status === 'failed') || [])

const indexStatusRows = computed(() => {
  const status = indexStatus.value
  if (!status) return []
  return [
    { label: '索引任务', values: status.jobs_by_status || {} },
    { label: 'Archive Chunk', values: status.chunks_by_status || {} },
    { label: 'Qdrant Point', values: status.points_by_status || {} }
  ].map((group) => ({
    label: group.label,
    entries: Object.entries(group.values).sort(([a], [b]) => a.localeCompare(b))
  }))
})

function formatDate(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

function newRequestID(action: string) {
  return `${action}_${archiveID.value}_${Date.now()}`
}

function statusText(status: string) {
  const labels: Record<string, string> = { active: '活跃', deleted: '已删除' }
  return labels[status] || status
}

async function loadDetail() {
  if (!auth.isAuthenticated || !archiveID.value) return
  loading.value = true
  error.value = ''
  try {
    const [detail, versionResponse] = await Promise.all([
      request<ArchiveDetailResponse>('/memory/archive/detail', {
        method: 'POST',
        body: { archive_id: archiveID.value }
      }),
      request<ArchiveVersionsResponse>('/memory/archive/versions', {
        method: 'POST',
        body: { archive_id: archiveID.value }
      })
    ])
    metadata.value = detail.metadata
    content.value = detail.content || ''
    versions.value = versionResponse.versions || []
    await Promise.all([loadAuditLogs(detail.metadata), loadIndexStatus()])
  } catch (err: any) {
    error.value = err.message || 'Archive 详情加载失败'
  } finally {
    loading.value = false
  }
}

async function loadIndexStatus() {
  if (!auth.isAuthenticated || !archiveID.value) return
  indexStatusLoading.value = true
  indexStatusError.value = ''
  try {
    indexStatus.value = await request<ArchiveIndexStatusResponse>('/memory/archive/index-status', {
      method: 'POST',
      body: { archive_id: archiveID.value }
    })
  } catch (err: any) {
    indexStatus.value = null
    indexStatusError.value = err.message || 'RAG 索引状态加载失败'
  } finally {
    indexStatusLoading.value = false
  }
}

async function loadAuditLogs(scope = metadata.value) {
  if (!auth.isAuthenticated || !scope) return
  auditLoading.value = true
  auditError.value = ''
  try {
    const response = await request<AuditListResponse>('/memory/audit/list', {
      method: 'POST',
      body: {
        org_id: scope.org_id,
        project_id: scope.project_id,
        resource_type: 'archive',
        resource_id: archiveID.value,
        limit: 50
      }
    })
    auditLogs.value = response.audit_logs || []
  } catch (err: any) {
    auditError.value = err.message || '审计日志加载失败'
  } finally {
    auditLoading.value = false
  }
}

async function saveArchive() {
  if (!content.value.trim()) {
    error.value = 'Archive 正文不能为空'
    return
  }
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    await request<ArchiveMetadata>('/memory/archive/edit', {
      method: 'POST',
      body: {
        request_id: newRequestID('archive_edit'),
        archive_id: archiveID.value,
        reason: editReason.value,
        content: content.value
      }
    })
    notice.value = 'Archive 已保存，并生成新的版本和索引代次。'
    await loadDetail()
  } catch (err: any) {
    error.value = err.message || 'Archive 保存失败'
  } finally {
    saving.value = false
  }
}

async function reindexArchive() {
  reindexing.value = true
  error.value = ''
  notice.value = ''
  try {
    const response = await request<ArchiveMetadata & { chunks?: number }>('/memory/archive/reindex', {
      method: 'POST',
      body: {
        request_id: newRequestID('archive_reindex'),
        archive_id: archiveID.value,
        reason: reindexReason.value
      }
    })
    notice.value = `已提交重建索引，切分 chunk 数：${response.chunks ?? 0}。`
    await loadDetail()
  } catch (err: any) {
    error.value = err.message || 'Archive 重建索引失败'
  } finally {
    reindexing.value = false
  }
}

async function retryIndexJobs() {
  if (!archiveID.value || failedIndexJobs.value.length === 0) return
  retryingIndexJobs.value = true
  error.value = ''
  notice.value = ''
  try {
    const response = await request<ArchiveIndexRetryResponse>('/memory/archive/index-retry', {
      method: 'POST',
      body: { archive_id: archiveID.value }
    })
    notice.value = `已重试失败索引任务：${response.retried_jobs} 个，索引代次 ${response.index_generation}。`
    await loadIndexStatus()
  } catch (err: any) {
    error.value = err.message || '失败索引任务重试失败'
  } finally {
    retryingIndexJobs.value = false
  }
}

async function deleteArchive() {
  deleting.value = true
  error.value = ''
  notice.value = ''
  try {
    await request<ArchiveMetadata>('/memory/archive/delete', {
      method: 'POST',
      body: {
        request_id: newRequestID('archive_delete'),
        archive_id: archiveID.value,
        reason: deleteReason.value
      }
    })
    notice.value = 'Archive 已软删除，版本和审计记录仍保留。'
    await loadDetail()
  } catch (err: any) {
    error.value = err.message || 'Archive 删除失败'
  } finally {
    deleting.value = false
  }
}

onMounted(async () => {
  auth.initFromStorage()
  await loadDetail()
})
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <p class="break-all text-sm font-bold text-stone-500">{{ archiveID }}</p>
        <h2 class="text-3xl font-black">Archive 编辑 / 版本 / 重索引</h2>
        <p v-if="metadata" class="mt-2 text-stone-600">
          {{ metadata.title || metadata.archive_id }} · {{ statusText(metadata.status) }} · 版本 {{ metadata.current_version }} · 索引代次 {{ metadata.index_generation }}
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <NuxtLink to="/archive" class="rounded-2xl border bg-white px-4 py-2 font-bold">返回归档库</NuxtLink>
        <button class="rounded-2xl border bg-white px-4 py-2 font-bold" :disabled="loading" @click="loadDetail">{{ loading ? '刷新中...' : '刷新' }}</button>
      </div>
    </div>

    <p v-if="!auth.isAuthenticated" class="mt-6 rounded-2xl bg-amber-50 p-4 text-sm text-amber-800">请先登录后治理 Archive。</p>
    <p v-if="error" class="mt-6 rounded-2xl bg-red-50 p-4 text-sm text-red-700">{{ error }}</p>
    <p v-if="notice" class="mt-6 rounded-2xl bg-lime-50 p-4 text-sm text-lime-800">{{ notice }}</p>

    <div v-if="loading" class="mt-6 rounded-3xl bg-white p-6 text-stone-600">正在加载 Archive 详情...</div>
    <template v-else-if="metadata">
      <section class="mt-6 rounded-3xl border bg-white p-5">
        <div class="grid gap-3 text-sm text-stone-600 lg:grid-cols-2">
          <p class="break-all"><strong class="text-stone-950">文件：</strong>{{ metadata.file_path }}</p>
          <p><strong class="text-stone-950">状态：</strong>{{ statusText(metadata.status) }}</p>
          <p><strong class="text-stone-950">创建：</strong>{{ formatDate(metadata.created_at) }}</p>
          <p><strong class="text-stone-950">更新：</strong>{{ formatDate(metadata.updated_at) }}</p>
          <p class="break-all lg:col-span-2"><strong class="text-stone-950">内容 hash：</strong>{{ metadata.content_hash }}</p>
        </div>
      </section>

      <section class="mt-6 rounded-3xl border bg-white p-5">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <h3 class="text-xl font-black">Markdown 正文</h3>
          <label class="text-sm font-bold text-stone-600">
            编辑原因
            <input v-model="editReason" class="ml-2 rounded-2xl border px-3 py-2 font-normal text-stone-950">
          </label>
        </div>
        <textarea v-model="content" class="mt-4 min-h-96 w-full rounded-3xl border bg-stone-50 p-4 font-mono text-sm" />
        <button class="mt-4 rounded-2xl bg-stone-950 px-4 py-3 font-bold text-white disabled:cursor-not-allowed disabled:bg-stone-400" :disabled="saving || metadata.status === 'deleted' || !content.trim()" @click="saveArchive">
          {{ saving ? '保存中...' : '保存并生成新版本' }}
        </button>
      </section>

      <section class="mt-6 grid gap-4 lg:grid-cols-2">
        <div class="rounded-3xl border bg-white p-5">
          <h3 class="text-xl font-black">重建索引</h3>
          <p class="mt-2 text-sm text-stone-600">调用 <code>/memory/archive/reindex</code>，后端会提升 index_generation 并入队 RAG 索引任务。</p>
          <input v-model="reindexReason" class="mt-4 w-full rounded-2xl border px-3 py-2 text-sm">
          <button class="mt-3 rounded-2xl bg-orange-100 px-4 py-3 font-bold text-orange-950 disabled:cursor-not-allowed disabled:bg-stone-100 disabled:text-stone-500" :disabled="reindexing || metadata.status === 'deleted'" @click="reindexArchive">
            {{ reindexing ? '提交中...' : '触发重建索引' }}
          </button>
        </div>
        <div class="rounded-3xl border bg-white p-5">
          <h3 class="text-xl font-black">软删除</h3>
          <p class="mt-2 text-sm text-stone-600">只调用软删除接口，保留 Markdown 文件、版本和审计记录。</p>
          <input v-model="deleteReason" class="mt-4 w-full rounded-2xl border px-3 py-2 text-sm">
          <button class="mt-3 rounded-2xl bg-red-100 px-4 py-3 font-bold text-red-950 disabled:cursor-not-allowed disabled:bg-stone-100 disabled:text-stone-500" :disabled="deleting || metadata.status === 'deleted'" @click="deleteArchive">
            {{ deleting ? '删除中...' : '软删除 Archive' }}
          </button>
        </div>
      </section>

      <section class="mt-6 rounded-3xl border bg-white p-5">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h3 class="text-xl font-black">RAG 索引状态</h3>
            <p class="mt-2 text-sm text-stone-600">调用 <code>/memory/archive/index-status</code>，只展示当前 Archive 当前索引代次的任务、chunk 和 Qdrant point 状态。</p>
          </div>
          <div class="flex flex-wrap gap-2">
            <button class="rounded-2xl border bg-white px-4 py-2 font-bold" :disabled="indexStatusLoading" @click="loadIndexStatus">{{ indexStatusLoading ? '刷新中...' : '刷新索引状态' }}</button>
            <button class="rounded-2xl bg-red-100 px-4 py-2 font-bold text-red-950 disabled:cursor-not-allowed disabled:bg-stone-100 disabled:text-stone-500" :disabled="retryingIndexJobs || failedIndexJobs.length === 0 || metadata.status === 'deleted'" @click="retryIndexJobs">
              {{ retryingIndexJobs ? '重试中...' : `重试失败索引任务（${failedIndexJobs.length}）` }}
            </button>
          </div>
        </div>
        <p v-if="indexStatusError" class="mt-4 rounded-2xl bg-red-50 p-4 text-sm text-red-700">{{ indexStatusError }}</p>
        <div v-if="indexStatusLoading && !indexStatus" class="mt-4 rounded-2xl bg-stone-50 p-4 text-stone-600">正在加载 RAG 索引状态...</div>
        <div v-else-if="indexStatus" class="mt-4 grid gap-3 lg:grid-cols-3">
          <article v-for="row in indexStatusRows" :key="row.label" class="rounded-2xl border bg-stone-50 p-4">
            <p class="font-black">{{ row.label }}</p>
            <div v-if="row.entries.length" class="mt-3 space-y-2">
              <div v-for="[name, count] in row.entries" :key="`${row.label}-${name}`" class="flex items-center justify-between rounded-xl bg-white px-3 py-2 text-sm">
                <span class="font-bold">{{ name }}</span>
                <span>{{ count }}</span>
              </div>
            </div>
            <p v-else class="mt-3 text-sm text-stone-600">暂无记录。</p>
          </article>
          <p class="text-xs text-stone-500 lg:col-span-3">
            Archive {{ indexStatus.archive_id }} · 索引代次 {{ indexStatus.index_generation }} · 最近 point 更新时间 {{ formatDate(indexStatus.latest_point_at) }}
          </p>
        </div>
        <p v-else class="mt-4 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">暂无索引状态。创建或重建索引后，worker 完成处理时这里会更新。</p>

        <div v-if="indexStatus" class="mt-5 grid gap-4 lg:grid-cols-2">
          <div class="rounded-2xl border bg-stone-50 p-4">
            <h4 class="font-black">索引任务明细</h4>
            <div v-if="indexStatus.index_jobs.length" class="mt-3 space-y-3">
              <article v-for="job in indexStatus.index_jobs" :key="job.idempotency_key" class="rounded-2xl bg-white p-3 text-sm">
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <p class="break-all font-bold">{{ job.idempotency_key }}</p>
                  <span class="rounded-full bg-stone-100 px-3 py-1 text-xs font-bold">{{ job.status }}</span>
                </div>
                <p class="mt-2 text-stone-600">尝试次数：{{ job.attempts }} / {{ job.max_attempts }}</p>
                <p v-if="job.error_message" class="mt-2 break-all rounded-xl bg-red-50 p-3 text-red-700">失败原因：{{ job.error_message }}</p>
                <p class="mt-2 text-xs text-stone-500">更新：{{ formatDate(job.updated_at) }} · 完成：{{ formatDate(job.completed_at) }}</p>
              </article>
            </div>
            <p v-else class="mt-3 text-sm text-stone-600">当前索引代次暂无任务明细。</p>
          </div>

          <div class="rounded-2xl border bg-stone-50 p-4">
            <h4 class="font-black">Chunk 明细</h4>
            <div v-if="indexStatus.archive_chunks.length" class="mt-3 max-h-96 space-y-3 overflow-auto pr-1">
              <article v-for="chunk in indexStatus.archive_chunks" :key="chunk.chunk_id" class="rounded-2xl bg-white p-3 text-sm">
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <p class="break-all font-bold">#{{ chunk.chunk_index }} · {{ chunk.chunk_id }}</p>
                  <span class="rounded-full bg-stone-100 px-3 py-1 text-xs font-bold">{{ chunk.vector_status }}</span>
                </div>
                <p class="mt-2 break-all text-stone-600">内容 hash：{{ chunk.content_hash }}</p>
                <p class="mt-2 break-all text-stone-600">Qdrant：{{ chunk.qdrant_point_id || '未写入 point' }} · {{ chunk.qdrant_vector_status || '无状态' }}</p>
                <p v-if="chunk.heading_path?.length" class="mt-2 text-xs text-stone-500">标题路径：{{ chunk.heading_path.join(' / ') }}</p>
              </article>
            </div>
            <p v-else class="mt-3 text-sm text-stone-600">当前索引代次暂无 chunk 明细。</p>
          </div>
        </div>
      </section>

      <section class="mt-6 rounded-3xl border bg-white p-5">
        <h3 class="text-xl font-black">版本历史</h3>
        <div v-if="versions.length === 0" class="mt-4 rounded-2xl bg-stone-50 p-4 text-stone-600">暂无版本记录。</div>
        <div v-else class="mt-4 grid gap-3">
          <article v-for="version in versions" :key="`${version.archive_id}-${version.version}`" class="rounded-2xl border bg-stone-50 p-4">
            <p class="font-black">版本 {{ version.version }} · {{ version.reason || '未填写原因' }}</p>
            <p class="mt-1 break-all text-sm text-stone-600">hash {{ version.content_hash }}</p>
            <p class="mt-1 text-xs text-stone-500">编辑人 {{ version.editor_user_id || '-' }} · {{ formatDate(version.created_at) }}</p>
          </article>
        </div>
      </section>

      <section class="mt-6 rounded-3xl border bg-white p-5">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h3 class="text-xl font-black">审计日志</h3>
            <p class="mt-2 text-sm text-stone-600">调用 <code>/memory/audit/list</code>，按 <code>resource_type: 'archive'</code> 和当前 Archive ID 过滤。</p>
          </div>
          <button class="rounded-2xl border bg-white px-4 py-2 font-bold" :disabled="auditLoading" @click="loadAuditLogs()">{{ auditLoading ? '刷新中...' : '刷新审计' }}</button>
        </div>
        <p v-if="auditError" class="mt-4 rounded-2xl bg-red-50 p-4 text-sm text-red-700">{{ auditError }}</p>
        <div v-if="auditLoading" class="mt-4 rounded-2xl bg-stone-50 p-4 text-stone-600">正在加载审计日志...</div>
        <div v-else-if="auditLogs.length === 0" class="mt-4 rounded-2xl bg-stone-50 p-4 text-stone-600">当前 Archive 暂无通用审计日志；版本历史仍保留编辑与删除审计线索。</div>
        <div v-else class="mt-4 grid gap-3">
          <article v-for="log in auditLogs" :key="log.id" class="rounded-2xl border bg-stone-50 p-4">
            <p class="font-black">{{ log.action }} · {{ log.result }}</p>
            <p class="mt-1 break-all text-sm text-stone-600">{{ log.resource_type }} / {{ log.resource_id }} · request {{ log.request_id || '-' }}</p>
            <p class="mt-1 text-xs text-stone-500">操作者 {{ log.actor_user_id || '-' }} · {{ formatDate(log.created_at) }}</p>
          </article>
        </div>
      </section>
    </template>
  </AppShell>
</template>
