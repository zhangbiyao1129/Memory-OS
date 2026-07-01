<script setup lang="ts">
const auth = useAuthStore()
const context = useContextStore()
const nav = [
  ['Dashboard', '/'],
  ['组织', '/orgs'],
  ['项目', '/projects'],
  ['Archive', '/archive'],
  ['Hot Memory', '/hot-memory'],
  ['Secret Vault', '/secrets'],
  ['Adapter Token', '/tokens'],
  ['Qdrant', '/qdrant'],
  ['检索测试', '/search-test']
]
</script>

<template>
  <div class="min-h-screen px-4 py-4 text-stone-950 sm:px-6 lg:px-8">
    <div class="mx-auto grid max-w-7xl gap-5 lg:grid-cols-[17rem_1fr]">
      <aside class="rounded-[2rem] border border-stone-900/10 bg-white/70 p-5 shadow-2xl shadow-stone-900/10 backdrop-blur">
        <NuxtLink to="/" class="block">
          <p class="text-xs uppercase tracking-[0.35em] text-orange-800">Memory OS</p>
          <h1 class="mt-2 text-3xl font-black tracking-tight">Native Memory Console</h1>
        </NuxtLink>
        <div class="mt-6 space-y-2 text-sm">
          <label class="block text-xs font-bold uppercase tracking-widest text-stone-500">Org / Project</label>
          <select v-model="context.orgId" class="w-full rounded-2xl border border-stone-300 bg-white px-3 py-2">
            <option value="org_1">org_1</option>
            <option value="org_lab">org_lab</option>
          </select>
          <select v-model="context.projectId" class="w-full rounded-2xl border border-stone-300 bg-white px-3 py-2" @change="context.setProject(context.projectId)">
            <option value="project_1">project_1</option>
            <option value="project_t480">project_t480</option>
          </select>
          <select v-model="context.agentId" class="w-full rounded-2xl border border-stone-300 bg-white px-3 py-2" @change="context.setAgent(context.agentId)">
            <option value="codex">codex</option>
            <option value="claude">claude</option>
            <option value="opencode">opencode</option>
            <option value="hermes">hermes</option>
          </select>
        </div>
        <nav class="mt-7 grid gap-2">
          <NuxtLink v-for="item in nav" :key="item[1]" :to="item[1]" class="rounded-2xl px-3 py-2 font-semibold text-stone-700 hover:bg-orange-100 hover:text-orange-900">
            {{ item[0] }}
          </NuxtLink>
        </nav>
        <div class="mt-7 rounded-3xl bg-stone-950 p-4 text-stone-50">
          <p class="text-xs text-stone-400">当前用户</p>
          <p class="font-bold">{{ auth.user.email }}</p>
          <button class="mt-3 rounded-full bg-white px-3 py-1 text-sm font-bold text-stone-950" @click="auth.logout()">登出</button>
        </div>
      </aside>
      <main class="rounded-[2rem] border border-white/70 bg-white/80 p-5 shadow-2xl shadow-stone-900/10 backdrop-blur sm:p-8">
        <slot />
      </main>
    </div>
  </div>
</template>
