package retrieval

import (
	"context"
	"testing"

	"memory-os/internal/llm"
)

func TestLLMRerankerMapsModelScoresToCandidateIDs(t *testing.T) {
	client := &fakeRerankClient{response: llm.RerankResponse{Results: []llm.RerankResult{{Index: 1, Score: 0.91}, {Index: 0, Score: 0.62}}}}
	reranker := NewLLMReranker(client)

	scores, err := reranker.Rerank("deploy API", []RerankCandidate{{ID: "a", Text: "alpha"}, {ID: "b", Text: "beta"}})
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}
	if client.query != "deploy API" || len(client.documents) != 2 || client.documents[1] != "beta" {
		t.Fatalf("captured query/documents = %q %#v", client.query, client.documents)
	}
	if len(scores) != 2 || scores[0].ID != "a" || scores[0].Score != 0.62 || scores[1].ID != "b" || scores[1].Score != 0.91 {
		t.Fatalf("scores = %#v", scores)
	}
}

type fakeRerankClient struct {
	query     string
	documents []string
	response  llm.RerankResponse
	err       error
}

func (c *fakeRerankClient) Rerank(ctx context.Context, query string, documents []string) (llm.RerankResponse, error) {
	c.query = query
	c.documents = append([]string(nil), documents...)
	if c.err != nil {
		return llm.RerankResponse{}, c.err
	}
	return c.response, nil
}
