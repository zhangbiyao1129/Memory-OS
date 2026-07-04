type ApiOptions = {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  body?: unknown
  token?: string
}

export function useApi() {
  const config = useRuntimeConfig()
  const auth = useAuthStore()
  const baseURL = computed(() => {
    const configured = String(config.public.apiBase || '').trim()
    if (configured) return configured.replace(/\/$/, '')
    if (typeof window !== 'undefined') {
      return `${window.location.protocol}//${window.location.hostname}:18081`
    }
    return 'http://localhost:18081'
  })

  async function request<T>(path: string, options: ApiOptions = {}): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json' }
    if (options.body !== undefined) headers['Content-Type'] = 'application/json'
    const token = options.token ?? auth.token
    if (token) headers.Authorization = `Bearer ${token}`
    try {
      return await $fetch<T>(`${baseURL.value}${path}`, {
        method: options.method || 'GET',
        body: options.body,
        headers
      })
    } catch (error: any) {
      const status = error?.statusCode || error?.response?.status
      if (status === 401) throw new Error('登录已过期，请重新登录')
      if (status === 403) throw new Error('当前账号没有访问权限')
      throw new Error(error?.data?.message || error?.data?.error || error?.message || 'Memory OS API 暂不可用')
    }
  }

  return { baseURL, request }
}
