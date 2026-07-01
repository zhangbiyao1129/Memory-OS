<script setup lang="ts">
defineProps<{ refs: Array<Record<string, any>> }>()
</script>

<template>
  <ul class="grid gap-2">
    <li v-for="(ref, index) in refs" :key="index" class="rounded-2xl border border-stone-200 bg-stone-50 p-3 text-sm">
      <span class="font-bold text-stone-700">{{ ref.kind || ref.type || 'source' }}</span>
      <pre class="mt-1 overflow-auto text-xs text-stone-600">{{ JSON.stringify(ref, null, 2) }}</pre>
    </li>
  </ul>
</template>
