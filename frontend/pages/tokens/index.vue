<script setup lang="ts">
const auth = useAuthStore()
const { request } = useApi()

type TokenMetadata = {
  id: string
  user_id: string
  name?: string
  org_id?: string
  project_id?: string
  agent_id?: string
  token_prefix: string
  scopes: string[]
  expires_at: string
  revoked_at?: string | null
  status: 'active' | 'revoked'
}

type TokenListResponse = { tokens: TokenMetadata[] }
type TokenCreateResponse = { token: string; token_metadata: TokenMetadata }

const loading = ref(false)
const creatingPAT = ref(false)
const error = ref('')
const oneTimeToken = ref('')
const oneTimeTokenKind = ref('')
const patTokens = ref<TokenMetadata[]>([])
const defaultMCPTokenName = 'memory-os-mcp'
const defaultMCPTokenScopes = ['memory:read', 'memory:write']
const defaultMCPTokenTTLSeconds = 30 * 24 * 60 * 60

const sessionPATTokens = computed(() => patTokens.value.filter((token) => token.name?.startsWith('password-login-')))
const manualPATTokens = computed(() => patTokens.value.filter((token) => !token.name?.startsWith('password-login-')))

function formatDate(value?: string | null) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

function showOneTimeToken(kind: string, token: string) {
  oneTimeTokenKind.value = kind
  oneTimeToken.value = token
}

function hideToken() {
  oneTimeToken.value = ''
  oneTimeTokenKind.value = ''
}

async function loadTokens() {
  if (!auth.isAuthenticated) return
  loading.value = true
  error.value = ''
  try {
    const patResponse = await request<TokenListResponse>('/memory/tokens/pat/list', {
      method: 'POST',
      body: { status: '', limit: 50 }
    })
    patTokens.value = patResponse.tokens || []
  } catch (err: any) {
    error.value = err.message || 'Token 列表加载失败'
  } finally {
    loading.value = false
  }
}

async function createPAT() {
  creatingPAT.value = true
  error.value = ''
  hideToken()
  try {
    const response = await request<TokenCreateResponse>('/memory/tokens/pat/create', {
      method: 'POST',
      body: {
        name: defaultMCPTokenName,
        scopes: defaultMCPTokenScopes,
        ttl_seconds: defaultMCPTokenTTLSeconds
      }
    })
    showOneTimeToken('MCP Token', response.token)
    await loadTokens()
  } catch (err: any) {
    error.value = err.message || 'PAT 创建失败'
  } finally {
    creatingPAT.value = false
  }
}

async function revokePAT(tokenID: string) {
  error.value = ''
  try {
    await request('/memory/tokens/pat/revoke', {
      method: 'POST',
      body: { token_id: tokenID }
    })
    await loadTokens()
  } catch (err: any) {
    error.value = err.message || 'PAT 撤销失败'
  }
}

onMounted(async () => {
  auth.initFromStorage()
  await loadTokens()
})

watch(() => auth.isAuthenticated, () => loadTokens())
</script>

