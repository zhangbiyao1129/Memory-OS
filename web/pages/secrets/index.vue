<script setup lang="ts">
const secretName = ref('MINIMAX_API_KEY')
const transientSecret = ref('')
const secrets = [{ ref: 'secret_ref_llm', name: 'MINIMAX_API_KEY', status: 'active' }, { ref: 'secret_ref_qdrant', name: 'QDRANT_TOKEN', status: 'disabled' }]
</script>

<template>
  <AppShell>
    <h2 class="text-3xl font-black">Secret Vault</h2>
    <p class="mt-2 text-stone-600">列表只展示 metadata，Secret 明文不进入日志、Markdown、Qdrant、Hot Memory 或持久页面状态。</p>
    <div class="mt-6 grid gap-4 lg:grid-cols-[1fr_1fr]">
      <section class="rounded-3xl border bg-white p-5">
        <h3 class="font-black">创建 / 更新 Secret</h3>
        <input v-model="secretName" class="mt-4 w-full rounded-2xl border px-4 py-3">
        <input v-model="transientSecret" class="mt-3 w-full rounded-2xl border px-4 py-3" type="password" placeholder="明文只在提交前短暂存在">
        <SecretValueGuard class="mt-3" :value="transientSecret" />
      </section>
      <section class="rounded-3xl border bg-white p-5">
        <h3 class="font-black">Metadata</h3>
        <div v-for="secret in secrets" :key="secret.ref" class="mt-3 rounded-2xl bg-stone-50 p-3 text-sm"><b>{{ secret.name }}</b><br>{{ secret.ref }} · {{ secret.status }}</div>
      </section>
    </div>
  </AppShell>
</template>
