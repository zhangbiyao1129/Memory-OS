export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: '',
    user: { id: 'user_1', name: 'Local Admin', email: 'admin@memory.local' },
    lastError: ''
  }),
  getters: {
    isAuthenticated: (state) => state.token.length > 0
  },
  actions: {
    login(email: string, password: string) {
      this.lastError = ''
      if (!email || !password) {
        this.lastError = '请输入邮箱和密码'
        return false
      }
      this.token = `local-session-${Date.now()}`
      this.user = { id: 'user_1', name: 'Local Admin', email }
      return true
    },
    logout() {
      this.token = ''
    }
  }
})
