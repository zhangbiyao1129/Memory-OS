<template>
  <AppShell>
    <h2 class="text-3xl font-black">用户 / 组织 / 权限</h2>
    <div class="mt-6 grid gap-4 lg:grid-cols-3">
      <section class="rounded-3xl border bg-white p-5"><h3 class="font-black">Users</h3><p class="mt-2 text-sm text-stone-600">alice@example.com · owner</p><p class="text-sm text-stone-600">dev@example.com · member</p></section>
      <section class="rounded-3xl border bg-white p-5"><h3 class="font-black">Orgs</h3><p class="mt-2 text-sm text-stone-600">org_1 · Memory Lab</p><p class="text-sm text-stone-600">org_lab · Adapter Lab</p></section>
      <section class="rounded-3xl border bg-white p-5"><h3 class="font-black">Permission Labels</h3><p class="mt-2 rounded-2xl bg-stone-100 p-2 text-sm">project:project_1:read</p><p class="mt-2 rounded-2xl bg-stone-100 p-2 text-sm">org:org_1:member</p></section>
    </div>
  </AppShell>
</template>
