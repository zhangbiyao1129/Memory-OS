# Memory OS Importers

Memory OS v0.4 importers convert external memory systems into native Memory OS objects. External systems are migration sources only; they are not runtime dependencies.

## Supported sources

- `mem0`: JSONL memory facts -> Hot Memory candidates.
- `fastgpt`: JSON document/chunks -> Markdown Archive content.
- `openmemory`, `zep`, `khoj`: schema-validation skeletons for future migration paths.

## CLI examples

```bash
go run ./cmd/memory-importer --source mem0 --batch batch_001 --dry-run --input internal/importer/fixtures/mem0_sample.jsonl
go run ./cmd/memory-importer --source mem0 --batch batch_001 --apply --export-bundle --input internal/importer/fixtures/mem0_sample.jsonl
```

## Safety rules

- Dry-run does not persist items.
- Apply is idempotent by `source_type + external_id`.
- Fake or real-looking secrets are replaced with `secret_ref_*` placeholders.
- Export bundles include Markdown, metadata, and source refs, but never secret plaintext.
