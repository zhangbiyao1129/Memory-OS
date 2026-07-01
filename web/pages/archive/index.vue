<script setup lang="ts">
const archives = [{ id: 'archive_1', title: 'Deployment Notes', version: 2, generation: 2 }, { id: 'archive_2', title: 'Adapter Transcript', version: 1, generation: 1 }]
</script>

<template>
  <AppShell>
    <h2 class="text-3xl font-black">Markdown Archive</h2>
    <div class="mt-6 grid gap-4">
      <NuxtLink v-for="archive in archives" :key="archive.id" :to="`/archive/${archive.id}`" class="rounded-3xl border bg-white p-5 hover:border-orange-300">
        <h3 class="font-black">{{ archive.title }}</h3>
        <p class="mt-2 text-sm text-stone-600">{{ archive.id }} · version {{ archive.version }} · index_generation {{ archive.generation }}</p>
      </NuxtLink>
    </div>
  </AppShell>
</template>
