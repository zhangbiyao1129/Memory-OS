# Memory Kernel 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 在现有 Memory OS 记忆链路上新增 Memory Kernel，让系统能自动生成当前可信记忆单元、识别过期/冲突/重复记忆、构建面向 Agent 任务的 Context Pack，并用 Memory CI 验证召回结果不被旧事实污染。

**架构：** 保留 `turn_events -> candidate_memories -> hot_memories/archive -> retrieval` 主链路，新增 `internal/memorykernel` 作为跨层治理服务。Memory Kernel 从候选、热记忆、归档、检索日志和最近事件收集上下文，调用 LLM 产出严格 JSON 决策，经本地校验后软更新候选/热记忆、生成可信 `memory_units`、记录 governance action，并按需创建修订 Archive。检索层先召回 `memory_units` 生成 canonical context，再合并 Hot Memory 与 Archive RAG。

**技术栈：** Go/Hertz、pgx/v5、PostgreSQL migrations、Nuxt 3、Qdrant、现有 `llm.ChatClient`、现有 `archive.Service`、现有 `retrieval.Service`、现有 `memorystats.Service`、Docker Compose T480 部署 SOP。

---

## 规格说明

### 用户目标

用户不想手工整理记忆。系统必须在后台高频使用 LLM 和 embedding 能力，自动把大量跨项目、无目录、项目内对话整理成可被 Agent 正确使用的当前上下文。

### 关键行为

- 原始流水继续完整保存到 `turn_events`。
- 候选记忆继续保存到 `candidate_memories`。
- Memory Kernel 从候选、热记忆、归档、最近事件和检索日志中生成 `memory_units`。
- `memory_units` 表示 Agent 以后应该相信和使用的当前记忆，类型包含 `fact`、`preference`、`procedure`、`decision`、`risk`、`task`、`environment`、`evidence`。
- `memory_claims` 只承载事实类冲突判断，作为 `memory_units` 的附属结构。
- Governance 只软处理：不物理删除、不直接改写旧 Archive、不自动确认高风险内容。
- 旧 Archive 保留历史证据价值；错误或过期内容通过新建“记忆修订” Archive 覆盖。
- `memory_search` 和新增 `memory_context_pack` 必须优先返回 `status=current` 的 Memory Unit。
- Memory CI 自动验证关键问题能召回新事实，并且旧事实不会污染结果。

### 非目标

- 不引入独立图数据库。
- 不把 Qdrant 改成多 collection。
- 不物理删除旧候选、旧热记忆或旧归档。
- 不让 LLM 绕过本地 ID 校验、权限校验、Secret 脱敏和高风险保护。

### 安全约束

- LLM 输入使用已经脱敏的事件与内容；输出再次经过 `secret.Sanitize`。
- 任何 `risk_level=high` 的候选只允许 `needs_review` 或生成高风险 `memory_unit`，不能自动 discard/promote/archive。
- `memory_units`、`memory_claims`、governance action、CI case、CI result 不能保存 secret 明文。
- HTTP 和 MCP 输出不能暴露 secret 明文。
- 所有检索仍使用 query-time payload filter；新增 Memory Unit 检索第一版走 PostgreSQL ILIKE/score，不写入 Qdrant。

### 验收基准

固定验收数据：

- 旧候选：`memory_archive 和 memory_stats 尚未实现`
- 新事实：`memory_archive 和 memory_stats 已实现、已部署、MCP /tools 可见`
- 问题：`memory_archive 现在实现了吗？`

通过标准：

- Governance run 后旧候选被标记为 `discarded` 或 action 中记录 `superseded`。
- 生成当前 `memory_unit`：`memory_archive 和 memory_stats 已实现并部署`。
- 若存在旧热记忆，状态被改为 `demoted`。
- 生成或复用一条 correction archive，标题以 `记忆修订:` 开头。
- `memory_search` 或 `memory_context_pack` 对验收问题返回新事实，结果中不出现 `尚未实现`、`阶段 1 未实现`。
- Memory CI case 状态为 `passed`，并记录 request_id、结果摘要、must_include/must_not_include 检查结果。

---

## 文件结构

### 新增后端包

- 创建：`internal/memorykernel/model.go`
  - 定义 `MemoryUnit`、`MemoryClaim`、`GovernanceRun`、`GovernanceAction`、`ContextPack`、`CICase`、`CIResult`。
- 创建：`internal/memorykernel/repository.go`
  - 定义 Repository 接口和 InMemoryRepository，服务测试先基于内存实现。
- 创建：`internal/memorykernel/pg_repository.go`
  - PostgreSQL 实现，负责 `memory_units`、`memory_claims`、`memory_governance_runs`、`memory_governance_actions`、`memory_ci_cases`、`memory_ci_results`。
- 创建：`internal/memorykernel/classifier.go`
  - LLM classifier，构造体检包、解析 JSON、校验 ID 和动作合法性。
- 创建：`internal/memorykernel/collector.go`
  - 从 candidate/hot/archive/retrieval/eventlog 收集 scope 输入。
- 创建：`internal/memorykernel/service.go`
  - Governance 主服务，执行 collect -> classify -> validate -> apply -> CI case generation。
- 创建：`internal/memorykernel/context_pack.go`
  - Context Pack 构建器，按任务意图选取 Memory Unit。
- 创建：`internal/memorykernel/ci.go`
  - Memory CI 执行器，调用 retrieval/context pack 验证 must_include 和 must_not_include。
- 创建：`internal/memorykernel/http.go`
  - HTTP DTO 和 response helper，避免继续膨胀 `internal/http/router.go`。

### 新增测试

- 创建：`internal/memorykernel/classifier_test.go`
- 创建：`internal/memorykernel/service_test.go`
- 创建：`internal/memorykernel/context_pack_test.go`
- 创建：`internal/memorykernel/ci_test.go`
- 创建：`internal/memorykernel/pg_repository_test.go`
- 修改：`internal/http/router_test.go`
- 修改：`internal/retrieval/service_test.go`
- 修改：`cmd/memory-worker/main_test.go`
- 修改：`cmd/memory-api/main_test.go`
- 修改：`cmd/memory-mcp/main_test.go`
- 修改：`internal/memorystats/service_test.go`

### 新增迁移

- 创建：`migrations/000029_memory_kernel.sql`

### 修改现有后端

- 修改：`internal/candidatememory/model.go`
  - 增加状态常量：`superseded`。
- 修改：`internal/candidatememory/repository.go`
  - 增加 `UpdateCandidateGovernance`，用于记录 governance 后的软状态。
- 修改：`internal/candidatememory/pg_repository.go`
  - 支持 `governance_status`、`governance_reason`、`superseded_by` 字段。
- 修改：`internal/hotmemory/repository.go`
  - 如果现有接口不足，增加按 `memory_id` 降级并记录原因的实现。
- 修改：`internal/hotmemory/pg_repository.go`
  - 复用 `Update` 降级，不新增物理删除。
- 修改：`internal/retrieval/model.go`
  - 增加 `SourceMemoryUnit` 和 `MemoryUnitID` source ref。
- 修改：`internal/retrieval/service.go`
  - collect 阶段优先注入 canonical Memory Unit candidates。
- 修改：`internal/memorystats/model.go`
  - 增加 `MemoryKernelStats`。
- 修改：`internal/memorystats/pg_repository.go`
  - 统计 memory_units、governance runs、CI results。
- 修改：`internal/http/router.go`
  - 注册 Memory Kernel API。
- 修改：`cmd/memory-api/main.go`
  - 装配 `memorykernel.Service`、`ContextPackService`、`CIRunner`。
- 修改：`cmd/memory-worker/main.go`
  - 装配后台 governance maintenance loop。
- 修改：`internal/jobs/runner.go`
  - 增加 `MemoryKernelMaintenance` loop。
- 修改：`internal/mcp/schema.go`
  - 新增 `memory_context_pack` tool schema。
- 修改：`cmd/memory-mcp/main.go`
  - 暴露 `memory_context_pack` tool。

### 修改前端

- 创建：`frontend/pages/memory-kernel/index.vue`
  - 记忆体检、可信事实、冲突/过期、CI 状态。
- 修改：`frontend/pages/candidates/index.vue`
  - 文案从“候选记忆”收敛到“待整理素材”，显示 governance 状态。
- 修改：`frontend/pages/topics/index.vue`
  - 文案从“主题沉淀”调整为“长期归档”。
- 修改：`frontend/pages/hot-memory/index.vue`
  - 显示“热记忆不是事实真相，低信号会被治理降级”。
- 修改：`frontend/composables/useMemoryLifecycleStats.ts`
  - 展示 Memory Kernel 统计。
- 修改：`frontend/pages/diagnostics/index.vue`
  - 加入 governance run 和 Memory CI 摘要。

### 文档与部署

- 修改：`README.md`
  - 增加 Memory Kernel 概念和使用方式。
- 修改：`DEPLOYMENT.md`
  - 增加 Memory Kernel 部署后验收项。
