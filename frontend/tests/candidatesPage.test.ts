import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const pageSource = readFileSync(resolve(__dirname, '../pages/candidates/index.vue'), 'utf8')

describe('candidates page', () => {
  it('keeps candidate maintenance self-driven without manual filter controls', () => {
    expect(pageSource).toContain('候选维护')
    expect(pageSource).toContain('AI 清洗')

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
})
