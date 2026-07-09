# Memory OS

Memory OS is a self-hosted memory platform for coding agents. It captures work from Codex, Claude Code, Cursor, opencode, Hermes, and MCP clients, then turns that stream into searchable, traceable, and governable long-term memory.

The project is built for one practical question:

> Can an agent remember useful engineering context without leaking secrets, mixing projects, or trusting stale facts forever?

Memory OS answers that with a production path for event ingestion, candidate extraction, hot memory, archive RAG, project permissions, MCP tools, and a governance layer called Memory Kernel.

## What It Does

- Captures agent activity as `TurnEvent` records from HTTP hooks or MCP tools.
- Extracts useful facts, preferences, decisions, and follow-up items into candidate memories.
- Routes candidates through review, discard, Hot Memory, or Markdown Archive flows.
- Searches Hot Memory and Archive RAG through one unified retrieval API.
- Exposes memory tools over Streamable HTTP MCP and a local stdio proxy.
- Keeps tenant/project permissions in PostgreSQL and applies query-time filters in Qdrant.
- Stores secrets only through the Secret Vault or local decrypt path, never in memory content.
- Builds task-specific Context Packs from current trusted Memory Units.

## Current Status

Memory OS is usable as a single-node deployment and is actively evolving.

| Area | Status |
| --- | --- |
| HTTP API | Implemented |
| Web console | Implemented |
| Streamable HTTP MCP | Implemented |
| Local stdio MCP proxy | Implemented |
| TurnEvent ingestion | Implemented |
| Candidate extraction | Implemented |
| Candidate AI maintenance | Implemented |
| Hot Memory | Implemented |
| Markdown Archive | Implemented |
| Archive RAG | Implemented |
| Unified retrieval | Implemented |
| Secret Vault / local secret tools | Implemented |
| Memory Kernel / Context Pack | Implemented, still being hardened |

## Architecture

```text
Agent / Hook / MCP client
        |
        v
memory-api / memory-mcp
        |
        v
TurnEvent
        |
        v
candidate_memory_jobs
        |
        v
memory-worker
        |
        v
Candidate Memory
        |
        +-------------------+-------------------+
        |                   |                   |
        v                   v                   v
   Hot Memory        AI Maintenance       Archive Composer
        |                   |                   |
        |                   v                   v
        |       discard / review / hot     Markdown Archive
        |              / archive                 |
        |                                       v
        +-------------------------------> Archive RAG
                                                |
                                                v
                                      Unified Retrieval
```

Data ownership:

- PostgreSQL is the source of truth for metadata, permissions, jobs, candidates, Hot Memory, Memory Units, and Archive records.
- Markdown files are the source of truth for Archive content.
- Qdrant stores rebuildable vector indexes only.
- Redis is used for queues, locks, cache, and rate limiting.

## Memory Lifecycle

1. An agent sends a `TurnEvent`.
2. The API authenticates the actor and resolves the org/project/workspace scope.
3. Useful event types enqueue candidate extraction jobs.
4. The worker calls an LLM extractor and writes candidate memories.
5. Candidate maintenance groups, filters, deduplicates, and routes candidates.
6. High-value facts can become Hot Memory.
7. Archive-ready material becomes Markdown Archive content.
8. Archive chunks are indexed into Qdrant.
9. `/memory/search` and MCP `memory_search` query Hot Memory and Archive RAG together.
10. Retrieval usage is logged and fed back into future ranking and governance.

Memory Kernel adds a current-fact layer on top of this history:

- `memory_units` represent trusted current facts.
- governance runs can detect outdated, conflicting, duplicate, or unsupported units.
- Context Pack builds task-ready context for agents.
- Memory CI checks whether recall answers are polluted by stale facts.

## MCP Tools

Remote Streamable HTTP endpoint:

```text
POST <MEMORY_OS_MCP_URL>/mcp
Authorization: Bearer <Memory OS PAT>
Accept: application/json, text/event-stream
```

Compatibility bridge:

```text
GET  <MEMORY_OS_MCP_URL>/tools
POST <MEMORY_OS_MCP_URL>/tools/call
```

Implemented MCP tools:

| Tool | Purpose |
| --- | --- |
| `memory_search` | Search Hot Memory and Archive RAG with traceable sources. |
| `memory_context_pack` | Build a Memory Kernel context pack for a task. |
| `memory_append_event` | Append an agent event and enqueue candidate extraction when applicable. |
| `memory_archive` | Create a manual Markdown Archive. |
| `memory_get_archive` | Read Archive metadata and Markdown content by permission. |
| `memory_mark_used` | Mark a memory result as used and update feedback signals. |
| `memory_stats` | Return memory lifecycle stats. |

For stdio-only clients, use the local proxy:

```bash
go build -o ~/bin/memory-mcp-local ./cmd/memory-mcp-local
```

