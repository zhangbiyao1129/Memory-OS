export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: '',
    user: { id: '', name: 'PAT Operator', email: 'pat-operator@memory.local' },
    lastError: '',
    hydrated: false
  }),
  getters: {
    isAuthenticated: (state) => state.token.length > 0
  },
  actions: {
    initFromStorage() {
      if (this.hydrated || !import.meta.client) return
      this.token = localStorage.getItem('memory-os-pat') || ''
      this.user.email = localStorage.getItem('memory-os-user-email') || localStorage.getItem('memory-os-operator-email') || this.user.email
      this.user.name = localStorage.getItem('memory-os-user-name') || this.user.name
      this.user.id = localStorage.getItem('memory-os-user-id') || this.user.id
      this.hydrated = true
    },
    loginWithToken(token: string, user: { id?: string, name?: string, email?: string }) {
      this.lastError = ''
      const trimmed = token.trim()
      if (!trimmed) {
        this.lastError = '登录令牌不能为空'
        return false
      }
      this.token = trimmed
      this.user = {
        id: user.id || '',
        name: user.name || 'Memory OS Operator',
        email: user.email || 'operator@memory.local'
      }
      if (import.meta.client) {
        localStorage.setItem('memory-os-pat', this.token)
        localStorage.setItem('memory-os-user-email', this.user.email)
        localStorage.setItem('memory-os-user-name', this.user.name)
        localStorage.setItem('memory-os-user-id', this.user.id)
      }
      return true
    },
    loginWithPAT(token: string, email: string) {
      if (!token.trim()) {
        this.lastError = '请输入 Personal Access Token'
        return false
      }
      return this.loginWithToken(token, {
        id: '',
        name: 'PAT Operator',
        email: email.trim() || 'pat-operator@memory.local'
      })
    },
    loginWithPassword(token: string, user: { user_id?: string, display_name?: string, email?: string }) {
      return this.loginWithToken(token, {
        id: user.user_id || '',
        name: user.display_name || 'Memory OS Operator',
        email: user.email || 'operator@memory.local'
      })
    },
    logout() {
      this.token = ''
      this.user = { id: '', name: 'PAT Operator', email: 'pat-operator@memory.local' }
      if (import.meta.client) {
        localStorage.removeItem('memory-os-pat')
        localStorage.removeItem('memory-os-operator-email')
        localStorage.removeItem('memory-os-user-email')
        localStorage.removeItem('memory-os-user-name')
        localStorage.removeItem('memory-os-user-id')
      }
    }
  }
})
