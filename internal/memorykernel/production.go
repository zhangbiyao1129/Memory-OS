package memorykernel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"memory-os/internal/archive"
	"memory-os/internal/candidatememory"
	"memory-os/internal/hotmemory"
	"memory-os/internal/retrieval"
	"memory-os/internal/secret"
)

type productionCandidateSource struct {
	repo candidatememory.Repository
}

func NewCandidateSource(repo candidatememory.Repository) CandidateSource {
	return productionCandidateSource{repo: repo}
}

func (s productionCandidateSource) ListKernelCandidates(ctx context.Context, scope Scope, limit int) ([]CandidateInput, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("candidate source is not configured")
	}
	candidates, err := s.repo.ListCandidates(ctx, candidatememory.ListFilter{
		OrgID:     scope.OrgID,
		ProjectID: scope.ProjectID,
		SourceKey: scope.SourceKey,
		ThreadID:  scope.ThreadID,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]CandidateInput, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, CandidateInput{
			ID:         candidate.CandidateID,
			Content:    candidate.Content,
			Summary:    candidate.Summary,
			Type:       string(candidate.MemoryType),
			RiskLevel:  string(candidate.RiskLevel),
			Status:     string(candidate.Status),
			Confidence: candidate.Confidence,
		})
	}
	return out, nil
}

type productionHotMemorySource struct {
	service hotmemory.Service
}

func NewHotMemorySource(service hotmemory.Service) HotMemorySource {
	return productionHotMemorySource{service: service}
}

func (s productionHotMemorySource) ListKernelHotMemories(ctx context.Context, scope Scope, limit int) ([]HotMemoryInput, error) {
	if !s.service.Configured() {
		return nil, fmt.Errorf("hot memory source is not configured")
	}
	memories, err := s.service.List(map[string][]string{
		"org_id":     {scope.OrgID},
		"project_id": {scope.ProjectID},
		"user_id":    {scope.UserID},
		"status":     {string(hotmemory.StatusActive)},
	})
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(memories) > limit {
		memories = memories[:limit]
	}
	out := make([]HotMemoryInput, 0, len(memories))
	for _, memory := range memories {
		out = append(out, HotMemoryInput{
			ID:            memory.MemoryID,
			Fact:          memory.Fact,
			Status:        string(memory.Status),
			AccessCount:   memory.AccessCount,
			ReturnedCount: memory.ReturnedCount,
			UsedCount:     memory.UsedCount,
			Pinned:        memory.Pinned,
		})
	}
	return out, nil
}

type productionArchiveSource struct {
	repo archive.Repository
}

func NewArchiveSource(repo archive.Repository) ArchiveSource {
	return productionArchiveSource{repo: repo}
}

func (s productionArchiveSource) ListKernelArchives(ctx context.Context, scope Scope, limit int) ([]ArchiveInput, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("archive source is not configured")
	}
	archives, err := s.repo.List(archive.ListFilter{
		UserID:    scope.UserID,
		OrgID:     scope.OrgID,
		ProjectID: scope.ProjectID,
		Status:    "active",
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ArchiveInput, 0, len(archives))
	for _, item := range archives {
		out = append(out, ArchiveInput{
			ID:        item.ArchiveID,
			Title:     item.Title,
			Status:    item.Status,
			UpdatedAt: item.UpdatedAt,
		})
	}
	return out, nil
}

type productionRetrievalSource struct {
	log retrieval.AccessLogReader
}

func NewRetrievalSource(log retrieval.AccessLogReader) RetrievalSource {
	return productionRetrievalSource{log: log}
}

func (s productionRetrievalSource) ListKernelRetrievals(ctx context.Context, scope Scope, limit int) ([]RetrievalInput, error) {
	if s.log == nil {
		return nil, fmt.Errorf("retrieval source is not configured")
	}
	results, err := s.log.ListResults(retrieval.AccessLogListFilter{
		OrgID:       scope.OrgID,
		ProjectID:   scope.ProjectID,
		ActorUserID: scope.UserID,
		Limit:       limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]RetrievalInput, 0, len(results))
	for _, result := range results {
		out = append(out, RetrievalInput{
			RequestID:  result.RequestID,
			SourceKind: result.SourceKind,
			SourceID:   retrievalSourceID(result.SourceRef),
			CreatedAt:  result.CreatedAt,
		})
	}
	return out, nil
}