- 修改：`docs/superpowers/plans/2026-07-09-memory-kernel.md`
  - 实现过程中持续更新复选框，不提交此过程文档，除非用户明确要求提交计划文档。

---

## 任务 0：基线确认与保护

**文件：**
- 读取：`AGENTS.md`
- 读取：`DEPLOYMENT.md`
- 读取：`internal/candidatememory/maintenance.go`
- 读取：`internal/retrieval/service.go`
- 读取：`cmd/memory-worker/main.go`

- [ ] **步骤 0.1：确认工作区状态**

运行：

```bash
git status --short
```

预期：记录当前 dirty 文件。若存在用户未提交改动，后续只改本计划列出的文件，不回滚用户改动。

- [ ] **步骤 0.2：确认当前测试基线**

运行：

```bash
go test ./...
npm --prefix frontend run build
make secret-scan
```

预期：全部 PASS。若存在和本任务无关的已知失败，记录失败命令、失败测试名、失败日志摘要，再继续写新失败测试。

- [ ] **步骤 0.3：Commit 基线记录**

若基线本身无源码变化，不提交。若为了修复基线测试做了改动，运行：

```bash
git status --short
git add internal cmd frontend migrations README.md DEPLOYMENT.md Makefile scripts
git commit -m "test: restore memory kernel baseline"
```

提交前必须用 `git diff --cached --name-only` 确认暂存内容只包含基线修复涉及的文件；若命令暂存了无关文件，使用 `git restore --staged path/to/unrelated-file` 只取消暂存，不回滚文件内容。

---

## 任务 1：数据库迁移与核心模型

**文件：**
- 创建：`migrations/000029_memory_kernel.sql`
- 创建：`internal/memorykernel/model.go`
- 修改：`internal/db/migrations_test.go`

- [ ] **步骤 1.1：编写迁移测试，先失败**

在 `internal/db/migrations_test.go` 中增加断言，要求 embedded migrations 包含以下片段：

```go
required := []string{
	"CREATE TABLE IF NOT EXISTS memory_units",
	"CREATE TABLE IF NOT EXISTS memory_claims",
	"CREATE TABLE IF NOT EXISTS memory_governance_runs",
	"CREATE TABLE IF NOT EXISTS memory_governance_actions",
	"CREATE TABLE IF NOT EXISTS memory_ci_cases",
	"CREATE TABLE IF NOT EXISTS memory_ci_results",
	"CREATE INDEX IF NOT EXISTS memory_units_scope_status_idx",
	"CREATE UNIQUE INDEX IF NOT EXISTS memory_claims_unit_subject_predicate_unique",
}
```

运行：

```bash
go test ./internal/db -run TestEmbeddedMigrationsContainExpectedSchema -count=1
```

预期：FAIL，提示缺少 Memory Kernel 表。

- [ ] **步骤 1.2：创建迁移**

创建 `migrations/000029_memory_kernel.sql`：

```sql
CREATE TABLE IF NOT EXISTS memory_units (
    id BIGSERIAL PRIMARY KEY,
    unit_id TEXT NOT NULL UNIQUE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    source_key TEXT NOT NULL DEFAULT '',
    thread_id TEXT NOT NULL DEFAULT '',
    user_id TEXT NOT NULL,
    agent_id TEXT NOT NULL DEFAULT '',
    unit_type TEXT NOT NULL,
    content TEXT NOT NULL,
    applies_when TEXT NOT NULL DEFAULT '',
    agent_should TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'current',
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    trust_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    risk_level TEXT NOT NULL DEFAULT 'low',
    source_refs JSONB NOT NULL DEFAULT '[]',
    superseded_by TEXT NOT NULL DEFAULT '',
    valid_from TIMESTAMPTZ,
    valid_to TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_units_scope_status_idx
    ON memory_units (org_id, project_id, source_key, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS memory_units_type_status_idx
    ON memory_units (unit_type, status, trust_score DESC);

CREATE TABLE IF NOT EXISTS memory_claims (
    id BIGSERIAL PRIMARY KEY,
    claim_id TEXT NOT NULL UNIQUE,
    unit_id TEXT NOT NULL REFERENCES memory_units(unit_id) ON DELETE CASCADE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    subject TEXT NOT NULL,
    predicate TEXT NOT NULL,
    value TEXT NOT NULL,
    polarity TEXT NOT NULL DEFAULT 'positive',
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    evidence_refs JSONB NOT NULL DEFAULT '[]',
    observed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS memory_claims_unit_subject_predicate_unique
    ON memory_claims (unit_id, subject, predicate);

CREATE INDEX IF NOT EXISTS memory_claims_scope_lookup_idx
    ON memory_claims (org_id, project_id, subject, predicate, created_at DESC);

CREATE TABLE IF NOT EXISTS memory_governance_runs (
    id BIGSERIAL PRIMARY KEY,
    run_id TEXT NOT NULL UNIQUE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    source_key TEXT NOT NULL DEFAULT '',
    thread_id TEXT NOT NULL DEFAULT '',
    trigger_type TEXT NOT NULL DEFAULT 'manual',
    status TEXT NOT NULL DEFAULT 'running',
    processed_candidates INTEGER NOT NULL DEFAULT 0,
    processed_hot_memories INTEGER NOT NULL DEFAULT 0,
    processed_archives INTEGER NOT NULL DEFAULT 0,
    created_units INTEGER NOT NULL DEFAULT 0,
    superseded_units INTEGER NOT NULL DEFAULT 0,
    stale_candidates INTEGER NOT NULL DEFAULT 0,
    demoted_hot_memories INTEGER NOT NULL DEFAULT 0,
    ci_cases_created INTEGER NOT NULL DEFAULT 0,
    ci_cases_passed INTEGER NOT NULL DEFAULT 0,
    correction_archive_id TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_governance_runs_scope_idx
    ON memory_governance_runs (org_id, project_id, source_key, thread_id, started_at DESC);

CREATE INDEX IF NOT EXISTS memory_governance_runs_status_idx
    ON memory_governance_runs (status, updated_at DESC);

CREATE TABLE IF NOT EXISTS memory_governance_actions (
    id BIGSERIAL PRIMARY KEY,
    action_id TEXT NOT NULL UNIQUE,
    run_id TEXT NOT NULL REFERENCES memory_governance_runs(run_id) ON DELETE CASCADE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    action TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    evidence_refs JSONB NOT NULL DEFAULT '[]',
    applied BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_governance_actions_target_idx
    ON memory_governance_actions (org_id, project_id, target_type, target_id, created_at DESC);

CREATE TABLE IF NOT EXISTS memory_ci_cases (
    id BIGSERIAL PRIMARY KEY,
    case_id TEXT NOT NULL UNIQUE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    source_key TEXT NOT NULL DEFAULT '',
    question TEXT NOT NULL,
    must_include TEXT[] NOT NULL DEFAULT '{}',
    must_not_include TEXT[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'active',
    source_run_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_ci_cases_scope_status_idx
    ON memory_ci_cases (org_id, project_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS memory_ci_results (
    id BIGSERIAL PRIMARY KEY,
    result_id TEXT NOT NULL UNIQUE,
    case_id TEXT NOT NULL REFERENCES memory_ci_cases(case_id) ON DELETE CASCADE,
    run_id TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    passed BOOLEAN NOT NULL DEFAULT false,
    matched_include TEXT[] NOT NULL DEFAULT '{}',
    matched_exclude TEXT[] NOT NULL DEFAULT '{}',
    response_excerpt TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_ci_results_case_idx
    ON memory_ci_results (case_id, created_at DESC);
```

- [ ] **步骤 1.3：创建模型**

创建 `internal/memorykernel/model.go`，定义：

```go
package memorykernel

import "time"

type UnitType string

const (
	UnitFact        UnitType = "fact"
	UnitPreference  UnitType = "preference"
	UnitProcedure   UnitType = "procedure"
	UnitDecision    UnitType = "decision"
	UnitRisk        UnitType = "risk"
	UnitTask        UnitType = "task"
	UnitEnvironment UnitType = "environment"
	UnitEvidence    UnitType = "evidence"
)

type UnitStatus string

const (
	UnitCurrent     UnitStatus = "current"
	UnitStale       UnitStatus = "stale"
	UnitSuperseded  UnitStatus = "superseded"
	UnitHistorical  UnitStatus = "historical"
	UnitNeedsReview UnitStatus = "needs_review"
)

type SourceRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type MemoryUnit struct {
	UnitID       string
	OrgID        string
	ProjectID    string
	SourceKey    string
	ThreadID     string
	UserID       string
	AgentID      string
	Type         UnitType
	Content      string
	AppliesWhen  string
	AgentShould  string
	Status       UnitStatus
	Confidence   float64
	TrustScore   float64
	RiskLevel    string
	SourceRefs   []SourceRef
	SupersededBy string
	ValidFrom    *time.Time
	ValidTo      *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type MemoryClaim struct {
	ClaimID      string
	UnitID       string
	OrgID        string
	ProjectID    string
	Subject      string
	Predicate    string
	Value        string
	Polarity     string
	Confidence   float64
	EvidenceRefs []SourceRef
	ObservedAt   *time.Time
	CreatedAt    time.Time
}
```

