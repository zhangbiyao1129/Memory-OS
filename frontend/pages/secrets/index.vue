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
  status: 'active' | 'disabled'
  current_version: number
}

type SecretListResponse = { secrets: SecretMetadata[] }

const loading = ref(false)
const creating = ref(false)
const disabling = ref('')
const error = ref('')
const success = ref('')
const secretName = ref('MEMORY_OS_TEST_SECRET')
const transientSecret = ref('')
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

async function createSecret() {
  if (!hasProjectContext.value) {
    error.value = '请先选择真实组织和项目'
    return
  }
  creating.value = true
  error.value = ''
  success.value = ''
  try {
    const metadata = await request<SecretMetadata>('/memory/secrets/create', {
      method: 'POST',
      body: {
        org_id: context.orgId,
        project_id: context.projectId,
        name: secretName.value,
        plaintext: transientSecret.value
      }
    })
    success.value = `已创建 ${metadata.secret_ref}，页面只保留 metadata。`
    transientSecret.value = ''
    if (statusFilter.value !== 'active') {
      statusFilter.value = 'active'
    } else {
      await loadSecrets()
    }
  } catch (err: any) {
    error.value = err.message || 'Secret 创建失败'
  } finally {
    creating.value = false
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
        <h2 class="text-3xl font-black">Secret Vault 管理</h2>
        <p class="mt-2 text-stone-600">接入真实 Secret Vault API。明文只在提交前短暂存在，列表和响应只展示 metadata。</p>
      </div>
      <button class="rounded-2xl border px-4 py-2 font-bold" @click="loadSecrets">刷新</button>
    </div>

    <p v-if="error" class="mt-4 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <p v-if="success" class="mt-4 rounded-2xl bg-emerald-50 p-3 text-sm text-emerald-800">{{ success }}</p>

    <div class="mt-6 grid gap-4 lg:grid-cols-[1fr_1fr]">
      <section class="rounded-3xl border bg-white p-5">
        <h3 class="font-black">创建 Secret</h3>
        <p class="mt-2 text-sm text-stone-600">当前组织 / 项目：</p>
        <div class="mt-2 rounded-2xl bg-stone-50 p-3 text-xs text-stone-600">
          <p>组织：{{ context.orgId || '未选择' }}</p>
          <p>项目：{{ context.projectId || '未选择' }}</p>
        </div>
        <input v-model="secretName" class="mt-4 w-full rounded-2xl border px-4 py-3" placeholder="Secret 名称">
        <input v-model="transientSecret" class="mt-3 w-full rounded-2xl border px-4 py-3" type="password" autocomplete="off" placeholder="明文只在提交前短暂存在">
        <SecretValueGuard class="mt-3" :value="transientSecret" />
        <button class="mt-4 w-full rounded-2xl bg-stone-950 px-4 py-3 font-black text-white" :disabled="creating || !hasProjectContext" @click="createSecret">
          {{ creating ? '创建中...' : '创建真实 Secret' }}
        </button>
        <p class="mt-3 text-xs text-stone-500">提交成功后会立即清空明文字段；请不要在名称中写入敏感值。</p>
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
          <p class="mt-2 break-all text-stone-600">owner: {{ secret.owner_user_id }}</p>
          <p class="mt-1 break-all text-stone-600">project: {{ secret.project_id }}</p>
          <p class="mt-1 text-stone-600">version: {{ secret.current_version }}</p>
        </div>
      </section>
    </div>
  </AppShell>
</template>
