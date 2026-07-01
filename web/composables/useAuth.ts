export function useAuth() {
  const auth = useAuthStore()
  return {
    auth,
    isAuthenticated: computed(() => auth.isAuthenticated),
    login: auth.login,
    logout: auth.logout
  }
}
