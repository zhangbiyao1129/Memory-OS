package hotmemory

import (
	"fmt"
	"strings"

	"memory-os/internal/eventlog"
	"memory-os/internal/secret"
)

type Extractor struct{}

func NewExtractor() Extractor {
	return Extractor{}
}

func (Extractor) ExtractFromTurnEvent(event eventlog.TurnEvent) []Candidate {
	text := payloadText(event.Payload)
	sanitized := secret.Sanitize(text, func(index int, match string) string { return fmt.Sprintf("secret_ref_hot_memory_%d", index) })
	seen := map[string]bool{}
	candidates := []Candidate{}
	for _, sentence := range strings.Split(sanitized.Text, ".") {
		fact := strings.TrimSpace(sentence)
		if !valuableFact(fact) {
			continue
		}
		key := normalizeFact(fact)
		if seen[key] {
			continue
		}
		seen[key] = true
		candidates = append(candidates, Candidate{Fact: fact, Scope: ScopeProject, SourceType: SourceTurnEvent, SourceRef: event.EventID, Confidence: 0.7})
	}
	return candidates
}

func payloadText(payload map[string]any) string {
	parts := []string{}
	for _, value := range payload {
		switch typed := value.(type) {
		case string:
			parts = append(parts, typed)
		}
	}
	return strings.Join(parts, "\n")
}

func valuableFact(fact string) bool {
	if len(fact) < 12 {
		return false
	}
	if strings.Contains(fact, "secret_ref_") {
		return false
	}
	return strings.Contains(fact, " ")
}
