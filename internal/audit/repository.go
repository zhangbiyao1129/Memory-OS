package audit

import "sync"

type Repository interface {
	Save(log Log) error
}

type MemoryRepository struct {
	mu   sync.Mutex
	logs []Log
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{}
}

func (r *MemoryRepository) Save(log Log) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, log)
	return nil
}

func (r *MemoryRepository) All() []Log {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Log(nil), r.logs...)
}
