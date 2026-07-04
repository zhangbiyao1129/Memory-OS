<script setup lang="ts">
const auth = useAuthStore()
const context = useContextStore()
const { request } = useApi()

type UserListResponse = { users: TenantUser[] }
type MembershipListResponse = { memberships: TenantMembership[] }
type MembershipResponse = TenantMembership
type RoleDefinition = {
  role: string
  display_name: string
  description: string
  permission_labels: string[]
}
type RolesListResponse = { roles: RoleDefinition[] }

const loading = ref(false)
const loadingUsers = ref(false)
const loadingRoles = ref(false)
const adding = ref(false)
const savingUserID = ref('')
const removingUserID = ref('')
const error = ref('')
const notice = ref('')
const users = ref<TenantUser[]>([])
const memberships = ref<TenantMembership[]>([])
const roleDefinitions = ref<RoleDefinition[]>([])
const roleEdits = ref<Record<string, string>>({})
const selectedUserID = ref('')
const selectedRole = ref('')

const userByID = computed(() => new Map(users.value.map((user) => [user.user_id, user])))
const roleOptions = computed(() => roleDefinitions.value)
const defaultRole = computed(() => roleDefinitions.value[0]?.role || '')

function labelsForRole(role: string) {
  return roleDefinitions.value.find((definition) => definition.role === role)?.permission_labels || []
}

function roleTitle(role: string) {
  const definition = roleDefinitions.value.find((item) => item.role === role)
  return definition?.display_name || role || '未选择'
}

function roleDescription(role: string) {
  const definition = roleDefinitions.value.find((item) => item.role === role)
  return definition?.description || '系统会按该角色自动生成检索、写入和 Secret 使用权限。'
}

function userLabel(userID: string) {
  const user = userByID.value.get(userID)
  if (!user) return userID
  return `${user.display_name || user.email} · ${user.email}`
}

async function loadUsers() {
  if (!auth.isAuthenticated) return
  loadingUsers.value = true
  error.value = ''
  try {
    const response = await request<UserListResponse>('/memory/tenant/users/list', {
      method: 'POST',
      body: { status: 'active' }
    })
    users.value = response.users || []
    if (!selectedUserID.value && users.value.length > 0) selectedUserID.value = users.value[0].user_id
  } catch (err: any) {
    error.value = err.message || '用户加载失败'
  } finally {
    loadingUsers.value = false
  }
}

async function loadRoles() {
  if (!auth.isAuthenticated || !context.orgId || !context.projectId) {
    roleDefinitions.value = []
    return
  }
  loadingRoles.value = true
  error.value = ''
  try {
    const response = await request<RolesListResponse>('/memory/tenant/roles/list', {
      method: 'POST',
      body: { org_id: context.orgId, project_id: context.projectId }
    })
    roleDefinitions.value = response.roles || []
    if (roleDefinitions.value.length > 0) {
      if (!roleDefinitions.value.some((definition) => definition.role === selectedRole.value)) {
        selectedRole.value = roleDefinitions.value[0].role
      }
    } else {
      selectedRole.value = ''
    }
  } catch (err: any) {
    roleDefinitions.value = []
    error.value = err.message || '角色定义加载失败'
  } finally {
    loadingRoles.value = false
  }
}

async function loadMemberships() {
  if (!auth.isAuthenticated || !context.orgId || !context.projectId) {
    memberships.value = []
    roleEdits.value = {}
    return
  }
  loading.value = true
  error.value = ''
  try {
    const response = await request<MembershipListResponse>('/memory/tenant/memberships/list', {
      method: 'POST',
      body: { org_id: context.orgId, project_id: context.projectId }
    })
    memberships.value = response.memberships || []
    roleEdits.value = Object.fromEntries(memberships.value.map((membership) => [membership.user_id, membership.role || defaultRole.value]))
  } catch (err: any) {
    error.value = err.message || '权限成员加载失败'
  } finally {
    loading.value = false
  }
}

async function refreshAll() {
  await loadUsers()
  await loadRoles()
  await loadMemberships()
}

async function addMembership() {
  if (!selectedUserID.value || !context.orgId || !context.projectId) {
    error.value = '请选择组织、项目和用户'
    return
  }
  adding.value = true
  error.value = ''
  notice.value = ''
  try {
    await request<MembershipResponse>('/memory/tenant/memberships/add', {
      method: 'POST',
      body: { user_id: selectedUserID.value, org_id: context.orgId, project_id: context.projectId, role: selectedRole.value }
    })
    notice.value = '授权已添加'
    selectedRole.value = defaultRole.value
    await loadMemberships()
  } catch (err: any) {
    error.value = err.message || '授权添加失败'
  } finally {
    adding.value = false
  }
}

