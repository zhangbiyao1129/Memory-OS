package eventlog

import (
	"errors"
	"sync"
)

type SaveResult struct {
	EventID string
	Deduped bool
}

var ErrEventNotFound = errors.New("event not found")

type Repository interface {
	Save(event SanitizedEvent, requestID string) (SaveResult, error)
	GetEvent(eventID string) (TurnEvent, error)
	Count() int
}

type MemoryRepository struct {
	mu         sync.Mutex
	events     map[string]TurnEvent
	payloads   map[string][]byte
	requestIDs map[string]string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{events: map[string]TurnEvent{}, payloads: map[string][]byte{}, requestIDs: map[string]string{}}
}

func (r *MemoryRepository) Save(sanitized SanitizedEvent, requestID string) (SaveResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	event := sanitized.Event
	if event.EventID == "" || requestID == "" {
		return SaveResult{}, errors.New("event id and request id are required")
	}
	if existingEventID, exists := r.requestIDs[requestID]; exists {
		return SaveResult{EventID: existingEventID, Deduped: true}, nil
	}
	if _, exists := r.events[event.EventID]; exists {
		r.requestIDs[requestID] = event.EventID
		return SaveResult{EventID: event.EventID, Deduped: true}, nil
	}
	r.events[event.EventID] = event
	r.payloads[event.EventID] = append([]byte(nil), sanitized.SafePayload...)
	r.requestIDs[requestID] = event.EventID
	return SaveResult{EventID: event.EventID, Deduped: false}, nil
}

func (r *MemoryRepository) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

// GetEvent 按 event_id 读取已保存(已脱敏)的事件,供候选提炼 worker 使用。
func (r *MemoryRepository) GetEvent(eventID string) (TurnEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, ok := r.events[eventID]
	if !ok {
		return TurnEvent{}, ErrEventNotFound
	}
	return event, nil
}
