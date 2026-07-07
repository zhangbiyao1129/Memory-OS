export type ContextSummaryInput = {
  orgName?: string
  projectName?: string
  agentId?: string
}

export function buildContextSummary(input: ContextSummaryInput) {
  return {
    primary: `${input.orgName || '未选择组织'} / ${input.projectName || '未选择项目'}`,
    secondary: `Agent: ${input.agentId || 'codex'}`
  }
}

export type LifecycleInput = {
  candidateJobsDone?: number
  candidateJobsFailed?: number
  candidatesActionable?: number
  hotMemories?: number
  archives?: number
}

export type LifecycleStep = {
  label: string
  detail: string
  tone: 'ok' | 'warning' | 'error' | 'muted'
}

export function explainLifecycle(input: LifecycleInput): LifecycleStep[] {
  return [
    { label: '已接收', detail: `${input.candidateJobsDone || 0} 个提炼任务完成`, tone: 'ok' },
    {
      label: '已提炼',
      detail: `${input.candidateJobsFailed || 0} 个任务失败`,
      tone: input.candidateJobsFailed ? 'error' : 'ok'
    },
    {
      label: '待确认',
      detail: `${input.candidatesActionable || 0} 条候选需要处理`,
      tone: input.candidatesActionable ? 'warning' : 'muted'
    },
    { label: '可快速召回', detail: `${input.hotMemories || 0} 条热记忆`, tone: input.hotMemories ? 'ok' : 'muted' },
    {
      label: input.archives ? '已长期归档' : '未长期归档',
      detail: `${input.archives || 0} 个归档`,
      tone: input.archives ? 'ok' : 'warning'
    }
  ]
}

export type SecretTemplateInput = {
  name?: string
  envName?: string
  site?: string
  purpose?: string
}

export function buildSecretCommandTemplates(input: SecretTemplateInput) {
  const name = input.name || 'my-secret'
  const envName = input.envName || 'SECRET_VALUE'
  const site = input.site || 'example'
  const purpose = input.purpose || '本机工具调用'
  return {
    create: `secret_create_local name=${name} env_name=${envName} site=${site} purpose="${purpose}" value="<在本机输入明文>"`,
    list: 'secret_list_local status=active',
    use: `secret_use_local secret_ref=<复制 secret_ref> command="<需要注入 ${envName} 的本机命令>"`,
    disable: 'secret_disable_local secret_ref=<复制 secret_ref>'
  }
}

export function explainSearchHit(source?: { kind?: string }) {
  if (source?.kind === 'hot_memory') return '快速记忆'
  if (source?.kind === 'archive_chunk') return '长期归档'
  return '检索结果'
}

export function friendlyApiError(error: any) {
  const message = String(error?.data?.message || error?.data?.error || error?.message || '').trim()
  if (
    !message ||
    message.includes('Failed to fetch') ||
    message.includes('<no response>') ||
    message.includes('NetworkError') ||
    message.includes('Load failed')
  ) {
    return 'Memory OS API 暂不可用，请确认后端服务已启动。'
  }
  return message
}

export type CandidateMaintenanceSummaryInput = {
  processed?: number
  discarded?: number
  kept?: number
  composed?: number
}

function safeCount(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0
}

export function formatCandidateMaintenanceSummary(input: CandidateMaintenanceSummaryInput) {
  return `清洗完成：处理 ${safeCount(input.processed)} 条，丢弃 ${safeCount(input.discarded)} 条，保留 ${safeCount(input.kept)} 条，沉淀 ${safeCount(input.composed)} 条。`
}

type CountMap = Record<string, number>
type ScoreBucket = { label: string; count: number }
type LifecycleStatsLike = {
  archives: { total: number; by_status: CountMap }
  hot_memories: { total: number; by_status: CountMap }
  candidates: {
    total: number
    actionable_total?: number
    by_status: CountMap
    by_risk: CountMap
    hot_score_buckets: ScoreBucket[]
    compose_score_buckets: ScoreBucket[]
  }
  candidate_jobs?: {
    total: number
    by_status: CountMap
    pending: number
    running: number
    failed: number
    done: number
    latest_error: string
    oldest_pending_at: string
    last_completed_at: string
  }
  topics: { total: number; ready_to_compose: number; composed: number; open: number }
}

