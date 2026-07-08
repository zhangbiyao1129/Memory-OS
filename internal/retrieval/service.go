package retrieval

import (
	"errors"
	"sort"
	"strings"

	"memory-os/internal/candidatememory"
	"memory-os/internal/hotmemory"
	"memory-os/internal/qdrant"
	"memory-os/internal/rag"
	"memory-os/internal/secret"
)

const defaultRerankCandidateLimit = 16

type HotMemory interface {
	Search(hotmemory.SearchRequest) ([]hotmemory.SearchResult, error)
	MarkAccessed(string) (hotmemory.Memory, error)
	MarkReturned(string) (hotmemory.Memory, error)
	MarkUsed(string) (hotmemory.Memory, error)
}

type ArchiveRAG interface {
	Search(rag.SearchRequest) ([]rag.SearchResult, error)
}

type ArchiveGenerationResolver interface {
	CurrentGeneration(ArchiveGenerationContext) (int, error)
}

type Options struct {
	HotMemory                 HotMemory
	ArchiveRAG                ArchiveRAG
	ArchiveGenerationResolver ArchiveGenerationResolver
	Reranker                  Reranker
	AccessLog                 AccessLog
	ArchiveFeedbackThreshold  int // Archive 高频命中生成候选阈值,0 用默认 3
	MinRerankScore            float64
	RerankCandidateLimit      int
}

type Service struct {
	hotMemory                 HotMemory
	archiveRAG                ArchiveRAG
	archiveGenerationResolver ArchiveGenerationResolver
	reranker                  Reranker
	accessLog                 AccessLog
	feedback                  *ArchiveFeedbackTracker
	minRerankScore            float64
	rerankCandidateLimit      int
}

func NewService(options Options) Service {
	threshold := options.ArchiveFeedbackThreshold
	if threshold <= 0 {
		threshold = 3
	}
	rerankCandidateLimit := options.RerankCandidateLimit
	if rerankCandidateLimit <= 0 {
		rerankCandidateLimit = defaultRerankCandidateLimit
	}
	return Service{hotMemory: options.HotMemory, archiveRAG: options.ArchiveRAG, archiveGenerationResolver: options.ArchiveGenerationResolver, reranker: options.Reranker, accessLog: options.AccessLog, feedback: NewArchiveFeedbackTracker(threshold), minRerankScore: options.MinRerankScore, rerankCandidateLimit: rerankCandidateLimit}
}

func (s Service) Configured() bool {
	return s.hotMemory != nil || s.archiveRAG != nil
}

// PendingArchiveCandidates 返回 Archive 高频命中生成的待处理候选。
func (s Service) PendingArchiveCandidates() []ArchiveCandidate {
	if s.feedback == nil {
		return nil
	}
	return s.feedback.PendingCandidates()
}

