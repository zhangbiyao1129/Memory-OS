<script setup lang="ts">
import { buildContextSummary } from '~/utils/memoryUx'

const props = withDefaults(defineProps<{ showContextSwitcher?: boolean }>(), {
  showContextSwitcher: true
})
const auth = useAuthStore()
const context = useContextStore()
const router = useRouter()
const { request } = useApi()
const loadingTenants = ref(false)
const tenantError = ref('')
const mobileNavOpen = ref(false)
const showContextSwitcher = computed(() => props.showContextSwitcher)
const nav = [
  ['接入向导', '/onboarding'],
  ['总览', '/'],
  ['记忆', '/memory'],
  ['检索', '/search'],
  ['写入诊断', '/diagnostics'],
  ['Secret', '/secrets'],
  ['日志', '/logs'],
  ['高级设置', '/settings']
]

type OrgListResponse = { orgs: TenantOrg[] }
type ProjectListResponse = { projects: TenantProject[] }

const selectedOrg = computed(() => context.orgs.find((org) => org.org_id === context.orgId))
const selectedProject = computed(() => context.projects.find((project) => project.project_id === context.projectId))
const contextSummary = computed(() =>
  buildContextSummary({
    orgName: selectedOrg.value?.name,
    projectName: selectedProject.value?.name,
    agentId: context.agentId
  })
)
const shellSubtitle = computed(() => props.showContextSwitcher ? contextSummary.value.primary : '全部工作区项目总览')

function closeMobileNav() {
  mobileNavOpen.value = false
}

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
  closeMobileNav()
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
      <header class="flex items-center justify-between gap-3 rounded-xl border border-stone-900/10 bg-white/85 p-3 shadow-lg shadow-stone-900/5 backdrop-blur lg:hidden">
        <NuxtLink to="/" class="min-w-0" @click="closeMobileNav">
          <p class="text-xs font-bold uppercase text-orange-800">Memory OS</p>
          <p class="truncate text-sm font-semibold text-stone-600">{{ shellSubtitle }}</p>
        </NuxtLink>
        <button
          class="shrink-0 rounded-lg border border-stone-900/10 bg-stone-950 px-3 py-2 text-sm font-bold text-white"
          type="button"
          :aria-expanded="mobileNavOpen"
          aria-controls="app-mobile-nav"
          @click="mobileNavOpen = !mobileNavOpen"
        >
          {{ mobileNavOpen ? '关闭' : '菜单' }}
        </button>
      </header>
      <aside
        id="app-mobile-nav"
        class="rounded-xl border border-stone-900/10 bg-white/75 p-5 shadow-xl shadow-stone-900/10 backdrop-blur lg:block"
        :class="mobileNavOpen ? 'block' : 'hidden'"
      >
        <NuxtLink to="/" class="block">
          <p class="text-xs font-bold uppercase text-orange-800">Memory OS</p>
          <h1 class="mt-2 text-2xl font-black tracking-tight">原生记忆控制台</h1>
        </NuxtLink>
        <div v-if="showContextSwitcher" class="mt-6">
          <ContextSwitcher />
          <p v-if="loadingTenants" class="mt-2 rounded-lg bg-white/70 p-2 text-xs text-orange-800">正在加载工作空间信息...</p>
          <p v-if="tenantError" class="mt-2 rounded-lg bg-red-50 p-2 text-xs text-red-700">{{ tenantError }}</p>
        </div>
        <p v-else-if="tenantError" class="mt-6 rounded-lg bg-red-50 p-2 text-xs text-red-700">{{ tenantError }}</p>
        <nav class="mt-7 grid gap-2">
          <NuxtLink
            v-for="item in nav"
            :key="item[1]"
            :to="item[1]"
            class="rounded-lg px-3 py-2 font-semibold text-stone-700 hover:bg-orange-100 hover:text-orange-900"
            @click="closeMobileNav"
          >
            {{ item[0] }}
          </NuxtLink>
        </nav>
        <div class="mt-7 rounded-xl bg-stone-950 p-4 text-stone-50">
          <p class="text-xs text-stone-400">当前用户</p>
          <p class="font-bold">{{ auth.user.email }}</p>
          <button class="mt-3 rounded-lg bg-white px-3 py-1 text-sm font-bold text-stone-950" @click="handleLogout">登出</button>
        </div>
      </aside>
      <main class="rounded-xl border border-white/70 bg-white/85 p-5 shadow-xl shadow-stone-900/10 backdrop-blur sm:p-8">
        <slot />
      </main>
    </div>
  </div>
</template>
