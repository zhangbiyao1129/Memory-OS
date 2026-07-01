package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"

	"memory-os/internal/eventlog"
)

type Service struct {
	repo Repository
	root string
}

type CreateRequest struct {
	RequestID string
	ArchiveID string
	Title     string
	UserID    string
	OrgID     string
	ProjectID string
	CreatedAt time.Time
	Events    []eventlog.TurnEvent
}

type EditRequest struct {
	RequestID   string
	ArchiveID   string
	ActorUserID string
	Reason      string
	Content     string
}

type Result struct {
	Metadata Metadata
	Deduped  bool
}

func NewService(repo Repository, root string) Service {
	return Service{repo: repo, root: root}
}

func (s Service) Create(request CreateRequest) (Result, error) {
	if request.RequestID == "" || request.ArchiveID == "" || request.UserID == "" || request.OrgID == "" || request.ProjectID == "" {
		return Result{}, errors.New("archive create ids are required")
	}
	content, err := RenderMarkdown(RenderRequest{ArchiveID: request.ArchiveID, Title: request.Title, Events: request.Events})
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
	metadata.CurrentVersion = newVersion
	metadata.IndexGeneration++
	metadata.ContentHash = contentHash(request.Content)
	metadata.UpdatedAt = time.Now().UTC()
	if err := os.WriteFile(metadata.FilePath, []byte(request.Content), 0o644); err != nil {
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

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