同文件继续定义 run/action/CI/context pack 类型，字段名必须与迁移一致。还必须定义治理输入输出类型，供 classifier、collector、service、HTTP、MCP 共用：

```go
type Scope struct {
	OrgID     string
	ProjectID string
	SourceKey string
	ThreadID  string
	UserID    string
	AgentID   string
}

type CandidateInput struct {
	ID         string
	Content    string
	Summary    string
	Type       string
	RiskLevel  string
	Status     string
	Confidence float64
}

type HotMemoryInput struct {
	ID            string
	Fact          string
	Status        string
	AccessCount   int
	ReturnedCount int
	UsedCount     int
	Pinned        bool
}

type ArchiveInput struct {
	ID        string
	Title     string
	Excerpt   string
	Status    string
	UpdatedAt time.Time
}

type RetrievalInput struct {
	RequestID  string
	SourceKind string
	SourceID   string
	CreatedAt  time.Time
}

type ClassifyInput struct {
	Scope         Scope
	Candidates    []CandidateInput
	HotMemories   []HotMemoryInput
	Archives      []ArchiveInput
	Retrievals     []RetrievalInput
	ExistingUnits []MemoryUnit
}

type ClassifyResult struct {
	Units   []MemoryUnit
	Claims  []MemoryClaim
	Actions []GovernanceAction
	CICases []CICase
	Summary string
}
```

- [ ] **步骤 1.4：验证迁移和模型**

运行：

```bash
go test ./internal/db ./internal/memorykernel -run Test -count=1
```

预期：PASS。

- [ ] **步骤 1.5：Commit**

```bash
git add migrations/000029_memory_kernel.sql internal/memorykernel/model.go internal/db/migrations_test.go
git commit -m "feat(memory): add memory kernel schema"
```

---

## 任务 2：Repository 与软治理状态

**文件：**
- 创建：`internal/memorykernel/repository.go`
- 创建：`internal/memorykernel/pg_repository.go`
- 创建：`internal/memorykernel/pg_repository_test.go`
- 修改：`internal/candidatememory/model.go`
- 修改：`internal/candidatememory/repository.go`
- 修改：`internal/candidatememory/pg_repository.go`
- 修改：`migrations/000029_memory_kernel.sql`

- [ ] **步骤 2.1：编写 Repository 测试，先失败**

创建 `internal/memorykernel/pg_repository_test.go`，测试：

```go
func memoryKernelPGTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.RunEmbeddedMigrations(context.Background(), pool); err != nil {
		t.Fatalf("RunEmbeddedMigrations() error = %v", err)
	}
	return pool
}

func TestPGRepositoryUpsertsUnitClaimAndGovernanceAction(t *testing.T) {
	ctx := context.Background()
	pool := memoryKernelPGTestPool(t)
	repo := NewPGRepository(pool)

	run, err := repo.CreateRun(ctx, GovernanceRun{
		RunID: "gov_run_repo_1", OrgID: "org_1", ProjectID: "project_1", TriggerType: "manual",
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if run.RunID != "gov_run_repo_1" || run.Status != RunRunning {
		t.Fatalf("run = %#v", run)
	}

	unit, err := repo.UpsertUnit(ctx, MemoryUnit{
		UnitID: "unit_repo_1", OrgID: "org_1", ProjectID: "project_1", UserID: "user_1",
		Type: UnitFact, Content: "memory_archive 已实现并部署", Status: UnitCurrent,
		Confidence: 0.95, TrustScore: 0.9,
		SourceRefs: []SourceRef{{Kind: "candidate", ID: "cand_new"}},
	})
	if err != nil {
		t.Fatalf("UpsertUnit() error = %v", err)
	}
	if unit.Status != UnitCurrent || unit.Content == "" {
		t.Fatalf("unit = %#v", unit)
	}

	claim, err := repo.UpsertClaim(ctx, MemoryClaim{
		ClaimID: "claim_repo_1", UnitID: "unit_repo_1", OrgID: "org_1", ProjectID: "project_1",
		Subject: "memory_archive", Predicate: "implementation_status", Value: "implemented_and_deployed",
		Polarity: "positive", Confidence: 0.95,
	})
	if err != nil {
		t.Fatalf("UpsertClaim() error = %v", err)
	}
	if claim.Value != "implemented_and_deployed" {
		t.Fatalf("claim = %#v", claim)
	}

	action, err := repo.RecordAction(ctx, GovernanceAction{
		ActionID: "gov_action_repo_1", RunID: "gov_run_repo_1", OrgID: "org_1", ProjectID: "project_1",
		TargetType: "candidate", TargetID: "cand_old", Action: ActionDiscardStale,
		Reason: "旧事实已被新事实覆盖", Applied: true,
	})
	if err != nil {
		t.Fatalf("RecordAction() error = %v", err)
	}
	if !action.Applied {
		t.Fatalf("action = %#v", action)
	}
}
```

运行：

```bash
go test ./internal/memorykernel -run TestPGRepositoryUpsertsUnitClaimAndGovernanceAction -count=1
```

预期：FAIL，缺少 repository。

- [ ] **步骤 2.2：实现 Repository 接口**

`internal/memorykernel/repository.go` 定义：

```go
type Repository interface {
	CreateRun(ctx context.Context, run GovernanceRun) (GovernanceRun, error)
	CompleteRun(ctx context.Context, runID string, update GovernanceRunUpdate) error
	FailRun(ctx context.Context, runID string, err error) error
	UpsertUnit(ctx context.Context, unit MemoryUnit) (MemoryUnit, error)
	UpsertClaim(ctx context.Context, claim MemoryClaim) (MemoryClaim, error)
	ListUnits(ctx context.Context, filter UnitFilter) ([]MemoryUnit, error)
	ListClaims(ctx context.Context, filter ClaimFilter) ([]MemoryClaim, error)
	MarkUnitSuperseded(ctx context.Context, orgID, unitID, supersededBy, reason string) (MemoryUnit, error)
	RecordAction(ctx context.Context, action GovernanceAction) (GovernanceAction, error)
	ListActions(ctx context.Context, filter ActionFilter) ([]GovernanceAction, error)
	UpsertCICase(ctx context.Context, c CICase) (CICase, error)
	RecordCIResult(ctx context.Context, result CIResult) (CIResult, error)
	ListCICases(ctx context.Context, filter CICaseFilter) ([]CICase, error)
	ListCIResults(ctx context.Context, filter CIResultFilter) ([]CIResult, error)
}
```

- [ ] **步骤 2.3：实现 PGRepository**

`internal/memorykernel/pg_repository.go` 必须：

- 所有写入带 `org_id`。
- JSONB 字段使用 `json.Marshal` 和 `json.Unmarshal`。
- `UpsertUnit` 使用 `ON CONFLICT (unit_id) DO UPDATE`，保留更高 `trust_score`，更新 `status/content/applies_when/agent_should/source_refs`。
- `RecordAction` 使用 `ON CONFLICT (action_id) DO UPDATE SET action_id=EXCLUDED.action_id RETURNING action_id, run_id, org_id, project_id, target_type, target_id, action, reason, evidence_refs, applied, created_at` 保证幂等。
- `FailRun` 写入经过 `secret.Sanitize` 的错误文本，最多 1000 字。

- [ ] **步骤 2.4：扩展候选软状态**

修改 `internal/candidatememory/model.go`：

```go
const (
	StatusSuperseded Status = "superseded"
)
```

给 `Candidate` 增加：

```go
GovernanceReason string
SupersededBy     string
```

迁移中给 `candidate_memories` 追加：

```sql
ALTER TABLE candidate_memories
    ADD COLUMN IF NOT EXISTS governance_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS superseded_by TEXT NOT NULL DEFAULT '';
```

修改 `candidateColumns`、`scanCandidate`、`CreateCandidate` 和 `UpdateCandidateStatus`，保证新字段读写不破坏旧测试。

- [ ] **步骤 2.5：验证 Repository**

运行：

```bash
go test ./internal/memorykernel ./internal/candidatememory -count=1
```

预期：PASS。

- [ ] **步骤 2.6：Commit**

```bash
git add internal/memorykernel internal/candidatememory migrations/000029_memory_kernel.sql
git commit -m "feat(memory): persist memory kernel governance"
```

---

## 任务 3：LLM Classifier 与零写入校验

**文件：**
- 创建：`internal/memorykernel/classifier.go`
- 创建：`internal/memorykernel/classifier_test.go`

- [ ] **步骤 3.1：编写解析测试，先失败**

创建测试：

