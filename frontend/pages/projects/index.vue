<script setup lang="ts">
const auth = useAuthStore()
const context = useContextStore()
const { request } = useApi()

const loading = ref(false)
const creating = ref(false)
const deletingProjectID = ref('')
const editingProjectID = ref('')
const editName = ref('')
const editSlug = ref('')
const savingProjectID = ref('')
const error = ref('')
const name = ref('')
const slug = ref('')

type ProjectListResponse = { projects: TenantProject[] }
type ProjectCreateResponse = { project: TenantProject }
type ProjectEditResponse = { project: TenantProject }
type ProjectDeleteResponse = { project: TenantProject }

const selectedProject = computed(() => context.projects.find((project) => project.project_id === context.projectId))

function isGitProject(project: TenantProject) {
  return project.source_type === 'git' && Boolean(project.source_key)
}

function statusLabel(status?: string) {
  return status === 'disabled' ? '已停用' : '可用'
}

function sourceLabel(project: TenantProject) {
  if (isGitProject(project)) return `Git 仓库：${project.source_key}`
  return '历史项目：还没有绑定 Git 仓库，后续 MCP 会按 Git 自动创建新项目。'
}

async function loadProjects() {
  if (!auth.isAuthenticated || !context.orgId) return
  loading.value = true
  error.value = ''
  try {
    const response = await request<ProjectListResponse>('/memory/tenant/projects/list', {
      method: 'POST',
      body: { org_id: context.orgId }
    })
    context.setProjects(response.projects || [])
  } catch (err: any) {
    error.value = err.message || '项目加载失败'
  } finally {
    loading.value = false
  }
}

async function createProject() {
  if (!context.orgId) {
    error.value = '请先选择组织'
    return
  }
  creating.value = true
  error.value = ''
  try {
    const response = await request<ProjectCreateResponse>('/memory/tenant/projects/create', {
      method: 'POST',
      body: { org_id: context.orgId, name: name.value, slug: slug.value }
    })
    context.setProject(response.project.project_id)
    name.value = ''
    slug.value = ''
    await loadProjects()
  } catch (err: any) {
    error.value = err.message || '项目创建失败'
  } finally {
    creating.value = false
  }
}

function startEditProject(project: TenantProject) {
  editingProjectID.value = project.project_id
  editName.value = project.name
  editSlug.value = project.slug
}

function cancelEditProject() {
  editingProjectID.value = ''
  editName.value = ''
  editSlug.value = ''
}

async function editProject(project: TenantProject) {
  savingProjectID.value = project.project_id
  error.value = ''
  try {
    await request<ProjectEditResponse>('/memory/tenant/projects/edit', {
      method: 'POST',
      body: { org_id: project.org_id, project_id: project.project_id, name: editName.value, slug: editSlug.value }
    })
    cancelEditProject()
    await loadProjects()
  } catch (err: any) {
    error.value = err.message || '项目保存失败'
  } finally {
    savingProjectID.value = ''
  }
}

async function deleteProject(project: TenantProject) {
  deletingProjectID.value = project.project_id
  error.value = ''
  try {
    await request<ProjectDeleteResponse>('/memory/tenant/projects/delete', {
      method: 'POST',
      body: { org_id: project.org_id, project_id: project.project_id }
    })
    if (context.projectId === project.project_id) context.setProject('')
    await loadProjects()
  } catch (err: any) {
    error.value = err.message || '项目删除失败'
  } finally {
    deletingProjectID.value = ''
  }
}

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  await loadProjects()
})

watch(() => context.orgId, async () => {
  await loadProjects()
})
</script>

