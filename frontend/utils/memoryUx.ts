export type ContextSummaryInput = {
  orgName?: string
  projectName?: string
  agentId?: string
}

export function buildContextSummary(input: ContextSummaryInput) {
  return {
    primary: `${input.orgName || '未选择组织'} / ${input.projectName || '未选择项目'}`,
    secondary: `Agent: ${input.agentId || 'codex'}`
  }
}

export type LifecycleInput = {
  candidateJobsDone?: number
  candidateJobsFailed?: number
  candidatesActionable?: number
  hotMemories?: number
  archives?: number
}

export type LifecycleStep = {
  label: string
  detail: string
  tone: 'ok' | 'warning' | 'error' | 'muted'
}

export function explainLifecycle(input: LifecycleInput): LifecycleStep[] {
  return [
    { label: '已接收', detail: `${input.candidateJobsDone || 0} 个提炼任务完成`, tone: 'ok' },
    {
      label: '已提炼',
      detail: `${input.candidateJobsFailed || 0} 个任务失败`,
      tone: input.candidateJobsFailed ? 'error' : 'ok'
    },
    {
      label: '待确认',
      detail: `${input.candidatesActionable || 0} 条候选需要处理`,
      tone: input.candidatesActionable ? 'warning' : 'muted'
    },
    { label: '可快速召回', detail: `${input.hotMemories || 0} 条热记忆`, tone: input.hotMemories ? 'ok' : 'muted' },
    {
      label: input.archives ? '已长期归档' : '未长期归档',
      detail: `${input.archives || 0} 个归档`,
      tone: input.archives ? 'ok' : 'warning'
    }
  ]
}

export type SecretTemplateInput = {
  name?: string
  envName?: string
  site?: string
  purpose?: string
}

export function buildSecretCommandTemplates(input: SecretTemplateInput) {
  const name = input.name || 'my-secret'
  const envName = input.envName || 'SECRET_VALUE'
  const site = input.site || 'example'
  const purpose = input.purpose || '本机工具调用'
  return {
    create: `secret_create_local name=${name} env_name=${envName} site=${site} purpose="${purpose}" value="<在本机输入明文>"`,
    list: 'secret_list_local status=active',
    use: `secret_use_local secret_ref=<复制 secret_ref> command="<需要注入 ${envName} 的本机命令>"`,
    disable: 'secret_disable_local secret_ref=<复制 secret_ref>'
  }
}

export function explainSearchHit(source?: { kind?: string }) {
  if (source?.kind === 'hot_memory') return '快速记忆'
  if (source?.kind === 'archive_chunk') return '长期归档'
  return '检索结果'
}

export function friendlyApiError(error: any) {
  const message = String(error?.data?.message || error?.data?.error || error?.message || '').trim()
  if (
    !message ||
    message.includes('Failed to fetch') ||
    message.includes('<no response>') ||
    message.includes('NetworkError') ||
    message.includes('Load failed')
  ) {
    return 'Memory OS API 暂不可用，请确认后端服务已启动。'
  }
  return message
}
