package hotmemory

import (
	"errors"
	"sync"
	"time"
)

type Repository interface {
	Upsert(memory Memory) (Memory, error)
	Get(memoryID string) (Memory, error)
	Search(filter map[string][]string) []Memory
	Update(memory Memory) (Memory, error)
}

type MemoryRepository struct {
	mu       sync.Mutex
	byID     map[string]Memory
	byDedupe map[string]string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{byID: map[string]Memory{}, byDedupe: map[string]string{}}
}

func (r *MemoryRepository) Upsert(memory Memory) (Memory, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if memory.MemoryID == "" {
		return Memory{}, errors.New("memory id is required")
	}
	if memory.FactHash == "" {
		return Memory{}, errors.New("fact hash is required")
	}
	key := dedupeKey(memory)
	if existingID, ok := r.byDedupe[key]; ok {
		existing := r.byID[existingID]
		existing.Sources = mergeSources(existing.Sources, memory.Sources)
		if memory.Confidence > existing.Confidence {
			existing.Confidence = memory.Confidence
		}
		existing.HotScore = score(existing)
		existing.UpdatedAt = time.Now().UTC()
		r.byID[existingID] = existing
		return cloneMemory(existing), nil
	}
	now := time.Now().UTC()
	memory.CreatedAt = now
	memory.UpdatedAt = now
	memory.HotScore = score(memory)
	r.byID[memory.MemoryID] = memory
	r.byDedupe[key] = memory.MemoryID
	return cloneMemory(memory), nil
}

func (r *MemoryRepository) Get(memoryID string) (Memory, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	memory, ok := r.byID[memoryID]
	if !ok {
		return Memory{}, errors.New("memory not found")
	}
	return cloneMemory(memory), nil
}

func (r *MemoryRepository) Search(filter map[string][]string) []Memory {
	r.mu.Lock()
	defer r.mu.Unlock()
	results := []Memory{}
	for _, memory := range r.byID {
		if memory.DeletedAt != nil || memory.Status == StatusDeleted {
			continue
		}
		if matchesFilter(memory, filter) {
			results = append(results, cloneMemory(memory))
		}
	}
	return results
}

func (r *MemoryRepository) Update(memory Memory) (Memory, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.byID[memory.MemoryID]
	if !ok {
		return Memory{}, errors.New("memory not found")
	}
	if memory.FactHash != existing.FactHash {
		key := dedupeKey(memory)
		if existingID, ok := r.byDedupe[key]; ok && existingID != memory.MemoryID {
			return Memory{}, errors.New("memory fact already exists in scope")
		}
		delete(r.byDedupe, dedupeKey(existing))
		r.byDedupe[key] = memory.MemoryID
	}
	memory.UpdatedAt = time.Now().UTC()
	memory.HotScore = score(memory)
	r.byID[memory.MemoryID] = memory
	return cloneMemory(memory), nil
}

func mergeSources(existing []Source, additions []Source) []Source {
	out := append([]Source(nil), existing...)
	for _, source := range additions {
		found := false
		for _, item := range out {
			if item.SourceType == source.SourceType && item.SourceRef == source.SourceRef {
				found = true
				break
			}
		}
		if !found {
			out = append(out, source)
		}
	}
	return out
}

func cloneMemory(memory Memory) Memory {
	memory.PermissionLabels = append([]string(nil), memory.PermissionLabels...)
	memory.Sources = append([]Source(nil), memory.Sources...)
	return memory
}
