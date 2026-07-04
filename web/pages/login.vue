<script setup lang="ts">
const auth = useAuthStore()
const router = useRouter()
const { request } = useApi()
const loginMode = ref<'password' | 'pat'>('password')
const email = ref('')
const password = ref('')
const operatorEmail = ref('pat-operator@memory.local')
const pat = ref('')
const pending = ref(false)

type PasswordLoginResponse = {
  token: string
  user: {
    user_id: string
    email: string
    display_name: string
    status: string
  }
}

onMounted(async () => {
  auth.initFromStorage()
  if (auth.isAuthenticated) await router.push('/')
})

async function submitPAT() {
  if (!auth.loginWithPAT(pat.value, operatorEmail.value)) return
  pending.value = true
  try {
    await request('/memory/tenant/orgs/list', { method: 'POST', body: {} })
    await router.push('/')
  } catch (error: any) {
    auth.logout()
    auth.lastError = error.message || 'PAT 验证失败'
  } finally {
    pending.value = false
  }
}

async function submitPassword() {
  auth.lastError = ''
  if (!email.value.trim() || !password.value.trim()) {
    auth.lastError = '请输入邮箱和密码'
    return
  }
  pending.value = true
  try {
    const response = await request<PasswordLoginResponse>('/memory/auth/login-password', {
      method: 'POST',
      body: { email: email.value, password: password.value },
      token: ''
    })
    if (!auth.loginWithPassword(response.token, response.user)) return
    await router.push('/')
  } catch (error: any) {
    auth.logout()
    auth.lastError = error.message || '密码登录失败'
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center px-4">
    <div class="w-full max-w-md rounded-[2rem] bg-white/85 p-8 shadow-2xl">
      <p class="text-xs font-bold uppercase tracking-[0.35em] text-orange-800">Memory OS 登录</p>
      <h1 class="mt-3 text-4xl font-black">登录管理台</h1>
      <p class="mt-3 text-stone-600">默认使用邮箱和密码登录。登录成功后，后端会签发短期 PAT 给当前浏览器会话；如果首个管理员还未初始化，请在服务器执行 <code>memory-bootstrap bootstrap</code>。</p>
      <div class="mt-6 grid grid-cols-2 gap-2 rounded-2xl bg-stone-100 p-1">
        <button class="rounded-2xl px-4 py-2 text-sm font-bold" :class="loginMode === 'password' ? 'bg-white text-stone-950 shadow' : 'text-stone-500'" @click="loginMode = 'password'">
          密码登录
        </button>
        <button class="rounded-2xl px-4 py-2 text-sm font-bold" :class="loginMode === 'pat' ? 'bg-white text-stone-950 shadow' : 'text-stone-500'" @click="loginMode = 'pat'">
          PAT 登录
        </button>
      </div>
      <form v-if="loginMode === 'password'" class="mt-4" @submit.prevent="submitPassword">
        <input v-model="email" class="w-full rounded-2xl border px-4 py-3" placeholder="管理员邮箱" type="email" autocomplete="username">
        <input v-model="password" class="mt-3 w-full rounded-2xl border px-4 py-3" placeholder="密码" type="password" autocomplete="current-password">
        <p v-if="auth.lastError" class="mt-3 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ auth.lastError }}</p>
        <button class="mt-5 w-full rounded-2xl bg-stone-950 px-4 py-3 font-black text-white" :disabled="pending">
          {{ pending ? '正在验证密码...' : '使用密码登录' }}
        </button>
      </form>
      <form v-else class="mt-4" @submit.prevent="submitPAT">
        <input v-model="operatorEmail" class="w-full rounded-2xl border px-4 py-3" placeholder="操作员标识，可选" type="email">
        <input v-model="pat" class="mt-3 w-full rounded-2xl border px-4 py-3" placeholder="Personal Access Token" type="password" autocomplete="off">
        <p class="mt-3 text-sm text-stone-600">PAT 仅保存在本机浏览器 localStorage，不会写入后端或日志。</p>
        <p v-if="auth.lastError" class="mt-3 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ auth.lastError }}</p>
        <button class="mt-5 w-full rounded-2xl bg-stone-950 px-4 py-3 font-black text-white" :disabled="pending">
          {{ pending ? '正在验证 PAT...' : '进入记忆控制台' }}
        </button>
      </form>
    </div>
  </div>
</template>
