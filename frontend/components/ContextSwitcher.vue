<script setup lang="ts">
import { buildContextSummary } from '~/utils/memoryUx'

const context = useContextStore()

const selectedOrg = computed(() => context.orgs.find((org) => org.org_id === context.orgId))
const selectedProject = computed(() => context.projects.find((project) => project.project_id === context.projectId))
const summary = computed(() => buildContextSummary({
  orgName: selectedOrg.value?.name,
  projectName: selectedProject.value?.name,
  agentId: context.agentId
}))

const agentOptions = [
  { value: 'codex', label: 'Codex' },
  { value: 'claude-code', label: 'Claude Code' },
  { value: 'opencode', label: 'opencode' },
  { value: 'manual', label: 'manual' }
]
</script>

<template>
  <section class="rounded-2xl bg-orange-50 p-4 text-sm text-orange-950">
    <p class="text-xs font-black uppercase tracking-widest text-orange-800">当前记忆上下文</p>
    <p class="mt-2 font-black leading-snug">{{ summary.primary }}</p>
    <p class="mt-1 text-xs font-bold text-orange-800">{{ summary.secondary }}</p>

    <label class="mt-3 block text-xs font-bold">
      组织
      <select
        :value="context.orgId"
        class="mt-1 w-full rounded-xl border border-orange-200 bg-white px-2 py-2 text-stone-950"
        @change="context.setOrg(($event.target as HTMLSelectElement).value)"
      >
        <option v-if="context.orgs.length === 0" value="">未加载组织</option>
        <option v-for="org in context.orgs" :key="org.org_id" :value="org.org_id">{{ org.name }}</option>
      </select>
    </label>

    <label class="mt-2 block text-xs font-bold">
      项目
      <select
        :value="context.projectId"
        class="mt-1 w-full rounded-xl border border-orange-200 bg-white px-2 py-2 text-stone-950"
        @change="context.setProject(($event.target as HTMLSelectElement).value)"
      >
        <option v-if="context.projects.length === 0" value="">未选择项目</option>
        <option v-for="project in context.projects" :key="project.project_id" :value="project.project_id">{{ project.name }}</option>
      </select>
    </label>

    <label class="mt-2 block text-xs font-bold">
      Agent
      <select
        :value="context.agentId"
        class="mt-1 w-full rounded-xl border border-orange-200 bg-white px-2 py-2 text-stone-950"
        @change="context.setAgent(($event.target as HTMLSelectElement).value)"
      >
        <option v-for="agent in agentOptions" :key="agent.value" :value="agent.value">{{ agent.label }}</option>
      </select>
    </label>
  </section>
</template>
