<script setup lang="ts">
const auth = useAuthStore()
const { request } = useApi()

type UserListResponse = { users: TenantUser[] }
type UserCreateResponse = { user: TenantUser }

const loading = ref(false)
const creating = ref(false)
const updatingUserID = ref('')
const error = ref('')
const notice = ref('')
const users = ref<TenantUser[]>([])
const statusFilter = ref<'active' | 'disabled' | 'deleted' | ''>('active')
const email = ref('')
const displayName = ref('')

function statusText(status?: string) {
  const labels: Record<string, string> = { active: '活跃', disabled: '已禁用', deleted: '已删除' }
  return labels[status || ''] || status || '-'
}

async function loadUsers() {
  if (!auth.isAuthenticated) return
  loading.value = true
  error.value = ''
  try {
    const response = await request<UserListResponse>('/memory/tenant/users/list', {
      method: 'POST',
      body: { status: statusFilter.value }
    })
    users.value = response.users || []
  } catch (err: any) {
    error.value = err.message || '用户加载失败'
  } finally {
    loading.value = false
  }
}

async function createUser() {
  if (!email.value.trim() || !displayName.value.trim()) {
    error.value = '邮箱和显示名称不能为空'
    return
  }
  creating.value = true
  error.value = ''
  notice.value = ''
  try {
    const response = await request<UserCreateResponse>('/memory/tenant/users/create', {
      method: 'POST',
      body: { email: email.value.trim(), display_name: displayName.value.trim() }
    })
    notice.value = `用户已创建：${response.user.email}`
    email.value = ''
    displayName.value = ''
    statusFilter.value = 'active'
    await loadUsers()
  } catch (err: any) {
    error.value = err.message || '用户创建失败'
  } finally {
    creating.value = false
  }
}

async function updateUserStatus(userID: string, status: 'active' | 'disabled') {
  updatingUserID.value = userID
  error.value = ''
  notice.value = ''
  try {
    const response = await request<UserCreateResponse>('/memory/tenant/users/update-status', {
      method: 'POST',
      body: { user_id: userID, status }
    })
    notice.value = `${response.user.email} 已${status === 'disabled' ? '禁用' : '启用'}。`
    await loadUsers()
  } catch (err: any) {
    error.value = err.message || '用户状态更新失败'
  } finally {
    updatingUserID.value = ''
  }
}

onMounted(async () => {
  auth.initFromStorage()
  await loadUsers()
})

watch(statusFilter, () => {
  void loadUsers()
})
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <h2 class="text-3xl font-black">用户管理</h2>
        <p class="mt-2 text-stone-600">读取和创建真实 PostgreSQL 用户 metadata，不展示密码、token 或凭据字段。</p>
      </div>
      <div class="flex flex-wrap gap-2">
        <select v-model="statusFilter" class="rounded-2xl border bg-white px-4 py-2 text-sm font-bold">
          <option value="">全部状态</option>
          <option value="active">active</option>
          <option value="disabled">disabled</option>
          <option value="deleted">deleted</option>
        </select>
        <button class="rounded-2xl border bg-white px-4 py-2 font-bold" :disabled="loading" @click="loadUsers">{{ loading ? '刷新中...' : '刷新' }}</button>
      </div>
    </div>

    <p v-if="!auth.isAuthenticated" class="mt-6 rounded-2xl bg-amber-50 p-4 text-sm text-amber-800">请先登录后管理用户。</p>
    <p v-if="error" class="mt-6 rounded-2xl bg-red-50 p-4 text-sm text-red-700">{{ error }}</p>
    <p v-if="notice" class="mt-6 rounded-2xl bg-lime-50 p-4 text-sm text-lime-800">{{ notice }}</p>

    <section class="mt-6 rounded-3xl border bg-white p-5">
      <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h3 class="text-xl font-black">创建用户</h3>
          <p class="mt-2 text-sm text-stone-600">调用真实 <code>/memory/tenant/users/create</code>，只返回用户 metadata。</p>
        </div>
        <button class="rounded-2xl bg-stone-950 px-4 py-3 font-bold text-white disabled:cursor-not-allowed disabled:bg-stone-400" :disabled="creating || !auth.isAuthenticated || !email.trim() || !displayName.trim()" @click="createUser">
          {{ creating ? '创建中...' : '创建真实用户' }}
        </button>
      </div>
      <div class="mt-4 grid gap-4 lg:grid-cols-[1fr_1fr]">
        <label class="text-sm font-bold text-stone-600">
          邮箱
          <input v-model="email" class="mt-2 w-full rounded-2xl border px-4 py-3 font-normal text-stone-950" placeholder="operator@company.test" type="email">
        </label>
        <label class="text-sm font-bold text-stone-600">
          显示名称
          <input v-model="displayName" class="mt-2 w-full rounded-2xl border px-4 py-3 font-normal text-stone-950" placeholder="Alice">
        </label>
      </div>
    </section>

    <section class="mt-6 rounded-3xl border bg-white p-5">
      <h3 class="text-xl font-black">真实用户列表</h3>
      <p v-if="loading" class="mt-3 text-sm text-stone-500">正在加载用户...</p>
      <p v-else-if="users.length === 0" class="mt-3 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">当前过滤条件下暂无用户。</p>
      <div v-else class="mt-4 grid gap-3">
        <article v-for="user in users" :key="user.user_id" class="rounded-2xl border bg-stone-50 p-4">
          <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <p class="font-black">{{ user.display_name || user.email }}</p>
              <p class="mt-1 break-all text-sm text-stone-600">{{ user.email }}</p>
              <p class="mt-1 break-all text-xs text-stone-500">{{ user.user_id }}</p>
            </div>
            <div class="flex flex-wrap items-center gap-2">
              <span class="rounded-full bg-white px-3 py-1 text-xs font-bold text-stone-700">{{ statusText(user.status) }}</span>
              <button
                v-if="user.status === 'active'"
                class="rounded-full bg-red-900 px-3 py-1 text-xs font-bold text-white disabled:cursor-not-allowed disabled:bg-stone-400"
                :disabled="updatingUserID === user.user_id"
                @click="updateUserStatus(user.user_id, 'disabled')"
              >
                {{ updatingUserID === user.user_id ? '禁用中...' : '禁用用户' }}
              </button>
              <button
                v-else-if="user.status === 'disabled'"
                class="rounded-full bg-emerald-700 px-3 py-1 text-xs font-bold text-white disabled:cursor-not-allowed disabled:bg-stone-400"
                :disabled="updatingUserID === user.user_id"
                @click="updateUserStatus(user.user_id, 'active')"
              >
                {{ updatingUserID === user.user_id ? '启用中...' : '启用用户' }}
              </button>
            </div>
          </div>
        </article>
      </div>
    </section>
  </AppShell>
</template>
