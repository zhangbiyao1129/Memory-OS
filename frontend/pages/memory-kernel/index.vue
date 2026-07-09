<script setup lang="ts">
const auth = useAuthStore()
const context = useContextStore()
const { request } = useApi()

type MemoryUnit = {
  unit_id: string
  unit_type: string
  content: string
  applies_when: string
  agent_should: string
  status: string
  confidence: number
  trust_score: number
  risk_level: string
  superseded_by: string
  created_at: string
  updated_at: string
}

type GovernanceRun = {
  run_id: string
  status: string
  created_units: number
  superseded_units: number
  stale_candidates: number
  demoted_hot_memories: number
  ci_cases_created: number
  ci_cases_passed: number
  summary: string
  started_at: string
  completed_at: string | null
}

type ContextPack = {
  request_id: string
  intent: string
  context: string
  units: MemoryUnit[]
  warnings: string[]
}

type CICase = {
  case_id: string
  question: string
  must_include: string[]
  must_not_include: string[]
  status: string
}

const loading = ref(false)
const error = ref('')
const units = ref<MemoryUnit[]>([])
const runs = ref<GovernanceRun[]>([])
const ciCases = ref<CICase[]>([])
const governing = ref(false)
const governanceResult = ref('')

// 上下文包
const contextQuery = ref('')
const contextPack = ref<ContextPack | null>(null)
const buildingPack = ref(false)

async function loadUnits() {
  if (!context.orgId || !context.projectId) return
  loading.value = true
  error.value = ''
  try {
    const res = await request<{ units: MemoryUnit[] }>('/memory/kernel/units/list', {
      method: 'POST',
      body: { org_id: context.orgId, project_id: context.projectId, status: 'current', limit: 100 }
    })
    units.value = res.units || []
  } catch (err: any) {
    error.value = err.message || '加载可信事实失败'
  } finally {
    loading.value = false
  }
}

async function runGovernance() {
  if (!context.orgId || !context.projectId) return
  governing.value = true
  governanceResult.value = ''
  try {
    const res = await request<{ run_id: string; status: string; created_units: number; summary: string }>(
      '/memory/kernel/governance/run',
      { method: 'POST', body: { org_id: context.orgId, project_id: context.projectId, trigger_type: 'manual' } }
    )
    governanceResult.value = `治理完成: ${res.summary || '无摘要'}，创建 ${res.created_units} 条可信事实`
    await loadUnits()
  } catch (err: any) {
    governanceResult.value = `治理失败: ${err.message || '未知错误'}`
  } finally {
    governing.value = false
  }
}

async function buildContextPack() {
  if (!context.orgId || !context.projectId || !contextQuery.value.trim()) return
  buildingPack.value = true
  try {
    contextPack.value = await request<ContextPack>('/memory/kernel/context-pack', {
      method: 'POST',
      body: { org_id: context.orgId, project_id: context.projectId, query: contextQuery.value }
    })
  } catch (err: any) {
    error.value = err.message || '构建上下文包失败'
  } finally {
    buildingPack.value = false
  }
}

function unitTypeLabel(type: string): string {
  const labels: Record<string, string> = {
    fact: '事实', preference: '偏好', procedure: '步骤', decision: '决策',
    risk: '风险', task: '任务', environment: '环境', evidence: '证据'
  }
  return labels[type] || type
}

function statusLabel(status: string): string {
  const labels: Record<string, string> = {
    current: '当前', stale: '过期', superseded: '已覆盖', historical: '历史', needs_review: '待确认'
  }
  return labels[status] || status
}

onMounted(() => { void loadUnits() })
</script>

