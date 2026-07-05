export type ScoreBucket = { label: string; count: number }
export type CountMap = Record<string, number>
export type LifecycleStats = {
  archives: { total: number; by_status: CountMap }
  hot_memories: { total: number; by_status: CountMap }
  candidates: {
    total: number
    by_status: CountMap
    by_risk: CountMap
    hot_score_buckets: ScoreBucket[]
    compose_score_buckets: ScoreBucket[]
  }
  topics: { total: number; ready_to_compose: number; composed: number; open: number }
}

export function useMemoryLifecycleStats() {
  const auth = useAuthStore()
  const context = useContextStore()
  const { request } = useApi()
  const loading = ref(false)
  const error = ref('')
  const stats = ref<LifecycleStats | null>(null)
  const hasProjectContext = computed(() => Boolean(context.orgId && context.projectId))

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
      stats.value = await request<LifecycleStats>('/memory/stats/lifecycle', {
        method: 'POST',
        body: { org_id: context.orgId, project_id: context.projectId }
      })
    } catch (err: any) {
      error.value = err.message || '记忆生命周期统计加载失败'
      stats.value = null
    } finally {
      loading.value = false
    }
  }

  watch(() => [auth.token, context.orgId, context.projectId], () => {
    void loadStats()
  })

  return { stats, loading, error, hasProjectContext, loadStats }
}
