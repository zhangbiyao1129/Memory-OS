<script setup lang="ts">
defineProps<{ status: string }>()
</script>

<template>
  <span class="inline-flex items-center gap-2 rounded-full border px-3 py-1 text-sm font-bold" :class="status === 'ok' ? 'border-emerald-300 bg-emerald-50 text-emerald-800' : 'border-amber-300 bg-amber-50 text-amber-800'">
    <span class="h-2 w-2 rounded-full" :class="status === 'ok' ? 'bg-emerald-500' : 'bg-amber-500'" />
    {{ status || 'unknown' }}
  </span>
</template>