<template>
  <div class="max-w-5xl mx-auto p-6 space-y-8">
    <h1 class="text-2xl font-bold">记忆体检</h1>

    <div v-if="error" class="bg-red-50 border border-red-200 rounded p-3 text-red-700 text-sm">{{ error }}</div>

    <!-- 顶部指标 -->
    <div class="grid grid-cols-2 md:grid-cols-4 gap-4">
      <div class="bg-white border rounded-lg p-4 text-center">
        <div class="text-2xl font-bold text-green-600">{{ units.length }}</div>
        <div class="text-sm text-gray-500">可信事实</div>
      </div>
      <div class="bg-white border rounded-lg p-4 text-center">
        <div class="text-2xl font-bold text-yellow-600">{{ units.filter(u => u.status === 'needs_review').length }}</div>
        <div class="text-sm text-gray-500">需确认</div>
      </div>
      <div class="bg-white border rounded-lg p-4 text-center">
        <div class="text-2xl font-bold text-orange-600">{{ units.filter(u => u.status === 'stale' || u.status === 'superseded').length }}</div>
        <div class="text-sm text-gray-500">过期/冲突</div>
      </div>
      <div class="bg-white border rounded-lg p-4 text-center">
        <div class="text-2xl font-bold text-blue-600">{{ ciCases.length }}</div>
        <div class="text-sm text-gray-500">Memory CI</div>
      </div>
    </div>

    <!-- 治理操作 -->
    <div class="bg-white border rounded-lg p-4">
      <div class="flex items-center gap-3">
        <button
          :disabled="governing || !context.orgId"
          class="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
          @click="runGovernance"
        >
          {{ governing ? '治理中...' : '运行记忆体检' }}
        </button>
        <span v-if="governanceResult" class="text-sm text-gray-600">{{ governanceResult }}</span>
      </div>
    </div>

    <!-- 上下文包预览 -->
    <div class="bg-white border rounded-lg p-4">
      <h2 class="text-lg font-semibold mb-3">上下文包</h2>
      <div class="flex gap-2">
        <input
          v-model="contextQuery"
          placeholder="输入查询，如：部署 Memory OS"
          class="flex-1 border rounded px-3 py-2 text-sm"
          @keyup.enter="buildContextPack"
        />
        <button
          :disabled="buildingPack || !contextQuery.trim()"
          class="px-4 py-2 bg-green-600 text-white rounded hover:bg-green-700 disabled:opacity-50 text-sm"
          @click="buildContextPack"
        >
          {{ buildingPack ? '构建中...' : '预览' }}
        </button>
      </div>
      <div v-if="contextPack" class="mt-3 bg-gray-50 border rounded p-3 text-sm whitespace-pre-wrap">{{ contextPack.context || '无匹配可信事实' }}</div>
      <div v-if="contextPack?.warnings?.length" class="mt-2 text-xs text-orange-600">
        <span v-for="w in contextPack.warnings" :key="w">{{ w }}</span>
      </div>
    </div>

    <!-- 可信事实列表 -->
    <div class="bg-white border rounded-lg p-4">
      <h2 class="text-lg font-semibold mb-3">可信事实</h2>
      <div v-if="loading" class="text-gray-400 text-sm">加载中...</div>
      <div v-else-if="units.length === 0" class="text-gray-400 text-sm">暂无可信事实，点击"运行记忆体检"生成</div>
      <table v-else class="w-full text-sm">
        <thead class="border-b">
          <tr>
            <th class="text-left py-2">类型</th>
            <th class="text-left py-2">内容</th>
            <th class="text-left py-2">状态</th>
            <th class="text-left py-2">信任分</th>
            <th class="text-left py-2">适用场景</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in units" :key="u.unit_id" class="border-b hover:bg-gray-50">
            <td class="py-2">{{ unitTypeLabel(u.unit_type) }}</td>
            <td class="py-2 max-w-md truncate">{{ u.content }}</td>
            <td class="py-2"><span class="px-2 py-0.5 rounded text-xs" :class="{
              'bg-green-100 text-green-700': u.status === 'current',
              'bg-yellow-100 text-yellow-700': u.status === 'needs_review',
              'bg-gray-100 text-gray-500': u.status === 'superseded'
            }">{{ statusLabel(u.status) }}</span></td>
            <td class="py-2">{{ u.trust_score.toFixed(2) }}</td>
            <td class="py-2 text-xs text-gray-500">{{ u.applies_when || '-' }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
