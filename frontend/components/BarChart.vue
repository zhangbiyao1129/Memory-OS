<script setup lang="ts">
const props = defineProps<{
  title: string
  rows: Array<{ label: string; value: number; tone?: string }>
}>()

const maxValue = computed(() => Math.max(...props.rows.map((row) => Number(row.value || 0)), 0))

function width(value: number) {
  if (maxValue.value <= 0) return '0%'
  return `${Math.max(4, Math.round((value / maxValue.value) * 100))}%`
}
</script>

<template>
  <section class="rounded-3xl border border-stone-200 bg-white p-5">
    <h3 class="font-black text-stone-950">{{ title }}</h3>
    <div v-if="rows.length === 0 || maxValue === 0" class="mt-4 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">暂无可视化数据。</div>
    <div v-else class="mt-4 grid gap-3">
      <div v-for="row in rows" :key="row.label" class="grid gap-2">
        <div class="flex items-center justify-between gap-3 text-sm">
          <span class="truncate font-bold text-stone-700">{{ row.label }}</span>
          <span class="font-black text-stone-950">{{ row.value }}</span>
        </div>
        <div class="h-3 overflow-hidden rounded-full bg-stone-100">
          <div class="h-full rounded-full" :class="row.tone || 'bg-stone-900'" :style="{ width: width(row.value) }" />
        </div>
      </div>
    </div>
  </section>
</template>