<template>
  <AppShell>
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div>
        <p class="text-xs font-black uppercase tracking-[0.35em] text-orange-800">Workspace</p>
        <h2 class="mt-2 text-3xl font-black">项目空间</h2>
        <p class="mt-2 max-w-3xl text-stone-600">
          项目会由 MCP 根据 Git 仓库自动创建；这里主要用于查看自动识别结果、整理名称和清理历史项目。成员协作请到“高级设置 → 成员权限”。
        </p>
        <p class="mt-2 text-sm text-stone-500">
          当前项目：<span class="font-bold text-stone-800">{{ selectedProject?.name || '尚未选择，Agent 使用时会自动归类' }}</span>
        </p>
      </div>
      <button class="rounded-2xl border px-4 py-2 font-bold" :disabled="loading" @click="loadProjects">
        {{ loading ? '刷新中...' : '刷新项目' }}
      </button>
    </div>

    <p v-if="error" class="mt-4 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>

    <section class="mt-6 grid gap-4 lg:grid-cols-[1fr_22rem]">
      <div class="rounded-3xl border bg-white p-5">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h3 class="text-xl font-black">已识别项目</h3>
            <p class="mt-1 text-sm text-stone-600">确认 Git 仓库是否被 Memory OS 正确归类；UUID 默认收进技术信息里。</p>
          </div>
          <span class="rounded-full bg-stone-100 px-3 py-1 text-xs font-bold text-stone-700">{{ context.projects.length }} 个项目</span>
        </div>

        <p v-if="loading" class="mt-4 text-sm text-stone-500">正在加载项目...</p>
        <p v-else-if="context.projects.length === 0" class="mt-4 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">
          还没有可见项目。把通用 MCP Token 配到 Agent 后，在 Git 仓库目录里使用 Memory OS，就会自动创建项目空间。
        </p>

        <article v-for="project in context.projects" :key="project.project_id" class="mt-4 rounded-3xl border bg-stone-50 p-5">
          <template v-if="editingProjectID === project.project_id">
            <label class="block text-sm font-bold text-stone-600">
              项目显示名
              <input v-model="editName" class="mt-2 w-full rounded-2xl border bg-white px-3 py-2 font-bold" placeholder="项目名称">
            </label>
            <label class="mt-3 block text-sm font-bold text-stone-600">
              URL slug
              <input v-model="editSlug" class="mt-2 w-full rounded-2xl border bg-white px-3 py-2" placeholder="唯一 slug">
            </label>
          </template>

          <template v-else>
            <div class="flex flex-wrap items-center gap-2">
              <h3 class="text-xl font-black">{{ project.name }}</h3>
              <span v-if="context.projectId === project.project_id" class="rounded-full bg-emerald-100 px-3 py-1 text-xs font-black text-emerald-900">当前项目</span>
              <span class="rounded-full bg-white px-3 py-1 text-xs font-bold text-stone-600">{{ statusLabel(project.status) }}</span>
            </div>
            <p class="mt-2 break-all text-sm text-stone-600">{{ project.slug }}</p>
            <p :class="isGitProject(project) ? 'mt-3 break-all rounded-2xl bg-emerald-50 p-3 text-sm font-bold text-emerald-900' : 'mt-3 rounded-2xl bg-white p-3 text-sm text-stone-600'">
              <span v-if="isGitProject(project)">Git 自动识别：</span>{{ sourceLabel(project) }}
            </p>
          </template>

          <div class="mt-4 flex flex-wrap gap-2">
            <button class="rounded-full bg-stone-950 px-3 py-1 text-xs font-bold text-white" @click="context.setProject(project.project_id)">设为当前项目</button>
            <button v-if="editingProjectID !== project.project_id" class="rounded-full bg-amber-100 px-3 py-1 text-xs font-bold text-amber-950" @click="startEditProject(project)">编辑名称</button>
            <button v-if="editingProjectID === project.project_id" class="rounded-full bg-emerald-100 px-3 py-1 text-xs font-bold text-emerald-950 disabled:cursor-not-allowed disabled:bg-stone-100 disabled:text-stone-500" :disabled="savingProjectID === project.project_id" @click="editProject(project)">
              {{ savingProjectID === project.project_id ? '保存中...' : '保存项目' }}
            </button>
            <button v-if="editingProjectID === project.project_id" class="rounded-full bg-white px-3 py-1 text-xs font-bold text-stone-700" @click="cancelEditProject">取消</button>
            <button class="rounded-full bg-red-100 px-3 py-1 text-xs font-bold text-red-950 disabled:cursor-not-allowed disabled:bg-stone-100 disabled:text-stone-500" :disabled="deletingProjectID === project.project_id" @click="deleteProject(project)">
              {{ deletingProjectID === project.project_id ? '删除中...' : '删除项目' }}
            </button>
          </div>

          <details class="mt-4 rounded-2xl bg-white p-3">
            <summary class="cursor-pointer text-xs font-black uppercase tracking-widest text-stone-500">技术信息</summary>
            <dl class="mt-3 grid gap-2 text-xs text-stone-600 sm:grid-cols-[8rem_1fr]">
              <dt class="font-black text-stone-500">project_id</dt>
              <dd class="break-all">{{ project.project_id }}</dd>
              <dt class="font-black text-stone-500">org_id</dt>
              <dd class="break-all">{{ project.org_id }}</dd>
              <dt class="font-black text-stone-500">source_type</dt>
              <dd class="break-all">{{ project.source_type || 'manual' }}</dd>
              <dt class="font-black text-stone-500">source_key</dt>
              <dd class="break-all">{{ project.source_key || '未绑定' }}</dd>
            </dl>
          </details>
        </article>
      </div>

      <aside class="space-y-4">
        <section class="rounded-3xl border bg-white p-5">
          <h3 class="text-xl font-black">什么时候需要这里？</h3>
          <div class="mt-4 space-y-3 text-sm text-stone-600">
            <p class="rounded-2xl bg-stone-50 p-3">确认不同 Git 仓库有没有被自动分到不同项目。</p>
            <p class="rounded-2xl bg-stone-50 p-3">把自动生成的项目名改成你看得懂的名字。</p>
            <p class="rounded-2xl bg-stone-50 p-3">删除历史测试项目或不再使用的项目。</p>
          </div>
        </section>

        <details class="rounded-3xl border bg-white p-5">
          <summary class="cursor-pointer text-xl font-black">高级操作</summary>
          <p class="mt-3 text-sm text-stone-600">
            正常情况下不需要手动创建项目；MCP 会根据 Git remote 自动创建。只有导入历史数据或非 Git 工作目录时，才使用手动创建项目。
          </p>
          <label class="mt-4 block text-sm font-bold text-stone-600">
            项目名称
            <input v-model="name" class="mt-2 w-full rounded-2xl border px-4 py-3" placeholder="项目名称">
          </label>
          <label class="mt-3 block text-sm font-bold text-stone-600">
            唯一 slug
            <input v-model="slug" class="mt-2 w-full rounded-2xl border px-4 py-3" placeholder="唯一 slug">
          </label>
          <button class="mt-4 w-full rounded-2xl bg-stone-950 px-4 py-3 font-black text-white disabled:cursor-not-allowed disabled:bg-stone-300" :disabled="creating || !context.orgId" @click="createProject">
            {{ creating ? '创建中...' : '手动创建项目' }}
          </button>
        </details>
      </aside>
    </section>
  </AppShell>
</template>
