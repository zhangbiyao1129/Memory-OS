<script setup lang="ts">
const auth = useAuthStore()
const context = useContextStore()
const { request } = useApi()

type RoleDefinition = {
  role: string
  display_name: string
  description: string
  permission_labels: string[]
}
type RolesListResponse = { roles: RoleDefinition[] }
type RoleUpsertResponse = { role: RoleDefinition }

const loading = ref(false)
const loadingRoles = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const roles = ref<RoleDefinition[]>([])
const editingRole = ref('')
const role = ref('')
const displayName = ref('')
const description = ref('')
const permissionInput = ref('')

const hasScope = computed(() => !!context.orgId && !!context.projectId)
const parsedPermissions = computed(() => {
  return permissionInput.value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter((item, index, list) => item && list.indexOf(item) === index)
})

function canSubmit() {
  return auth.isAuthenticated && hasScope.value && role.value.trim().length > 0 && displayName.value.trim().length > 0 && parsedPermissions.value.length > 0
}

function resetForm() {
  editingRole.value = ''
  role.value = ''
  displayName.value = ''
  description.value = ''
  permissionInput.value = ''
  error.value = ''
  notice.value = ''
}

function startEdit(item: RoleDefinition) {
  editingRole.value = item.role
  role.value = item.role
  displayName.value = item.display_name
  description.value = item.description
  permissionInput.value = item.permission_labels.join('\n')
}

async function loadRoles() {
  if (!auth.isAuthenticated || !context.orgId || !context.projectId) {
    roles.value = []
    return
  }
  loadingRoles.value = true
  error.value = ''
  try {
    const response = await request<RolesListResponse>('/memory/tenant/roles/list', {
      method: 'POST',
      body: {
        org_id: context.orgId,
        project_id: context.projectId
      }
    })
    roles.value = response.roles || []
  } catch (err: any) {
    roles.value = []
    error.value = err.message || '角色目录加载失败'
  } finally {
    loadingRoles.value = false
  }
}

async function upsertRole() {
  if (!canSubmit()) {
    error.value = '请补齐角色、显示名与至少一条权限标签'
    return
  }
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    const response = await request<RoleUpsertResponse>('/memory/tenant/roles/upsert', {
      method: 'POST',
      body: {
        org_id: context.orgId,
        project_id: context.projectId,
        role: role.value.trim(),
        display_name: displayName.value.trim(),
        description: description.value.trim(),
        permission_labels: parsedPermissions.value
      }
    })
    notice.value = `角色 ${response.role.role} 已更新`
    await loadRoles()
    resetForm()
  } catch (err: any) {
    error.value = err.message || '角色保存失败'
  } finally {
    saving.value = false
  }
}

async function onRefresh() {
  loading.value = true
  await loadRoles()
  loading.value = false
}

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  await loadRoles()
})

watch(() => [context.orgId, context.projectId], () => {
  void loadRoles()
})
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <p class="text-xs font-black uppercase tracking-[0.35em] text-orange-800">Advanced</p>
        <h2 class="mt-2 text-3xl font-black">角色目录</h2>
        <p class="mt-2 text-stone-600">高级管理员入口：管理项目级角色定义。普通日常使用不需要手写权限标签，成员权限页会直接使用这里的默认角色。</p>
      </div>
      <button class="rounded-2xl border bg-white px-4 py-2 font-bold" :disabled="loading || loadingRoles" @click="onRefresh">
        {{ loading || loadingRoles ? '刷新中...' : '刷新角色' }}
      </button>
    </div>

    <p v-if="!auth.isAuthenticated" class="mt-6 rounded-2xl bg-amber-50 p-4 text-sm text-amber-800">请先登录后管理角色。</p>
    <p v-if="error" class="mt-6 rounded-2xl bg-red-50 p-4 text-sm text-red-700">{{ error }}</p>
    <p v-if="notice" class="mt-6 rounded-2xl bg-lime-50 p-4 text-sm text-lime-800">{{ notice }}</p>

    <section class="mt-6 grid gap-4 lg:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]">
      <article class="rounded-3xl border bg-white p-5">
        <h3 class="text-xl font-black">当前角色</h3>
        <p v-if="loadingRoles" class="mt-3 text-sm text-stone-500">正在加载角色目录...</p>
        <p v-else-if="roles.length === 0" class="mt-3 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">
          请选择组织/项目后查看角色；若无角色配置会显示空列表。
        </p>
        <div v-else class="mt-4 space-y-3">
          <article v-for="item in roles" :key="item.role" class="rounded-2xl border bg-stone-50 p-4">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <p class="font-black break-all">{{ item.role }}</p>
                <p class="mt-1 break-all text-sm text-stone-600">{{ item.display_name }}</p>
                <p class="mt-1 break-all text-xs text-stone-500">{{ item.description || '未填写描述' }}</p>
              </div>
              <button class="rounded-full bg-stone-950 px-3 py-1 text-xs font-bold text-white" @click="startEdit(item)">编辑</button>
            </div>
            <div class="mt-3 flex flex-wrap gap-2">
              <span v-for="label in item.permission_labels" :key="label" class="rounded-full bg-white px-3 py-1 text-xs font-bold text-stone-700">{{ label }}</span>
            </div>
          </article>
        </div>
      </article>

      <article class="rounded-3xl border bg-white p-5">
        <h3 class="text-xl font-black">创建 / 更新角色</h3>
        <p class="mt-2 text-sm text-stone-600">编辑后点击保存即执行 upsert；相同角色名会覆盖。</p>
        <div class="mt-4 space-y-4">
          <label class="block text-sm font-bold text-stone-600">
            角色标识
            <input v-model="role" class="mt-2 w-full rounded-2xl border px-4 py-3" placeholder="custom_reader" :disabled="loading || loadingRoles || saving">
          </label>
          <label class="block text-sm font-bold text-stone-600">
            显示名称
            <input v-model="displayName" class="mt-2 w-full rounded-2xl border px-4 py-3" placeholder="Custom Reader" :disabled="loading || loadingRoles || saving">
          </label>
          <label class="block text-sm font-bold text-stone-600">
            角色描述
            <textarea v-model="description" class="mt-2 min-h-24 w-full rounded-2xl border px-4 py-3" placeholder="用于只读检索访问..." :disabled="loading || loadingRoles || saving"></textarea>
          </label>
          <label class="block text-sm font-bold text-stone-600">
            权限标签（每行/逗号分隔）
            <textarea v-model="permissionInput" class="mt-2 min-h-24 w-full rounded-2xl border px-4 py-3" placeholder="memory:read&#10;project:xxx:read" :disabled="loading || loadingRoles || saving"></textarea>
          </label>
        </div>
        <button class="mt-4 w-full rounded-2xl bg-stone-950 px-4 py-3 font-black text-white disabled:cursor-not-allowed disabled:bg-stone-400"
          :disabled="!canSubmit() || saving || loadingRoles || loading" @click="upsertRole">
          {{ saving ? '保存中...' : (editingRole ? '更新角色' : '新建角色') }}
        </button>
        <button class="mt-3 w-full rounded-2xl border bg-white px-4 py-3 font-black" :disabled="loadingRoles || loading || saving" @click="resetForm">
          {{ editingRole ? '清空编辑' : '重置表单' }}
        </button>
        <p v-if="!hasScope" class="mt-4 rounded-2xl bg-amber-50 p-3 text-xs text-amber-800">请先在侧边栏选择组织和项目。</p>
      </article>
    </section>
  </AppShell>
</template>