```go
func TestLLMClassifierParsesUnitsClaimsActionsAndSanitizes(t *testing.T) {
	resp := `{
	  "units":[{"unit_id":"unit_1","type":"fact","content":"memory_archive 已实现并部署","applies_when":"询问 MCP 当前能力","agent_should":"回答当前能力并引用部署证据","status":"current","confidence":0.95,"trust_score":0.9,"risk_level":"low","source_refs":[{"kind":"candidate","id":"cand_new"}]}],
	  "claims":[{"claim_id":"claim_1","unit_id":"unit_1","subject":"memory_archive","predicate":"implementation_status","value":"implemented_and_deployed","polarity":"positive","confidence":0.95,"evidence_refs":[{"kind":"candidate","id":"cand_new"}]}],
	  "actions":[{"action_id":"act_1","target_type":"candidate","target_id":"cand_old","action":"discard_stale","reason":"旧事实被 cand_new 覆盖 sk-test-redacted-example","evidence_refs":[{"kind":"candidate","id":"cand_new"}]}],
	  "ci_cases":[{"case_id":"ci_1","question":"memory_archive 现在实现了吗？","must_include":["已实现","已部署"],"must_not_include":["尚未实现"]}],
	  "summary":"修复旧事实 sk-test-redacted-example"
	}`
	classifier := NewLLMClassifier(stubChatClient{text: resp}).WithModel("test-model")
	result, err := classifier.Classify(context.Background(), ClassifyInput{
		Scope: Scope{OrgID: "org_1", ProjectID: "project_1"},
		Candidates: []CandidateInput{{ID: "cand_old", RiskLevel: "low"}, {ID: "cand_new", RiskLevel: "low"}},
	})
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if result.Units[0].Content != "memory_archive 已实现并部署" {
		t.Fatalf("unit content = %q", result.Units[0].Content)
	}
	if strings.Contains(result.Actions[0].Reason, "sk-test") || strings.Contains(result.Summary, "sk-test") {
		t.Fatalf("classifier leaked secret-shaped text: %#v", result)
	}
}
```

运行：

```bash
go test ./internal/memorykernel -run TestLLMClassifierParsesUnitsClaimsActionsAndSanitizes -count=1
```

预期：FAIL。

- [ ] **步骤 3.2：实现 action 白名单**

`classifier.go` 定义：

```go
type Classifier interface {
	Classify(ctx context.Context, input ClassifyInput) (ClassifyResult, error)
}

type GovernanceActionName string

const (
	ActionKeep                 GovernanceActionName = "keep"
	ActionDiscardNoise         GovernanceActionName = "discard_noise"
	ActionDiscardStale         GovernanceActionName = "discard_stale"
	ActionMarkSuperseded       GovernanceActionName = "mark_superseded"
	ActionDemoteHotMemory      GovernanceActionName = "demote_hot_memory"
	ActionCreateUnit           GovernanceActionName = "create_memory_unit"
	ActionSupersedeUnit        GovernanceActionName = "supersede_memory_unit"
	ActionCreateCorrection     GovernanceActionName = "create_correction_archive"
	ActionCreateCICase         GovernanceActionName = "create_ci_case"
	ActionNeedsReview          GovernanceActionName = "needs_review"
)
```

- [ ] **步骤 3.3：实现严格解析**

解析逻辑必须：

- 只接受 JSON 对象。
- 所有 `target_id` 必须来自输入候选、热记忆、归档、unit ID 集合。
- 所有 `source_refs` 和 `evidence_refs` 必须来自输入 ID 集合。
- 高风险 candidate 的 action 只能是 `needs_review`、`keep`、`create_memory_unit`，且 unit status 必须是 `needs_review`。
- `summary`、`reason`、`content`、`agent_should` 均调用 `secret.Sanitize`。
- 空 `unit_id`、空 `content`、空 `question` 直接返回错误，service 不写库。

- [ ] **步骤 3.4：补高风险拒绝测试**

增加：

```go
func TestLLMClassifierRejectsAutoDiscardForHighRiskCandidate(t *testing.T) {
	resp := `{"units":[],"claims":[],"actions":[{"action_id":"act_1","target_type":"candidate","target_id":"cand_secret","action":"discard_stale","reason":"丢弃"}],"ci_cases":[],"summary":"x"}`
	classifier := NewLLMClassifier(stubChatClient{text: resp})
	_, err := classifier.Classify(context.Background(), ClassifyInput{
		Scope: Scope{OrgID: "org_1", ProjectID: "project_1"},
		Candidates: []CandidateInput{{ID: "cand_secret", RiskLevel: "high"}},
	})
	if err == nil || !strings.Contains(err.Error(), "high risk") {
		t.Fatalf("Classify() error = %v, want high risk rejection", err)
	}
}
```

- [ ] **步骤 3.5：验证 Classifier**

运行：

```bash
go test ./internal/memorykernel -run 'TestLLMClassifier|TestParse' -count=1
```

预期：PASS。

- [ ] **步骤 3.6：Commit**

```bash
git add internal/memorykernel/classifier.go internal/memorykernel/classifier_test.go
git commit -m "feat(memory): classify memory kernel governance"
```

---

## 任务 4：Collector 体检包

**文件：**
- 创建：`internal/memorykernel/collector.go`
- 创建：`internal/memorykernel/collector_test.go`
- 修改：`internal/archive/repository.go`
- 修改：`internal/archive/pg_repository.go`
- 修改：`internal/retrieval/access_log.go`
- 修改：`internal/retrieval/pg_access_log.go`

- [ ] **步骤 4.1：编写 collector 测试，先失败**

测试必须证明同一 scope 可以收集候选、热记忆、归档摘要和检索日志：

```go
func TestCollectorBuildsGovernanceInputForScope(t *testing.T) {
	collector := NewCollector(CollectorOptions{
		Candidates: fakeCandidateSource{items: []CandidateInput{{ID: "cand_old", Content: "memory_archive 尚未实现", RiskLevel: "low"}}},
		HotMemories: fakeHotMemorySource{items: []HotMemoryInput{{ID: "hm_old", Fact: "memory_archive 尚未实现", ReturnedCount: 0}}},
		Archives: fakeArchiveSource{items: []ArchiveInput{{ID: "archive_new", Title: "记忆修订: MCP 当前能力", Excerpt: "memory_archive 已实现"}}},
		RetrievalLogs: fakeRetrievalSource{items: []RetrievalInput{{RequestID: "req_1", SourceKind: "archive_chunk"}}},
	})
	input, err := collector.Collect(context.Background(), Scope{OrgID: "org_1", ProjectID: "project_1"})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(input.Candidates) != 1 || len(input.HotMemories) != 1 || len(input.Archives) != 1 || len(input.Retrievals) != 1 {
		t.Fatalf("input = %#v", input)
	}
}
```

- [ ] **步骤 4.2：实现 source interfaces**

`collector.go` 定义：

```go
type Collector interface {
	Collect(ctx context.Context, scope Scope) (ClassifyInput, error)
}

type CandidateSource interface {
	ListKernelCandidates(ctx context.Context, scope Scope, limit int) ([]CandidateInput, error)
}

type HotMemorySource interface {
	ListKernelHotMemories(ctx context.Context, scope Scope, limit int) ([]HotMemoryInput, error)
}

type ArchiveSource interface {
	ListKernelArchives(ctx context.Context, scope Scope, limit int) ([]ArchiveInput, error)
}

type RetrievalSource interface {
	ListKernelRetrievals(ctx context.Context, scope Scope, limit int) ([]RetrievalInput, error)
}
```

用小 adapter 包装现有 repository，不把 collector 直接绑死到 PG。

- [ ] **步骤 4.3：归档摘要读取**

给 archive repository 或 service 增加只读方法：

```go
type KernelArchiveReader interface {
	ListKernelArchives(ctx context.Context, orgID, projectID string, limit int) ([]KernelArchiveSummary, error)
}
```

第一版摘要使用标题、archive_id、updated_at 和 Markdown 前 1000 字，调用 `secret.Sanitize` 后进入 LLM。

- [ ] **步骤 4.4：检索日志读取**

复用 `retrieval.AccessLogReader`，collector 只拿 `request_id`、`source_kind`、`source_ref`、`created_at`，不拿原始 query。

- [ ] **步骤 4.5：验证 Collector**

运行：

```bash
go test ./internal/memorykernel ./internal/archive ./internal/retrieval -run 'TestCollector|TestPG.*Kernel|Test.*AccessLog' -count=1
```

预期：PASS。

- [ ] **步骤 4.6：Commit**

```bash
git add internal/memorykernel/collector.go internal/memorykernel/collector_test.go internal/archive internal/retrieval
git commit -m "feat(memory): collect memory kernel governance input"
```

---

## 任务 5：Governance Service 与动作应用

**文件：**
- 创建：`internal/memorykernel/service.go`
- 创建：`internal/memorykernel/service_test.go`
- 修改：`internal/candidatememory/repository.go`
- 修改：`internal/candidatememory/pg_repository.go`
- 修改：`internal/hotmemory/repository.go`
- 修改：`internal/hotmemory/pg_repository.go`

- [ ] **步骤 5.1：编写核心场景测试，先失败**

测试旧事实被新事实覆盖：

