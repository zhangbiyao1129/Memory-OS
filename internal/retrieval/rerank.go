package retrieval

import "errors"

type Reranker interface {
	Rerank(query string, candidates []RerankCandidate) ([]RerankScore, error)
}

type RerankCandidate struct {
	ID   string
	Text string
}

type RerankScore struct {
	ID    string
	Score float64
}

type StaticReranker struct {
	Scores map[string]float64
}

func (r StaticReranker) Rerank(query string, candidates []RerankCandidate) ([]RerankScore, error) {
	scores := []RerankScore{}
	for _, candidate := range candidates {
		score, ok := r.Scores[candidate.ID]
		if !ok {
			score = 0
		}
		scores = append(scores, RerankScore{ID: candidate.ID, Score: score})
	}
	return scores, nil
}

type FailingReranker struct {
	Err error
}

func (r FailingReranker) Rerank(query string, candidates []RerankCandidate) ([]RerankScore, error) {
	if r.Err != nil {
		return nil, r.Err
	}
	return nil, errors.New("rerank failed")
}
