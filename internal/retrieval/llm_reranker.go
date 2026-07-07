package retrieval

import (
	"context"
	"errors"
	"strings"

	"memory-os/internal/llm"
)

type LLMReranker struct {
	client llm.RerankClient
}

func NewLLMReranker(client llm.RerankClient) LLMReranker {
	return LLMReranker{client: client}
}

func (r LLMReranker) Rerank(query string, candidates []RerankCandidate) ([]RerankScore, error) {
	if r.client == nil {
		return nil, errors.New("rerank client is required")
	}
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("rerank query is required")
	}
	if len(candidates) == 0 {
		return nil, errors.New("rerank candidates are required")
	}
	documents := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		documents = append(documents, candidate.Text)
	}
	response, err := r.client.Rerank(context.Background(), query, documents)
	if err != nil {
		return nil, err
	}
	byIndex := map[int]float64{}
	for _, result := range response.Results {
		if result.Index < 0 || result.Index >= len(candidates) {
			continue
		}
		byIndex[result.Index] = result.Score
	}
	scores := make([]RerankScore, 0, len(candidates))
	for index, candidate := range candidates {
		scores = append(scores, RerankScore{ID: candidate.ID, Score: byIndex[index]})
	}
	return scores, nil
}