```go
func TestServiceRunGovernanceSupersedesStaleCandidateAndCreatesCurrentUnit(t *testing.T) {
	repo := NewInMemoryRepository()
	candidates := newFakeCandidateGovernanceRepo([]CandidateInput{
		{ID: "cand_old", Content: "memory_archive 尚未实现", RiskLevel: "low"},
		{ID: "cand_new", Content: "memory_archive 已实现并部署", RiskLevel: "low"},
	})
	classifier := fakeClassifier{result: ClassifyResult{
		Units: []MemoryUnit{{UnitID: "unit_memory_archive_current", OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Type: UnitFact, Content: "memory_archive 已实现并部署", Status: UnitCurrent, TrustScore: 0.95}},
		Claims: []MemoryClaim{{ClaimID: "claim_memory_archive_current", UnitID: "unit_memory_archive_current", OrgID: "org_1", ProjectID: "project_1", Subject: "memory_archive", Predicate: "implementation_status", Value: "implemented_and_deployed", Confidence: 0.95}},
		Actions: []GovernanceAction{{ActionID: "act_discard_old", OrgID: "org_1", ProjectID: "project_1", TargetType: "candidate", TargetID: "cand_old", Action: ActionDiscardStale, Reason: "旧事实已被 cand_new 覆盖"}},
		CICases: []CICase{{CaseID: "ci_memory_archive_status", OrgID: "org_1", ProjectID: "project_1", Question: "memory_archive 现在实现了吗？", MustInclude: []string{"已实现", "已部署"}, MustNotInclude: []string{"尚未实现"}}},
		Summary: "生成当前事实并丢弃旧候选",
	}}
	service := NewService(ServiceOptions{Repository: repo, Collector: fakeCollector{candidates: candidates.items}, Classifier: classifier, CandidateApplier: candidates})
	run, err := service.RunGovernance(context.Background(), GovernanceRequest{OrgID: "org_1", ProjectID: "project_1", TriggerType: "manual"})
	if err != nil {
		t.Fatalf("RunGovernance() error = %v", err)
	}
	if run.CreatedUnits != 1 || run.StaleCandidates != 1 || run.CICasesCreated != 1 {
		t.Fatalf("run = %#v", run)
	}
	if candidates.statusByID["cand_old"] != "discarded" {
		t.Fatalf("cand_old status = %q", candidates.statusByID["cand_old"])
	}
}
```

- [ ] **步骤 5.2：实现 ServiceOptions**

`service.go` 定义：

```go
type ServiceOptions struct {
	Repository       Repository
	Collector        Collector
	Classifier       Classifier
	CandidateApplier CandidateApplier
	HotMemoryApplier HotMemoryApplier
	ArchiveCreator   CorrectionArchiveCreator
	CIRunner          CIRunner
}
```

- [ ] **步骤 5.3：实现动作应用规则**

`RunGovernance` 顺序：

1. 创建 run。
2. collect。
3. classify。
4. 本地二次校验。
5. upsert units。
6. upsert claims。
7. 应用 action：
   - `discard_stale` -> candidate `discarded`，写 `governance_reason`。
   - `mark_superseded` -> candidate `superseded`，写 `superseded_by`。
   - `demote_hot_memory` -> hot memory `demoted`。
   - `create_correction_archive` -> 调用 correction archive creator。
   - `needs_review` -> candidate 保持 `pending` 且 `needs_review=true`。
8. upsert CI cases。
9. 若 CIRunner 配置，执行 CI 并写结果。
10. complete run。

任何步骤失败，run `failed`，不继续后续写入。已经成功写入的动作通过 action log 可追溯。

- [ ] **步骤 5.4：实现 candidate applier**

给 candidatememory repository 增加：

```go
UpdateCandidateGovernance(ctx context.Context, orgID, candidateID string, status Status, needsReview bool, reason string, supersededBy string) (Candidate, error)
```

PG SQL：

```sql
UPDATE candidate_memories
SET status=$1,
    needs_review=$2,
    governance_reason=$3,
    superseded_by=$4,
    updated_at=now()
WHERE org_id=$5 AND candidate_id=$6
RETURNING candidate_id, org_id, project_id, source_key, user_id, agent_id, thread_id, session_id, source_event_ids, memory_type, content, summary, risk_level, confidence, status, similar_refs, scores, needs_review, governance_reason, superseded_by, created_at, updated_at
```

- [ ] **步骤 5.5：实现 hot memory applier**

复用 `hotmemory.Repository.Update`：

- 读取 memory。
- 若 `Pinned=true`，拒绝自动 demote，记录 action `applied=false`。
- 若 status 为 active/promoted，改为 demoted。
- 不修改 fact 文本。

- [ ] **步骤 5.6：验证 Service**

运行：

```bash
go test ./internal/memorykernel ./internal/candidatememory ./internal/hotmemory -run 'TestService|Test.*Governance|Test.*Demote' -count=1
```

预期：PASS。

- [ ] **步骤 5.7：Commit**

```bash
git add internal/memorykernel/service.go internal/memorykernel/service_test.go internal/candidatememory internal/hotmemory migrations/000029_memory_kernel.sql
git commit -m "feat(memory): apply memory kernel governance actions"
```

---

## 任务 6：Correction Archive

**文件：**
- 修改：`internal/memorykernel/service.go`
- 创建：`internal/memorykernel/correction_archive_test.go`
- 修改：`internal/jobs/archive_creator.go`

- [ ] **步骤 6.1：编写 correction archive 测试，先失败**

测试 governance 生成修订归档：

```go
func TestServiceCreatesCorrectionArchiveWithoutEditingOldArchive(t *testing.T) {
	creator := &fakeCorrectionArchiveCreator{}
	service := NewService(ServiceOptions{
		Repository: NewInMemoryRepository(),
		Collector: fakeCollector{},
		Classifier: fakeClassifier{result: ClassifyResult{
			Actions: []GovernanceAction{{ActionID: "act_correction", TargetType: "archive", TargetID: "archive_old", Action: ActionCreateCorrection, Reason: "旧归档包含过期结论"}},
			Units: []MemoryUnit{{UnitID: "unit_current", OrgID: "org_1", ProjectID: "project_1", UserID: "user_1", Type: UnitFact, Content: "memory_archive 已实现并部署", Status: UnitCurrent}},
			Summary: "创建修订归档",
		}},
		ArchiveCreator: creator,
	})
	run, err := service.RunGovernance(context.Background(), GovernanceRequest{OrgID: "org_1", ProjectID: "project_1", TriggerType: "manual"})
	if err != nil {
		t.Fatalf("RunGovernance() error = %v", err)
	}
	if creator.createdTitle != "记忆修订: Memory Kernel 当前可信结论" {
		t.Fatalf("title = %q", creator.createdTitle)
	}
	if run.CorrectionArchiveID == "" {
		t.Fatalf("run correction archive empty: %#v", run)
	}
}
```

- [ ] **步骤 6.2：实现 CorrectionArchiveCreator**

接口：

```go
type CorrectionArchiveCreator interface {
	CreateCorrectionArchive(ctx context.Context, req CorrectionArchiveRequest) (CorrectionArchiveResult, error)
}
```

Markdown 固定结构：

```markdown
# 记忆修订: Memory Kernel 当前可信结论

## 当前结论

## 已覆盖的旧记忆

## 证据

## 对 Agent 的影响

## 来源
```

所有内容来自已脱敏 units/actions/evidence refs，不读取 secret 明文。

- [ ] **步骤 6.3：接入生产 archive service**

在 `jobs.NewProductionArchiveCreator` 附近新增 adapter 或在 `memorykernel` 内实现 adapter，调用 `archive.Service.Create` 并 enqueue RAG index。

ArchiveID 生成：

```go
func correctionArchiveID(req CorrectionArchiveRequest) string {
	return fmt.Sprintf("archive_memory_correction_%s_%s_%d", sanitizeForID(req.SourceKey), sanitizeForID(req.ThreadID), time.Now().UnixNano())
}
```

- [ ] **步骤 6.4：验证 Archive**

运行：

```bash
go test ./internal/memorykernel ./internal/jobs ./internal/archive -run 'Test.*Correction|Test.*ArchiveCreator|Test.*Archive' -count=1
```

预期：PASS。

- [ ] **步骤 6.5：Commit**

```bash
git add internal/memorykernel internal/jobs internal/archive
git commit -m "feat(memory): create correction archives from governance"
```

---

## 任务 7：Context Pack 与 Memory Unit 检索

**文件：**
- 创建：`internal/memorykernel/context_pack.go`
- 创建：`internal/memorykernel/context_pack_test.go`
- 修改：`internal/retrieval/model.go`
- 修改：`internal/retrieval/service.go`
- 修改：`internal/retrieval/service_test.go`

- [ ] **步骤 7.1：编写 Context Pack 测试，先失败**

