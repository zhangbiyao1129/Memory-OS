<script setup lang="ts">
const auth = useAuthStore()
const context = useContextStore()
const { request } = useApi()
const loading = ref(false)
const creating = ref(false)
const deletingOrgID = ref('')
const editingOrgID = ref('')
const editName = ref('')
const editSlug = ref('')
const savingOrgID = ref('')
const error = ref('')
const name = ref('')
const slug = ref('')

type OrgListResponse = { orgs: TenantOrg[] }
type OrgCreateResponse = { org: TenantOrg }
type OrgEditResponse = { org: TenantOrg }
type OrgDeleteResponse = { org: TenantOrg }

async function loadOrgs() {
  if (!auth.isAuthenticated) return
  loading.value = true
  error.value = ''
  try {
    const response = await request<OrgListResponse>('/memory/tenant/orgs/list', { method: 'POST', body: {} })
    context.setOrgs(response.orgs || [])
  } catch (err: any) {
    error.value = err.message || '组织加载失败'
  } finally {
    loading.value = false
  }
}

async function createOrg() {
  creating.value = true
  error.value = ''
  try {
    const response = await request<OrgCreateResponse>('/memory/tenant/orgs/create', {
      method: 'POST',
      body: { name: name.value, slug: slug.value }
    })
    context.setOrg(response.org.org_id)
    name.value = ''
    slug.value = ''
    await loadOrgs()
  } catch (err: any) {
    error.value = err.message || '组织创建失败'
  } finally {
    creating.value = false
  }
}

function startEditOrg(org: TenantOrg) {
  editingOrgID.value = org.org_id
  editName.value = org.name
  editSlug.value = org.slug
}

function cancelEditOrg() {
  editingOrgID.value = ''
  editName.value = ''
  editSlug.value = ''
}

async function editOrg(org: TenantOrg) {
  savingOrgID.value = org.org_id
  error.value = ''
  try {
    await request<OrgEditResponse>('/memory/tenant/orgs/edit', {
      method: 'POST',
      body: { org_id: org.org_id, name: editName.value, slug: editSlug.value }
    })
    cancelEditOrg()
    await loadOrgs()
  } catch (err: any) {
    error.value = err.message || '组织保存失败'
  } finally {
    savingOrgID.value = ''
  }
}

async function deleteOrg(org: TenantOrg) {
  deletingOrgID.value = org.org_id
  error.value = ''
  try {
    await request<OrgDeleteResponse>('/memory/tenant/orgs/delete', {
      method: 'POST',
      body: { org_id: org.org_id }
    })
    if (context.orgId === org.org_id) {
      context.setOrg('')
      context.setProjects([])
    }
    await loadOrgs()
  } catch (err: any) {
    error.value = err.message || '组织删除失败'
  } finally {
    deletingOrgID.value = ''
  }
}

onMounted(async () => {
  auth.initFromStorage()
  context.initFromStorage()
  await loadOrgs()
})
</script>

<template>
  <AppShell>
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div>
        <h2 class="text-3xl font-black">组织管理</h2>
        <p class="mt-2 text-stone-600">这里读取和创建真实 PostgreSQL 租户组织，不再展示静态假数据。</p>
      </div>
      <button class="rounded-2xl border px-4 py-2 font-bold" @click="loadOrgs">刷新</button>
    </div>
    <p v-if="error" class="mt-4 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <div class="mt-6 grid gap-4 lg:grid-cols-[1fr_22rem]">
      <section class="rounded-3xl border bg-white p-5">
        <h3 class="font-black">真实组织列表</h3>
        <p v-if="loading" class="mt-3 text-sm text-stone-500">正在加载...</p>
        <p v-else-if="context.orgs.length === 0" class="mt-3 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">当前 PAT 暂无可见组织。</p>
        <div v-for="org in context.orgs" :key="org.org_id" class="mt-3 rounded-2xl bg-stone-50 p-4 text-sm">
          <template v-if="editingOrgID === org.org_id">
            <input v-model="editName" class="w-full rounded-2xl border bg-white px-3 py-2 font-bold" placeholder="组织名称">
            <input v-model="editSlug" class="mt-2 w-full rounded-2xl border bg-white px-3 py-2" placeholder="唯一 slug">
          </template>
          <template v-else>
            <p class="font-black">{{ org.name }}</p>
            <p class="mt-1 break-all text-stone-600">{{ org.org_id }} · {{ org.slug }} · {{ org.status || 'active' }}</p>
          </template>
          <div class="mt-3 flex flex-wrap gap-2">
            <button class="rounded-full bg-stone-950 px-3 py-1 text-xs font-bold text-white" @click="context.setOrg(org.org_id)">设为当前组织</button>
            <button v-if="editingOrgID !== org.org_id" class="rounded-full bg-amber-100 px-3 py-1 text-xs font-bold text-amber-950" @click="startEditOrg(org)">编辑组织</button>
            <button v-if="editingOrgID === org.org_id" class="rounded-full bg-emerald-100 px-3 py-1 text-xs font-bold text-emerald-950 disabled:cursor-not-allowed disabled:bg-stone-100 disabled:text-stone-500" :disabled="savingOrgID === org.org_id" @click="editOrg(org)">
              {{ savingOrgID === org.org_id ? '保存中...' : '保存组织' }}
            </button>
            <button v-if="editingOrgID === org.org_id" class="rounded-full bg-white px-3 py-1 text-xs font-bold text-stone-700" @click="cancelEditOrg">取消</button>
            <button class="rounded-full bg-red-100 px-3 py-1 text-xs font-bold text-red-950 disabled:cursor-not-allowed disabled:bg-stone-100 disabled:text-stone-500" :disabled="deletingOrgID === org.org_id" @click="deleteOrg(org)">
              {{ deletingOrgID === org.org_id ? '删除中...' : '删除组织' }}
            </button>
          </div>
        </div>
      </section>
      <section class="rounded-3xl border bg-white p-5">
        <h3 class="font-black">创建组织</h3>
        <input v-model="name" class="mt-4 w-full rounded-2xl border px-4 py-3" placeholder="组织名称">
        <input v-model="slug" class="mt-3 w-full rounded-2xl border px-4 py-3" placeholder="唯一 slug">
        <button class="mt-4 w-full rounded-2xl bg-stone-950 px-4 py-3 font-black text-white" :disabled="creating" @click="createOrg">
          {{ creating ? '创建中...' : '创建真实组织' }}
        </button>
      </section>
    </div>
  </AppShell>
</template>
