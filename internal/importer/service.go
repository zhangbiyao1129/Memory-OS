package importer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"memory-os/internal/archive"
	"memory-os/internal/eventlog"
	"memory-os/internal/hotmemory"
	"memory-os/internal/secret"
)

type Service struct {
	repository     Repository
	productionSink *ProductionSink
}

func NewService(repository Repository) Service {
	return Service{repository: repository}
}

func NewServiceWithProductionSink(repository Repository, sink ProductionSink) Service {
	return Service{repository: repository, productionSink: &sink}
}

func (s Service) Repository() Repository {
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
		created, err := s.repository.Upsert(item)
		if err != nil {
			return ImportResult{}, err
		}
		if created {
			result.CreatedCount++
		} else {
			result.DedupedCount++
		}
		if s.productionSink != nil {
			if err := s.productionSink.Import(item, request.Scope); err != nil {
				return ImportResult{}, err
			}
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
	case SourceBundle:
		return parseBundle(request)
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

func parseBundle(request ImportRequest) ([]ImportItem, error) {
	content := string(request.Content)
	if !strings.Contains(content, "# Memory OS Export Bundle") {
		return nil, errors.New("bundle header is required")
	}
	body := bundleMarkdownBody(content)
	sourceRefs := bundleSourceRefs(content)
	items := []ImportItem{}
	var currentKind ItemKind
	var currentExternalID string
	var currentText strings.Builder

	flush := func() error {
		if currentExternalID == "" {
			return nil
		}
		text := strings.TrimSpace(currentText.String())
		if text == "" {
			return fmt.Errorf("bundle item %s has empty text", currentExternalID)
		}
		items = append(items, ImportItem{
			BatchID:    request.BatchID,
			SourceType: SourceBundle,
			ExternalID: currentExternalID,
			Kind:       currentKind,
			Text:       sanitize(text),
			SourceRef:  sourceRefForBundleItem(sourceRefs, currentExternalID),
		})
		currentText.Reset()
		return nil
	}

	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "## ") {
			if currentExternalID != "" {
				currentText.WriteString(line)
				currentText.WriteString("\n")
			}
			continue
		}
		if err := flush(); err != nil {
			return nil, err
		}
		kind, externalID, err := parseBundleHeading(line)
		if err != nil {
			return nil, err
		}
		currentKind = kind
		currentExternalID = externalID
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("bundle has no importable items")
	}
	return items, nil
}

func bundleMarkdownBody(content string) string {
	body := content
	if index := strings.Index(body, "\nmetadata:\n"); index >= 0 {
		body = body[:index]
	}
	if index := strings.Index(body, "\nsource_refs:\n"); index >= 0 {
		body = body[:index]
	}
	return body
}