```go
func TestContextPackBuildsDeploymentPackFromProceduresRisksAndEvidence(t *testing.T) {
	repo := NewInMemoryRepository()
	_, _ = repo.UpsertUnit(context.Background(), MemoryUnit{UnitID: "unit_deploy_proc", OrgID: "org_1", ProjectID: "project_1", Type: UnitProcedure, Content: "部署前阅读 DEPLOYMENT.md", AppliesWhen: "部署 重启 上线 排障", AgentShould: "先读部署手册", Status: UnitCurrent, TrustScore: 0.95})
	_, _ = repo.UpsertUnit(context.Background(), MemoryUnit{UnitID: "unit_secret_risk", OrgID: "org_1", ProjectID: "project_1", Type: UnitRisk, Content: "Secret 明文不得入库", AppliesWhen: "任何工具和日志", AgentShould: "使用 secret_ref", Status: UnitCurrent, TrustScore: 0.9})
	builder := NewContextPackBuilder(repo)
	pack, err := builder.Build(context.Background(), ContextPackRequest{OrgID: "org_1", ProjectID: "project_1", Query: "部署 Memory OS"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !strings.Contains(pack.Context, "部署前阅读 DEPLOYMENT.md") || !strings.Contains(pack.Context, "Secret 明文不得入库") {
		t.Fatalf("pack context = %s", pack.Context)
	}
}
```

- [ ] **步骤 7.2：实现 Context Pack Builder**

`context_pack.go` 先定义服务接口，HTTP 和 MCP 只依赖这个接口：

```go
type ContextPackService interface {
	Build(ctx context.Context, request ContextPackRequest) (ContextPack, error)
}
```

排序规则：

1. `status=current`。
2. `applies_when` 命中 query token。
3. `unit_type` 优先级：procedure、risk、preference、decision、fact、environment、task、evidence。
4. `trust_score` 高优先。
5. `updated_at` 新优先。

输出：

```go
type ContextPack struct {
	RequestID string
	Intent string
	Context string
	Units []MemoryUnit
	Warnings []string
}
```

- [ ] **步骤 7.3：Retrieval 注入 Memory Unit**

`retrieval.Options` 增加：

```go
MemoryUnitSource MemoryUnitSource
```

接口：

```go
type MemoryUnitSource interface {
	SearchUnits(ctx context.Context, request MemoryUnitSearchRequest) ([]MemoryUnitSearchResult, error)
}
```

`collect` 开头先收 current units，score 基础值 `1.20`，高于 hot/archive 原始分。`SourceRef` 增加：

```go
const SourceMemoryUnit SourceKind = "memory_unit"

type SourceRef struct {
	MemoryUnitID string `json:"memory_unit_id,omitempty"`
}
```

- [ ] **步骤 7.4：检索污染测试**

在 `internal/retrieval/service_test.go` 增加：

```go
func TestSearchRanksCurrentMemoryUnitAboveStaleArchive(t *testing.T) {
	unitSource := fakeMemoryUnitSource{results: []MemoryUnitSearchResult{{UnitID: "unit_current", Text: "memory_archive 已实现并部署", Score: 1.2}}}
	archiveRAG := &capturingArchiveRAG{results: []rag.SearchResult{{Text: "memory_archive 尚未实现", Score: 0.95, Source: rag.SourceRef{ArchiveID: "archive_old", ChunkID: "chunk_old"}}}}
	service := NewService(Options{MemoryUnitSource: unitSource, ArchiveRAG: archiveRAG, ArchiveGenerationResolver: &fakeArchiveGenerationResolver{generation: 1}})
	resp, err := service.Search(SearchRequest{RequestID: "req_rank", Query: "memory_archive 现在实现了吗", Actor: Actor{UserID: "u", OrgID: "o", ProjectID: "p", AgentID: "codex"}, Visibility: "project", PermissionLabels: []string{"project:p:read"}})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if resp.Results[0].Source.Kind != SourceMemoryUnit {
		t.Fatalf("top result = %#v", resp.Results[0])
	}
	if strings.Contains(resp.Context, "尚未实现") && !strings.Contains(resp.Context, "已实现并部署") {
		t.Fatalf("context polluted: %s", resp.Context)
	}
}
```

- [ ] **步骤 7.5：验证 Context Pack 和 Retrieval**

运行：

```bash
go test ./internal/memorykernel ./internal/retrieval -run 'TestContextPack|TestSearchRanksCurrentMemoryUnit' -count=1
```

预期：PASS。

- [ ] **步骤 7.6：Commit**

```bash
git add internal/memorykernel internal/retrieval
git commit -m "feat(memory): prioritize canonical memory units in retrieval"
```

---

## 任务 8：Memory CI

**文件：**
- 创建：`internal/memorykernel/ci.go`
- 创建：`internal/memorykernel/ci_test.go`

- [ ] **步骤 8.1：编写 CI 测试，先失败**

```go
func TestCIRunnerFailsWhenForbiddenPhraseAppears(t *testing.T) {
	repo := NewInMemoryRepository()
	_, _ = repo.UpsertCICase(context.Background(), CICase{
		CaseID: "ci_memory_archive", OrgID: "org_1", ProjectID: "project_1",
		Question: "memory_archive 现在实现了吗？",
		MustInclude: []string{"已实现"},
		MustNotInclude: []string{"尚未实现"},
		Status: "active",
	})
	runner := NewCIRunner(repo, fakeSearchService{context: "memory_archive 尚未实现"})
	result, err := runner.RunCase(context.Background(), "ci_memory_archive")
	if err != nil {
		t.Fatalf("RunCase() error = %v", err)
	}
	if result.Passed {
		t.Fatalf("result should fail: %#v", result)
	}
	if len(result.MatchedExclude) != 1 {
		t.Fatalf("matched exclude = %#v", result.MatchedExclude)
	}
}
```

- [ ] **步骤 8.2：实现 CI Runner**

规则：

- 调用 `memory_search` 或 context pack builder。
- `must_include` 全部命中才通过。
- `must_not_include` 任一命中则失败。
- `response_excerpt` 截断到 2000 字并脱敏。
- 记录 request_id，格式由 `fmt.Sprintf("memory_ci_%s_%d", sanitizeForID(caseID), time.Now().UnixNano())` 生成。

- [ ] **步骤 8.3：Governance 后自动创建和运行 CI**

`RunGovernance` 中：

- classifier 输出 `ci_cases` 后 upsert。
- 若 `CIRunner` 配置，立即运行本次新建 case。
- run 记录 `ci_cases_created` 和 `ci_cases_passed`。

- [ ] **步骤 8.4：验证 CI**

运行：

```bash
go test ./internal/memorykernel -run 'TestCI|TestService.*CI' -count=1
```

预期：PASS。

- [ ] **步骤 8.5：Commit**

```bash
git add internal/memorykernel/ci.go internal/memorykernel/ci_test.go internal/memorykernel/service.go internal/memorykernel/service_test.go
git commit -m "feat(memory): validate memory recall with ci"
```

---

## 任务 9：HTTP API 与统计

**文件：**
- 修改：`internal/http/router.go`
- 修改：`internal/http/router_test.go`
- 修改：`internal/memorystats/model.go`
- 修改：`internal/memorystats/pg_repository.go`
- 修改：`internal/memorystats/service_test.go`
- 修改：`cmd/memory-api/main.go`

- [ ] **步骤 9.1：编写 HTTP 测试，先失败**

新增测试：

```go
type routerKernelCollector struct{}

func (routerKernelCollector) Collect(ctx context.Context, scope memorykernel.Scope) (memorykernel.ClassifyInput, error) {
	return memorykernel.ClassifyInput{Scope: scope}, nil
}

type routerKernelClassifier struct{}

func (routerKernelClassifier) Classify(ctx context.Context, input memorykernel.ClassifyInput) (memorykernel.ClassifyResult, error) {
	return memorykernel.ClassifyResult{Summary: "HTTP 触发治理成功"}, nil
}

func TestMemoryKernelRunAndStatusAPIsRequireWriteAndReadPermissions(t *testing.T) {
	authService, token := testAuthServiceWithPAT(t, "user_1", []string{"memory:read", "memory:write"})
	tenantService := testTenantServiceWithProject(t, "user_1", "org_1", "project_1")
	service := memorykernel.NewService(memorykernel.ServiceOptions{Repository: memorykernel.NewInMemoryRepository(), Collector: routerKernelCollector{}, Classifier: routerKernelClassifier{}})
	h := server.Default()
	RegisterRoutes(h.Engine, RouterOptions{HealthService: health.NewService(nil), AuthService: authService, TenantService: tenantService, MemoryKernelService: service})

	body := `{"org_id":"org_1","project_id":"project_1","trigger_type":"manual"}`
	resp := ut.PerformRequest(h.Engine, "POST", "/memory/kernel/governance/run", &ut.Body{Body: strings.NewReader(body), Len: len(body)}, ut.Header{Key: "Authorization", Value: "Bearer " + token}, ut.Header{Key: "Content-Type", Value: "application/json"})
	assert.DeepEqual(t, 200, resp.Code)
	if !strings.Contains(resp.Body.String(), `"run_id"`) {
		t.Fatalf("response = %s", resp.Body.String())
	}
}
```

