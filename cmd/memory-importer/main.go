package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	archivepkg "memory-os/internal/archive"
	"memory-os/internal/config"
	"memory-os/internal/db"
	"memory-os/internal/hotmemory"
	"memory-os/internal/importer"
	"memory-os/internal/jobs"
	"memory-os/internal/llm"
	"memory-os/internal/qdrant"
)

func main() {
	output, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(output)
}

func run(args []string) (string, error) {
	flags := flag.NewFlagSet("memory-importer", flag.ContinueOnError)
	source := flags.String("source", "mem0", "source type")
	batch := flags.String("batch", "", "batch id")
	input := flags.String("input", "", "input path")
	state := flags.String("state", "", "persistent importer state path")
	dryRun := flags.Bool("dry-run", false, "preview import")
	apply := flags.Bool("apply", false, "apply import")
	exportBundle := flags.Bool("export-bundle", false, "export markdown/rag bundle after apply")
	productionSink := flags.Bool("production-sink", false, "apply imported items to production hot memory/archive services")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	if *batch == "" {
		return "", errors.New("batch is required")
	}
	if (*dryRun || *apply) && *input == "" {
		return "", errors.New("input is required for dry-run or apply")
	}
	repository, err := newRepository(*state)
	if err != nil {
		return "", err
	}
	service := importer.NewService(repository)
	if *productionSink && *apply {
		productionService, cleanup, err := newProductionSinkService(context.Background(), repository)
		if err != nil {
			return "", err
		}
		defer cleanup()
		service = productionService
	}
	if *exportBundle && !*apply && !*dryRun {
		bundle, err := service.ExportBundle(*batch)
		if err != nil {
			return "", err
		}
		return bundle.Markdown + "\nmetadata:\n" + bundle.MetadataJSON + "\nsource_refs:\n" + bundle.SourceRefsJSON, nil
	}
	content, err := os.ReadFile(*input)
	if err != nil {
		return "", err
	}
	request := importer.ImportRequest{BatchID: *batch, SourceType: importer.SourceType(*source), Content: content, Scope: importer.DefaultScope()}
	if *dryRun {
		result, err := service.DryRun(request)
		if err != nil {
			return "", err
		}
		return encode(result)
	}
	if *apply {
		result, err := service.Apply(request)
		if err != nil {
			return "", err
		}
		if *exportBundle {
			bundle, err := service.ExportBundle(*batch)
			if err != nil {
				return "", err
			}
			return bundle.Markdown + "\nmetadata:\n" + bundle.MetadataJSON + "\nsource_refs:\n" + bundle.SourceRefsJSON, nil
		}
		return encode(result)
	}
	return "", errors.New("either dry-run or apply is required")
}

func newProductionSinkService(ctx context.Context, repository importer.Repository) (importer.Service, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return importer.Service{}, nil, err
	}
	if cfg.PostgresDSN == "" {
		return importer.Service{}, nil, errors.New("POSTGRES_DSN is required for production sink")
	}
	if cfg.ArchiveDir == "" {
		return importer.Service{}, nil, errors.New("ARCHIVE_DIR is required for production sink")
	}
	pool, err := db.NewPool(ctx, cfg.PostgresDSN)
	if err != nil {
		return importer.Service{}, nil, err
	}
	cleanup := func() { pool.Close() }
	sink, err := newProductionSink(cfg, pool)
	if err != nil {
		cleanup()
		return importer.Service{}, nil, err
	}
	return importer.NewServiceWithProductionSink(repository, sink), cleanup, nil
}

var newImporterProductionHotMemory = func(cfg config.Config, pool *pgxpool.Pool) (importer.HotMemorySink, error) {
	qdrantClient, err := qdrant.NewClient(cfg.QdrantURL)
	if err != nil {
		return nil, err
	}
	if err := qdrantClient.EnsureCollectionSchema(context.Background(), qdrant.CollectionConfig{Name: qdrant.DefaultCollectionName, VectorSize: qdrant.DefaultVectorSize, Distance: qdrant.DefaultDistance}, qdrant.DefaultPayloadIndexConfigs()); err != nil {
		return nil, err
	}
	embedder, err := llm.NewOpenAICompatible(llm.OpenAICompatibleConfig{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, EmbeddingModel: cfg.EmbeddingModel})
	if err != nil {
		return nil, err
	}
	return hotmemory.NewServiceWithVectorIndex(hotmemory.NewPGRepository(pool), hotmemory.NewQdrantIndex(pool, qdrantClient, embedder, qdrant.DefaultCollectionName)), nil
}

var newImporterArchiveIndexQueue = func(pool *pgxpool.Pool) ragIndexQueue {
	return jobs.NewPGArchiveIndexQueue(pool, jobs.PGArchiveIndexQueueOptions{})
}

func newProductionSink(cfg config.Config, pool *pgxpool.Pool) (importer.ProductionSink, error) {
	hot, err := newImporterProductionHotMemory(cfg, pool)
	if err != nil {
		return importer.ProductionSink{}, err
	}
	return importer.ProductionSink{
		HotMemory:         hot,
		Archive:           archivepkg.NewService(archivepkg.NewPGRepository(pool), cfg.ArchiveDir),
		ArchiveIndexQueue: archiveIndexQueueAdapter{queue: newImporterArchiveIndexQueue(pool)},
	}, nil
}

type ragIndexQueue interface {
	Enqueue(ctx context.Context, job jobs.RAGIndexJob) error
}

type archiveIndexQueueAdapter struct {
	queue ragIndexQueue
}

func (a archiveIndexQueueAdapter) EnqueueArchiveIndex(ctx context.Context, job importer.ArchiveIndexJob) error {
	return a.queue.Enqueue(ctx, jobs.RAGIndexJob{
		IdempotencyKey:   job.IdempotencyKey,
		OrgID:            job.OrgID,
		ProjectID:        job.ProjectID,
		UserID:           job.UserID,
		Visibility:       job.Visibility,
		PermissionLabels: append([]string(nil), job.PermissionLabels...),
		Chunks:           job.Chunks,
	})
}

func newRepository(statePath string) (importer.Repository, error) {
	if statePath == "" {
		return importer.NewMemoryRepository(), nil
	}
	return importer.NewFileRepository(statePath)
}

func encode(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