func bundleSourceRefs(content string) map[string]map[string]string {
	const marker = "\nsource_refs:\n"
	index := strings.LastIndex(content, marker)
	if index < 0 {
		return nil
	}
	raw := strings.TrimSpace(content[index+len(marker):])
	if raw == "" {
		return nil
	}
	var payload struct {
		SourceRefs []map[string]string `json:"source_refs"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	refs := map[string]map[string]string{}
	for _, ref := range payload.SourceRefs {
		externalID := ref["external_id"]
		if externalID == "" {
			continue
		}
		refs[externalID] = ref
	}
	return refs
}

func parseBundleHeading(line string) (ItemKind, string, error) {
	parts := strings.Fields(strings.TrimPrefix(line, "## "))
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid bundle heading: %s", line)
	}
	kind := ItemKind(parts[0])
	switch kind {
	case KindHotMemory, KindArchive:
	default:
		return "", "", fmt.Errorf("unsupported bundle item kind: %s", kind)
	}
	return kind, parts[1], nil
}

func sourceRefForBundleItem(refs map[string]map[string]string, externalID string) map[string]string {
	if ref, ok := refs[externalID]; ok {
		copied := map[string]string{}
		for key, value := range ref {
			copied[key] = value
		}
		return copied
	}
	return map[string]string{"source_type": string(SourceBundle), "external_id": externalID}
}

func sanitize(text string) string {
	result := secret.Sanitize(text, func(index int, match string) string { return fmt.Sprintf("secret_ref_import_%d", index) })
	return result.Text
}

type HotMemorySink interface {
	Upsert(request hotmemory.UpsertRequest) (hotmemory.Memory, error)
}

type ArchiveSink interface {
	Create(request archive.CreateRequest) (archive.Result, error)
}

type ArchiveIndexQueue interface {
	EnqueueArchiveIndex(ctx context.Context, job ArchiveIndexJob) error
}

type ArchiveIndexJob struct {
	IdempotencyKey   string
	ArchiveID        string
	IndexGeneration  int
	OrgID            string
	ProjectID        string
	UserID           string
	Visibility       string
	PermissionLabels []string
	Chunks           []archive.Chunk
}

type ProductionSink struct {
	HotMemory         HotMemorySink
	Archive           ArchiveSink
	ArchiveIndexQueue ArchiveIndexQueue
	Now               func() time.Time
}

func (s ProductionSink) Import(item ImportItem, scope Scope) error {
	switch item.Kind {
	case KindHotMemory:
		return s.importHotMemory(item, scope)
	case KindArchive:
		return s.importArchive(item, scope)
	default:
		return fmt.Errorf("unsupported import item kind: %s", item.Kind)
	}
}

func (s ProductionSink) importHotMemory(item ImportItem, scope Scope) error {
	if s.HotMemory == nil {
		return errors.New("hot memory production sink is not configured")
	}
	_, err := s.HotMemory.Upsert(hotmemory.UpsertRequest{
		OrgID:            scope.OrgID,
		ProjectID:        scope.ProjectID,
		UserID:           scope.UserID,
		AgentID:          scope.AgentID,
		Scope:            hotMemoryScope(scope),
		Visibility:       scope.Visibility,
		PermissionLabels: append([]string(nil), scope.PermissionLabels...),
		Fact:             item.Text,
		SourceType:       hotmemory.SourceArchive,
		SourceRef:        importSourceRef(item),
		Confidence:       0.8,
	})
	return err
}

func (s ProductionSink) importArchive(item ImportItem, scope Scope) error {
	if s.Archive == nil {
		return errors.New("archive production sink is not configured")
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	result, err := s.Archive.Create(archive.CreateRequest{
		RequestID: importRequestID(item, "archive"),
		ArchiveID: "archive_" + importIDPart(string(item.SourceType)) + "_" + importIDPart(item.ExternalID),
		Title:     "Imported " + string(item.SourceType) + " " + item.ExternalID,
		UserID:    scope.UserID,
		OrgID:     scope.OrgID,
		ProjectID: scope.ProjectID,
		CreatedAt: now,
		Events: []eventlog.TurnEvent{{
			Version:   "v1",
			EventID:   importRequestID(item, "event"),
			TurnID:    importRequestID(item, "turn"),
			ThreadID:  importRequestID(item, "thread"),
			SessionID: importRequestID(item, "session"),
			Type:      eventlog.EventManualArchive,
			CreatedAt: now,
			Actor: eventlog.Actor{
				UserID:    scope.UserID,
				OrgID:     scope.OrgID,
				ProjectID: scope.ProjectID,
				AgentID:   scope.AgentID,
			},
			Source: eventlog.Source{Platform: "importer"},
			Payload: map[string]any{
				"text":                 item.Text,
				"source_type":          string(item.SourceType),
				"external_id":          item.ExternalID,
				"original_source_type": originalSourceType(item),
				"original_external_id": originalExternalID(item),
			},
		}},
	})
	if err != nil {
		return err
	}
	if s.ArchiveIndexQueue == nil {
		return nil
	}
	return s.enqueueArchiveIndex(result.Metadata)
}

func hotMemoryScope(scope Scope) hotmemory.Scope {
	switch scope.Visibility {
	case "private":
		return hotmemory.ScopeUser
	case "org":
		return hotmemory.ScopeOrg
	default:
		return hotmemory.ScopeProject
	}
}

func importSourceRef(item ImportItem) string {
	return strings.Join([]string{"importer", originalSourceType(item), originalExternalID(item)}, ":")
}

func importRequestID(item ImportItem, suffix string) string {
	return strings.Join([]string{"importer", item.BatchID, string(item.SourceType), item.ExternalID, suffix}, ":")
}

func originalSourceType(item ImportItem) string {
	if item.SourceRef != nil && item.SourceRef["source_type"] != "" {
		return item.SourceRef["source_type"]
	}
	return string(item.SourceType)
}

func originalExternalID(item ImportItem) string {
	if item.SourceRef != nil && item.SourceRef["external_id"] != "" {
		return item.SourceRef["external_id"]
	}
	return item.ExternalID
}

func (s ProductionSink) enqueueArchiveIndex(metadata archive.Metadata) error {
	content, err := os.ReadFile(metadata.FilePath)
	if err != nil {
		return err
	}
	chunks, err := archive.ChunkMarkdown(archive.ChunkRequest{ArchiveID: metadata.ArchiveID, IndexGeneration: metadata.IndexGeneration, Content: string(content)})
	if err != nil {
		return err
	}
	return s.ArchiveIndexQueue.EnqueueArchiveIndex(context.Background(), ArchiveIndexJob{
		IdempotencyKey:   importRAGIndexIdempotencyKey(metadata.ArchiveID, metadata.IndexGeneration),
		ArchiveID:        metadata.ArchiveID,
		IndexGeneration:  metadata.IndexGeneration,
		OrgID:            metadata.OrgID,
		ProjectID:        metadata.ProjectID,
		UserID:           metadata.UserID,
		Visibility:       "project",
		PermissionLabels: []string{"project:" + metadata.ProjectID + ":read"},
		Chunks:           chunks,
	})
}

func importRAGIndexIdempotencyKey(archiveID string, generation int) string {
	return fmt.Sprintf("rag_%s_g%d", archiveID, generation)
}

func importIDPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "empty"
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	result := strings.Trim(builder.String(), "_")
	if result == "" {
		return "empty"
	}
	return result
}
