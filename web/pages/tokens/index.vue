<script setup lang="ts">
const oneTimeToken = ref('')
function createToken() {
  oneTimeToken.value = `adapter_once_${Math.random().toString(36).slice(2)}`
}
function hideToken() {
  oneTimeToken.value = ''
}
</script>

<template>
  <AppShell>
    <h2 class="text-3xl font-black">Adapter Token</h2>
    <p class="mt-2 text-stone-600">明文 token 只显示一次，刷新或隐藏后不可再查看。</p>
    <button class="mt-6 rounded-2xl bg-stone-950 px-4 py-2 font-bold text-white" @click="createToken">创建 Adapter Token</button>
    <div v-if="oneTimeToken" class="mt-4 rounded-3xl border border-amber-200 bg-amber-50 p-5">
      <p class="font-black text-amber-900">一次性明文 token</p>
      <code class="mt-2 block break-all text-sm">{{ oneTimeToken }}</code>
      <button class="mt-3 rounded-2xl bg-amber-900 px-4 py-2 font-bold text-white" @click="hideToken">我已保存，立即隐藏</button>
    </div>
  </AppShell>
</template>
