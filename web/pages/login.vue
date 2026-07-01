<script setup lang="ts">
const auth = useAuthStore()
const router = useRouter()
const email = ref('admin@memory.local')
const password = ref('')
function submit() {
  if (auth.login(email.value, password.value)) router.push('/')
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center px-4">
    <form class="w-full max-w-md rounded-[2rem] bg-white/85 p-8 shadow-2xl" @submit.prevent="submit">
      <p class="text-xs font-bold uppercase tracking-[0.35em] text-orange-800">Memory OS Login</p>
      <h1 class="mt-3 text-4xl font-black">登录管理台</h1>
      <p class="mt-3 text-stone-600">本地开发登录只保存会话 token，不记录密码或 Secret。</p>
      <input v-model="email" class="mt-6 w-full rounded-2xl border px-4 py-3" placeholder="邮箱" type="email">
      <input v-model="password" class="mt-3 w-full rounded-2xl border px-4 py-3" placeholder="密码" type="password">
      <p v-if="auth.lastError" class="mt-3 rounded-2xl bg-red-50 p-3 text-sm text-red-700">{{ auth.lastError }}</p>
      <button class="mt-5 w-full rounded-2xl bg-stone-950 px-4 py-3 font-black text-white">进入 Memory Console</button>
    </form>
  </div>
</template>
