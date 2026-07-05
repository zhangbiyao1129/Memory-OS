package eventlog

import "testing"

func TestMemoryRepositoryGetEvent(t *testing.T) {
	repo := NewMemoryRepository()
	event := TurnEvent{
		EventID: "e1", TurnID: "t1", ThreadID: "th1", SessionID: "s1",
		Type: EventAssistantFinal, Actor: Actor{UserID: "u", OrgID: "o", ProjectID: "p", AgentID: "a"},
	}
	repo.Save(SanitizedEvent{Event: event, SafePayload: []byte(`{}`)}, "req-1")

	got, err := repo.GetEvent("e1")
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if got.EventID != "e1" || got.Type != EventAssistantFinal || got.ThreadID != "th1" {
		t.Fatalf("event 字段不正确: %+v", got)
	}
	if got.Actor.OrgID != "o" || got.Actor.ProjectID != "p" {
		t.Fatalf("actor 未恢复: %+v", got.Actor)
	}
}

func TestMemoryRepositoryGetEventMissing(t *testing.T) {
	repo := NewMemoryRepository()
	if _, err := repo.GetEvent("missing"); err != ErrEventNotFound {
		t.Fatalf("期望 ErrEventNotFound,得到 %v", err)
	}
}

func TestServiceGetEventDelegatesToRepo(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, SanitizerOptions{})
	repo.Save(SanitizedEvent{Event: TurnEvent{EventID: "e2", Type: EventTurnCompleted, Actor: Actor{UserID: "u", OrgID: "o", ProjectID: "p", AgentID: "a"}}}, "req-2")

	got, err := svc.GetEvent("e2")
	if err != nil {
		t.Fatalf("service get event: %v", err)
	}
	if got.EventID != "e2" {
		t.Fatalf("event_id 不正确: %s", got.EventID)
	}
}
