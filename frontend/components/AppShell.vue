<script setup lang="ts">
const auth = useAuthStore()
const context = useContextStore()
const router = useRouter()
const { request } = useApi()
const loadingTenants = ref(false)
const tenantError = ref('')
const nav = [
  ['总览', '/'],
  ['记忆', '/memory'],
  ['检索', '/search'],
  ['日志', '/logs'],
  ['高级设置', '/settings']
]

type OrgListResponse = { orgs: TenantOrg[] }
type ProjectListResponse = { projects: TenantProject[] }

async function loadProjects() {
  if (!auth.isAuthenticated || !context.orgId) return
  const response = await request<ProjectListResponse>('/memory/tenant/projects/list', {
    method: 'POST',
    body: { org_id: context.orgId }
  })
  context.setProjects(response.projects || [])
}

async function loadTenants() {
  if (!auth.isAuthenticated) return
  loadingTenants.value = true
  tenantError.value = ''
  try {
    const response = await request<OrgListResponse>('/memory/tenant/orgs/list', { method: 'POST', body: {} })
    context.setOrgs(response.orgs || [])
    await loadProjects()
  } catch (error: any) {
    tenantError.value = error.message || '工作空间加载失败'
    if (tenantError.value.includes('登录已过期')) {
      auth.logout()
      await router.push('/login')
    }
  } finally {
    loadingTenants.value = false
  }
}

async function handleLogout() {
  auth.logout()
  await router.push('/login')
}

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  if (!auth.isAuthenticated) {
    await router.push('/login')
    return
  }
  await loadTenants()
})

watch(() => context.orgId, async (_next, previous) => {
  if (!auth.isAuthenticated || !previous) return
  try {
    await loadProjects()
  } catch (error: any) {
    tenantError.value = error.message || '项目加载失败'
  }
})
</script>

<template>
  <div class="min-h-screen px-4 py-4 text-stone-950 sm:px-6 lg:px-8">
    <div class="mx-auto grid max-w-7xl gap-5 lg:grid-cols-[17rem_1fr]">
      <aside class="rounded-[2rem] border border-stone-900/10 bg-white/70 p-5 shadow-2xl shadow-stone-900/10 backdrop-blur">
        <NuxtLink to="/" class="block">
          <p class="text-xs uppercase tracking-[0.35em] text-orange-800">Memory OS</p>
          <h1 class="mt-2 text-3xl font-black tracking-tight">原生记忆控制台</h1>
        </NuxtLink>
        <div class=”mt-6 rounded-3xl bg-orange-50 p-4 text-sm text-orange-950”>
          <p class=”text-xs font-black uppercase tracking-widest text-orange-800”>当前记忆上下文</p>
          <p class=”mt-2 font-bold”>{{ context.orgs[0]?.name || '我的 Memory OS' }}</p>
          <p class=”mt-1 text-xs text-orange-800”>项目会在 Agent 写入时按目录 / Git 自动归类；日常使用只需要关注当前项目的记忆与检索。</p>
          <p v-if="loadingTenants" class="mt-2 rounded-2xl bg-white/70 p-2 text-xs text-orange-800">正在加载工作空间信息...</p>
          <p v-if="tenantError" class="rounded-2xl bg-red-50 p-2 text-xs text-red-700">{{ tenantError }}</p>
        </div>
        <nav class="mt-7 grid gap-2">
          <NuxtLink v-for="item in nav" :key="item[1]" :to="item[1]" class="rounded-2xl px-3 py-2 font-semibold text-stone-700 hover:bg-orange-100 hover:text-orange-900">
            {{ item[0] }}
          </NuxtLink>
        </nav>
        <div class="mt-7 rounded-3xl bg-stone-950 p-4 text-stone-50">
          <p class="text-xs text-stone-400">当前用户</p>
          <p class="font-bold">{{ auth.user.email }}</p>
          <button class="mt-3 rounded-full bg-white px-3 py-1 text-sm font-bold text-stone-950" @click="handleLogout">登出</button>
        </div>
      </aside>
      <main class="rounded-[2rem] border border-white/70 bg-white/80 p-5 shadow-2xl shadow-stone-900/10 backdrop-blur sm:p-8">
        <slot />
      </main>
    </div>
  </div>
</template>
