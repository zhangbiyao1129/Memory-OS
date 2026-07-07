<script setup lang="ts">
const auth = useAuthStore()
const context = useContextStore()
const { request } = useApi()
const { hasWorkspaceContext, requestWorkspaceProjects } = useWorkspaceProjectScopes()

type HotMemory = {
  memory_id: string
  org_id: string
  project_id: string
  user_id: string
  agent_id: string
  scope: string
  visibility: string
  permission_labels: string[]
  fact: string
  confidence: number
  access_count: number
  returned_count: number
  used_count: number
  last_accessed_at?: string
  last_returned_at?: string
  last_used_at?: string
  pinned?: boolean
  hot_score: number
  status: 'active' | 'promoted' | 'demoted' | 'deleted'
  created_at: string
  updated_at: string
}

type HotMemoryListResponse = { memories: HotMemory[] }

const loading = ref(false)
const creating = ref(false)
const error = ref('')
const success = ref('')
const memories = ref<HotMemory[]>([])
const fact = ref('Memory OS 生产部署默认在 thinkpad:/opt/memory-os 完成')
const scope = ref<'project' | 'user' | 'org' | 'agent_specific'>('project')
const visibility = ref('project')
const statusFilter = ref<'active' | 'promoted' | 'demoted'>('active')
const confidence = ref(0.8)
const editingMemoryID = ref('')
const editFact = ref('')
const editConfidence = ref(0.8)
const savingMemoryID = ref('')

const hasProjectContext = computed(() => Boolean(context.orgId && context.projectId))

function formatDate(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

function statusText(status: string) {
  const labels: Record<string, string> = { active: '活跃', promoted: '已提升', demoted: '已降权', deleted: '已删除' }
  return labels[status] || status
}

function requestBody(extra: Record<string, unknown> = {}) {
  return {
    org_id: context.orgId,
    project_id: context.projectId,
    agent_id: context.agentId,
    ...extra
  }
}

async function loadMemories() {
  if (!auth.isAuthenticated || !hasWorkspaceContext.value) return
  loading.value = true
  error.value = ''
  try {
    const items = await requestWorkspaceProjects<HotMemoryListResponse, HotMemory>({
      path: '/memory/hot-memory/list',
      buildBody: (project) => ({
        org_id: project.org_id,
        project_id: project.project_id,
        agent_id: context.agentId,
        scope: scope.value,
        visibility: visibility.value,
        status: statusFilter.value,
        limit: 50
      }),
      pick: (response) => response.memories || []
    })
    memories.value = items.sort((a, b) => Date.parse(b.updated_at || '') - Date.parse(a.updated_at || ''))
  } catch (err: any) {
    error.value = err.message || 'Hot Memory 加载失败'
  } finally {
    loading.value = false
  }
}

async function createMemory() {
  if (!hasProjectContext.value) {
    error.value = '请先选择真实组织和项目'
    return
  }
  creating.value = true
  error.value = ''
  success.value = ''
  try {
    await request<HotMemory>('/memory/hot-memory/create', {
      method: 'POST',
      body: requestBody({
        fact: fact.value,
        scope: scope.value,
        visibility: visibility.value,
        source_type: 'archive',
        source_ref: 'manual_hot_memory',
        confidence: Number(confidence.value) || 0.7
      })
    })
    fact.value = ''
    success.value = 'Hot Memory 已创建，并切换到活跃筛选。'
    if (statusFilter.value !== 'active') {
      statusFilter.value = 'active'
    } else {
      await loadMemories()
    }
  } catch (err: any) {
    error.value = err.message || 'Hot Memory 创建失败'
  } finally {
    creating.value = false
  }
}

function startEditMemory(memory: HotMemory) {
  editingMemoryID.value = memory.memory_id
  editFact.value = memory.fact
  editConfidence.value = memory.confidence
}

function cancelEditMemory() {
  editingMemoryID.value = ''
  editFact.value = ''
  editConfidence.value = 0.8
}

async function saveMemory(memoryID: string) {
  if (!editFact.value.trim()) {
    error.value = 'Hot Memory 事实不能为空'
    return
  }
  savingMemoryID.value = memoryID
  error.value = ''
  success.value = ''
  try {
    await request<HotMemory>('/memory/hot-memory/edit', {
      method: 'POST',
      body: {
        memory_id: memoryID,
        fact: editFact.value.trim(),
        confidence: Number(editConfidence.value) || 0.7
      }
    })
    cancelEditMemory()
    success.value = 'Hot Memory 编辑已保存。'
    await loadMemories()
  } catch (err: any) {
    error.value = err.message || 'Hot Memory 编辑失败'
  } finally {
    savingMemoryID.value = ''
  }
}

function actionSuccessMessage(endpoint: string) {
  switch (endpoint) {
    case '/memory/hot-memory/promote':
      return 'Hot Memory 已提升，已切换到已提升筛选。'
    case '/memory/hot-memory/demote':
      return 'Hot Memory 已降权，已切换到已降权筛选。'
    case '/memory/hot-memory/mark-used':
      return 'Hot Memory 已标记为使用。'
    case '/memory/hot-memory/delete':
      return 'Hot Memory 已软删除，当前筛选下将不再显示。'
    default:
      return 'Hot Memory 操作已完成。'
  }
}

function actionStatusFilter(endpoint: string) {
  switch (endpoint) {
    case '/memory/hot-memory/promote':
      return 'promoted'
    case '/memory/hot-memory/demote':
      return 'demoted'
    default:
      return ''
  }
}

async function updateMemory(endpoint: string, memoryID: string) {
  error.value = ''
  success.value = ''
  try {
    await request<HotMemory>(endpoint, {
      method: 'POST',
      body: { memory_id: memoryID }
    })
    success.value = actionSuccessMessage(endpoint)
    const nextFilter = actionStatusFilter(endpoint)
    if (nextFilter && statusFilter.value !== nextFilter) {
      statusFilter.value = nextFilter
    } else {
      await loadMemories()
    }
  } catch (err: any) {
    error.value = err.message || 'Hot Memory 操作失败'
  }
}

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  await loadMemories()
})

