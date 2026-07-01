package importer

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"memory-os/internal/secret"
)

type Service struct {
	repository *MemoryRepository
}

func NewService(repository *MemoryRepository) Service {
	return Service{repository: repository}
}

func (s Service) Repository() *MemoryRepository {
	return s.repository
}

func (s Service) DryRun(request ImportRequest) (ImportResult, error) {
	items, err := parse(request)
	if err != nil {
		return ImportResult{}, err
	}
	return ImportResult{BatchID: request.BatchID, SourceType: request.SourceType, DryRun: true, ItemCount: len(items), Preview: items}, nil
}

func (s Service) Apply(request ImportRequest) (ImportResult, error) {
	items, err := parse(request)
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{BatchID: request.BatchID, SourceType: request.SourceType, DryRun: false, ItemCount: len(items)}
	for _, item := range items {
		if s.repository.Upsert(item) {
			result.CreatedCount++
		} else {
			result.DedupedCount++
		}
	}
	return result, nil
}

func (s Service) ExportBundle(batchID string) (Bundle, error) {
	items := s.repository.ItemsByBatch(batchID)
	if len(items) == 0 {
		return Bundle{}, errors.New("batch has no imported items")
	}
	markdown := strings.Builder{}
	markdown.WriteString("# Memory OS Export Bundle\n\n")
	sourceRefs := []map[string]string{}
	for _, item := range items {
		markdown.WriteString("## " + string(item.Kind) + " " + item.ExternalID + "\n\n")
		markdown.WriteString(item.Text + "\n\n")
		sourceRefs = append(sourceRefs, item.SourceRef)
	}
	metadata, _ := json.Marshal(map[string]any{"batch_id": batchID, "item_count": len(items)})
	refs, _ := json.Marshal(map[string]any{"source_refs": sourceRefs})
	return Bundle{Markdown: markdown.String(), MetadataJSON: string(metadata), SourceRefsJSON: string(refs)}, nil
}

func parse(request ImportRequest) ([]ImportItem, error) {
	if request.BatchID == "" {
		return nil, errors.New("batch id is required")
	}
	switch request.SourceType {
	case SourceMem0:
		return parseMem0(request)
	case SourceFastGPT:
		return parseFastGPT(request)
	case SourceOpenMemory, SourceZep, SourceKhoj:
		return parseSkeleton(request)
	default:
		return nil, errors.New("unsupported source type")
	}
}

type mem0Record struct {
	ID     string         `json:"id"`
	Memory string         `json:"memory"`
	UserID string         `json:"user_id"`
	Meta   map[string]any `json:"metadata"`
}

func parseMem0(request ImportRequest) ([]ImportItem, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(request.Content)))
	items := []ImportItem{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record mem0Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, err
		}
		text := sanitize(record.Memory)
		items = append(items, ImportItem{BatchID: request.BatchID, SourceType: request.SourceType, ExternalID: record.ID, Kind: KindHotMemory, Text: text, SourceRef: map[string]string{"source_type": string(SourceMem0), "external_id": record.ID}})
	}
	return items, scanner.Err()
}

type fastGPTPayload struct {
	Documents []struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Chunks []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		} `json:"chunks"`
	} `json:"documents"`
}

func parseFastGPT(request ImportRequest) ([]ImportItem, error) {
	var payload fastGPTPayload
	if err := json.Unmarshal(request.Content, &payload); err != nil {
		return nil, err
	}
	items := []ImportItem{}
	for _, doc := range payload.Documents {
		builder := strings.Builder{}
		builder.WriteString("# " + doc.Title + "\n\n")
		for _, chunk := range doc.Chunks {
			builder.WriteString("Source ref: `" + chunk.ID + "`\n\n")
			builder.WriteString(sanitize(chunk.Content) + "\n\n")
		}
		items = append(items, ImportItem{BatchID: request.BatchID, SourceType: request.SourceType, ExternalID: doc.ID, Kind: KindArchive, Text: builder.String(), SourceRef: map[string]string{"source_type": string(SourceFastGPT), "external_id": doc.ID}})
	}
	return items, nil
}

func parseSkeleton(request ImportRequest) ([]ImportItem, error) {
	var payload struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(request.Content, &payload); err != nil {
		return nil, err
	}
	if payload.Items == nil {
		return nil, fmt.Errorf("%s import requires items array", request.SourceType)
	}
	return []ImportItem{}, nil
}

func sanitize(text string) string {
	result := secret.Sanitize(text, func(index int, match string) string { return fmt.Sprintf("secret_ref_import_%d", index) })
	return result.Text
}
