import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const pageSource = readFileSync(resolve(__dirname, '../pages/candidates/index.vue'), 'utf8')

describe('candidates page', () => {
  it('keeps candidate maintenance self-driven without manual filter controls', () => {
    expect(pageSource).toContain('记忆整理')
    expect(pageSource).toContain('AI 整理')

    expect(pageSource).not.toContain('statusFilter')
    expect(pageSource).not.toContain('riskFilter')
    expect(pageSource).not.toContain('sourceKeyFilter')
    expect(pageSource).not.toContain('threadIdFilter')
    expect(pageSource).not.toContain('cleanSourceKey')
    expect(pageSource).not.toContain('cleanThreadId')
    expect(pageSource).not.toContain('composeTopic')
    expect(pageSource).not.toContain('指定主题沉淀')
    expect(pageSource).not.toContain('强制沉淀')
  })

  it('loads pending and archive material candidates by default without calling them all actionable', () => {
    expect(pageSource).toContain("VISIBLE_CANDIDATE_STATUSES = ['pending', 'in_compose_pool']")
    expect(pageSource).toContain('VISIBLE_CANDIDATE_STATUSES.map')
    expect(pageSource).toContain('loadProjectScopes()')
    expect(pageSource).toContain('useMemoryLifecycleStats({ userScoped: true })')
    expect(pageSource).toContain('全部工作区待确认')
    expect(pageSource).toContain('归档素材')
    expect(pageSource).toContain('status')
    expect(pageSource).toContain('待整理候选与归档素材')
    expect(pageSource).toContain('refreshCandidateView()')
    expect(pageSource).not.toContain('<h3 class="text-xl font-black">候选列表（当前页')
  })

  it('starts one workspace maintenance run instead of project fan-out', () => {
    expect(pageSource).toContain('async function runMaintenance()')
    expect(pageSource).toContain("'/memory/organize/workspace/run'")
    expect(pageSource).toContain("'/memory/organize/workspace/status'")
    expect(pageSource).toContain('系统按全部工作区项目自动整理候选')

    const runMaintenanceSource = pageSource.slice(
      pageSource.indexOf('async function runMaintenance()'),
      pageSource.indexOf('onMounted(async')
    )
    expect(runMaintenanceSource).not.toContain('body: requestBody()')
    expect(runMaintenanceSource).not.toContain("'/memory/candidates/maintenance/run'")
    expect(runMaintenanceSource).not.toContain("'/memory/candidates/maintenance/workspace/run'")
    expect(runMaintenanceSource).not.toContain('scopes.map')
  })
})