watch(() => [context.orgId, context.projectId, context.agentId, scope.value, visibility.value, statusFilter.value], () => {
  void loadMemories()
})
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <h2 class="text-3xl font-black">热记忆</h2>
        <p class="mt-2 text-stone-600">这里展示系统用于快速召回的工作记忆；长期事实优先进入 Archive。</p>
        <p class="mt-2 rounded-2xl bg-stone-100 p-3 text-sm text-stone-600">热记忆是可重建的快速索引快照，不是权威记忆源。热度由召回、返回、使用三段信号驱动。</p>
      </div>
      <div class="flex flex-wrap gap-2">
        <NuxtLink class="rounded-2xl border bg-white px-4 py-2 font-bold" to="/candidates">去候选记忆</NuxtLink>
        <NuxtLink class="rounded-2xl border bg-white px-4 py-2 font-bold" to="/archive">去归档库</NuxtLink>
        <button class="rounded-2xl border bg-white px-4 py-2 font-bold" :disabled="loading" @click="loadMemories">{{ loading ? '刷新中...' : '刷新' }}</button>
      </div>
    </div>

    <details class="mt-6 rounded-3xl border bg-white p-5">
      <summary class="cursor-pointer text-xl font-black">高级：手动创建热记忆</summary>
      <p class="mt-3 text-sm text-stone-600">日常优先从候选接受或归档沉淀生成热记忆。只有需要临时补充快速召回事实时，才手动创建。</p>
      <div class="mt-4 grid gap-4 lg:grid-cols-[1fr_180px_160px_140px]">
        <label class="text-sm font-bold text-stone-600">
          记忆事实
          <textarea v-model="fact" class="mt-2 min-h-24 w-full rounded-2xl border p-3 font-normal text-stone-950" placeholder="写入一个可复用的项目事实" />
        </label>
        <label class="text-sm font-bold text-stone-600">
          Scope
          <select v-model="scope" class="mt-2 w-full rounded-2xl border p-3 font-normal text-stone-950">
            <option value="project">project</option>
            <option value="user">user</option>
            <option value="org">org</option>
            <option value="agent_specific">agent_specific</option>
          </select>
        </label>
        <label class="text-sm font-bold text-stone-600">
          置信度
          <input v-model.number="confidence" class="mt-2 w-full rounded-2xl border p-3 font-normal text-stone-950" type="number" min="0.1" max="1" step="0.1">
        </label>
        <button class="self-end rounded-2xl bg-stone-950 px-4 py-3 font-bold text-white disabled:cursor-not-allowed disabled:bg-stone-400" :disabled="creating || !auth.isAuthenticated || !hasProjectContext || !fact.trim()" @click="createMemory">
          {{ creating ? '创建中...' : '创建' }}
        </button>
      </div>
      <p v-if="!auth.isAuthenticated" class="mt-3 rounded-2xl bg-amber-50 p-3 text-sm text-amber-800">请先登录后管理 Hot Memory。</p>
      <p v-else-if="!hasProjectContext" class="mt-3 rounded-2xl bg-amber-50 p-3 text-sm text-amber-800">请先选择组织和项目。</p>
    </details>

    <p v-if="error" class="mt-4 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <p v-if="success" class="mt-4 rounded-2xl bg-emerald-50 p-3 text-sm text-emerald-700">{{ success }}</p>

    <section class="mt-6 rounded-3xl border bg-white p-5">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <h3 class="text-xl font-black">当前记忆</h3>
        <select v-model="statusFilter" class="rounded-2xl border p-3 text-sm">
          <option value="active">活跃</option>
          <option value="promoted">已提升</option>
          <option value="demoted">已降权</option>
        </select>
      </div>
      <div v-if="loading" class="mt-4 rounded-2xl bg-stone-50 p-4 text-stone-600">正在加载 Hot Memory...</div>
      <div v-else-if="memories.length === 0" class="mt-4 rounded-2xl bg-stone-50 p-4 text-stone-600">当前工作区暂无 Hot Memory。</div>
      <div v-else class="mt-4 grid gap-4">
        <article v-for="memory in memories" :key="memory.memory_id" class="rounded-3xl border bg-stone-50 p-5">
          <div class="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
            <div>
              <template v-if="editingMemoryID === memory.memory_id">
                <textarea v-model="editFact" class="min-h-24 w-full rounded-2xl border bg-white p-3 text-sm text-stone-950" placeholder="编辑 Hot Memory 事实" />
                <label class="mt-3 block text-sm font-bold text-stone-600">
                  置信度
                  <input v-model.number="editConfidence" class="ml-2 rounded-2xl border bg-white px-3 py-2 font-normal text-stone-950" type="number" min="0.1" max="1" step="0.1">
                </label>
              </template>
              <p v-else class="text-lg font-black text-stone-950">{{ memory.fact }}</p>
              <p class="mt-2 text-sm text-stone-600">{{ statusText(memory.status) }}<span v-if="memory.pinned" class="ml-1 rounded-full bg-amber-100 px-2 py-0.5 text-xs font-bold text-amber-900">已固定</span></p>
              <div class="mt-2 flex flex-wrap gap-2 text-xs">
                <span class="rounded-full bg-stone-200 px-2 py-1 font-bold text-stone-700" title="被检索召回的次数（access_count）：记忆进入候选集">召回 {{ memory.access_count }}</span>
                <span class="rounded-full bg-sky-100 px-2 py-1 font-bold text-sky-800" title="实际返回给调用方的次数（returned_count）：通过重排后进入最终结果">返回 {{ memory.returned_count }}</span>
                <span class="rounded-full bg-emerald-100 px-2 py-1 font-bold text-emerald-800" title="被显式使用/上下文注入的次数（used_count）：只由 mark-used 显式触发，检索命中不计入">使用 {{ memory.used_count }}</span>
              </div>
              <p class="mt-1 text-xs text-stone-500">最近召回 {{ formatDate(memory.last_accessed_at) }} · 最近返回 {{ formatDate(memory.last_returned_at) }} · 最近使用 {{ formatDate(memory.last_used_at) }}</p>
              <p class="mt-1 text-xs text-stone-500">更新于 {{ formatDate(memory.updated_at) }}</p>
              <details class="mt-3 rounded-2xl bg-white p-3">
                <summary class="cursor-pointer text-xs font-black uppercase tracking-widest text-stone-500">技术信息</summary>
                <dl class="mt-3 grid gap-2 text-xs text-stone-600 sm:grid-cols-[7rem_1fr]">
                  <dt class="font-black">memory_id</dt>
                  <dd class="break-all">{{ memory.memory_id }}</dd>
                  <dt class="font-black">scope</dt>
                  <dd>{{ memory.scope }}</dd>
                  <dt class="font-black">agent</dt>
                  <dd class="break-all">{{ memory.agent_id }}</dd>
                  <dt class="font-black">hot_score</dt>
                  <dd>{{ memory.hot_score.toFixed(2) }}</dd>
                </dl>
              </details>
            </div>
            <div class="flex flex-wrap gap-2">
              <button v-if="editingMemoryID !== memory.memory_id" class="rounded-2xl bg-white px-3 py-2 text-sm font-bold text-stone-900" @click="startEditMemory(memory)">编辑</button>
              <button v-if="editingMemoryID === memory.memory_id" class="rounded-2xl bg-emerald-100 px-3 py-2 text-sm font-bold text-emerald-900 disabled:cursor-not-allowed disabled:bg-stone-100 disabled:text-stone-500" :disabled="savingMemoryID === memory.memory_id" @click="saveMemory(memory.memory_id)">
                {{ savingMemoryID === memory.memory_id ? '保存中...' : '保存编辑' }}
              </button>
              <button v-if="editingMemoryID === memory.memory_id" class="rounded-2xl bg-white px-3 py-2 text-sm font-bold text-stone-700" @click="cancelEditMemory">取消</button>
              <button class="rounded-2xl bg-lime-100 px-3 py-2 text-sm font-bold text-lime-900" @click="updateMemory('/memory/hot-memory/promote', memory.memory_id)">提升</button>
              <button class="rounded-2xl bg-orange-100 px-3 py-2 text-sm font-bold text-orange-900" @click="updateMemory('/memory/hot-memory/demote', memory.memory_id)">降权</button>
              <button class="rounded-2xl bg-blue-100 px-3 py-2 text-sm font-bold text-blue-900" @click="updateMemory('/memory/hot-memory/mark-used', memory.memory_id)">标记使用</button>
              <button class="rounded-2xl bg-red-100 px-3 py-2 text-sm font-bold text-red-900" @click="updateMemory('/memory/hot-memory/delete', memory.memory_id)">删除</button>
            </div>
          </div>
        </article>
      </div>
    </section>
  </AppShell>
</template>
