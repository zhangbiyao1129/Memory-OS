import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const pageSource = readFileSync(resolve(__dirname, '../pages/memory/index.vue'), 'utf8')

describe('memory page', () => {
  it('uses the same user-scoped lifecycle stats as overview', () => {
    expect(pageSource).toContain('useMemoryLifecycleStats({ userScoped: true })')
    expect(pageSource).toContain('当前用户全部记忆生命周期')
    expect(pageSource).not.toContain('当前项目的记忆生命周期')
    expect(pageSource).not.toContain('请先选择组织和项目')
  })
})
