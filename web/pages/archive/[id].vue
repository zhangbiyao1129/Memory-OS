<script setup lang="ts">
const route = useRoute()
const content = ref('# Archive Detail\n\nSource ref: `turn_event_1`\n\nDeploy API through docker compose on T480.')
const sourceRefs = [{ kind: 'turn_event', event_id: 'turn_event_1' }, { kind: 'archive_chunk', chunk_id: 'chunk_1' }]
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div><p class="text-sm font-bold text-stone-500">{{ route.params.id }}</p><h2 class="text-3xl font-black">Archive 编辑 / 版本 / 重索引</h2></div>
      <button class="rounded-2xl bg-stone-950 px-4 py-2 font-bold text-white">触发重索引</button>
    </div>
    <textarea v-model="content" class="mt-6 min-h-72 w-full rounded-3xl border bg-white p-4 font-mono text-sm" />
    <h3 class="mt-6 font-black">Source Refs</h3>
    <SourceRefList class="mt-3" :refs="sourceRefs" />
  </AppShell>
</template>
