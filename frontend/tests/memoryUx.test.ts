import { describe, expect, it } from 'vitest'
import {
  buildContextSummary,
  buildSecretCommandTemplates,
  explainLifecycle,
  explainSearchHit,
  friendlyApiError
} from '../utils/memoryUx'

describe('memory UX helpers', () => {
  it('summarizes selected org, project, and agent without exposing ids as primary copy', () => {
    const summary = buildContextSummary({
      orgName: '个人空间',
      projectName: 'Memory OS',
      agentId: 'codex'
    })

    expect(summary.primary).toBe('个人空间 / Memory OS')
    expect(summary.secondary).toBe('Agent: codex')
  })

  it('uses readable fallback when context is missing', () => {
    const summary = buildContextSummary({})
    expect(summary.primary).toBe('未选择组织 / 未选择项目')
    expect(summary.secondary).toBe('Agent: codex')
  })

  it('explains lifecycle layers in user language', () => {
    const steps = explainLifecycle({
      candidateJobsDone: 3,
      candidateJobsFailed: 1,
      candidatesActionable: 2,
      hotMemories: 5,
      archives: 0
    })

    expect(steps.map((step) => step.label)).toEqual(['已接收', '已提炼', '待确认', '可快速召回', '未长期归档'])
    expect(steps[4].tone).toBe('warning')
  })

  it('builds local secret MCP templates without a plaintext value', () => {
    const templates = buildSecretCommandTemplates({
      name: 'gitlab-ee-pat',
      envName: 'GITLAB_TOKEN',
      site: 'gitlab-ee',
      purpose: 'GitLab API'
    })

    expect(templates.create).toContain('secret_create_local')
    expect(templates.create).toContain('<在本机输入明文>')
    expect(templates.create).not.toContain('真实令牌值')
    expect(templates.use).toContain('secret_use_local')
  })

  it('turns search source kinds into readable labels', () => {
    expect(explainSearchHit({ kind: 'hot_memory' })).toBe('快速记忆')
    expect(explainSearchHit({ kind: 'archive_chunk' })).toBe('长期归档')
    expect(explainSearchHit({ kind: 'unknown' })).toBe('检索结果')
    expect(explainSearchHit(undefined)).toBe('检索结果')
  })

  it('turns network failures into readable API errors', () => {
    const message = friendlyApiError({
      message: '[POST] "http://127.0.0.1:18081/memory/stats/lifecycle": <no response> Failed to fetch'
    })

    expect(message).toBe('Memory OS API 暂不可用，请确认后端服务已启动。')
  })

  it('keeps backend error messages readable', () => {
    expect(friendlyApiError({ data: { message: '项目加载失败' } })).toBe('项目加载失败')
    expect(friendlyApiError({ data: { error: '缺少权限' } })).toBe('缺少权限')
  })
})
