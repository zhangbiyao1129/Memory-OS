<template>
  <AppShell>
    <h2 class="text-3xl font-black">Qdrant 索引状态</h2>
    <div class="mt-6 grid gap-4 md:grid-cols-3">
      <section class="rounded-3xl border bg-white p-5"><p class="text-sm text-stone-500">Collection</p><h3 class="mt-2 font-black">memory_os</h3><p class="text-sm">single collection</p></section>
      <section class="rounded-3xl border bg-white p-5"><p class="text-sm text-stone-500">Payload Filter</p><h3 class="mt-2 font-black">query-time enforced</h3><p class="text-sm">user/org/project/visibility/labels</p></section>
      <section class="rounded-3xl border bg-white p-5"><p class="text-sm text-stone-500">T480</p><h3 class="mt-2 font-black">max_search_threads=2</h3><p class="text-sm">single-node profile</p></section>
    </div>
  </AppShell>
</template>
