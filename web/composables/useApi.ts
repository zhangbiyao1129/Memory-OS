type ApiOptions = {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  body?: unknown
  token?: string
}

export function useApi() {
  const config = useRuntimeConfig()
  const baseURL = computed(() => String(config.public.apiBase || 'http://localhost:18081').replace(/\/$/, ''))

  async function request<T>(path: string, options: ApiOptions = {}): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json' }
    if (options.body !== undefined) headers['Content-Type'] = 'application/json'
    if (options.token) headers.Authorization = `Bearer ${options.token}`
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
      throw new Error(error?.data?.message || error?.message || 'Memory OS API 暂不可用')
    }
  }

  return { baseURL, request }
}
