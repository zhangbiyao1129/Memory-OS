package audit

import "sync"

type Repository interface {
	Save(log Log) error
	List(filter ListFilter) ([]Log, error)
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

func (r *MemoryRepository) List(filter ListFilter) ([]Log, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	items := []Log{}
	for i := len(r.logs) - 1; i >= 0; i-- {
		log := r.logs[i]
		if filter.OrgID != "" && log.OrgID != filter.OrgID {
			continue
		}
		if filter.ProjectID != "" && log.ProjectID != filter.ProjectID {
			continue
		}
		if filter.ActorUserID != "" && log.ActorUserID != filter.ActorUserID {
			continue
		}
		if filter.ResourceType != "" && log.ResourceType != filter.ResourceType {
			continue
		}
		if filter.ResourceID != "" && log.ResourceID != filter.ResourceID {
			continue
		}
		items = append(items, log)
		if len(items) >= filter.Limit {
			break
		}
	}
	return items, nil
}

func (r *MemoryRepository) All() []Log {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Log(nil), r.logs...)
}
