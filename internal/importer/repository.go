package importer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type Repository interface {
	Upsert(item ImportItem) (bool, error)
	Count() int
	Items() []ImportItem
	ItemsByBatch(batchID string) []ImportItem
}

type MemoryRepository struct {
	mu    sync.Mutex
	items map[string]ImportItem
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{items: map[string]ImportItem{}}
}

func (r *MemoryRepository) Upsert(item ImportItem) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := importItemKey(item)
	if _, ok := r.items[key]; ok {
		return false, nil
	}
	r.items[key] = item
	return true, nil
}

func (r *MemoryRepository) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.items)
}

func (r *MemoryRepository) Items() []ImportItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	return sortedImportItems(r.items)
}

func (r *MemoryRepository) ItemsByBatch(batchID string) []ImportItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	filtered := map[string]ImportItem{}
	for _, item := range r.items {
		if item.BatchID == batchID {
			filtered[importItemKey(item)] = item
		}
	}
	return sortedImportItems(filtered)
}

type FileRepository struct {
	mu    sync.Mutex
	path  string
	items map[string]ImportItem
}

func NewFileRepository(path string) (*FileRepository, error) {
	if path == "" {
		return nil, errors.New("state path is required")
	}
	repository := &FileRepository{path: path, items: map[string]ImportItem{}}
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return repository, nil
		}
		return nil, err
	}
	if len(content) == 0 {
		return repository, nil
	}
	var items []ImportItem
	if err := json.Unmarshal(content, &items); err != nil {
		return nil, err
	}
	for _, item := range items {
		repository.items[importItemKey(item)] = item
	}
	return repository, nil
}

func (r *FileRepository) Upsert(item ImportItem) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := importItemKey(item)
	if _, ok := r.items[key]; ok {
		return false, nil
	}
	r.items[key] = item
	if err := r.persistLocked(); err != nil {
		delete(r.items, key)
		return false, err
	}
	return true, nil
}

func (r *FileRepository) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.items)
}

func (r *FileRepository) Items() []ImportItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	return sortedImportItems(r.items)
}

func (r *FileRepository) ItemsByBatch(batchID string) []ImportItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	filtered := map[string]ImportItem{}
	for _, item := range r.items {
		if item.BatchID == batchID {
			filtered[importItemKey(item)] = item
		}
	}
	return sortedImportItems(filtered)
}

func (r *FileRepository) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(sortedImportItems(r.items), "", "  ")
	if err != nil {
		return err
	}
	tmpPath := r.path + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, r.path)
}

func importItemKey(item ImportItem) string {
	return string(item.SourceType) + ":" + item.ExternalID
}

func sortedImportItems(items map[string]ImportItem) []ImportItem {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]ImportItem, 0, len(keys))
	for _, key := range keys {
		result = append(result, items[key])
	}
	return result
}
