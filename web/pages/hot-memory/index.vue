<script setup lang="ts">
const memories = ref([
  { id: 'hm_1', fact: 'Project deploy API with docker compose on T480', score: 18.5, status: 'promoted' },
  { id: 'hm_2', fact: 'Use bge-m3 for embeddings', score: 9.4, status: 'active' }
])
function demote(id: string) {
  const item = memories.value.find(memory => memory.id === id)
  if (item) item.status = 'demoted'
}
</script>

<template>
  <AppShell>
    <h2 class="text-3xl font-black">Hot Memory 管理</h2>
    <div class="mt-6 grid gap-4">
      <article v-for="memory in memories" :key="memory.id" class="rounded-3xl border bg-white p-5">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div><p class="font-black">{{ memory.fact }}</p><p class="text-sm text-stone-600">{{ memory.id }} · hot_score {{ memory.score }} · {{ memory.status }}</p></div>
          <button class="rounded-2xl bg-orange-100 px-4 py-2 font-bold text-orange-900" @click="demote(memory.id)">demote</button>
        </div>
      </article>
    </div>
  </AppShell>
</template>