- [ ] **步骤 9.2：注册 API**

在 `RouterOptions` 增加：

```go
MemoryKernelService memorykernel.Service
ContextPackService  memorykernel.ContextPackService
```

注册：

```go
engine.POST("/memory/kernel/governance/run", MemoryKernelGovernanceRunHandler(options.MemoryKernelService, options.AuthService, options.TenantService))
engine.POST("/memory/kernel/governance/status", MemoryKernelGovernanceStatusHandler(options.MemoryKernelService, options.AuthService, options.TenantService))
engine.POST("/memory/kernel/units/list", MemoryKernelUnitListHandler(options.MemoryKernelService, options.AuthService, options.TenantService))
engine.POST("/memory/kernel/context-pack", MemoryKernelContextPackHandler(options.ContextPackService, options.AuthService, options.TenantService))
engine.POST("/memory/kernel/ci/run", MemoryKernelCIRunHandler(options.MemoryKernelService, options.AuthService, options.TenantService))
engine.POST("/memory/kernel/ci/list", MemoryKernelCIListHandler(options.MemoryKernelService, options.AuthService, options.TenantService))
```

- [ ] **步骤 9.3：权限规则**

- governance run：`memory:write` + `project:{project_id}:write`。
- status/list/context-pack/ci-list：`memory:read`。
- user scoped workspace run 第一版不暴露，避免绕过项目权限。

- [ ] **步骤 9.4：扩展生命周期统计**

`memorystats.Snapshot` 增加：

```go
MemoryKernel MemoryKernelStats `json:"memory_kernel"`
```

字段：

```go
type MemoryKernelStats struct {
	UnitsTotal int64 `json:"units_total"`
	UnitsCurrent int64 `json:"units_current"`
	UnitsNeedsReview int64 `json:"units_needs_review"`
	GovernanceRunsTotal int64 `json:"governance_runs_total"`
	GovernanceRunsFailed int64 `json:"governance_runs_failed"`
	CICasesActive int64 `json:"ci_cases_active"`
	CILatestFailed int64 `json:"ci_latest_failed"`
	LastRunAt string `json:"last_run_at"`
	LastRunSummary string `json:"last_run_summary"`
}
```

- [ ] **步骤 9.5：API 装配**

`cmd/memory-api/main.go`：

- 创建 `memorykernel.PGRepository(pool)`。
- 创建 collector adapters。
- 创建 `LLMClassifier(llmClient)`。
- 创建 governance service。
- 将 service 注入 RouterOptions。
- 将 memory unit source 注入 retrieval options。

- [ ] **步骤 9.6：验证 HTTP 与 stats**

运行：

```bash
go test ./internal/http ./internal/memorystats ./cmd/memory-api -run 'TestMemoryKernel|TestMemoryStats|TestRouterOptions' -count=1
```

预期：PASS。

- [ ] **步骤 9.7：Commit**

```bash
git add internal/http internal/memorystats cmd/memory-api
git commit -m "feat(memory): expose memory kernel APIs"
```

---

## 任务 10：Worker 自动治理

**文件：**
- 修改：`internal/jobs/runner.go`
- 修改：`internal/jobs/runner_test.go`
- 修改：`cmd/memory-worker/main.go`
- 修改：`cmd/memory-worker/main_test.go`

- [ ] **步骤 10.1：编写 runner 测试，先失败**

```go
func TestRunnerConfiguresMemoryKernelMaintenanceLoop(t *testing.T) {
	maintenance := &fakeMemoryKernelMaintenance{}
	ctx, cancel := context.WithCancel(context.Background())
	maintenance.afterRun = cancel
	runner := NewRunner(Options{MemoryKernelMaintenance: maintenance, MemoryKernelMaintenanceInterval: time.Millisecond})
	err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if maintenance.runs != 1 {
		t.Fatalf("runs = %d", maintenance.runs)
	}
}
```

- [ ] **步骤 10.2：扩展 jobs.Options**

增加：

```go
type MemoryKernelMaintenance interface {
	RunAutoGovernance(ctx context.Context) (int, error)
}
```

`Options` 增加：

```go
MemoryKernelMaintenance MemoryKernelMaintenance
MemoryKernelMaintenanceInterval time.Duration
```

Runner `Run` 中增加 loop。错误处理遵循 Hot Memory maintenance：记录后继续，不能因为 LLM 429 退出 worker。

- [ ] **步骤 10.3：实现 AutoGovernance**

`memorykernel.Service` 增加：

```go
func (s Service) RunAutoGovernance(ctx context.Context) (int, error)
```

扫描范围：

- 最近 24 小时有 pending/in_compose_pool/accepted candidate 的项目 scope。
- 最近 24 小时有 active/promoted hot memory 的项目 scope。
- 最近 24 小时有 active archive 更新的项目 scope。
- 每次最多 20 个 scope，按 updated_at 倒序，避免打爆模型。

- [ ] **步骤 10.4：装配 worker**

`cmd/memory-worker/main.go`：

- 在 LLM configured 分支创建 Memory Kernel service。
- 将 service 注入 `jobs.Options`。
- 默认 interval：`15 * time.Minute`。

- [ ] **步骤 10.5：验证 Worker**

运行：

```bash
go test ./internal/jobs ./cmd/memory-worker -run 'TestRunner.*MemoryKernel|TestNew.*MemoryKernel|TestBuildWorker' -count=1
```

预期：PASS。

- [ ] **步骤 10.6：Commit**

```bash
git add internal/jobs cmd/memory-worker internal/memorykernel
git commit -m "feat(memory): run memory kernel governance automatically"
```

---

## 任务 11：MCP Context Pack

**文件：**
- 修改：`internal/mcp/schema.go`
- 修改：`internal/mcp/schema_test.go`
- 修改：`cmd/memory-mcp/main.go`
- 修改：`cmd/memory-mcp/main_test.go`

- [ ] **步骤 11.1：编写 schema 测试，先失败**

```go
func TestToolsExposeMemoryContextPack(t *testing.T) {
	tools := Tools()
	var found bool
	for _, tool := range tools {
		if tool.Name == "memory_context_pack" {
			found = true
			required := tool.InputSchema["required"].([]any)
			if !slices.Contains(required, any("query")) {
				t.Fatalf("required = %#v", required)
			}
		}
	}
	if !found {
		t.Fatal("memory_context_pack tool missing")
	}
}
```

- [ ] **步骤 11.2：新增 tool schema**

`Tools()` 增加：

```go
{Name: "memory_context_pack", Description: "Build task-oriented Memory OS context pack", InputSchema: memoryContextPackSchema()}
```

schema 字段：

- `request_id`
- `query`
- `workspace`
- `actor`
- `max_context_bytes`

- [ ] **步骤 11.3：MCP handler**

`cmd/memory-mcp/main.go`：

- `Server` 增加 `ContextPackService memorykernel.ContextPackService`。
- `handleTool` 增加 `memory_context_pack`。
- 权限同 `memory_search`，必须套用 workspace/project 权限。
- 返回：

```json
{
  "request_id": "ctx_memory_archive_1",
  "intent": "deploy",
  "context": "部署前必须阅读 DEPLOYMENT.md；memory_archive 已实现并部署。",
  "units": [{"unit_id":"unit_deploy_proc","type":"procedure","source_refs":[{"kind":"archive","id":"archive_deploy_guide"}]}],
  "warnings": []
}
```

- [ ] **步骤 11.4：MCP 测试**

新增测试证明：

- `/tools` 暴露 `memory_context_pack`。
- tool call 在无 workspace 时落到 inbox 权限上下文。
- tool call 不返回被 `superseded` 的 unit。

- [ ] **步骤 11.5：验证 MCP**

运行：

```bash
go test ./internal/mcp ./cmd/memory-mcp -run 'TestToolsExposeMemoryContextPack|TestToolsCallMemoryContextPack' -count=1
```

预期：PASS。

- [ ] **步骤 11.6：Commit**

```bash
git add internal/mcp cmd/memory-mcp
git commit -m "feat(memory): expose context pack over mcp"
```

---

## 任务 12：前端记忆体检页与文案收敛

**文件：**
- 创建：`frontend/pages/memory-kernel/index.vue`
- 修改：`frontend/pages/candidates/index.vue`
- 修改：`frontend/pages/topics/index.vue`
- 修改：`frontend/pages/hot-memory/index.vue`
- 修改：`frontend/pages/diagnostics/index.vue`
- 修改：`frontend/composables/useMemoryLifecycleStats.ts`

- [ ] **步骤 12.1：编写前端类型和页面**

`frontend/pages/memory-kernel/index.vue` 必须包含：

- 顶部四个指标：当前可信事实、需确认、过期/冲突、Memory CI。
- “运行记忆体检”按钮，调用 `/memory/kernel/governance/run`。
- “上下文包预览”输入框，调用 `/memory/kernel/context-pack`。
- Run 列表：状态、summary、修复数量、CI 结果。
- Action 列表：target、action、reason、applied。
- CI 列表：question、must_include、must_not_include、最近结果。

