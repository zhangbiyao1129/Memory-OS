package llm

import "context"

// ChatClient 定义后续对话模型调用边界。
type ChatClient interface {
	Chat(ctx context.Context, request ChatRequest) (ChatResponse, error)
}

// EmbeddingClient 定义向量模型调用边界。
type EmbeddingClient interface {
	Embed(ctx context.Context, texts []string) (EmbeddingResponse, error)
}

// RerankClient 定义 rerank 模型调用边界。
type RerankClient interface {
	Rerank(ctx context.Context, query string, documents []string) (RerankResponse, error)
}

type ChatRequest struct {
	Messages []Message
	Model    string
}

type Message struct {
	Role    string
	Content string
}

type ChatResponse struct {
	Text string
}

type EmbeddingResponse struct {
	Vectors [][]float32
	Dim     int
}

type RerankResponse struct {
	Results []RerankResult
}

type RerankResult struct {
	Index int
	Score float64
}