func (s Service) Search(request SearchRequest) (SearchResponse, error) {
	if err := validateRequest(request); err != nil {
		return SearchResponse{}, err
	}
	candidates, err := s.collect(request)
	if err != nil {
		return SearchResponse{}, err
	}
	rerankDegraded := false
	if s.reranker != nil && len(candidates) > 0 {
		candidates = limitCandidatesForRerank(candidates, s.rerankCandidateLimit)
		scores, err := s.reranker.Rerank(request.Query, rerankCandidates(candidates))
		if err != nil {
			rerankDegraded = true
		} else {
			applyRerankScores(candidates, scores)
			candidates = filterByMinScore(candidates, s.minRerankScore)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	results := resultsFromCandidates(candidates)
	markedUsed := s.markReturnedHotResults(results)
	context := compress(results, request.MaxContextBytes)
	response := SearchResponse{RequestID: request.RequestID, Context: context, Results: results, RerankDegraded: rerankDegraded, MarkedUsedCount: markedUsed}
	if s.accessLog != nil {
		_ = s.accessLog.LogRequest(request, rerankDegraded)
		for index, result := range results {
			_ = s.accessLog.LogResult(request.RequestID, index+1, result)
			response.AccessLogCount++
		}
	}
	return response, nil
}

func (s Service) collect(request SearchRequest) ([]candidate, error) {
	candidates := []candidate{}
	recallQuery := primaryRecallQuery(request.Query)
	if s.hotMemory != nil {
		projectFilter, err := hotmemory.BuildFilter(hotmemory.FilterContext{OrgID: request.Actor.OrgID, ProjectID: request.Actor.ProjectID, UserID: request.Actor.UserID, AgentID: request.Actor.AgentID, Scope: request.Scope, Visibility: request.Visibility, PermissionLabels: request.PermissionLabels})
		if err != nil {
			return nil, err
		}
		results, err := s.hotMemory.Search(hotmemory.SearchRequest{Query: recallQuery, Filter: projectFilter})
		if err != nil {
			return nil, err
		}
		for _, result := range results {
			candidates = append(candidates, hotMemoryCandidate("hot_memory:", result, result.Score))
		}
		if request.Scope != hotmemory.ScopeAgentSpecific {
			agentFilter, err := hotmemory.BuildFilter(hotmemory.FilterContext{OrgID: request.Actor.OrgID, ProjectID: request.Actor.ProjectID, UserID: request.Actor.UserID, AgentID: request.Actor.AgentID, Scope: hotmemory.ScopeAgentSpecific, Visibility: request.Visibility, PermissionLabels: request.PermissionLabels})
			if err != nil {
				return nil, err
			}
			agentResults, err := s.hotMemory.Search(hotmemory.SearchRequest{Query: recallQuery, Filter: agentFilter})
			if err != nil {
				return nil, err
			}
			for _, result := range agentResults {
				candidates = append(candidates, hotMemoryCandidate("hot_memory:", result, result.Score))
			}
		}
		if request.Scope == hotmemory.ScopeProject {
			globalFilter, err := hotmemory.BuildFilter(hotmemory.FilterContext{
				OrgID:      request.Actor.OrgID,
				UserID:     request.Actor.UserID,
				AgentID:    request.Actor.AgentID,
				Scope:      hotmemory.ScopeUser,
				Visibility: "private",
			})
			if err != nil {
				return nil, err
			}
			globalResults, err := s.hotMemory.Search(hotmemory.SearchRequest{Query: recallQuery, Filter: globalFilter})
			if err != nil {
				return nil, err
			}
			for _, result := range globalResults {
				memory := result.Memory
				if memory.ProjectID != candidatememory.GlobalHotMemoryProjectID || memory.Scope != hotmemory.ScopeUser {
					continue
				}
				candidates = append(candidates, hotMemoryCandidate("hot_memory_global:", result, result.Score*0.72))
			}
		}
	}
	if s.archiveRAG != nil {
		generation, err := s.archiveIndexGeneration(request)
		if err != nil {
			return nil, err
		}
		if generation <= 0 {
			return dedupeCandidates(candidates), nil
		}
		filter, err := qdrant.BuildPayloadFilter(qdrant.FilterContext{OrgID: request.Actor.OrgID, ProjectID: request.Actor.ProjectID, UserID: request.Actor.UserID, Visibility: request.Visibility, PermissionLabels: request.PermissionLabels, DocType: "archive_chunk", IndexGeneration: generation})
		if err != nil {
			return nil, err
		}
		results, err := s.archiveRAG.Search(rag.SearchRequest{Query: recallQuery, Filter: filter})
		if err != nil {
			return nil, err
		}
		for _, result := range results {
			candidates = append(candidates, candidate{id: "archive:" + result.Source.ChunkID, text: result.Text, score: result.Score, source: SourceRef{Kind: SourceArchiveChunk, ArchiveID: result.Source.ArchiveID, ChunkID: result.Source.ChunkID, SourceEventIDs: result.Source.SourceEventIDs}})
			// Phase 7: 记录 Archive 命中,高频时生成摘要型候选。
			if s.feedback != nil {
				s.feedback.RecordHit(ArchiveHit{
					ArchiveID: result.Source.ArchiveID,
					ChunkID:   result.Source.ChunkID,
					Content:   result.Text,
					OrgID:     request.Actor.OrgID,
					ProjectID: request.Actor.ProjectID,
					UserID:    request.Actor.UserID,
				})
			}
		}
	}
	// Phase 7: 对所有候选施加内容类型 boost（短事实 +20%，完整过程 +10%）。
	for i := range candidates {
		boostCandidate(&candidates[i])
	}
	return dedupeCandidates(candidates), nil
}

func hotMemoryCandidate(prefix string, result hotmemory.SearchResult, score float64) candidate {
	memory := result.Memory
	return candidate{
		id:    prefix + memory.MemoryID,
		text:  memory.Fact,
		score: score,
		source: SourceRef{
			Kind:      SourceHotMemory,
			MemoryID:  memory.MemoryID,
			Scope:     string(memory.Scope),
			ProjectID: memory.ProjectID,
		},
	}
}

func (s Service) markReturnedHotResults(results []SearchResult) int {
	if s.hotMemory == nil {
		return 0
	}
	count := 0
	for _, result := range results {
		if result.Source.Kind != SourceHotMemory || result.Source.MemoryID == "" {
			continue
		}
		if _, err := s.hotMemory.MarkAccessed(result.Source.MemoryID); err != nil {
			continue
		}
		if _, err := s.hotMemory.MarkReturned(result.Source.MemoryID); err != nil {
			continue
		}
		count++
	}
	return count
}

func (s Service) archiveIndexGeneration(request SearchRequest) (int, error) {
	if request.ArchiveIndexGeneration > 0 {
		return request.ArchiveIndexGeneration, nil
	}
	if s.archiveGenerationResolver == nil {
		return 0, errors.New("archive index generation is required")
	}
	generation, err := s.archiveGenerationResolver.CurrentGeneration(ArchiveGenerationContext{UserID: request.Actor.UserID, OrgID: request.Actor.OrgID, ProjectID: request.Actor.ProjectID})
	if err != nil {
		return 0, err
	}
	return generation, nil
}

func primaryRecallQuery(query string) string {
	return strings.TrimSpace(query)
}

func validateRequest(request SearchRequest) error {
	if strings.TrimSpace(request.Query) == "" {
		return errors.New("query is required")
	}
	if request.Actor.UserID == "" || request.Actor.OrgID == "" || request.Actor.ProjectID == "" || request.Actor.AgentID == "" {
		return errors.New("actor context is required")
	}
	if request.Scope != hotmemory.ScopeUser && request.Scope != hotmemory.ScopeProject && request.Scope != hotmemory.ScopeOrg && request.Scope != hotmemory.ScopeAgentSpecific {
		return errors.New("invalid scope")
	}
	if request.Visibility == "" {
		return errors.New("visibility is required")
	}
	if request.Visibility != "private" && len(request.PermissionLabels) == 0 {
		return errors.New("permission labels are required")
	}
	return nil
}

func rerankCandidates(candidates []candidate) []RerankCandidate {
	items := []RerankCandidate{}
	for _, candidate := range candidates {
		items = append(items, RerankCandidate{ID: candidate.id, Text: candidate.text})
	}
	return items
}

func applyRerankScores(candidates []candidate, scores []RerankScore) {
	byID := map[string]float64{}
	for _, score := range scores {
		byID[score.ID] = score.Score
	}
	for index := range candidates {
		if score, ok := byID[candidates[index].id]; ok {
			candidates[index].score = score
		}
	}
}

func filterByMinScore(candidates []candidate, minScore float64) []candidate {
	if minScore <= 0 {
		return candidates
	}
	filtered := []candidate{}
	for _, candidate := range candidates {
		if candidate.score >= minScore {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func limitCandidatesForRerank(candidates []candidate, limit int) []candidate {
	if limit <= 0 || len(candidates) <= limit {
		return candidates
	}
	limited := append([]candidate(nil), candidates...)
	sort.SliceStable(limited, func(i, j int) bool { return limited[i].score > limited[j].score })
	return limited[:limit]
}

func dedupeCandidates(candidates []candidate) []candidate {
	seen := map[string]bool{}
	out := []candidate{}
	for _, candidate := range candidates {
		if seen[candidate.id] {
			continue
		}
		seen[candidate.id] = true
		out = append(out, candidate)
	}
	return out
}

func resultsFromCandidates(candidates []candidate) []SearchResult {
	results := []SearchResult{}
	for _, candidate := range candidates {
		results = append(results, SearchResult{Text: candidate.text, Score: candidate.score, Source: candidate.source})
	}
	return results
}

func compress(results []SearchResult, budget int) string {
	builder := strings.Builder{}
	for _, result := range results {
		line := result.Text
		sanitized := secret.Sanitize(line, func(index int, match string) string { return "secret_ref_retrieval" })
		line = sanitized.Text
		if budget > 0 && builder.Len()+len(line) > budget {
			remaining := budget - builder.Len()
			if remaining > 0 {
				builder.WriteString(line[:remaining])
			}
			break
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(line)
	}
	return builder.String()
}
