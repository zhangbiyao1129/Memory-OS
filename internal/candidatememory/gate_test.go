package candidatememory

import (
	"testing"
)

func TestGateManualArchiveAllowed(t *testing.T) {
	decision := ShouldExtract(ExtractionRequest{
		Events: []ExtractionEvent{
			{EventID: "e1", Type: "manual_archive_request", Payload: []byte(`{"topic":"short"}`)},
		},
	})
	if !decision.Allow {
		t.Fatalf("manual_archive_request 应始终允许,得到 decision.Allow=%v, Reason=%s", decision.Allow, decision.Reason)
	}
	if decision.Reason != "manual_archive" {
		t.Fatalf("Reason 应为 manual_archive,得到 %s", decision.Reason)
	}
}

func TestGateAssistantFinalWithUserPreferenceAllowed(t *testing.T) {
	decision := ShouldExtract(ExtractionRequest{
		Events: []ExtractionEvent{
			{EventID: "e1", Type: "assistant_final", Payload: []byte(`{"text":"我希望以后都用 Go 写后端"}`)},
		},
	})
	if !decision.Allow {
		t.Fatalf("assistant_final 含用户偏好应允许,得到 decision.Allow=%v, Reason=%s", decision.Allow, decision.Reason)
	}
}

func TestGateAssistantFinalWithStableConstraintAllowed(t *testing.T) {
	decision := ShouldExtract(ExtractionRequest{
		Events: []ExtractionEvent{
			{EventID: "e1", Type: "assistant_final", Payload: []byte(`{"text":"项目使用 PostgreSQL 作为权威元数据源"}`)},
		},
	})
	if !decision.Allow {
		t.Fatalf("assistant_final 含稳定约束应允许,得到 decision.Allow=%v, Reason=%s", decision.Allow, decision.Reason)
	}
}

func TestGateAssistantFinalNoiseOnlyRejected(t *testing.T) {
	decision := ShouldExtract(ExtractionRequest{
		Events: []ExtractionEvent{
			{EventID: "e1", Type: "assistant_final", Payload: []byte(`{"text":"命令完成，测试通过"}`)},
			{EventID: "e2", Type: "assistant_final", Payload: []byte(`{"text":"我正在查看 /tmp/test.log"}`)},
		},
	})
	if decision.Allow {
		t.Fatalf("assistant_final 只有噪声应拒绝,得到 decision.Allow=%v, Reason=%s", decision.Allow, decision.Reason)
	}
	if decision.Reason != "noise_only" {
		t.Fatalf("Reason 应为 noise_only,得到 %s", decision.Reason)
	}
}

func TestGateUnsupportedEventTypeRejected(t *testing.T) {
	decision := ShouldExtract(ExtractionRequest{
		Events: []ExtractionEvent{
			{EventID: "e1", Type: "unknown_type", Payload: []byte(`{"text":"some content"}`)},
		},
	})
	if decision.Allow {
		t.Fatalf("unsupported event type 应拒绝,得到 decision.Allow=%v, Reason=%s", decision.Allow, decision.Reason)
	}
	if decision.Reason != "unsupported_event_type" {
		t.Fatalf("Reason 应为 unsupported_event_type,得到 %s", decision.Reason)
	}
}

func TestGateMultipleEventsOneValuableAllowed(t *testing.T) {
	decision := ShouldExtract(ExtractionRequest{
		Events: []ExtractionEvent{
			{EventID: "e1", Type: "assistant_final", Payload: []byte(`{"text":"命令完成"}`)},
			{EventID: "e2", Type: "assistant_final", Payload: []byte(`{"text":"决定采用 Redis 作为缓存层"}`)},
		},
	})
	if !decision.Allow {
		t.Fatalf("多事件中一个高价值应允许,得到 decision.Allow=%v, Reason=%s", decision.Allow, decision.Reason)
	}
}

func TestGateAssistantFinalWithDecisionAllowed(t *testing.T) {
	decision := ShouldExtract(ExtractionRequest{
		Events: []ExtractionEvent{
			{EventID: "e1", Type: "assistant_final", Payload: []byte(`{"text":"最终方案是使用 Docker Compose 部署"}`)},
		},
	})
	if !decision.Allow {
		t.Fatalf("assistant_final 含明确决策应允许,得到 decision.Allow=%v, Reason=%s", decision.Allow, decision.Reason)
	}
}

func TestGateAssistantFinalWithFollowUpAllowed(t *testing.T) {
	decision := ShouldExtract(ExtractionRequest{
		Events: []ExtractionEvent{
			{EventID: "e1", Type: "assistant_final", Payload: []byte(`{"text":"需要后续处理数据库迁移"}`)},
		},
	})
	if !decision.Allow {
		t.Fatalf("assistant_final 含待办应允许,得到 decision.Allow=%v, Reason=%s", decision.Allow, decision.Reason)
	}
}

func TestGateAssistantFinalWithRiskAllowed(t *testing.T) {
	decision := ShouldExtract(ExtractionRequest{
		Events: []ExtractionEvent{
			{EventID: "e1", Type: "assistant_final", Payload: []byte(`{"text":"安全红线：禁止在日志中输出 secret"}`)},
		},
	})
	if !decision.Allow {
		t.Fatalf("assistant_final 含风险约束应允许,得到 decision.Allow=%v, Reason=%s", decision.Allow, decision.Reason)
	}
}

func TestGateEmptyEventsRejected(t *testing.T) {
	decision := ShouldExtract(ExtractionRequest{Events: []ExtractionEvent{}})
	if decision.Allow {
		t.Fatalf("空事件应拒绝,得到 decision.Allow=%v, Reason=%s", decision.Allow, decision.Reason)
	}
	if decision.Reason != "empty_events" {
		t.Fatalf("Reason 应为 empty_events,得到 %s", decision.Reason)
	}
}
