<script setup lang="ts">
import { buildSecretCommandTemplates } from '~/utils/memoryUx'

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
const templateName = ref('gitlab-ee-pat')
const templateEnvName = ref('GITLAB_TOKEN')
const templateSite = ref('gitlab-ee')
const templatePurpose = ref('GitLab API')
const copiedTemplate = ref('')

const hasProjectContext = computed(() => Boolean(context.orgId && context.projectId))
const templates = computed(() => buildSecretCommandTemplates({
  name: templateName.value,
  envName: templateEnvName.value,
  site: templateSite.value,
  purpose: templatePurpose.value
}))

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

async function copyTemplate(key: string) {
  const command = templates.value[key as keyof typeof templates.value]
  if (!command) return
  await navigator.clipboard.writeText(command)
  copiedTemplate.value = key
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
        <p class="mt-2 text-sm text-stone-600">Web 不接收明文。复制模板后，在本机 MCP/终端里输入真实值。</p>
        <div class="mt-4 grid gap-3 sm:grid-cols-2">
          <label class="text-sm font-bold text-stone-600">
            名称
            <input v-model="templateName" class="mt-2 w-full rounded-2xl border px-3 py-2 font-normal text-stone-950">
          </label>
          <label class="text-sm font-bold text-stone-600">
            环境变量
            <input v-model="templateEnvName" class="mt-2 w-full rounded-2xl border px-3 py-2 font-normal text-stone-950">
          </label>
          <label class="text-sm font-bold text-stone-600">
            站点
            <input v-model="templateSite" class="mt-2 w-full rounded-2xl border px-3 py-2 font-normal text-stone-950">
          </label>
          <label class="text-sm font-bold text-stone-600">
            用途
            <input v-model="templatePurpose" class="mt-2 w-full rounded-2xl border px-3 py-2 font-normal text-stone-950">
          </label>
        </div>
        <div class="mt-4 grid gap-3">
          <div v-for="(command, key) in templates" :key="key" class="rounded-2xl bg-stone-50 p-3">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <code class="break-all text-sm text-stone-800">{{ command }}</code>
              <button class="rounded-xl bg-stone-950 px-3 py-1 text-xs font-black text-white" @click="copyTemplate(String(key))">
                {{ copiedTemplate === key ? '已复制' : '复制' }}
              </button>
            </div>
          </div>
        </div>
        <p class="mt-3 rounded-2xl bg-amber-50 p-3 text-xs text-amber-800">服务端只保存密文和元信息；明文留在本机设备密钥保护范围内。</p>
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
          <details class="mt-3 rounded-2xl bg-white p-3">
            <summary class="cursor-pointer text-xs font-black uppercase tracking-widest text-stone-500">技术信息</summary>
            <dl class="mt-3 grid gap-2 text-xs text-stone-600 sm:grid-cols-[7rem_1fr]">
              <dt class="font-black text-stone-500">owner</dt>
              <dd class="break-all">{{ secret.owner_user_id }}</dd>
              <dt class="font-black text-stone-500">project</dt>
              <dd class="break-all">{{ secret.project_id }}</dd>
              <dt class="font-black text-stone-500">version</dt>
              <dd>{{ secret.current_version }}</dd>
            </dl>
          </details>
        </div>
      </section>
    </div>
  </AppShell>
</template>
