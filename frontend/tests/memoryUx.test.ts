import { describe, expect, it } from 'vitest'
import {
  aggregateLifecycleStats,
  buildContextSummary,
  buildSecretCommandTemplates,
  explainLifecycle,
  explainSearchHit,
  formatCandidateMaintenanceSummary,
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

  it('aggregates lifecycle stats across projects for overview', () => {
    const stats = aggregateLifecycleStats([
      {
        archives: { total: 1, by_status: { active: 1 } },
        hot_memories: { total: 3, by_status: { active: 3 } },
	        candidates: {
	          total: 5,
	          actionable_total: 2,
	          pending_organize_total: 1,
	          archive_material_total: 1,
	          needs_review_total: 2,
	          by_status: { pending: 1, in_compose_pool: 1, promoted_to_hot: 3 },
          by_risk: { low: 5 },
          hot_score_buckets: [{ label: 'high', count: 1 }],
          compose_score_buckets: []
        },
        candidate_jobs: {
          total: 4,
          by_status: { done: 4 },
          pending: 0,
          running: 0,
          failed: 0,
          done: 4,
          latest_error: '',
          oldest_pending_at: '',
          last_completed_at: '2026-07-07T01:00:00Z'
        },
        topics: { total: 1, ready_to_compose: 0, composed: 0, open: 1 }
      },
      {
        archives: { total: 2, by_status: { active: 2 } },
        hot_memories: { total: 4, by_status: { active: 4 } },
	        candidates: {
	          total: 6,
	          actionable_total: 3,
	          pending_organize_total: 2,
	          archive_material_total: 1,
	          needs_review_total: 3,
	          by_status: { pending: 2, in_compose_pool: 1, promoted_to_hot: 3 },
          by_risk: { low: 4, medium: 2 },
          hot_score_buckets: [{ label: 'high', count: 2 }],
          compose_score_buckets: []
        },
        candidate_jobs: {
          total: 5,
          by_status: { done: 3, pending: 2 },
          pending: 2,
          running: 0,
          failed: 0,
          done: 3,
          latest_error: '',
          oldest_pending_at: '2026-07-07T02:00:00Z',
          last_completed_at: '2026-07-07T03:00:00Z'
        },
        topics: { total: 2, ready_to_compose: 1, composed: 1, open: 0 }
      }
    ])

    expect(stats.archives.total).toBe(3)
    expect(stats.hot_memories.total).toBe(7)
	    expect(stats.candidates.total).toBe(11)
	    expect(stats.candidates.actionable_total).toBe(5)
	    expect(stats.candidates.pending_organize_total).toBe(3)
	    expect(stats.candidates.archive_material_total).toBe(2)
	    expect(stats.candidates.needs_review_total).toBe(5)
    expect(stats.candidates.by_status.pending).toBe(3)
    expect(stats.candidates.by_status.in_compose_pool).toBe(2)
    expect(stats.candidate_jobs?.total).toBe(9)
    expect(stats.candidate_jobs?.pending).toBe(2)
    expect(stats.topics.total).toBe(3)
    expect(stats.topics.composed).toBe(1)
  })

  it('formats candidate maintenance results from one shared summary', () => {
	    expect(
	      formatCandidateMaintenanceSummary({
	        processed: 4,
	        discarded: 0,
	        kept: 1,
	        composed: 8,
	        archive_material: 2,
	        promoted_hot: 1,
	        needs_review: 1
	      })
	    ).toBe('整理完成：处理 4 条，丢弃 0 条，保留 1 条，归档素材 2 条，写入热记忆 1 条，待确认 1 条，生成归档 8 条。')

	    expect(formatCandidateMaintenanceSummary({ processed: undefined, discarded: Number.NaN, kept: 1, composed: 0 })).toBe(
	      '整理完成：处理 0 条，丢弃 0 条，保留 1 条，归档素材 0 条，写入热记忆 0 条，待确认 0 条，生成归档 0 条。'
	    )
  })
})
