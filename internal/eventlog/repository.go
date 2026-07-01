package eventlog

import (
	"errors"
	"sync"
)

type SaveResult struct {
	EventID string
	Deduped bool
}

type Repository interface {
	Save(event TurnEvent, safePayload []byte, requestID string) (SaveResult, error)
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

func (r *MemoryRepository) Save(event TurnEvent, safePayload []byte, requestID string) (SaveResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.payloads[event.EventID] = append([]byte(nil), safePayload...)
	r.requestIDs[requestID] = event.EventID
	return SaveResult{EventID: event.EventID, Deduped: false}, nil
}

func (r *MemoryRepository) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}
