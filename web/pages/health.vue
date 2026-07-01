<script setup lang="ts">
const config = useRuntimeConfig()
const { data, pending, error } = await useFetch('/healthz', {
  baseURL: config.public.apiBase
})
</script>

<template>
  <main class="min-h-screen px-6 py-10 md:px-12">
    <section class="mx-auto max-w-4xl rounded-[2rem] border border-black/10 bg-white/60 p-8 shadow-2xl backdrop-blur">
      <NuxtLink class="text-sm font-bold text-[#b8603b]" to="/">返回首页</NuxtLink>
      <h1 class="mt-4 text-4xl font-black">系统健康状态</h1>
      <p v-if="pending" class="mt-6">正在读取 Memory API...</p>
      <p v-else-if="error" class="mt-6 rounded-2xl bg-red-100 p-4 text-red-800">API 暂不可用：{{ error.message }}</p>
      <pre v-else class="mt-6 overflow-auto rounded-2xl bg-[#17211b] p-5 text-sm text-[#f4efe3]">{{ data }}</pre>
    </section>
  </main>
</template>