async function updateMembershipRole(membership: TenantMembership) {
  savingUserID.value = membership.user_id
  error.value = ''
  notice.value = ''
  try {
    await request<MembershipResponse>('/memory/tenant/memberships/update-role', {
      method: 'POST',
      body: {
        user_id: membership.user_id,
        org_id: membership.org_id,
        project_id: membership.project_id,
        role: roleEdits.value[membership.user_id] || membership.role
      }
    })
    notice.value = '角色已保存'
    await loadMemberships()
  } catch (err: any) {
    error.value = err.message || '角色保存失败'
  } finally {
    savingUserID.value = ''
  }
}

async function removeMembership(membership: TenantMembership) {
  removingUserID.value = membership.user_id
  error.value = ''
  notice.value = ''
  try {
    await request<MembershipResponse>('/memory/tenant/memberships/remove', {
      method: 'POST',
      body: { user_id: membership.user_id, org_id: membership.org_id, project_id: membership.project_id }
    })
    notice.value = '授权已移除'
    await loadMemberships()
  } catch (err: any) {
    error.value = err.message || '授权移除失败'
  } finally {
    removingUserID.value = ''
  }
}

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  await refreshAll()
})

watch(() => [context.orgId, context.projectId], () => {
  void loadRoles()
  void loadMemberships()
})
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <p class="text-xs font-black uppercase tracking-[0.35em] text-orange-800">Advanced</p>
        <h2 class="mt-2 text-3xl font-black">成员权限</h2>
        <p class="mt-2 max-w-3xl text-stone-600">
          这个页面只在多人协作时有用：把用户加入当前项目，并选择“所有者 / 协作者 / 只读者”等权限等级。单人使用通用 MCP Token 时可以忽略。
        </p>
      </div>
      <button class="rounded-2xl border bg-white px-4 py-2 font-bold" :disabled="loading || loadingUsers || loadingRoles" @click="refreshAll">
        {{ loading || loadingUsers || loadingRoles ? '刷新中...' : '刷新成员权限' }}
      </button>
    </div>

    <p v-if="!auth.isAuthenticated" class="mt-6 rounded-2xl bg-amber-50 p-4 text-sm text-amber-800">请先登录后管理权限。</p>
    <p v-if="error" class="mt-6 rounded-2xl bg-red-50 p-4 text-sm text-red-700">{{ error }}</p>
    <p v-if="notice" class="mt-6 rounded-2xl bg-lime-50 p-4 text-sm text-lime-800">{{ notice }}</p>

    <section class="mt-6 grid gap-4 lg:grid-cols-[1fr_22rem]">
      <div class="rounded-3xl border bg-white p-5">
        <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <h3 class="text-xl font-black">当前项目成员</h3>
            <p class="mt-2 break-all text-sm text-stone-500">组织：{{ context.orgId || '未选择' }} · 项目：{{ context.projectId || '未选择' }}</p>
          </div>
          <span class="rounded-full bg-stone-100 px-3 py-1 text-xs font-bold text-stone-700">{{ memberships.length }} 位成员</span>
        </div>

        <p v-if="loading" class="mt-4 text-sm text-stone-500">正在加载权限成员...</p>
        <p v-else-if="memberships.length === 0" class="mt-4 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">当前项目暂无成员，或当前 PAT 没有读取权限。</p>

        <article v-for="membership in memberships" :key="`${membership.user_id}:${membership.project_id}`" class="mt-4 rounded-3xl border bg-stone-50 p-4">
          <div class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
            <div>
              <p class="break-all font-black">{{ userLabel(membership.user_id) }}</p>
              <p class="mt-1 break-all text-xs text-stone-500">{{ membership.user_id }}</p>
              <p class="mt-1 text-sm text-stone-600">状态：{{ membership.status || 'active' }}</p>
              <p class="mt-2 rounded-full bg-white px-3 py-1 text-sm font-bold text-stone-800">权限等级：{{ roleTitle(roleEdits[membership.user_id] || membership.role) }}</p>
              <p class="mt-2 max-w-xl text-sm text-stone-600">{{ roleDescription(roleEdits[membership.user_id] || membership.role) }}</p>
            </div>
            <div class="flex flex-wrap items-center gap-2">
              <select v-model="roleEdits[membership.user_id]" class="rounded-full border bg-white px-3 py-2 text-sm font-bold">
                <option v-for="role in roleOptions" :key="role.role" :value="role.role">{{ role.display_name || role.role }}</option>
              </select>
              <button class="rounded-full bg-emerald-100 px-3 py-2 text-sm font-bold text-emerald-950 disabled:cursor-not-allowed disabled:bg-stone-100 disabled:text-stone-500" :disabled="savingUserID === membership.user_id || membership.status === 'disabled'" @click="updateMembershipRole(membership)">
                {{ savingUserID === membership.user_id ? '保存中...' : '保存权限等级' }}
              </button>
              <button class="rounded-full bg-red-100 px-3 py-2 text-sm font-bold text-red-950 disabled:cursor-not-allowed disabled:bg-stone-100 disabled:text-stone-500" :disabled="removingUserID === membership.user_id || membership.status === 'disabled'" @click="removeMembership(membership)">
                {{ removingUserID === membership.user_id ? '移除中...' : '移除成员' }}
              </button>
            </div>
          </div>
          <details class="mt-4 rounded-2xl bg-white p-3">
            <summary class="cursor-pointer text-xs font-black uppercase tracking-widest text-stone-500">高级权限标签</summary>
            <div class="mt-2 flex flex-wrap gap-2">
              <span v-for="label in labelsForRole(roleEdits[membership.user_id] || membership.role)" :key="label" class="rounded-full bg-orange-50 px-3 py-1 text-xs font-bold text-orange-950">{{ label }}</span>
              <span v-if="labelsForRole(roleEdits[membership.user_id] || membership.role).length === 0" class="text-xs text-stone-500">角色定义尚未加载</span>
            </div>
          </details>
        </article>
      </div>

      <aside class="rounded-3xl border bg-white p-5">
        <h3 class="text-xl font-black">添加项目成员</h3>
        <p class="mt-2 text-sm text-stone-600">调用真实成员 API，写入 PostgreSQL memberships。权限标签由角色自动带出，不需要手填。</p>
        <label class="mt-4 block text-sm font-bold text-stone-600">
          用户
          <select v-model="selectedUserID" class="mt-2 w-full rounded-2xl border bg-white px-4 py-3 font-normal text-stone-950">
            <option value="">选择用户</option>
            <option v-for="user in users" :key="user.user_id" :value="user.user_id">{{ user.display_name || user.email }} · {{ user.email }}</option>
          </select>
        </label>
        <label class="mt-4 block text-sm font-bold text-stone-600">
          权限等级
          <select v-model="selectedRole" class="mt-2 w-full rounded-2xl border bg-white px-4 py-3 font-normal text-stone-950">
            <option v-if="roleOptions.length === 0" value="" disabled>请先刷新加载角色目录</option>
            <option v-for="role in roleOptions" :key="role.role" :value="role.role">{{ role.display_name || role.role }}</option>
          </select>
        </label>
        <div class="mt-4 rounded-2xl bg-stone-50 p-4">
          <p class="text-sm font-black text-stone-700">将获得的权限等级</p>
          <p class="mt-2 text-sm text-stone-600">{{ roleTitle(selectedRole) }}</p>
          <p class="mt-1 text-xs text-stone-500">{{ roleDescription(selectedRole) }}</p>
        </div>
        <details class="mt-4 rounded-2xl bg-stone-50 p-4">
          <summary class="cursor-pointer text-xs font-black uppercase tracking-widest text-stone-500">高级权限标签</summary>
          <div class="mt-2 flex flex-wrap gap-2">
            <span v-for="label in labelsForRole(selectedRole)" :key="label" class="rounded-full bg-white px-3 py-1 text-xs font-bold text-stone-700">{{ label }}</span>
            <span v-if="labelsForRole(selectedRole).length === 0" class="text-xs text-stone-500">选择组织和项目后加载后端角色定义</span>
          </div>
        </details>
        <button class="mt-4 w-full rounded-2xl bg-stone-950 px-4 py-3 font-black text-white disabled:cursor-not-allowed disabled:bg-stone-300" :disabled="adding || !context.orgId || !context.projectId || !selectedUserID || roleOptions.length === 0" @click="addMembership">
          {{ adding ? '添加中...' : '添加成员权限' }}
        </button>
      </aside>
    </section>
  </AppShell>
</template>
