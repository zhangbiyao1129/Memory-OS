package main

import (
	"strings"
	"testing"
)

func TestRunDryRunMem0(t *testing.T) {
	out, err := run([]string{"--source", "mem0", "--batch", "cli_batch", "--dry-run", "--input", "../../internal/importer/fixtures/mem0_sample.jsonl"})
	if err != nil {
		t.Fatalf("run dry-run error = %v", err)
	}
	if !strings.Contains(out, `"dry_run":true`) || !strings.Contains(out, `"item_count":2`) {
		t.Fatalf("dry-run output unexpected: %s", out)
	}
	if strings.Contains(out, "sk-test-redacted-example") {
		t.Fatalf("dry-run leaked fake secret: %s", out)
	}
}

func TestRunExportBundle(t *testing.T) {
	out, err := run([]string{"--source", "mem0", "--batch", "cli_batch", "--apply", "--export-bundle", "--input", "../../internal/importer/fixtures/mem0_sample.jsonl"})
	if err != nil {
		t.Fatalf("run export error = %v", err)
	}
	if !strings.Contains(out, "Memory OS Export Bundle") || !strings.Contains(out, "source_refs") {
		t.Fatalf("export output unexpected: %s", out)
	}
	if strings.Contains(out, "sk-test-redacted-example") {
		t.Fatalf("export leaked fake secret: %s", out)
	}
}
