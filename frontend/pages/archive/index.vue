<script setup lang="ts">
const auth = useAuthStore()
const context = useContextStore()
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

type ArchiveListResponse = { archives: ArchiveMetadata[] }

const loading = ref(false)
const creating = ref(false)
const error = ref('')
const notice = ref('')
const archives = ref<ArchiveMetadata[]>([])
const statusFilter = ref<'active' | 'deleted'>('active')
const newTitle = ref('管理台手动归档')
const newContent = ref('记录这次工作中的关键决策、上下文和后续动作。')

const hasProjectContext = computed(() => Boolean(context.orgId && context.projectId))

function safeID(prefix: string) {
  const random = globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(16).slice(2)}`
  return `${prefix}_${random.replace(/[^a-zA-Z0-9_-]/g, '_')}`
}

function formatDate(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

function statusText(status: string) {
  const labels: Record<string, string> = { active: '活跃', deleted: '已删除' }
  return labels[status] || status
}

async function loadArchives() {
  if (!auth.isAuthenticated || !hasProjectContext.value) return
  loading.value = true
  error.value = ''
  try {
    const response = await request<ArchiveListResponse>('/memory/archive/list', {
      method: 'POST',
      body: {
        org_id: context.orgId,
        project_id: context.projectId,
        status: statusFilter.value,
        limit: 50
      }
    })
    archives.value = response.archives || []
  } catch (err: any) {
    error.value = err.message || 'Archive 加载失败'
  } finally {
    loading.value = false
  }
}

async function createArchive() {
  if (!hasProjectContext.value) {
    error.value = '请先选择组织和项目'
    return
  }
  if (!newTitle.value.trim() || !newContent.value.trim()) {
    error.value = '标题和正文不能为空'
    return
  }
  creating.value = true
  error.value = ''
  notice.value = ''
  const now = new Date().toISOString()
  const archiveID = safeID('archive_manual')
  const eventID = safeID('event_manual')
  try {
    const created = await request<ArchiveMetadata>('/memory/archive/create', {
      method: 'POST',
      body: {
        request_id: safeID('archive_create'),
        archive_id: archiveID,
        title: newTitle.value.trim(),
        org_id: context.orgId,
        project_id: context.projectId,
        created_at: now,
        events: [{
          version: 'v1',
          event_id: eventID,
          turn_id: safeID('turn_manual'),
          thread_id: safeID('thread_manual'),
          session_id: safeID('session_manual'),
          type: 'manual_archive_request',
          created_at: now,
          actor: {
            user_id: '',
            org_id: context.orgId,
            project_id: context.projectId,
            agent_id: context.agentId
          },
          source: { platform: 'memory-os-web' },
          payload: { text: newContent.value.trim() }
        }]
      }
    })
    notice.value = `Archive 已创建：${created.archive_id}`
    newTitle.value = '管理台手动归档'
    newContent.value = ''
    statusFilter.value = 'active'
    await loadArchives()
  } catch (err: any) {
    error.value = err.message || 'Archive 创建失败'
  } finally {
    creating.value = false
  }
}

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  await loadArchives()
})

watch(() => [context.orgId, context.projectId, statusFilter.value], () => {
  void loadArchives()
})
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <h2 class="text-3xl font-black">Markdown 归档库</h2>
        <p class="mt-2 text-stone-600">连接真实 <code>/memory/archive/list</code> API，按当前组织和项目读取可治理归档。</p>
      </div>
      <div class="flex flex-wrap gap-2">
        <select v-model="statusFilter" class="rounded-2xl border bg-white px-4 py-2 text-sm font-bold">
          <option value="active">活跃</option>
          <option value="deleted">已删除</option>
        </select>
        <button class="rounded-2xl border bg-white px-4 py-2 font-bold" :disabled="loading" @click="loadArchives">{{ loading ? '刷新中...' : '刷新' }}</button>
      </div>
    </div>

    <p v-if="!auth.isAuthenticated" class="mt-6 rounded-2xl bg-amber-50 p-4 text-sm text-amber-800">请先登录后查看归档库。</p>
    <p v-else-if="!hasProjectContext" class="mt-6 rounded-2xl bg-amber-50 p-4 text-sm text-amber-800">请先选择组织和项目。</p>
    <p v-if="error" class="mt-6 rounded-2xl bg-red-50 p-4 text-sm text-red-700">{{ error }}</p>
    <p v-if="notice" class="mt-6 rounded-2xl bg-lime-50 p-4 text-sm text-lime-800">{{ notice }}</p>

    <details class="mt-6 rounded-3xl border bg-white p-5">
      <summary class="cursor-pointer text-xl font-black">高级：手动创建归档</summary>
      <div class="mt-4 flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
        <p class="max-w-3xl text-sm text-stone-600">调用真实 <code>/memory/archive/create</code>，用 <code>manual_archive_request</code> 事件生成 Markdown 归档。日常优先从候选沉淀进入 Archive。</p>
        <button class="rounded-2xl bg-stone-950 px-4 py-3 font-bold text-white disabled:cursor-not-allowed disabled:bg-stone-400" :disabled="creating || !auth.isAuthenticated || !hasProjectContext || !newTitle.trim() || !newContent.trim()" @click="createArchive">
          {{ creating ? '创建中...' : '创建真实 Archive' }}
        </button>
      </div>
      <div class="mt-4 grid gap-4 lg:grid-cols-[280px_1fr]">
        <label class="text-sm font-bold text-stone-600">
          标题
          <input v-model="newTitle" class="mt-2 w-full rounded-2xl border px-4 py-3 font-normal text-stone-950" placeholder="例如：生产部署验收记录">
        </label>
        <label class="text-sm font-bold text-stone-600">
          正文
          <textarea v-model="newContent" class="mt-2 min-h-28 w-full rounded-2xl border p-3 font-normal text-stone-950" placeholder="写入需要沉淀的项目事实、排障结论或后续动作" />
        </label>
      </div>
    </details>

    <div v-if="loading" class="mt-6 rounded-3xl bg-white p-6 text-stone-600">正在加载 Archive...</div>
    <div v-else-if="auth.isAuthenticated && hasProjectContext && archives.length === 0" class="mt-6 rounded-3xl bg-white p-6 text-stone-600">当前过滤条件下暂无 Archive。</div>
    <div v-else class="mt-6 grid gap-4">
      <NuxtLink v-for="archive in archives" :key="archive.archive_id" :to="`/archive/${archive.archive_id}`" class="rounded-3xl border bg-white p-5 hover:border-orange-300">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <h3 class="font-black">{{ archive.title || archive.archive_id }}</h3>
            <p class="mt-2 text-sm text-stone-600">{{ statusText(archive.status) }} · 版本 {{ archive.current_version }}</p>
            <details class="mt-3 rounded-2xl bg-stone-50 p-3">
              <summary class="cursor-pointer text-sm font-black">技术信息</summary>
              <dl class="mt-3 grid gap-2 text-xs text-stone-600 sm:grid-cols-[9rem_1fr]">
                <dt class="font-black">archive_id</dt>
                <dd class="break-all">{{ archive.archive_id }}</dd>
                <dt class="font-black">file_path</dt>
                <dd class="break-all">{{ archive.file_path }}</dd>
                <dt class="font-black">index_generation</dt>
                <dd>{{ archive.index_generation }}</dd>
                <dt class="font-black">content_hash</dt>
                <dd class="break-all">{{ archive.content_hash }}</dd>
              </dl>
            </details>
          </div>
          <p class="text-sm text-stone-500">更新于 {{ formatDate(archive.updated_at) }}</p>
        </div>
      </NuxtLink>
    </div>
  </AppShell>
</template>
