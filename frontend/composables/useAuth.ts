export function useAuth() {
  const auth = useAuthStore()
  return {
    auth,
    isAuthenticated: computed(() => auth.isAuthenticated),
    loginWithPAT: auth.loginWithPAT,
    logout: auth.logout
  }
}