function addCounts(target: CountMap, source: CountMap = {}) {
  for (const [key, value] of Object.entries(source)) {
    target[key] = (target[key] || 0) + value
  }
}

function addBuckets(target: ScoreBucket[], source: ScoreBucket[] = []) {
  const byLabel = new Map(target.map((bucket) => [bucket.label, bucket]))
  for (const bucket of source) {
    const existing = byLabel.get(bucket.label)
    if (existing) {
      existing.count += bucket.count
    } else {
      const next = { label: bucket.label, count: bucket.count }
      target.push(next)
      byLabel.set(next.label, next)
    }
  }
}

function emptyLifecycleStats(): LifecycleStatsLike {
  return {
    archives: { total: 0, by_status: {} },
    hot_memories: { total: 0, by_status: {} },
    candidates: {
      total: 0,
      actionable_total: 0,
      by_status: {},
      by_risk: {},
      hot_score_buckets: [],
      compose_score_buckets: []
    },
    candidate_jobs: {
      total: 0,
      by_status: {},
      pending: 0,
      running: 0,
      failed: 0,
      done: 0,
      latest_error: '',
      oldest_pending_at: '',
      last_completed_at: ''
    },
    topics: { total: 0, ready_to_compose: 0, composed: 0, open: 0 }
  }
}

export function aggregateLifecycleStats(items: LifecycleStatsLike[]) {
  const out = emptyLifecycleStats()
  for (const item of items) {
    out.archives.total += item.archives.total || 0
    addCounts(out.archives.by_status, item.archives.by_status)

    out.hot_memories.total += item.hot_memories.total || 0
    addCounts(out.hot_memories.by_status, item.hot_memories.by_status)

    out.candidates.total += item.candidates.total || 0
    out.candidates.actionable_total = (out.candidates.actionable_total || 0) + (item.candidates.actionable_total || 0)
    addCounts(out.candidates.by_status, item.candidates.by_status)
    addCounts(out.candidates.by_risk, item.candidates.by_risk)
    addBuckets(out.candidates.hot_score_buckets, item.candidates.hot_score_buckets)
    addBuckets(out.candidates.compose_score_buckets, item.candidates.compose_score_buckets)

    if (item.candidate_jobs && out.candidate_jobs) {
      out.candidate_jobs.total += item.candidate_jobs.total || 0
      out.candidate_jobs.pending += item.candidate_jobs.pending || 0
      out.candidate_jobs.running += item.candidate_jobs.running || 0
      out.candidate_jobs.failed += item.candidate_jobs.failed || 0
      out.candidate_jobs.done += item.candidate_jobs.done || 0
      if (!out.candidate_jobs.latest_error && item.candidate_jobs.latest_error) out.candidate_jobs.latest_error = item.candidate_jobs.latest_error
      if (!out.candidate_jobs.oldest_pending_at || item.candidate_jobs.oldest_pending_at < out.candidate_jobs.oldest_pending_at) {
        out.candidate_jobs.oldest_pending_at = item.candidate_jobs.oldest_pending_at
      }
      if (item.candidate_jobs.last_completed_at > out.candidate_jobs.last_completed_at) {
        out.candidate_jobs.last_completed_at = item.candidate_jobs.last_completed_at
      }
      addCounts(out.candidate_jobs.by_status, item.candidate_jobs.by_status)
    }

    out.topics.total += item.topics.total || 0
    out.topics.ready_to_compose += item.topics.ready_to_compose || 0
    out.topics.composed += item.topics.composed || 0
    out.topics.open += item.topics.open || 0
  }
  return out
}
