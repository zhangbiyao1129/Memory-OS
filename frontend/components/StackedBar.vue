<script setup lang="ts">
const props = defineProps<{
  title: string
  segments: Array<{ label: string; value: number; className: string }>
}>()

const total = computed(() => props.segments.reduce((sum, item) => sum + Number(item.value || 0), 0))

function segmentWidth(value: number) {
  if (total.value <= 0) return '0%'
  return `${Math.max(3, (value / total.value) * 100)}%`
}
</script>

<template>
  <section class="rounded-3xl border border-stone-200 bg-white p-5">
    <div class="flex items-center justify-between gap-3">
      <h3 class="font-black text-stone-950">{{ title }}</h3>
      <span class="text-sm font-black text-stone-500">{{ total }}</span>
    </div>
    <div v-if="total === 0" class="mt-4 rounded-2xl bg-stone-50 p-4 text-sm text-stone-600">暂无可视化数据。</div>
    <template v-else>
      <div class="mt-4 flex h-4 overflow-hidden rounded-full bg-stone-100">
        <div v-for="segment in segments" :key="segment.label" class="h-full" :class="segment.className" :style="{ width: segmentWidth(segment.value) }" />
      </div>
      <div class="mt-4 flex flex-wrap gap-2">
        <span v-for="segment in segments" :key="segment.label" class="rounded-full bg-stone-50 px-3 py-1 text-xs font-bold text-stone-700">
          {{ segment.label }} {{ segment.value }}
        </span>
      </div>
    </template>
  </section>
</template>
