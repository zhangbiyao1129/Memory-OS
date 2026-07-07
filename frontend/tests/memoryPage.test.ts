import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const pageSource = readFileSync(resolve(__dirname, '../pages/memory/index.vue'), 'utf8')
const archivePageSource = readFileSync(resolve(__dirname, '../pages/archive/index.vue'), 'utf8')
const hotMemoryPageSource = readFileSync(resolve(__dirname, '../pages/hot-memory/index.vue'), 'utf8')
const topicsPageSource = readFileSync(resolve(__dirname, '../pages/topics/index.vue'), 'utf8')

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
})
