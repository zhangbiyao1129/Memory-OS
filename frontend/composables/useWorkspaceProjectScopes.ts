type WorkspaceProjectScope = {
  org_id: string
  project_id: string
}

type ProjectListResponse = { projects: TenantProject[] }

type WorkspaceProjectRequestOptions<TResponse, TItem> = {
  path: string
  buildBody: (scope: WorkspaceProjectScope) => Record<string, unknown>
  pick: (response: TResponse) => TItem[]
}

export function useWorkspaceProjectScopes() {
  const auth = useAuthStore()
  const context = useContextStore()
  const { request } = useApi()
  const loadingProjects = ref(false)

  const hasWorkspaceContext = computed(() => Boolean(auth.isAuthenticated && context.orgId))

  const projectScopes = computed<WorkspaceProjectScope[]>(() => {
    if (context.projects.length > 0) {
      return context.projects.map((project) => ({ org_id: project.org_id, project_id: project.project_id }))
    }
    if (context.orgId && context.projectId) {
      return [{ org_id: context.orgId, project_id: context.projectId }]
    }
    return []
  })

  async function loadProjectScopes() {
    auth.initFromStorage()
    context.initFromStorage()
    if (!auth.isAuthenticated || !context.orgId) return []
    if (context.projects.length === 0) {
      loadingProjects.value = true
      try {
        const response = await request<ProjectListResponse>('/memory/tenant/projects/list', {
          method: 'POST',
          body: { org_id: context.orgId }
        })
        context.setProjects(response.projects || [])
      } finally {
        loadingProjects.value = false
      }
    }
    return projectScopes.value
  }

  async function requestWorkspaceProjects<TResponse, TItem>(options: WorkspaceProjectRequestOptions<TResponse, TItem>) {
    const scopes = await loadProjectScopes()
    if (scopes.length === 0) return []
    const responses = await Promise.all(scopes.map((scope) => request<TResponse>(options.path, {
      method: 'POST',
      body: options.buildBody(scope)
    })))
    return responses.flatMap(options.pick)
  }

  return { hasWorkspaceContext, loadingProjects, projectScopes, loadProjectScopes, requestWorkspaceProjects }
}
