<script setup lang="ts">
const entries = [
  { label: '组织', eyebrow: 'Workspace', to: '/orgs', description: '单公司默认自动选择，只有治理和整理时需要进入。' },
  { label: '用户', eyebrow: 'Users', to: '/users', description: '查看和维护用户 metadata，不展示密码、token 或凭据字段。' },
  { label: '项目', eyebrow: 'Projects', to: '/projects', description: '查看 Agent 按目录 / Git 自动识别出的项目空间。' },
  { label: 'MCP 接入', eyebrow: 'MCP', to: '/tokens', description: '创建通用 MCP Token，用于把 Agent 接入 Memory OS。' },
  { label: '成员权限', eyebrow: 'Members', to: '/permissions', description: '多人协作时才需要，把用户加入当前项目并选择权限等级。' },
  { label: '角色目录', eyebrow: 'Roles', to: '/roles', description: '高级管理员入口，管理角色显示名、描述和权限标签。' },
  { label: 'Secret Vault', eyebrow: 'Vault', to: '/secrets', description: '系统保留能力，仅管理 metadata，不作为日常记忆入口。' },
  { label: '索引诊断', eyebrow: 'Index', to: '/qdrant', description: '排查向量索引、payload schema 和 query-time filter 状态。' }
]
</script>

<template>
  <AppShell>
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <p class="text-xs font-black uppercase tracking-[0.35em] text-orange-800">Advanced</p>
        <h2 class="mt-2 text-3xl font-black">高级设置</h2>
        <p class="mt-2 max-w-3xl text-stone-600">
          日常使用可以忽略这里。这里保留多人协作、项目治理和底层权限调试入口，避免把复杂能力放在主流程里干扰 MCP 自动记忆。
        </p>
      </div>
    </div>

    <section class="mt-6 grid gap-4 md:grid-cols-2">
      <NuxtLink v-for="entry in entries" :key="entry.to" :to="entry.to" class="group rounded-3xl border bg-white p-6 shadow-sm transition hover:-translate-y-0.5 hover:border-orange-200 hover:shadow-xl">
        <p class="text-xs font-black uppercase tracking-widest text-orange-800">{{ entry.eyebrow }}</p>
        <h3 class="mt-3 text-2xl font-black">{{ entry.label }}</h3>
        <p class="mt-2 text-sm leading-6 text-stone-600">{{ entry.description }}</p>
        <p class="mt-5 text-sm font-black text-stone-950 group-hover:text-orange-900">进入{{ entry.label }} →</p>
      </NuxtLink>
    </section>
  </AppShell>
</template>
