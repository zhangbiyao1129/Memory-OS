<script setup lang="ts">
const props = defineProps<{
  title: string
  value: number
  total: number
  label: string
}>()

const percent = computed(() => props.total <= 0 ? 0 : Math.round((props.value / props.total) * 100))
const dash = computed(() => `${percent.value} ${100 - percent.value}`)
</script>

<template>
  <section class="rounded-3xl border border-stone-200 bg-white p-5">
    <h3 class="font-black text-stone-950">{{ title }}</h3>
    <div class="mt-4 flex items-center gap-5">
      <svg viewBox="0 0 36 36" class="h-24 w-24 shrink-0">
        <circle cx="18" cy="18" r="15.915" fill="none" class="stroke-stone-100" stroke-width="4" />
        <circle cx="18" cy="18" r="15.915" fill="none" class="stroke-emerald-500" stroke-width="4" :stroke-dasharray="dash" stroke-linecap="round" transform="rotate(-90 18 18)" />
      </svg>
      <div>
        <p class="text-3xl font-black text-stone-950">{{ percent }}%</p>
        <p class="mt-1 text-sm text-stone-600">{{ label }} {{ value }} / {{ total }}</p>
      </div>
    </div>
  </section>
</template>
