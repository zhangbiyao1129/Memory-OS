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
      <NuxtLink to="/permissions" class="group rounded-3xl border bg-white p-6 shadow-sm transition hover:-translate-y-0.5 hover:border-orange-200 hover:shadow-xl">
        <p class="text-xs font-black uppercase tracking-widest text-orange-800">Members</p>
        <h3 class="mt-3 text-2xl font-black">成员权限</h3>
        <p class="mt-2 text-sm leading-6 text-stone-600">
          多人协作时才需要：把用户加入当前项目，并选择所有者、协作者或只读者。单人使用 Memory OS 可以不用打开。
        </p>
        <p class="mt-5 text-sm font-black text-stone-950 group-hover:text-orange-900">进入成员权限 →</p>
      </NuxtLink>

      <NuxtLink to="/roles" class="group rounded-3xl border bg-white p-6 shadow-sm transition hover:-translate-y-0.5 hover:border-orange-200 hover:shadow-xl">
        <p class="text-xs font-black uppercase tracking-widest text-orange-800">Roles</p>
        <h3 class="mt-3 text-2xl font-black">角色目录</h3>
        <p class="mt-2 text-sm leading-6 text-stone-600">
          高级管理员入口：管理角色显示名、描述和系统权限标签。普通项目使用默认角色即可，不需要手写权限标签。
        </p>
        <p class="mt-5 text-sm font-black text-stone-950 group-hover:text-orange-900">进入角色目录 →</p>
      </NuxtLink>
    </section>
  </AppShell>
</template>
