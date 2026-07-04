package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"memory-os/internal/eventlog"
	"memory-os/internal/secret"
)

type Service struct {
	repo Repository
	root string
}

type CreateRequest struct {
	RequestID  string
	ArchiveID  string
	Title      string
	UserID     string
	OrgID      string
	ProjectID  string
	CreatedAt  time.Time
	RenderMode string
	Events     []eventlog.TurnEvent
}

type EditRequest struct {
	RequestID   string
	ArchiveID   string
	ActorUserID string
	Reason      string
	Content     string
}

type DeleteRequest struct {
	RequestID   string
	ArchiveID   string
	ActorUserID string
	Reason      string
}

type ReindexRequest struct {
	RequestID string
	ArchiveID string
	Reason    string
}

type Result struct {
	Metadata Metadata
	Deduped  bool
}

type ReindexResult struct {
	Metadata Metadata
	Chunks   []Chunk
	Deduped  bool
}

type DetailResult struct {
	Metadata Metadata
	Content  string
}

func NewService(repo Repository, root string) Service {
	return Service{repo: repo, root: root}
}

func (s Service) Configured() bool {
	return s.repo != nil && s.root != ""
}

func (s Service) Create(request CreateRequest) (Result, error) {
	if request.RequestID == "" || request.ArchiveID == "" || request.UserID == "" || request.OrgID == "" || request.ProjectID == "" {
		return Result{}, errors.New("archive create ids are required")
	}
	content, err := renderArchiveMarkdown(request)
	if err != nil {
		return Result{}, err
	}
	path, err := BuildPath(s.root, PathContext{OrgID: request.OrgID, ProjectID: request.ProjectID, UserID: request.UserID, ArchiveID: request.ArchiveID, CreatedAt: request.CreatedAt})
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Result{}, err
	}
	now := time.Now().UTC()
	hash := contentHash(content)
	metadata := Metadata{ArchiveID: request.ArchiveID, UserID: request.UserID, OrgID: request.OrgID, ProjectID: request.ProjectID, Title: request.Title, FilePath: path, Status: "active", IndexGeneration: 1, CurrentVersion: 1, ContentHash: hash, CreatedAt: now, UpdatedAt: now}
	version := Version{ArchiveID: request.ArchiveID, Version: 1, FilePath: path, ContentHash: hash, EditorUserID: request.UserID, Reason: "initial archive", CreatedAt: now}
	eventIDs := make([]string, 0, len(request.Events))
	for _, event := range request.Events {
		eventIDs = append(eventIDs, event.EventID)
	}
	saved, deduped, err := s.repo.SaveCreate(metadata, version, eventIDs, request.RequestID)
	if err != nil {
		return Result{}, err
	}
	return Result{Metadata: saved, Deduped: deduped}, nil
}

func renderArchiveMarkdown(request CreateRequest) (string, error) {
	renderRequest := RenderRequest{ArchiveID: request.ArchiveID, Title: request.Title, Events: request.Events}
	if request.RenderMode == "knowledge" {
		return RenderKnowledgeMarkdown(renderRequest)
	}
	return RenderMarkdown(renderRequest)
}

func (s Service) Edit(request EditRequest) (Result, error) {
	if request.RequestID == "" || request.ArchiveID == "" || request.ActorUserID == "" || request.Content == "" {
		return Result{}, errors.New("archive edit fields are required")
	}
	metadata, err := s.repo.Get(request.ArchiveID)
	if err != nil {
		return Result{}, err
	}
	oldVersion := metadata.CurrentVersion
	oldHash := metadata.ContentHash
	newVersion := oldVersion + 1
	content := sanitizeManualContent(request.Content, metadata.ArchiveID, newVersion)
	metadata.CurrentVersion = newVersion
	metadata.IndexGeneration++
	metadata.ContentHash = contentHash(content)
	metadata.UpdatedAt = time.Now().UTC()
	if err := os.WriteFile(metadata.FilePath, []byte(content), 0o644); err != nil {
		return Result{}, err
	}
	version := Version{ArchiveID: metadata.ArchiveID, Version: newVersion, FilePath: metadata.FilePath, ContentHash: metadata.ContentHash, EditorUserID: request.ActorUserID, Reason: request.Reason, CreatedAt: metadata.UpdatedAt}
	audit := EditAuditLog{ArchiveID: metadata.ArchiveID, ActorUserID: request.ActorUserID, OldVersion: oldVersion, NewVersion: newVersion, OldContentHash: oldHash, NewContentHash: metadata.ContentHash, RequestID: request.RequestID, Reason: request.Reason, CreatedAt: metadata.UpdatedAt}
	saved, deduped, err := s.repo.SaveEdit(metadata, version, audit, request.RequestID)
	if err != nil {
		return Result{}, err
	}
	return Result{Metadata: saved, Deduped: deduped}, nil
}

