<script setup lang="ts">
const props = defineProps<{ value?: string; revealLabel?: string }>()
const revealed = ref(false)
const masked = computed(() => props.value ? '*'.repeat(Math.min(16, props.value.length)) : '未设置')
</script>

<template>
  <div class="rounded-2xl border border-amber-200 bg-amber-50 p-3">
    <p class="text-xs font-bold uppercase tracking-widest text-amber-700">Secret 明文保护</p>
    <div class="mt-2 flex items-center justify-between gap-3">
      <code class="break-all text-sm">{{ revealed ? value : masked }}</code>
      <button class="rounded-full bg-amber-900 px-3 py-1 text-sm font-bold text-white" @click="revealed = !revealed">{{ revealed ? '隐藏' : (revealLabel || '临时显示') }}</button>
    </div>
  </div>
</template>
