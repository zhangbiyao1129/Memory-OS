package candidatememory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"memory-os/internal/llm"
)

type fakeChatClient struct {
	text string
	err  error
}

func (f fakeChatClient) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	if f.err != nil {
		return llm.ChatResponse{}, f.err
	}
	return llm.ChatResponse{Text: f.text}, nil
}

func TestLLMExtractorNotConfigured(t *testing.T) {
	e := NewLLMExtractor(nil)
	_, err := e.Extract(context.Background(), ExtractionRequest{Events: []ExtractionEvent{{EventID: "e1"}}})
	if err != ErrExtractorNotConfigured {
		t.Fatalf("期望 ErrExtractorNotConfigured,得到 %v", err)
	}
}

func TestLLMExtractorRejectsEmptyEvents(t *testing.T) {
	e := NewLLMExtractor(fakeChatClient{})
	if _, err := e.Extract(context.Background(), ExtractionRequest{}); err == nil {
		t.Fatal("空事件应报错")
	}
}

func TestLLMExtractorParsesCandidatesAndFillsOwnership(t *testing.T) {
	resp := `{"candidates":[
		{"memory_type":"fact","content":"项目使用 PostgreSQL 作为权威元数据源","summary":"DB 选型","confidence":0.9},
		{"memory_type":"bugfix","content":"修复 schema 迁移导致的归档为空","summary":"迁移修复","confidence":0.8}
	]}`
	e := NewLLMExtractor(fakeChatClient{text: resp})
	result, err := e.Extract(context.Background(), ExtractionRequest{
		OrgID: "o", ProjectID: "p", SourceKey: "github.com/acme/web",
		UserID: "u", AgentID: "a", ThreadID: "t", SessionID: "s",
		Events: []ExtractionEvent{{EventID: "e1", Type: "assistant_final", Payload: []byte("{}")}},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("期望 2 个候选,得到 %d", len(result.Candidates))
	}
	fact := result.Candidates[0]
	if fact.MemoryType != MemoryTypeFact || fact.Content != "项目使用 PostgreSQL 作为权威元数据源" {
		t.Fatalf("fact 候选解析不正确: %+v", fact)
	}
	if fact.OrgID != "o" || fact.ProjectID != "p" || fact.SourceKey != "github.com/acme/web" {
		t.Fatalf("归属未填充: %+v", fact)
	}
	if len(fact.SourceEventIDs) != 1 || fact.SourceEventIDs[0] != "e1" {
		t.Fatalf("source_event_ids 未填充: %v", fact.SourceEventIDs)
	}
	if fact.Status != StatusPending {
		t.Fatalf("status 默认应为 pending: %s", fact.Status)
	}
	// 第二条含 "schema 迁移" → 应被 AssessRisk 提升为 high
	bugfix := result.Candidates[1]
	if bugfix.RiskLevel != RiskHigh {
		t.Fatalf("含 schema 迁移关键词应识别为高风险: %v", bugfix.RiskLevel)
	}
}

func TestLLMExtractorLimitsCandidatesPerEvent(t *testing.T) {
	resp := `{"candidates":[
		{"memory_type":"fact","content":"事实一","summary":"","confidence":0.9},
		{"memory_type":"decision","content":"决策二","summary":"","confidence":0.9},
		{"memory_type":"bugfix","content":"修复三","summary":"","confidence":0.9},
		{"memory_type":"risk","content":"风险四","summary":"","confidence":0.9},
		{"memory_type":"follow_up","content":"后续五","summary":"","confidence":0.9}
	]}`
	e := NewLLMExtractor(fakeChatClient{text: resp})
	result, err := e.Extract(context.Background(), ExtractionRequest{
		OrgID: "o", ProjectID: "p", SourceKey: "github.com/acme/web",
		Events: []ExtractionEvent{{EventID: "e1", Type: "assistant_final", Payload: []byte("{}")}},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(result.Candidates) != 3 {
		t.Fatalf("单事件最多应保留 3 个候选,得到 %d", len(result.Candidates))
	}
	for _, c := range result.Candidates {
		if c.Content == "风险四" || c.Content == "后续五" {
			t.Fatalf("超出预算的候选不应保留: %+v", c)
		}
	}
}

func TestLLMExtractorSanitizesSecrets(t *testing.T) {
	resp := `{"candidates":[{"memory_type":"fact","content":"使用 token sk-abcd1234567890 鉴权","summary":"","confidence":0.9}]}`
	e := NewLLMExtractor(fakeChatClient{text: resp})
	result, err := e.Extract(context.Background(), ExtractionRequest{
		OrgID: "o", ProjectID: "p", SourceKey: "sk",
		Events: []ExtractionEvent{{EventID: "e1"}},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if strings.Contains(result.Candidates[0].Content, "sk-abcd1234567890") {
		t.Fatalf("secret 明文未脱敏: %s", result.Candidates[0].Content)
	}
}

func TestLLMExtractorLLMFailureReturnsError(t *testing.T) {
	e := NewLLMExtractor(fakeChatClient{err: errors.New("llm timeout")})
	if _, err := e.Extract(context.Background(), ExtractionRequest{Events: []ExtractionEvent{{EventID: "e1"}}}); err == nil {
		t.Fatal("LLM 失败应返回 error(供 worker 重试)")
	}
}

func TestLLMExtractorMalformedJSONReturnsError(t *testing.T) {
	e := NewLLMExtractor(fakeChatClient{text: "not json"})
	if _, err := e.Extract(context.Background(), ExtractionRequest{Events: []ExtractionEvent{{EventID: "e1"}}}); err == nil {
		t.Fatal("非法 JSON 应返回 error")
	}
}