页面文案使用：

```text
记忆体检
可信事实
待整理素材
长期归档
上下文包
召回验收
```

避免使用“主题沉淀”作为主标签。

- [ ] **步骤 12.2：更新候选页文案**

`frontend/pages/candidates/index.vue`：

- 标题改为 `待整理素材`。
- 说明改为：`这里显示从对话中提炼出来、尚未完成治理的素材；当前可信结论以记忆体检中的“可信事实”为准。`
- 列表显示 `governance_reason` 和 `superseded_by`。

- [ ] **步骤 12.3：更新 topics/hot/diagnostics**

- `topics`：`主题沉淀` 改为 `长期归档`。
- `hot-memory`：增加短说明 `热记忆用于快速召回，不等于最终真相；低信号或被新事实覆盖后会自动降级。`
- `diagnostics`：展示 Memory Kernel stats。

- [ ] **步骤 12.4：构建验证**

运行：

```bash
npm --prefix frontend run build
```

预期：PASS。

- [ ] **步骤 12.5：Commit**

```bash
git add frontend
git commit -m "feat(memory): add memory kernel dashboard"
```

---

## 任务 13：文档、部署手册与安装脚本验收提示

**文件：**
- 修改：`README.md`
- 修改：`DEPLOYMENT.md`
- 修改：`docs/superpowers/plans/2026-07-09-memory-kernel.md`

- [ ] **步骤 13.1：README 增加 Memory Kernel**

写清：

- Memory Kernel 是当前可信上下文层。
- Archive 是历史证据库，不等于当前事实。
- Memory CI 用于验证召回是否正确。
- `memory_context_pack` 用于 Agent 获取任务上下文。

- [ ] **步骤 13.2：DEPLOYMENT 增加验收项**

在部署后验收清单增加：

```text
- `/memory/kernel/governance/run` 可触发项目级记忆体检。
- `/memory/kernel/context-pack` 对“部署”类问题返回 DEPLOYMENT.md 规则。
- MCP `/tools` 暴露 `memory_context_pack`。
- Memory CI 用例 `memory_archive 现在实现了吗？` 通过，返回不含“尚未实现”。
```

- [ ] **步骤 13.3：全量验证**

运行：

```bash
go test ./...
go vet ./...
npm --prefix frontend run build
make secret-scan
```

预期：全部 PASS。

- [ ] **步骤 13.4：Commit**

```bash
git add README.md DEPLOYMENT.md docs/superpowers/plans/2026-07-09-memory-kernel.md
git commit -m "docs(memory): document memory kernel operations"
```

---

## 任务 14：生产部署与端到端验收

**文件：**
- 不新增源码文件。
- 读取：`DEPLOYMENT.md`

- [ ] **步骤 14.1：部署前安全检查**

运行：

```bash
make deploy-safety-check
make t480-deploy-dry-run
```

预期：

- safety check PASS。
- dry-run 显示服务包含 API、Worker、MCP、Web。
- verify mode 为 full，因为包含 migrations、retrieval、candidate、hot memory、HTTP、MCP、frontend。

- [ ] **步骤 14.2：执行部署**

用户已在本计划需求中允许部署和重启。执行：

```bash
make t480-deploy
```

预期：

- 远端迁移成功。
- API、Worker、MCP、Web 重启成功。
- post-deploy verify PASS。

- [ ] **步骤 14.3：生产验收脚本**

在本地或通过 SSH 调用生产 API，完成以下验收：

1. 写入旧事实候选或直接插入测试候选：`memory_archive 尚未实现`。
2. 写入新事实事件：`memory_archive 和 memory_stats 已实现并部署`。
3. 触发 `/memory/kernel/governance/run`。
4. 查询 `/memory/kernel/units/list`，确认 current unit 存在。
5. 查询 `/memory/kernel/ci/list`，确认 CI case passed。
6. MCP `/tools` 确认 `memory_context_pack` 存在。
7. MCP `memory_context_pack` 查询 `memory_archive 现在实现了吗？`，确认返回包含 `已实现`，不包含 `尚未实现`。
8. 查询 DB，确认没有测试 secret 明文进入 `memory_units`、`memory_claims`、`memory_governance_actions`、`memory_ci_results`、Qdrant payload。

- [ ] **步骤 14.4：收集部署证据**

最终报告必须包含：

- 执行的部署命令。
- 部署计划 services 和 verify_mode。
- post deploy verify logs 路径。
- 远端 timing.env 耗时。
- Memory Kernel 验收结果摘要。
- 若失败，列出失败 API、run_id、CI case_id、日志路径。

- [ ] **步骤 14.5：最终 Commit 状态**

确认：

```bash
git status --short
git log --oneline -5
```

若还有计划内改动未提交，提交：

```bash
git status --short
git add internal/memorykernel internal/candidatememory internal/hotmemory internal/retrieval internal/memorystats internal/http internal/jobs internal/mcp cmd/memory-api cmd/memory-worker cmd/memory-mcp frontend migrations README.md DEPLOYMENT.md docs/superpowers/plans/2026-07-09-memory-kernel.md
git diff --cached --name-only
git commit -m "feat(memory): complete memory kernel"
```

---

## 自驱执行规则

执行 Agent 必须遵守：

- 每个任务按 TDD 顺序执行：失败测试 -> 实现 -> 通过 -> commit。
- 每完成一个任务运行该任务指定测试。
- 每三个任务运行一次 `go test ./...`，避免后面积累集成错误。
- 任何 LLM JSON 解析失败、ID 校验失败、高风险自动动作失败，都必须零写入。
- 任何测试 secret 只能使用假值，并且最终要扫 DB/Qdrant/API 输出确认无明文。
- 如果部署失败，不继续做新功能，先修复部署验证。
- 如果同一失败连续出现三次，停止执行并输出阻塞原因、已尝试命令、相关日志路径。

---

## 多维挑刺审查与修复记录

### 审查第 1 轮：范围是否过大

问题：

- Memory Kernel 包含治理、Context Pack、CI、前端、MCP，范围大。
- 如果一次性实现所有内容，容易跨太多模块导致部署风险高。

修复：

- 任务拆成 14 个小任务，每个任务都有独立测试和 commit。
- 第一版不接入 Qdrant，不做知识图谱，不改旧 Archive 原文。
- `memory_search` 只增加 Memory Unit source，不重写检索主流程。

### 审查第 2 轮：是否会误删或污染记忆

问题：

- AI 自动治理可能误删有价值内容。
- 旧 Archive 如果被直接改写，会破坏历史证据。

修复：

- 所有治理动作软处理：discard/demote/supersede，不物理删除。
- 高风险只允许 needs_review。
- 旧 Archive 不改写，只创建 correction archive。
- 所有 action 写入 `memory_governance_actions`。

### 审查第 3 轮：是否能证明真的修复了用户痛点

问题：

- 只看统计增长不能证明召回正确。
- 旧事实可能仍通过 Archive RAG 污染回答。

修复：

- 增加 Memory CI，固定 must_include 和 must_not_include。
- retrieval 优先返回 current Memory Unit。
- 验收必须证明 `memory_archive 现在实现了吗？` 不返回 `尚未实现`。

### 审查第 4 轮：是否有安全缺口

问题：

- LLM 输入输出可能携带 secret。
- MCP context pack 可能暴露跨项目记忆。

修复：

- Classifier 输出字段全部 `secret.Sanitize`。
- MCP `memory_context_pack` 复用 `memory_search` 权限模型。
- `memory_units` 不进入 Qdrant 第一版，避免新增 payload 泄露面。
- 生产验收包含 DB/Qdrant/API 明文扫描。

### 审查第 5 轮：是否真正能后台自驱

问题：

- 如果只提供手动按钮，仍然需要用户介入。

修复：

- Worker 增加 `MemoryKernelMaintenance` loop。
- 自动扫描最近活跃 scope，每 15 分钟运行。
- API 手动触发只作为人工验收和临时修复入口。

### 审查第 6 轮：是否容易和现有功能文案混淆

问题：

- 候选清洗、主题沉淀、热记忆整理、记忆体检容易看起来重复。

修复：

- 候选页改为“待整理素材”。
- topics 改为“长期归档”。
- 新页面叫“记忆体检”。
- 首页和诊断页展示“可信事实”与“召回验收”，不把 Archive 叫真相库。

---

## 最终执行结论

本计划已经把 Memory Kernel 拆成可自驱执行的独立任务，并包含：

- 明确的文件边界。
- 数据库迁移。
- LLM 严格 JSON 协议。
- 本地零写入校验。
- 后台自动治理。
- HTTP/MCP 接入。
- 前端体检页。
- Memory CI 验收。
- 生产部署与证据收集。

执行本计划不需要用户中途介入；已明确允许按 `DEPLOYMENT.md` 执行部署和重启。遇到同一阻塞连续三次时才停止并汇报。
