export type ScoreBucket = { label: string; count: number }
export type CountMap = Record<string, number>

export type LifecycleStats = {
  archives: { total: number; by_status: CountMap }
  hot_memories: { total: number; by_status: CountMap }
	  candidates: {
	    total: number
	    actionable_total?: number
	    pending_organize_total?: number
	    archive_material_total?: number
	    needs_review_total?: number
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
  memory_kernel?: {
    units_total: number
    units_current: number
    units_needs_review: number
    governance_runs_total: number
    governance_runs_failed: number
    ci_cases_active: number
    ci_latest_failed: number
    last_run_at: string
    last_run_summary: string
  }
}

type LifecycleStatsOptions = { userScoped?: boolean }

export function useMemoryLifecycleStats(options: LifecycleStatsOptions = {}) {
  const auth = useAuthStore()
  const context = useContextStore()
  const { request } = useApi()
  const userScoped = options.userScoped === true
  const loading = ref(false)
  const error = ref('')
  const stats = ref<LifecycleStats | null>(null)
  const hasProjectContext = computed(() => userScoped ? auth.isAuthenticated : Boolean(context.orgId && context.projectId))

  async function loadProjectStats(orgId: string, projectId: string) {
    return await request<LifecycleStats>('/memory/stats/lifecycle', {
      method: 'POST',
      body: { org_id: orgId, project_id: projectId }
    })
  }

  async function loadUserStats() {
    stats.value = await request<LifecycleStats>('/memory/stats/lifecycle', {
      method: 'POST',
      body: {}
    })
  }

  async function loadStats() {
    auth.initFromStorage()
    context.initFromStorage()
    if (!auth.isAuthenticated || !hasProjectContext.value) {
      stats.value = null
      return
    }
    loading.value = true
    error.value = ''
    try {
      if (userScoped) {
        await loadUserStats()
      } else {
        stats.value = await loadProjectStats(context.orgId, context.projectId)
      }
    } catch (err: any) {
      error.value = err.message || '记忆生命周期统计加载失败'
      stats.value = null
    } finally {
      loading.value = false
    }
  }

  watch(() => userScoped ? [auth.token] : [auth.token, context.orgId, context.projectId], () => {
    void loadStats()
  })

  return { stats, loading, error, hasProjectContext, loadStats }
}
