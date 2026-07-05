<script setup lang="ts">
defineProps<{
  label: string
  value: number | string
  detail?: string
}>()
</script>

<template>
  <article class="rounded-3xl border border-stone-200 bg-white p-5">
    <p class="text-sm font-bold text-stone-500">{{ label }}</p>
    <p class="mt-3 text-3xl font-black text-stone-950">{{ value }}</p>
    <p v-if="detail" class="mt-2 text-sm leading-6 text-stone-600">{{ detail }}</p>
  </article>
</template>
