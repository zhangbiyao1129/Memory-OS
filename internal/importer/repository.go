package importer

import "sync"

type MemoryRepository struct {
	mu    sync.Mutex
	items map[string]ImportItem
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{items: map[string]ImportItem{}}
}

func (r *MemoryRepository) Upsert(item ImportItem) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := string(item.SourceType) + ":" + item.ExternalID
	if _, ok := r.items[key]; ok {
		return false
	}
	r.items[key] = item
	return true
}

func (r *MemoryRepository) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.items)
}

func (r *MemoryRepository) Items() []ImportItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []ImportItem{}
	for _, item := range r.items {
		items = append(items, item)
	}
	return items
}

func (r *MemoryRepository) ItemsByBatch(batchID string) []ImportItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []ImportItem{}
	for _, item := range r.items {
		if item.BatchID == batchID {
			items = append(items, item)
		}
	}
	return items
}
