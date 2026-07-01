export const useContextStore = defineStore('context', {
  state: () => ({
    orgId: 'org_1',
    projectId: 'project_1',
    agentId: 'codex',
    permissionLabels: ['project:project_1:read']
  }),
  actions: {
    setProject(projectId: string) {
      this.projectId = projectId
      this.permissionLabels = [`project:${projectId}:read`]
    },
    setAgent(agentId: string) {
      this.agentId = agentId
    }
  }
})