func (s Service) List(filter ListFilter) ([]Metadata, error) {
	if filter.UserID == "" || filter.OrgID == "" || filter.ProjectID == "" {
		return nil, errors.New("archive list scope is required")
	}
	if filter.Status == "" {
		filter.Status = "active"
	}
	return s.repo.List(filter)
}

func (s Service) Delete(request DeleteRequest) (Result, error) {
	if request.RequestID == "" || request.ArchiveID == "" || request.ActorUserID == "" {
		return Result{}, errors.New("archive delete fields are required")
	}
	metadata, err := s.repo.Get(request.ArchiveID)
	if err != nil {
		return Result{}, err
	}
	now := time.Now().UTC()
	oldStatus := metadata.Status
	metadata.Status = "deleted"
	metadata.UpdatedAt = now
	audit := EditAuditLog{ArchiveID: metadata.ArchiveID, ActorUserID: request.ActorUserID, OldVersion: metadata.CurrentVersion, NewVersion: metadata.CurrentVersion, OldContentHash: metadata.ContentHash, NewContentHash: metadata.ContentHash, RequestID: request.RequestID, Reason: "delete: " + request.Reason, CreatedAt: now}
	if oldStatus == "deleted" {
		audit.Reason = "delete duplicate: " + request.Reason
	}
	saved, deduped, err := s.repo.SoftDelete(metadata, audit, request.RequestID)
	if err != nil {
		return Result{}, err
	}
	return Result{Metadata: saved, Deduped: deduped}, nil
}

func (s Service) Metadata(archiveID string) (Metadata, error) {
	if archiveID == "" {
		return Metadata{}, errors.New("archive id is required")
	}
	return s.repo.Get(archiveID)
}

func (s Service) Detail(archiveID string) (DetailResult, error) {
	if archiveID == "" {
		return DetailResult{}, errors.New("archive id is required")
	}
	metadata, err := s.Metadata(archiveID)
	if err != nil {
		return DetailResult{}, err
	}
	content, err := os.ReadFile(metadata.FilePath)
	if err != nil {
		return DetailResult{}, err
	}
	return DetailResult{Metadata: metadata, Content: string(content)}, nil
}

func (s Service) Reindex(request ReindexRequest) (ReindexResult, error) {
	if request.RequestID == "" || request.ArchiveID == "" {
		return ReindexResult{}, errors.New("archive reindex fields are required")
	}
	metadata, err := s.repo.Get(request.ArchiveID)
	if err != nil {
		return ReindexResult{}, err
	}
	if metadata.Status == "deleted" {
		return ReindexResult{}, errors.New("deleted archive cannot be reindexed")
	}
	content, err := os.ReadFile(metadata.FilePath)
	if err != nil {
		return ReindexResult{}, err
	}
	metadata.IndexGeneration++
	metadata.UpdatedAt = time.Now().UTC()
	saved, deduped, err := s.repo.MarkReindex(metadata, request.RequestID, request.Reason)
	if err != nil {
		return ReindexResult{}, err
	}
	chunks, err := ChunkMarkdown(ChunkRequest{ArchiveID: saved.ArchiveID, IndexGeneration: saved.IndexGeneration, Content: string(content)})
	if err != nil {
		return ReindexResult{}, err
	}
	return ReindexResult{Metadata: saved, Chunks: chunks, Deduped: deduped}, nil
}

func (s Service) Versions(archiveID string) ([]Version, error) {
	if archiveID == "" {
		return nil, errors.New("archive id is required")
	}
	return s.repo.Versions(archiveID)
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func sanitizeManualContent(content, archiveID string, version int) string {
	sanitized := secret.Sanitize(content, func(index int, match string) string {
		return fmt.Sprintf("secret_ref_archive_%s_v%d_%d", archiveID, version, index)
	})
	return sanitized.Text
}
