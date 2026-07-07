<script setup lang="ts">
const auth = useAuthStore()
const context = useContextStore()
const { request } = useApi()

type TokenMetadata = {
  id: string
  name?: string
  status: 'active' | 'revoked'
  token_prefix: string
  expires_at: string
}
type TokenListResponse = { tokens: TokenMetadata[] }
type TokenCreateResponse = { token: string; setup_command: string }

const creating = ref(false)
const loadingTokens = ref(false)
const error = ref('')
const setupCommand = ref('')
const copied = ref(false)
const tokens = ref<TokenMetadata[]>([])

const selectedOrg = computed(() => context.orgs.find((org) => org.org_id === context.orgId))
const selectedProject = computed(() => context.projects.find((project) => project.project_id === context.projectId))
const activeMCPToken = computed(() => tokens.value.find((token) => token.status === 'active' && token.name === 'memory-os-mcp'))
const hasProject = computed(() => Boolean(context.orgId && context.projectId))

async function loadTokens() {
  if (!auth.isAuthenticated) return
  loadingTokens.value = true
  error.value = ''
  try {
    const response = await request<TokenListResponse>('/memory/tokens/pat/list', {
      method: 'POST',
      body: { status: 'active', limit: 50 }
    })
    tokens.value = response.tokens || []
  } catch (err: any) {
    error.value = err.message || 'Token metadata 加载失败'
  } finally {
    loadingTokens.value = false
  }
}

async function createInstallCommand() {
  creating.value = true
  error.value = ''
  copied.value = false
  try {
    const response = await request<TokenCreateResponse>('/memory/tokens/pat/create', {
      method: 'POST',
      body: {
        name: 'memory-os-mcp',
        scopes: ['memory:read', 'memory:write'],
        ttl_seconds: 30 * 24 * 60 * 60
      }
    })
    setupCommand.value = response.setup_command
    await loadTokens()
  } catch (err: any) {
    error.value = err.message || '安装命令生成失败'
  } finally {
    creating.value = false
  }
}

async function copyCommand() {
  if (!setupCommand.value) return
  await navigator.clipboard.writeText(setupCommand.value)
  copied.value = true
}

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  await loadTokens()
})
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <p class="text-xs font-black uppercase tracking-[0.35em] text-orange-800">Onboarding</p>
        <h2 class="mt-2 text-3xl font-black">接入向导</h2>
        <p class="mt-2 max-w-3xl text-stone-600">按顺序完成 MCP Token、安装命令、项目确认和检索验证。</p>
      </div>
      <NuxtLink class="rounded-2xl border bg-white px-4 py-2 font-bold" to="/tokens">Token 管理</NuxtLink>
    </div>

    <p v-if="error" class="mt-4 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>

    <section class="mt-6 grid gap-4">
      <article class="rounded-3xl border bg-white p-5">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <p class="text-xs font-black uppercase tracking-widest text-orange-800">Step 1</p>
            <h3 class="mt-2 text-xl font-black">生成本机安装命令</h3>
            <p class="mt-2 text-sm text-stone-600">命令会配置 MCP、hook 和本机 Secret 设备密钥；旧 token 明文不会再次展示。</p>
            <p v-if="activeMCPToken" class="mt-3 rounded-2xl bg-emerald-50 p-3 text-sm text-emerald-800">
              已有 active 的 memory-os-mcp metadata：{{ activeMCPToken.token_prefix }}。如需重新安装，可重新生成命令。
            </p>
          </div>
          <button class="rounded-2xl bg-stone-950 px-4 py-3 font-black text-white disabled:cursor-not-allowed disabled:bg-stone-300" :disabled="creating || loadingTokens" @click="createInstallCommand">
            {{ creating ? '生成中...' : '生成安装命令' }}
          </button>
        </div>
      </article>

      <article class="rounded-3xl border bg-white p-5">
        <p class="text-xs font-black uppercase tracking-widest text-orange-800">Step 2</p>
        <h3 class="mt-2 text-xl font-black">复制命令到本机终端执行</h3>
        <p v-if="!setupCommand" class="mt-3 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">先生成安装命令。页面不会长期保存 token 明文。</p>
        <div v-else class="mt-3 rounded-2xl bg-stone-950 p-4 text-stone-50">
          <code class="block break-all text-sm">{{ setupCommand }}</code>
          <button class="mt-4 rounded-2xl bg-white px-4 py-2 font-black text-stone-950" @click="copyCommand">{{ copied ? '已复制' : '复制命令' }}</button>
        </div>
      </article>

      <article class="rounded-3xl border bg-white p-5">
        <p class="text-xs font-black uppercase tracking-widest text-orange-800">Step 3</p>
        <h3 class="mt-2 text-xl font-black">确认记忆上下文</h3>
        <div class="mt-4 grid gap-3 md:grid-cols-3">
          <div class="rounded-2xl bg-stone-50 p-4 text-sm">
            <p class="font-black text-stone-500">组织</p>
            <p class="mt-1 font-bold">{{ selectedOrg?.name || '未选择' }}</p>
          </div>
          <div class="rounded-2xl bg-stone-50 p-4 text-sm">
            <p class="font-black text-stone-500">项目</p>
            <p class="mt-1 font-bold">{{ selectedProject?.name || '未选择' }}</p>
          </div>
          <div class="rounded-2xl bg-stone-50 p-4 text-sm">
            <p class="font-black text-stone-500">Agent</p>
            <p class="mt-1 font-bold">{{ context.agentId }}</p>
          </div>
        </div>
        <NuxtLink v-if="!hasProject" class="mt-4 inline-flex rounded-2xl bg-orange-600 px-4 py-2 font-black text-white" to="/projects">选择项目</NuxtLink>
      </article>

      <article class="rounded-3xl border bg-white p-5">
        <p class="text-xs font-black uppercase tracking-widest text-orange-800">Step 4</p>
        <h3 class="mt-2 text-xl font-black">验证写入和检索</h3>
        <div class="mt-4 flex flex-wrap gap-3">
          <NuxtLink class="rounded-2xl bg-stone-950 px-4 py-2 font-black text-white" to="/diagnostics">查看写入诊断</NuxtLink>
          <NuxtLink class="rounded-2xl border bg-white px-4 py-2 font-black text-stone-950" to="/search">去检索验证</NuxtLink>
        </div>
      </article>
    </section>
  </AppShell>
</template>