Example MCP client config:

```json
{
  "mcpServers": {
    "memory-os": {
      "command": "/Users/you/bin/memory-mcp-local",
      "env": {
        "MEMORY_OS_MCP_URL": "https://memory.example.com",
        "MEMORY_OS_API_URL": "https://memory-api.example.com",
        "MEMORY_OS_TOKEN": "${MEMORY_OS_TOKEN}"
      }
    }
  }
}
```

Do not put real tokens in committed config. Load them from your local secret manager or environment.

## HTTP Surfaces

Common production endpoints:

| Endpoint | Purpose |
| --- | --- |
| `GET /healthz` | Runtime health for database, Redis, and Qdrant. |
| `GET /version` | Build version, commit, build time, and dirty flag. |
| `GET /openapi.json` | Runtime API schema. |
| `POST /memory/turn-event` | Agent event ingestion. |
| `POST /memory/search` | Unified retrieval. |
| `POST /memory/candidates/maintenance/run` | Manual candidate maintenance run. |
| `POST /memory/kernel/governance/run` | Memory Kernel governance run. |
| `POST /memory/kernel/context-pack` | Build a Context Pack. |

## Web Console

The Nuxt console provides:

- workspace-level overview
- candidate memory review
- Hot Memory management
- Archive and retrieval inspection
- diagnostics for ingestion and maintenance
- Secret Vault guidance
- audit and access logs
- tenant, token, and project settings

The overview aggregates by user/workspace. Deeper workflows still keep project scoping and permission boundaries.

## Deployment

Reference stack:

- Go / Hertz API
- PostgreSQL
- Redis
- Qdrant
- Nuxt 3 frontend
- Docker Compose

Typical production services:

| Service | Role |
| --- | --- |
| `memory-api` | HTTP API and backend for the console. |
| `memory-worker` | Background jobs, extraction, maintenance, archive indexing. |
| `memory-mcp` | Streamable HTTP MCP server. |
| `memory-web` | Web console. |
| `postgres` | Metadata source of truth. |
| `redis` | Queue, locks, cache, rate limiting. |
| `qdrant` | Rebuildable vector index. |

This repository includes deployment automation for the maintainer's single-node host. Read [DEPLOYMENT.md](DEPLOYMENT.md) before running deployment, restart, or production verification commands.

Common verification commands:

```bash
go test ./...
go vet ./...
npm --prefix frontend run build
git diff --check
```

Post-deploy verification:

```bash
VERIFY_MODE=full make post-deploy-verify
```

Runtime checks:

```bash
curl "$MEMORY_OS_API_URL/healthz"
curl "$MEMORY_OS_API_URL/version"
curl "$MEMORY_OS_API_URL/openapi.json"
```

## Security Model

Memory OS is designed around conservative memory safety:

- Real API keys, PATs, passwords, cookies, private keys, and tokens must not enter code, logs, Markdown, Qdrant, Hot Memory, README examples, or test snapshots.
- PAT plaintext is shown only once at creation.
- Secret plaintext is allowed only in Secret Vault encryption or local decrypt injection paths.
- Qdrant access must use query-time payload filters.
- HTTP handlers do protocol/auth/validation/error mapping only.
- Services own business behavior.
- Repositories own SQL and transactions.
- Adapters convert events; they do not write Markdown, Hot Memory, or Qdrant directly.

## Repository Layout

```text
cmd/
  memory-api          HTTP API
  memory-worker       background worker
  memory-mcp          Streamable HTTP MCP server
  memory-mcp-local    stdio MCP local proxy
  memory-bootstrap    first-admin bootstrap helper

internal/
  eventlog            TurnEvent ingestion and payload handling
  candidatememory     candidate extraction, triage, maintenance, archive material
  hotmemory           short-term high-value memory
  memorykernel        current facts, governance, context packs
  archive             Markdown Archive metadata and content
  retrieval           unified Hot Memory + Archive RAG retrieval
  qdrant              vector index and payload filtering
  tenant              users, orgs, projects, permissions
  secret              encrypted Secret Vault
  secretlocal         local secret decrypt bridge
  mcp                 MCP schema and tool handling
  http                API routes

frontend/
  Nuxt 3 web console

deploy/
  Docker Compose and production container files

scripts/
  verification, deployment, backup, restore, and safety tooling

migrations/
  PostgreSQL schema migrations
```

## Project Principles

- Keep memory traceable to source event, archive, chunk, project, thread, and actor.
- Prefer explicit permission context over inferred trust.
- Treat Archive as historical evidence and Memory Units as current context.
- Make production health observable through logs, tables, diagnostics, and verification scripts.
- Never call a schema-exposed MCP tool "done" until the server handler and downstream path work.

## License

No license file is included yet. Treat the repository as private/proprietary unless a license is added.
