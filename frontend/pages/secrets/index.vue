<script setup lang="ts">
const auth = useAuthStore()
const context = useContextStore()
const { request } = useApi()

type SecretMetadata = {
  secret_ref: string
  owner_user_id: string
  org_id: string
  project_id: string
  name: string
  env_name?: string
  site?: string
  purpose?: string
  expires_at?: string
  status: 'active' | 'disabled'
  current_version: number
}

type SecretListResponse = { secrets: SecretMetadata[] }

const loading = ref(false)
const disabling = ref('')
const error = ref('')
const success = ref('')
const statusFilter = ref<'active' | 'disabled'>('active')
const secrets = ref<SecretMetadata[]>([])

const hasProjectContext = computed(() => Boolean(context.orgId && context.projectId))

async function loadSecrets() {
  if (!auth.isAuthenticated || !hasProjectContext.value) {
    secrets.value = []
    return
  }
  loading.value = true
  error.value = ''
  try {
    const response = await request<SecretListResponse>('/memory/secrets/list', {
      method: 'POST',
      body: {
        org_id: context.orgId,
        project_id: context.projectId,
        status: statusFilter.value,
        limit: 50
      }
    })
    secrets.value = response.secrets || []
  } catch (err: any) {
    error.value = err.message || 'Secret metadata 加载失败'
  } finally {
    loading.value = false
  }
}

async function disableSecret(secretRef: string) {
  disabling.value = secretRef
  error.value = ''
  success.value = ''
  try {
    const metadata = await request<SecretMetadata>('/memory/secrets/disable', {
      method: 'POST',
      body: {
        secret_ref: secretRef,
        org_id: context.orgId,
        project_id: context.projectId
      }
    })
    success.value = `已禁用 ${metadata.secret_ref}，并切换到 disabled 筛选。`
    if (statusFilter.value !== 'disabled') {
      statusFilter.value = 'disabled'
    } else {
      await loadSecrets()
    }
  } catch (err: any) {
    error.value = err.message || 'Secret 禁用失败'
  } finally {
    disabling.value = ''
  }
}

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  await loadSecrets()
})

watch(() => [context.orgId, context.projectId, statusFilter.value], () => loadSecrets())
</script>

<template>
  <AppShell>
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div>
        <h2 class="text-3xl font-black">Secret 管理</h2>
        <p class="mt-2 text-stone-600">服务端只保存密文和元信息。创建 secret 请通过本机 MCP，Web 端不接收明文。</p>
      </div>
      <button class="rounded-2xl border px-4 py-2 font-bold" @click="loadSecrets">刷新</button>
    </div>

    <p v-if="error" class="mt-4 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <p v-if="success" class="mt-4 rounded-2xl bg-emerald-50 p-3 text-sm text-emerald-800">{{ success }}</p>

    <div class="mt-6 grid gap-4 lg:grid-cols-[1fr_1fr]">
      <section class="rounded-3xl border bg-white p-5">
        <h3 class="font-black">如何创建 Secret</h3>
        <p class="mt-2 text-sm text-stone-600">明文永远不进入服务端。请在本机通过 MCP 工具创建：</p>
        <ol class="mt-3 space-y-2 text-sm text-stone-600">
          <li>1. 本机 MCP 工具 <code class="rounded bg-stone-100 px-1">secret_create_local</code> 传入明文与用途。</li>
          <li>2. 本机用设备密钥（<code class="rounded bg-stone-100 px-1">~/.config/memory-os/secret-device-key.json</code>）加密。</li>
          <li>3. 只有密文和元信息上传到服务端，明文留在本机。</li>
        </ol>
        <p class="mt-3 rounded-2xl bg-amber-50 p-3 text-xs text-amber-800">Web 端只做 metadata 查看与禁用，不提供明文创建入口，也无法取回明文。</p>
      </section>

      <section class="rounded-3xl border bg-white p-5">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h3 class="font-black">Secret Metadata</h3>
          <select v-model="statusFilter" class="rounded-2xl border px-3 py-2 text-sm">
            <option value="active">active</option>
            <option value="disabled">disabled</option>
          </select>
        </div>
        <p v-if="!hasProjectContext" class="mt-3 rounded-2xl bg-amber-50 p-4 text-sm text-amber-800">请先创建并选择真实组织 / 项目。</p>
        <p v-else-if="loading" class="mt-3 text-sm text-stone-500">正在加载...</p>
        <p v-else-if="secrets.length === 0" class="mt-3 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">当前项目暂无 {{ statusFilter }} Secret metadata。</p>
        <div v-for="secret in secrets" :key="secret.secret_ref" class="mt-3 rounded-2xl bg-stone-50 p-4 text-sm">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div>
              <p class="font-black">{{ secret.name }}</p>
              <p class="mt-1 break-all text-stone-600">{{ secret.secret_ref }} · {{ secret.status }}</p>
            </div>
            <button
              v-if="secret.status === 'active'"
              class="rounded-full bg-red-900 px-3 py-1 text-xs font-bold text-white"
              :disabled="disabling === secret.secret_ref"
              @click="disableSecret(secret.secret_ref)"
            >
              {{ disabling === secret.secret_ref ? '禁用中...' : '禁用' }}
            </button>
          </div>
          <p v-if="secret.env_name" class="mt-2 text-stone-600">env: {{ secret.env_name }}</p>
          <p v-if="secret.site" class="mt-1 text-stone-600">site: {{ secret.site }}</p>
          <p v-if="secret.purpose" class="mt-1 text-stone-600">purpose: {{ secret.purpose }}</p>
          <p class="mt-2 break-all text-stone-600">owner: {{ secret.owner_user_id }}</p>
          <p class="mt-1 break-all text-stone-600">project: {{ secret.project_id }}</p>
          <p class="mt-1 text-stone-600">version: {{ secret.current_version }}</p>
        </div>
      </section>
    </div>
  </AppShell>
</template>
