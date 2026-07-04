package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"memory-os/internal/secret"
)

type ChunkRequest struct {
	ArchiveID       string
	IndexGeneration int
	Content         string
	MaxBytes        int
}

type Chunk struct {
	ChunkID         string
	ArchiveID       string
	IndexGeneration int
	ChunkIndex      int
	HeadingPath     []string
	SourceEventIDs  []string
	Content         string
	ContentHash     string
}

var sourceRefPattern = regexp.MustCompile("(?:Source ref|event_id): `([^`]+)`")

func ChunkMarkdown(request ChunkRequest) ([]Chunk, error) {
	if request.ArchiveID == "" {
		return nil, errors.New("archive id is required")
	}
	if request.IndexGeneration <= 0 {
		return nil, errors.New("index generation is required")
	}
	if strings.TrimSpace(request.Content) == "" {
		return nil, errors.New("archive content is required")
	}
	if request.MaxBytes <= 0 {
		request.MaxBytes = 2048
	}

	lines := strings.Split(request.Content, "\n")
	headings := []string{}
	sourceRefs := []string{}
	current := strings.Builder{}
	chunks := []Chunk{}

	flush := func() {
		content := strings.TrimSpace(current.String())
		if content == "" {
			return
		}
		sanitized := secret.Sanitize(content, func(index int, match string) string { return fmt.Sprintf("secret_ref_chunk_%d", index) })
		content = sanitized.Text
		sum := sha256.Sum256([]byte(content))
		chunkIndex := len(chunks)
		chunks = append(chunks, Chunk{
			ChunkID:         fmt.Sprintf("%s_g%d_c%d", request.ArchiveID, request.IndexGeneration, chunkIndex),
			ArchiveID:       request.ArchiveID,
			IndexGeneration: request.IndexGeneration,
			ChunkIndex:      chunkIndex,
			HeadingPath:     append([]string(nil), headings...),
			SourceEventIDs:  append([]string(nil), sourceRefs...),
			Content:         content,
			ContentHash:     hex.EncodeToString(sum[:]),
		})
		current.Reset()
		sourceRefs = nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if heading != "" {
				headings = append(headings, heading)
			}
		}
		if current.Len()+len(line)+1 > request.MaxBytes {
			flush()
		}
		for _, match := range sourceRefPattern.FindAllStringSubmatch(line, -1) {
			if len(match) == 2 {
				sourceRefs = append(sourceRefs, match[1])
			}
		}
		current.WriteString(line)
		current.WriteString("\n")
	}
	flush()
	return chunks, nil
}
