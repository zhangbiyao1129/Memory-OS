package eventlog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"memory-os/internal/secret"
)

type SanitizerOptions struct {
	MaxTurnEventBytes  int
	MaxToolOutputBytes int
}

type SanitizedEvent struct {
	Event         TurnEvent
	SafePayload   []byte
	PayloadHash   string
	OriginalBytes int
	SafeBytes     int
	Truncated     bool
	Warnings      []string
}

func Sanitize(event TurnEvent, options SanitizerOptions) (SanitizedEvent, error) {
	if options.MaxTurnEventBytes <= 0 {
		options.MaxTurnEventBytes = 256 * 1024
	}
	if options.MaxToolOutputBytes <= 0 {
		options.MaxToolOutputBytes = 64 * 1024
	}

	original, err := json.Marshal(event.Payload)
	if err != nil {
		return SanitizedEvent{}, err
	}
	if len(original) > options.MaxTurnEventBytes {
		return SanitizedEvent{}, errors.New("turn event payload exceeds max bytes")
	}

	safePayload := clonePayload(event.Payload)
	warnings := []string{}
	truncated := false

	if value, ok := safePayload["tool_output"].(string); ok && len([]byte(value)) > options.MaxToolOutputBytes {
		sum := sha256.Sum256([]byte(value))
		safePayload["tool_output"] = value[:options.MaxToolOutputBytes]
		safePayload["tool_output_hash"] = hex.EncodeToString(sum[:])
		safePayload["tool_output_original_bytes"] = len([]byte(value))
		safePayload["tool_output_truncated"] = true
		warnings = append(warnings, "tool_output_truncated")
		truncated = true
	}

	for key, value := range safePayload {
		text, ok := value.(string)
		if !ok {
			continue
		}
		sanitized := secret.Sanitize(text, func(index int, match string) string {
			return fmt.Sprintf("secret_ref_%s_%d", event.EventID, index)
		})
		if len(sanitized.Secrets) > 0 {
			safePayload[key] = sanitized.Text
			warnings = append(warnings, "secret_ref_replaced")
		}
	}

	safe, err := json.Marshal(safePayload)
	if err != nil {
		return SanitizedEvent{}, err
	}
	sum := sha256.Sum256(safe)
	event.Payload = safePayload
	event.Warnings = append(event.Warnings, warnings...)

	return SanitizedEvent{
		Event:         event,
		SafePayload:   safe,
		PayloadHash:   hex.EncodeToString(sum[:]),
		OriginalBytes: len(original),
		SafeBytes:     len(safe),
		Truncated:     truncated,
		Warnings:      warnings,
	}, nil
}

func clonePayload(payload map[string]any) map[string]any {
	cloned := make(map[string]any, len(payload))
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}
