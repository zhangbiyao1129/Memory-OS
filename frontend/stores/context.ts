export type TenantOrg = {
  org_id: string
  name: string
  slug: string
  status?: string
}

export type TenantUser = {
  user_id: string
  email: string
  display_name: string
  status?: string
}

export type TenantProject = {
  project_id: string
  org_id: string
  name: string
  slug: string
  status?: string
  source_type?: string
  source_key?: string
}

export type TenantMembership = {
  user_id: string
  org_id: string
  project_id: string
  role: string
  status: string
}

export const useContextStore = defineStore('context', {
  state: () => ({
    orgId: '',
    projectId: '',
    agentId: 'codex',
    permissionLabels: [] as string[],
    orgs: [] as TenantOrg[],
    projects: [] as TenantProject[],
    hydrated: false
  }),
  actions: {
    initFromStorage() {
      if (this.hydrated || !import.meta.client) return
      this.orgId = localStorage.getItem('memory-os-org-id') || ''
      this.projectId = localStorage.getItem('memory-os-project-id') || ''
      this.agentId = localStorage.getItem('memory-os-agent-id') || this.agentId
      this.permissionLabels = this.projectId ? [`project:${this.projectId}:read`] : []
      this.hydrated = true
    },
    setOrgs(orgs: TenantOrg[]) {
      this.orgs = orgs
      if (orgs.length === 0) {
        this.setOrg('')
        this.setProjects([])
      } else if (!orgs.some((org) => org.org_id === this.orgId)) {
        this.setOrg(orgs[0].org_id)
      }
    },
    setProjects(projects: TenantProject[]) {
      this.projects = projects
      if (projects.length === 0) {
        this.setProject('')
      } else if (!projects.some((project) => project.project_id === this.projectId)) {
        this.setProject(projects[0].project_id)
      }
    },
    setOrg(orgId: string) {
      this.orgId = orgId
      if (import.meta.client) localStorage.setItem('memory-os-org-id', orgId)
    },
    setProject(projectId: string) {
      this.projectId = projectId
      this.permissionLabels = projectId ? [`project:${projectId}:read`] : []
      if (import.meta.client) localStorage.setItem('memory-os-project-id', projectId)
    },
    setAgent(agentId: string) {
      this.agentId = agentId
      if (import.meta.client) localStorage.setItem('memory-os-agent-id', agentId)
    }
  }
})
