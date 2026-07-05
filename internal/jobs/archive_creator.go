package jobs

import (
	"context"
	"errors"
	"os"
	"time"

	"memory-os/internal/archive"
	"memory-os/internal/candidatememory"
)

// productionArchiveCreator 适配 candidatememory.ArchiveCreator 接口,
// 调 archive.Service.Create(Markdown=...) → 读文件 → ChunkMarkdown → RAGIndexQueue.Enqueue。
type productionArchiveCreator struct {
	archiveService archive.Service
	indexEnqueuer  ragIndexEnqueuer
}

// NewProductionArchiveCreator 创建生产 ArchiveCreator,供 TopicComposer 沉淀用。
func NewProductionArchiveCreator(archiveService archive.Service, indexEnqueuer ragIndexEnqueuer) candidatememory.ArchiveCreator {
	return productionArchiveCreator{archiveService: archiveService, indexEnqueuer: indexEnqueuer}
}

func (c productionArchiveCreator) Create(ctx context.Context, req candidatememory.ArchiveCreateRequest) (candidatememory.ArchiveCreateResult, error) {
	if !c.archiveService.Configured() {
		return candidatememory.ArchiveCreateResult{}, errors.New("archive service not configured")
	}
	if req.ArchiveID == "" || req.Markdown == "" || req.OrgID == "" || req.ProjectID == "" || req.UserID == "" {
		return candidatememory.ArchiveCreateResult{}, errors.New("archive creator: archive_id, markdown, org_id, project_id, user_id are required")
	}
	result, err := c.archiveService.Create(archive.CreateRequest{
		RequestID: "topic_compose_" + req.ArchiveID,
		ArchiveID: req.ArchiveID,
		Title:     req.Title,
		UserID:    req.UserID,
		OrgID:     req.OrgID,
		ProjectID: req.ProjectID,
		CreatedAt: time.Now().UTC(),
		Markdown:  req.Markdown,
	})
	if err != nil {
		return candidatememory.ArchiveCreateResult{}, err
	}
	if result.Deduped {
		return candidatememory.ArchiveCreateResult{ArchiveID: req.ArchiveID}, nil
	}
	// 读文件 → chunk → enqueue RAG index(同 ArchiveWorker.enqueueRAGIndex 范式)。
	if err := c.enqueueRAGIndex(ctx, result.Metadata); err != nil {
		return candidatememory.ArchiveCreateResult{}, err
	}
	return candidatememory.ArchiveCreateResult{ArchiveID: req.ArchiveID}, nil
}

func (c productionArchiveCreator) enqueueRAGIndex(ctx context.Context, metadata archive.Metadata) error {
	if c.indexEnqueuer == nil {
		return nil
	}
	content, err := os.ReadFile(metadata.FilePath)
	if err != nil {
		return err
	}
	chunks, err := archive.ChunkMarkdown(archive.ChunkRequest{ArchiveID: metadata.ArchiveID, IndexGeneration: metadata.IndexGeneration, Content: string(content)})
	if err != nil {
		return err
	}
	return c.indexEnqueuer.Enqueue(ctx, RAGIndexJob{
		IdempotencyKey:   ragIndexIdempotencyKey(metadata.ArchiveID, metadata.IndexGeneration),
		OrgID:            metadata.OrgID,
		ProjectID:        metadata.ProjectID,
		UserID:           metadata.UserID,
		Visibility:       "project",
		PermissionLabels: []string{"project:" + metadata.ProjectID + ":read"},
		Chunks:           chunks,
	})
}
