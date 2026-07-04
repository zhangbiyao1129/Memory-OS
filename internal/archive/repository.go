package archive

import (
	"errors"
	"sync"
)

type Repository interface {
	SaveCreate(metadata Metadata, version Version, eventIDs []string, requestID string) (Metadata, bool, error)
	Get(archiveID string) (Metadata, error)
	List(filter ListFilter) ([]Metadata, error)
	SaveEdit(metadata Metadata, version Version, audit EditAuditLog, requestID string) (Metadata, bool, error)
	SoftDelete(metadata Metadata, audit EditAuditLog, requestID string) (Metadata, bool, error)
	MarkReindex(metadata Metadata, requestID string, reason string) (Metadata, bool, error)
	Versions(archiveID string) ([]Version, error)
}

type MemoryRepository struct {
	mu         sync.Mutex
	archives   map[string]Metadata
	versions   map[string][]Version
	auditLogs  map[string][]EditAuditLog
	requestIDs map[string]string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{archives: map[string]Metadata{}, versions: map[string][]Version{}, auditLogs: map[string][]EditAuditLog{}, requestIDs: map[string]string{}}
}

func (r *MemoryRepository) SaveCreate(metadata Metadata, version Version, eventIDs []string, requestID string) (Metadata, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existingID, ok := r.requestIDs[requestID]; ok {
		return r.archives[existingID], true, nil
	}
	if _, exists := r.archives[metadata.ArchiveID]; exists {
		return Metadata{}, false, errors.New("archive already exists")
	}
	r.archives[metadata.ArchiveID] = metadata
	r.versions[metadata.ArchiveID] = []Version{version}
	r.requestIDs[requestID] = metadata.ArchiveID
	return metadata, false, nil
}

func (r *MemoryRepository) Get(archiveID string) (Metadata, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	metadata, ok := r.archives[archiveID]
	if !ok {
		return Metadata{}, errors.New("archive not found")
	}
	return metadata, nil
}

func (r *MemoryRepository) List(filter ListFilter) ([]Metadata, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	results := []Metadata{}
	for _, metadata := range r.archives {
		if filter.UserID != "" && metadata.UserID != filter.UserID {
			continue
		}
		if filter.OrgID != "" && metadata.OrgID != filter.OrgID {
			continue
		}
		if filter.ProjectID != "" && metadata.ProjectID != filter.ProjectID {
			continue
		}
		if filter.Status != "" && metadata.Status != filter.Status {
			continue
		}
		results = append(results, metadata)
	}
	return results, nil
}

func (r *MemoryRepository) SaveEdit(metadata Metadata, version Version, audit EditAuditLog, requestID string) (Metadata, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existingID, ok := r.requestIDs[requestID]; ok {
		return r.archives[existingID], true, nil
	}
	if _, ok := r.archives[metadata.ArchiveID]; !ok {
		return Metadata{}, false, errors.New("archive not found")
	}
	r.archives[metadata.ArchiveID] = metadata
	r.versions[metadata.ArchiveID] = append(r.versions[metadata.ArchiveID], version)
	r.auditLogs[metadata.ArchiveID] = append(r.auditLogs[metadata.ArchiveID], audit)
	r.requestIDs[requestID] = metadata.ArchiveID
	return metadata, false, nil
}

func (r *MemoryRepository) SoftDelete(metadata Metadata, audit EditAuditLog, requestID string) (Metadata, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existingID, ok := r.requestIDs[requestID]; ok {
		return r.archives[existingID], true, nil
	}
	if _, ok := r.archives[metadata.ArchiveID]; !ok {
		return Metadata{}, false, errors.New("archive not found")
	}
	r.archives[metadata.ArchiveID] = metadata
	r.auditLogs[metadata.ArchiveID] = append(r.auditLogs[metadata.ArchiveID], audit)
	r.requestIDs[requestID] = metadata.ArchiveID
	return metadata, false, nil
}

func (r *MemoryRepository) MarkReindex(metadata Metadata, requestID string, reason string) (Metadata, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existingID, ok := r.requestIDs[requestID]; ok {
		return r.archives[existingID], true, nil
	}
	if _, ok := r.archives[metadata.ArchiveID]; !ok {
		return Metadata{}, false, errors.New("archive not found")
	}
	r.archives[metadata.ArchiveID] = metadata
	r.requestIDs[requestID] = metadata.ArchiveID
	return metadata, false, nil
}

func (r *MemoryRepository) Versions(archiveID string) ([]Version, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Version(nil), r.versions[archiveID]...), nil
}

func (r *MemoryRepository) AuditLogs(archiveID string) []EditAuditLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]EditAuditLog(nil), r.auditLogs[archiveID]...)
}