<template>
  <AppShell>
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div>
        <h2 class="text-3xl font-black">Token 管理</h2>
        <p class="mt-2 text-stone-600">
          创建一个通用 MCP Token，配到 Codex、Claude Code、opencode、Hermes 等所有 Agent。项目由工作目录 / Git 自动识别，Agent 只作为来源 metadata 记录。明文 token 只在创建响应中显示一次。
        </p>
      </div>
      <button class="rounded-2xl border px-4 py-2 font-bold" @click="loadTokens">刷新</button>
    </div>

    <p v-if="error" class="mt-4 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>

    <div v-if="oneTimeToken" class="mt-4 rounded-3xl border border-amber-200 bg-amber-50 p-5">
      <p class="font-black text-amber-900">{{ oneTimeTokenKind }} 一次性明文</p>
      <p class="mt-1 text-sm text-amber-800">关闭后不可再查看。不要把它写入日志、文档、截图或归档。</p>
      <code class="mt-3 block break-all rounded-2xl bg-white p-3 text-sm">{{ oneTimeToken }}</code>
      <button class="mt-3 rounded-2xl bg-amber-900 px-4 py-2 font-bold text-white" @click="hideToken">我已保存，立即隐藏</button>
    </div>

    <div class="mt-6 grid gap-4 xl:grid-cols-[1fr_0.9fr]">
      <section class="rounded-3xl border bg-white p-5">
        <h3 class="font-black">创建通用 MCP Token</h3>
        <p class="mt-2 text-sm text-stone-600">
          这个 token 代表当前用户，不绑定项目、不绑定 Agent。把它配置到各个 Agent 的 Memory OS MCP 后，就能共享同一套记忆和知识库。
        </p>
        <p class="mt-4 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">
          系统会自动使用推荐权限和默认有效期；你不需要理解 scope、TTL 或 token 名称。
        </p>
        <button class="mt-4 w-full rounded-2xl bg-stone-950 px-4 py-3 font-black text-white" :disabled="creatingPAT" @click="createPAT">
          {{ creatingPAT ? '创建中...' : '一键创建通用 MCP Token' }}
        </button>
      </section>

      <section class="rounded-3xl border bg-white p-5">
        <h3 class="font-black">推荐使用方式</h3>
        <div class="mt-3 space-y-3 text-sm text-stone-700">
          <p class="rounded-2xl bg-stone-50 p-3">1. 在这里创建一个通用 MCP Token。</p>
          <p class="rounded-2xl bg-stone-50 p-3">2. 把同一个 token 配到所有 Agent 的 Memory OS MCP。</p>
          <p class="rounded-2xl bg-stone-50 p-3">3. Agent 在不同目录工作时，后端按目录 / Git 自动归类项目。</p>
        </div>
        <p class="mt-4 rounded-2xl bg-orange-50 p-3 text-xs text-orange-800">
          高级的项目绑定 Adapter Token 接口仍保留给兼容场景，但日常使用不需要按项目或 Agent 创建多个 token。
        </p>
      </section>
    </div>

    <div class="mt-6 grid gap-4">
      <section class="rounded-3xl border bg-white p-5">
        <h3 class="font-black">通用 MCP Token Metadata</h3>
        <p v-if="loading" class="mt-3 text-sm text-stone-500">正在加载...</p>
        <p v-else-if="manualPATTokens.length === 0" class="mt-3 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">当前用户暂无通用 MCP Token metadata；登录产生的短期会话凭证在下方单独展示。</p>
        <div v-for="token in manualPATTokens" :key="token.id" class="mt-3 rounded-2xl bg-stone-50 p-4 text-sm">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div>
              <p class="font-black">{{ token.name || token.id }}</p>
              <p class="mt-1 break-all text-stone-600">{{ token.id }} · {{ token.status }}</p>
            </div>
            <button v-if="token.status === 'active'" class="rounded-full bg-red-900 px-3 py-1 text-xs font-bold text-white" @click="revokePAT(token.id)">撤销</button>
          </div>
          <p class="mt-2 text-stone-600">prefix: {{ token.token_prefix }} · expires: {{ formatDate(token.expires_at) }}</p>
          <p class="mt-1 break-all text-stone-600">scopes: {{ token.scopes?.join(', ') || '-' }}</p>
        </div>
        <div class="mt-5 rounded-2xl border border-orange-100 bg-orange-50 p-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h4 class="font-black text-orange-950">登录会话 PAT</h4>
              <p class="mt-1 text-xs text-orange-800">这些是密码登录自动签发的短期会话凭证，保留 metadata 用于安全追踪；撤销后仍会保留记录。</p>
            </div>
            <NuxtLink class="rounded-full bg-orange-900 px-3 py-1 text-xs font-bold text-white" to="/logs">查看日志</NuxtLink>
          </div>
          <p v-if="sessionPATTokens.length === 0" class="mt-3 text-sm text-orange-800">当前没有登录会话 PAT metadata。</p>
          <div v-for="token in sessionPATTokens" :key="token.id" class="mt-3 rounded-2xl bg-white p-3 text-sm">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div>
                <p class="font-black">{{ token.name || token.id }}</p>
                <p class="mt-1 break-all text-stone-600">{{ token.id }} · {{ token.status }}</p>
              </div>
              <button v-if="token.status === 'active'" class="rounded-full bg-red-900 px-3 py-1 text-xs font-bold text-white" @click="revokePAT(token.id)">撤销</button>
            </div>
            <p class="mt-2 text-stone-600">prefix: {{ token.token_prefix }} · expires: {{ formatDate(token.expires_at) }}</p>
            <p class="mt-1 break-all text-stone-600">scopes: {{ token.scopes?.join(', ') || '-' }}</p>
          </div>
        </div>
      </section>
    </div>
  </AppShell>
</template>