func retrievalSourceID(sourceRef map[string]any) string {
	if sourceRef == nil {
		return ""
	}
	for _, key := range []string{"memory_id", "archive_id", "chunk_id"} {
		if value, ok := sourceRef[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type productionCandidateApplier struct {
	repo candidatememory.Repository
}

func NewCandidateApplier(repo candidatememory.Repository) CandidateApplier {
	return productionCandidateApplier{repo: repo}
}

func (a productionCandidateApplier) UpdateCandidateGovernance(ctx context.Context, orgID, candidateID string, status interface{}, needsReview bool, reason string, supersededBy string) (interface{}, error) {
	if a.repo == nil {
		return nil, fmt.Errorf("candidate applier is not configured")
	}
	return a.repo.UpdateCandidateGovernance(ctx, orgID, candidateID, candidatememory.Status(status.(string)), needsReview, reason, supersededBy)
}

type productionHotMemoryApplier struct {
	service hotmemory.Service
}

func NewHotMemoryApplier(service hotmemory.Service) HotMemoryApplier {
	return productionHotMemoryApplier{service: service}
}

func (a productionHotMemoryApplier) Get(memoryID string) (HotMemoryGetResult, error) {
	if !a.service.Configured() {
		return HotMemoryGetResult{}, fmt.Errorf("hot memory applier is not configured")
	}
	memory, err := a.service.Get(memoryID)
	if err != nil {
		return HotMemoryGetResult{}, err
	}
	return HotMemoryGetResult{MemoryID: memory.MemoryID, Pinned: memory.Pinned, Status: string(memory.Status)}, nil
}

func (a productionHotMemoryApplier) Update(req HotMemoryUpdateRequest) (HotMemoryUpdateResult, error) {
	if !a.service.Configured() {
		return HotMemoryUpdateResult{}, fmt.Errorf("hot memory applier is not configured")
	}
	switch strings.ToLower(strings.TrimSpace(req.Status)) {
	case string(hotmemory.StatusDemoted):
		memory, err := a.service.Demote(req.MemoryID)
		if err != nil {
			return HotMemoryUpdateResult{}, err
		}
		return HotMemoryUpdateResult{MemoryID: memory.MemoryID, Status: string(memory.Status)}, nil
	case string(hotmemory.StatusPromoted):
		memory, err := a.service.Promote(req.MemoryID)
		if err != nil {
			return HotMemoryUpdateResult{}, err
		}
		return HotMemoryUpdateResult{MemoryID: memory.MemoryID, Status: string(memory.Status)}, nil
	default:
		memory, err := a.service.Get(req.MemoryID)
		if err != nil {
			return HotMemoryUpdateResult{}, err
		}
		return HotMemoryUpdateResult{MemoryID: memory.MemoryID, Status: string(memory.Status)}, nil
	}
}

type productionCorrectionArchiveCreator struct {
	repo           Repository
	archiveService archive.Service
}

func NewCorrectionArchiveCreator(repo Repository, archiveService archive.Service) CorrectionArchiveCreator {
	return productionCorrectionArchiveCreator{repo: repo, archiveService: archiveService}
}

func (c productionCorrectionArchiveCreator) CreateCorrectionArchive(ctx context.Context, req CorrectionArchiveRequest) (CorrectionArchiveResult, error) {
	if !c.archiveService.Configured() {
		return CorrectionArchiveResult{}, fmt.Errorf("correction archive service is not configured")
	}
	units := append([]MemoryUnit(nil), req.Units...)
	if len(units) == 0 && c.repo != nil {
		fetched, err := c.repo.ListUnits(ctx, UnitFilter{OrgID: req.OrgID, ProjectID: req.ProjectID, SourceKey: req.SourceKey, Status: string(UnitCurrent), Limit: 100})
		if err != nil {
			return CorrectionArchiveResult{}, err
		}
		units = fetched
	}
	actions := append([]GovernanceAction(nil), req.Actions...)
	if len(actions) == 0 && c.repo != nil {
		fetched, err := c.repo.ListActions(ctx, ActionFilter{OrgID: req.OrgID, ProjectID: req.ProjectID, Limit: 100})
		if err != nil {
			return CorrectionArchiveResult{}, err
		}
		actions = fetched
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		for _, unit := range units {
			if strings.TrimSpace(unit.UserID) != "" {
				userID = unit.UserID
				break
			}
		}
	}
	if userID == "" {
		return CorrectionArchiveResult{}, fmt.Errorf("correction archive user id is required")
	}
	title := "记忆修订: Memory Kernel 当前可信结论"
	archiveID := correctionArchiveID(req)
	requestID := correctionArchiveRequestID(req)
	markdown := renderCorrectionArchiveMarkdown(units, actions)
	result, err := c.archiveService.Create(archive.CreateRequest{
		RequestID:  requestID,
		ArchiveID:  archiveID,
		Title:      title,
		UserID:     userID,
		OrgID:      req.OrgID,
		ProjectID:  req.ProjectID,
		CreatedAt:  time.Now().UTC(),
		Markdown:   markdown,
		RenderMode: "knowledge",
	})
	if err != nil {
		return CorrectionArchiveResult{}, err
	}
	return CorrectionArchiveResult{ArchiveID: result.Metadata.ArchiveID, Title: title}, nil
}

type productionCIRunner struct {
	repo        Repository
	contextPack ContextPackService
}

func NewProductionCIRunner(repo Repository, contextPack ContextPackService) CIRunner {
	return productionCIRunner{repo: repo, contextPack: contextPack}
}

func (r productionCIRunner) RunCase(ctx context.Context, caseID string) (CIResult, error) {
	if r.repo == nil {
		return CIResult{}, fmt.Errorf("ci runner repository is not configured")
	}
	cases, err := r.repo.ListCICases(ctx, CICaseFilter{CaseID: caseID, Limit: 1})
	if err != nil {
		return CIResult{}, fmt.Errorf("list ci cases: %w", err)
	}
	if len(cases) == 0 {
		return CIResult{}, fmt.Errorf("ci case %q not found", caseID)
	}
	c := cases[0]
	if r.contextPack == nil {
		return CIResult{}, fmt.Errorf("ci runner context pack is not configured")
	}
	pack, err := r.contextPack.Build(ctx, ContextPackRequest{
		OrgID:     c.OrgID,
		ProjectID: c.ProjectID,
		Query:     c.Question,
	})
	if err != nil {
		return CIResult{}, fmt.Errorf("build context pack: %w", err)
	}
	response := pack.Context
	if len(response) > 2000 {
		response = response[:2000]
	}
	sanitized := secret.Sanitize(response, nil)
	response = sanitized.Text

	matchedInclude := []string{}
	allIncluded := true
	for _, phrase := range c.MustInclude {
		if strings.Contains(response, phrase) {
			matchedInclude = append(matchedInclude, phrase)
		} else {
			allIncluded = false
		}
	}

	matchedExclude := []string{}
	for _, phrase := range c.MustNotInclude {
		if strings.Contains(response, phrase) {
			matchedExclude = append(matchedExclude, phrase)
		}
	}

	passed := allIncluded && len(matchedExclude) == 0
	requestID := fmt.Sprintf("memory_ci_%s_%d", caseID, time.Now().UnixNano())
	return CIResult{
		ResultID:        fmt.Sprintf("ci_result_%s_%d", caseID, time.Now().UnixNano()),
		CaseID:          caseID,
		RequestID:       requestID,
		Passed:          passed,
		MatchedInclude:  matchedInclude,
		MatchedExclude:  matchedExclude,
		ResponseExcerpt: response,
	}, nil
}

func correctionArchiveID(req CorrectionArchiveRequest) string {
	return fmt.Sprintf("archive_memory_correction_%s_%s_%d", sanitizeForID(req.SourceKey), sanitizeForID(req.ThreadID), time.Now().UTC().UnixNano())
}

func correctionArchiveRequestID(req CorrectionArchiveRequest) string {
	return fmt.Sprintf("memory_kernel_correction_%s_%s_%d", sanitizeForID(req.SourceKey), sanitizeForID(req.ThreadID), time.Now().UTC().UnixNano())
}

func renderCorrectionArchiveMarkdown(units []MemoryUnit, actions []GovernanceAction) string {
	var sb strings.Builder
	sb.WriteString("# 记忆修订: Memory Kernel 当前可信结论\n\n")
	sb.WriteString("## 当前结论\n")
	if len(units) == 0 {
		sb.WriteString("- 暂无 current unit\n")
	} else {
		for _, unit := range units {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", unit.UnitID, secret.Sanitize(unit.Content, nil).Text))
		}
	}
	sb.WriteString("\n## 已覆盖的旧记忆\n")
	if len(actions) == 0 {
		sb.WriteString("- 暂无治理动作\n")
	} else {
		for _, action := range actions {
			sb.WriteString(fmt.Sprintf("- %s %s -> %s\n", action.Action, action.TargetType, action.TargetID))
		}
	}
	sb.WriteString("\n## 证据\n")
	for _, unit := range units {
		for _, ref := range unit.SourceRefs {
			sb.WriteString(fmt.Sprintf("- unit %s <- %s:%s\n", unit.UnitID, ref.Kind, ref.ID))
		}
	}
	for _, action := range actions {
		for _, ref := range action.EvidenceRefs {
			sb.WriteString(fmt.Sprintf("- action %s <- %s:%s\n", action.ActionID, ref.Kind, ref.ID))
		}
	}
	sb.WriteString("\n## 对 Agent 的影响\n")
	if len(units) == 0 {
		sb.WriteString("- 无可用 current unit\n")
	} else {
		for _, unit := range units {
			if strings.TrimSpace(unit.AgentShould) != "" {
				sb.WriteString(fmt.Sprintf("- %s\n", secret.Sanitize(unit.AgentShould, nil).Text))
			}
		}
	}
	sb.WriteString("\n## 来源\n")
	if len(units) == 0 && len(actions) == 0 {
		sb.WriteString("- 无\n")
	}
	return sb.String()
}

func sanitizeForID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "default"
	}
	return out
}
