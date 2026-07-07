import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const pageSource = readFileSync(resolve(__dirname, '../pages/memory/index.vue'), 'utf8')
const archivePageSource = readFileSync(resolve(__dirname, '../pages/archive/index.vue'), 'utf8')
const hotMemoryPageSource = readFileSync(resolve(__dirname, '../pages/hot-memory/index.vue'), 'utf8')
const topicsPageSource = readFileSync(resolve(__dirname, '../pages/topics/index.vue'), 'utf8')
const ragSearchWorkbenchSource = readFileSync(resolve(__dirname, '../components/RagSearchWorkbench.vue'), 'utf8')

describe('memory page', () => {
  it('uses the same user-scoped lifecycle stats as overview', () => {
    expect(pageSource).toContain('useMemoryLifecycleStats({ userScoped: true })')
    expect(pageSource).toContain('当前用户全部记忆生命周期')
    expect(pageSource).not.toContain('当前项目的记忆生命周期')
    expect(pageSource).not.toContain('请先选择组织和项目')
  })

  it('loads secondary memory lists across workspace projects', () => {
    for (const source of [archivePageSource, hotMemoryPageSource, topicsPageSource]) {
      expect(source).toContain('useWorkspaceProjectScopes')
      expect(source).not.toContain('按当前组织和项目读取')
      expect(source).not.toContain('当前过滤条件下暂无')
    }
  })

  it('runs the search workbench across all loaded workspace projects', () => {
    expect(ragSearchWorkbenchSource).toContain('searchProjectScopes')
    expect(ragSearchWorkbenchSource).toContain('Promise.all')
    expect(ragSearchWorkbenchSource).toContain('mergeSearchResponses')
    expect(ragSearchWorkbenchSource).not.toContain("actor: { user_id: '', org_id: context.orgId, project_id: context.projectId")
    expect(ragSearchWorkbenchSource).not.toContain('请先选择组织和项目')
  })
})
