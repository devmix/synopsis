# Synopsis[MEMEX]

Zero-Infrastructure RAG & Knowledge Graph with hybrid search and [MCP](https://modelcontextprotocol.io) interface for LLM agents. Built in Go, runs entirely offline with SQLite + ONNX Runtime — no external dependencies required at runtime (an OpenAI-compatible API is optional, used only for LLM-based NER and cross-domain entity linking).

## Overview

Synopsis ingests documentation (Markdown, JSON datasets, MediaWiki dumps, saved web pages, unstructured files), builds a searchable index combining **lexical** (FTS5/BM25) and **semantic** (vector/cosine) search via Reciprocal Rank Fusion, extracts entities, relations, and facts into a knowledge graph with cross-domain entity linking, and exposes everything through 12 MCP tools consumable by Claude Desktop, Cursor, or any MCP-compatible client.

- **Hybrid search**: BM25 + cosine similarity fused with RRF, plus recency/authority boosts
- **Knowledge graph**: entities and relations stored in SQLite, loaded into memory at startup; BFS traversal up to depth 10
- **Facts**: subject–predicate–object triples extracted during ingestion, each with a review status (`pending` / `approved`) — only approved facts are exposed in search expansion
- **Cross-domain linking**: entities matched across domains via CEL expressions, normalized name equality, and optional LLM-based resolution
- **NER pipeline**: composable stages configured per ontology — `regex`, `prose` (statistical), `llm`
- **Auto-update**: file watcher re-indexes changed sources incrementally while the server runs

## Architecture at a Glance

| Component  | Technology                                                                       |
|------------|----------------------------------------------------------------------------------|
| Language   | Go 1.25.5+ (CGO enabled)                                                         |
| Database   | SQLite with FTS5 + sqlite-vec (vec0), compiled in via CGO                        |
| Embeddings | Local ONNX Runtime or OpenAI-compatible API                                      |
| Search     | Reciprocal Rank Fusion (RRF) — BM25 + cosine similarity                          |
| Graph      | Entity relations & facts in SQLite, in-memory BFS traversal up to 10 levels      |
| Interface  | MCP server over HTTP (SSE transport), `GET /sse`, `POST /message`, `GET /health` |

## Requirements

- **Go** 1.25.5+ (module-aware toolchain)
- **C toolchain** (gcc or clang) — required for CGO / `go-sqlite3` / `sqlite-vec`
- *(Optional)* **Zig 0.14+** — for cross-compilation to other platforms

Embedding models and the ONNX Runtime itself are downloaded on demand by the application; no manual model setup is needed (see [Model Management](#model-management)).

## Installation

### Prerequisites

- **Go** 1.25.5+ (for building from source)
- Or download a pre-built release from [GitHub Releases](https://github.com/devmix/synopsis/releases)

### Pre-built binaries

1. Download the archive for your platform:

   | Platform | Archive |
      |----------|---------|
   | Linux x86_64 | `synopsis-linux-amd64.tar.gz` |
   | Linux ARM64 | `synopsis-linux-arm64.tar.gz` |
   | macOS Intel | `synopsis-darwin-amd64.tar.gz` |
   | macOS Apple Silicon | `synopsis-darwin-arm64.tar.gz` |
   | Windows x86_64 | `synopsis-windows-amd64.tar.gz` |

2. Extract and run:
   ```bash
   tar -xzf synopsis-*.tar.gz
    ./synopsis-* --config configs/config.default.yaml serve
   ```

### Build from source

```bash
# Clone the repository
git clone https://github.com/devmix/synopsis.git
cd synopsis

# Build for current platform
make build

# Build for all platforms (requires Zig 0.14+)
make build-all

# Or build single platforms into dist/ (same output as CI releases)
./scripts/build.sh linux-amd64
./scripts/build.sh macos-arm64   # macOS targets link against bundled SDK stubs automatically

# Run
./bin/synopsis --config configs/config.default.yaml serve
```

sqlite-vec is compiled into the binary via CGO — no external libraries required.

## Quick Start

### 1. Clone and build

```bash
git clone <repo-url> && cd synopsis
make build
```

This produces `bin/synopsis` for your current platform.

### 2. Configure

Configuration lives in three places:

- **`configs/config.default.yaml`** — main application config (database, embeddings, ingestion, search, graph, server). Select a different file with `--config`, or a different preset with `--preset <name>` (uses `config.<name>.yaml`).
- **`configs/onnx.yaml`** — ONNX Runtime platform settings and the embedding model registry. Referenced via `paths.onnx_config`. Copy from `onnx.yaml.default` if missing.
- **Ontology directory** (`data/ontology` by default, see [Ontology & Domains](#ontology--domains)) — defines data sources, entity schemas, extraction rules, and cross-domain linking.

Key sections of the main config:

```yaml
database:
  path: "./data/knowledge.db"
  pragma: { mmap_size: "268435456", journal_mode: WAL, synchronous: NORMAL, cache_size: "-64000" }

embeddings:
  mode: "local"            # or "api" for an OpenAI-compatible endpoint (Ollama, etc.)
  local:
    model_name: "bge-small-en-v1.5"   # auto-downloaded from the registry on first use
    vector_dim: 384
  api:
    base_url: "http://localhost:1234/v1"
    model_name: "text-embedding-3-large"
    vector_dim: 3072

ingestion:
  chunking: { ... }        # per-format chunker settings (markdown, json)
  ner: # NER pipeline provider config
    llm: { api_base_url: ..., model_name: ..., response_format: "json_schema", ... }
    prose: { min_confidence: 0.5, location_min_confidence: 0.75, ... }
  batch_size: 100
  resolver:
    similarity_threshold: 0.85   # Jaro-Winkler dedup threshold

search:
  rrf_k: 20
  lexical_top_k: 20
  semantic_top_k: 20
  final_top_k: 10
  enable_lexical: true
  enable_semantic: true
  timeout_ms: 10000
  # relevance boosts
  official_boost: 1.5
  recent_boost: 1.2        # documents updated within recent_days (90)
  authority_boost: { policy: 1.5, regulation: 1.3, default: 1.0 }

graph:
  enable_graph: true
  max_depth: 5
  max_nodes: 1000
  load_on_startup: true

auto_update: # file watcher + initial sync behavior for `serve`
  enabled: true
  debounce_seconds: 1
  watch_sources: true
  initial_sync: true

scheduler: # periodic background jobs (gocron, singleton mode)
  jobs:
    orphan_cleanup: { enabled: true, interval_seconds: 300 }

paths:
  data_dir: "data"
  migrations_dir: "migrations"
  global_config_path: "data/ontology"   # ontology directory (global.xml + domains/*.xml)
  prompts_path: "configs/prompts"       # NER / entity-linker prompt templates (.tmpl)
  onnx_config: "configs/onnx.yaml"

server:
  host: "0.0.0.0"
  port: 8080
```

At runtime the `data/` directory holds the SQLite databases (`knowledge.db`, `cache.db`), downloaded models (`models/<name>/`), the ONNX Runtime library (`onnxruntime/`), and the ontology files.

### 3. Index documents

```bash
./bin/synopsis --config configs/config.default.yaml sync
```

This parses all sources declared in the ontology's `global.xml`, chunks text, generates embeddings, runs the configured NER stages (entity/relation/fact extraction + cross-domain linking), and stores everything in SQLite. Use `--rebuild` to clear existing data first:

```bash
./bin/synopsis --config configs/config.default.yaml sync --rebuild
```

### 4. Start MCP server

```bash
./bin/synopsis --config configs/config.default.yaml serve
```

The server runs an **initial sync** on startup (unless `auto_update.initial_sync` is false or `--no-initial-sync` is given), then serves MCP over HTTP and watches the source directories, re-indexing documents automatically as files change. It also starts a job scheduler for periodic maintenance (orphaned entity/fact cleanup).

| Endpoint                      | Description                                                       |
|-------------------------------|-------------------------------------------------------------------|
| `GET /sse`                    | MCP SSE transport endpoint (client connection)                    |
| `POST /message?sessionId=...` | MCP message endpoint                                              |
| `GET /health`                 | JSON health status (database, document count, embedding provider) |

Point an MCP client at `http://localhost:8080/sse`.

## CLI Reference

```bash
synopsis [--config PATH] [--preset NAME] [--db PATH] <command> [flags]
```

**Global flags** (before the command):

| Flag        | Default         | Description                                           |
|-------------|-----------------|-------------------------------------------------------|
| `--config`  | auto-search     | Path to YAML configuration file                       |
| `--preset`  | `default`       | Configuration preset name (uses config.{preset}.yaml) |
| `--db`      | *(from config)* | Override SQLite database path                         |
| `--version` | —               | Print version string and exit                         |

**Commands:**

| Command        | Description                                                                                                                      |
|----------------|----------------------------------------------------------------------------------------------------------------------------------|
| `serve`        | Unified mode: initial sync + MCP server + file watching (auto-update) + job scheduler. This is the only process you need to run. |
| `sync`         | One-shot re-index of all sources, then exit                                                                                      |
| `model`        | Manage embedding models (`list`, `download`, `delete`, `info`, `benchmark`)                                                      |
| `onnx-runtime` | Manage the ONNX Runtime library used for local embeddings (`install`, `status`, `uninstall`)                                     |

**`serve` flags:**

| Flag                     | Description                                                                  |
|--------------------------|------------------------------------------------------------------------------|
| `--no-initial-sync`      | Skip the full source scan on startup                                         |
| `--port N`               | Override `server.port` (default `8080`)                                      |
| `--auto-rebuild-vectors` | Re-embed all chunks automatically if a vector dimension mismatch is detected |

**`sync` flags:**

| Flag                     | Description                                                             |
|--------------------------|-------------------------------------------------------------------------|
| `--rebuild`              | Clear all existing data before re-indexing                              |
| `--auto-rebuild-vectors` | Same as for `serve` (ignored with `--rebuild`, which resets everything) |

## Model Management

Embedding models come from a registry defined in the ONNX config file (`configs/onnx.yaml`, referenced via `paths.onnx_config`). Models are stored in `<data_dir>/models/<name>/` and downloaded on first use when `embeddings.local.model_name` is set. The legacy `model_path` option still works for pointing at an explicit ONNX model file (skips the registry).

```bash
./bin/synopsis --config configs/config.default.yaml model list            # all registered models + install status
./bin/synopsis --config configs/config.default.yaml model download <name> # download with progress bar
./bin/synopsis --config configs/config.default.yaml model info <name>     # files, sizes, source repo
./bin/synopsis --config configs/config.default.yaml model delete <name>   # remove files + cache entry
```

With no name given, `model download` uses the registry default (`models.default` in `onnx.yaml`).

### Available Models (default registry)

| Name                                    | Display Name                   | Vector Dim | Source                               |
|-----------------------------------------|--------------------------------|------------|--------------------------------------|
| `bge-m3-int8`                           | BGE-M3 INT8 Quantized          | 1024       | HuggingFace (BAAI/bge-m3)            |
| `bge-small-en-v1.5`                     | BGE Small English v1.5         | 384        | HuggingFace (BAAI/bge-small-en-v1.5) |
| `paraphrase-multilingual-MiniLM-L12-v2` | Paraphrase Multilingual MiniLM | 384        | HuggingFace (sentence-transformers)  |

### Auto-download on Startup

The application checks for the configured model on startup (`serve` or `sync`) and downloads it automatically if not present. The ONNX Runtime library itself is managed separately:

```bash
./bin/synopsis --config configs/config.default.yaml onnx-runtime install   # download + verify for this platform
./bin/synopsis --config configs/config.default.yaml onnx-runtime status
./bin/synopsis --config configs/config.default.yaml onnx-runtime uninstall
```

## Ontology & Domains

All domain knowledge lives in the ontology directory (`paths.global_config_path`, default `data/ontology`):

- **`global.xml`** — shared entity/relation pool available to every domain, global extraction rules (regex), cross-domain link configuration, NER pipeline stages, and the list of ingestion data sources.
- **`domains/*.xml`** — one file per domain (e.g. `domain_hr.xml`). Each defines domain-specific entities, relations, extraction rules, and confidence thresholds (`auto_publish_threshold`, `review_threshold`, `reject_threshold`) for the fact review workflow.

Missing or malformed domain XML files cause startup errors. If a domain defines an entity with the same ID as one in the global pool, the domain definition wins (a warning is logged).

### Data sources

Sources are declared in `global.xml` — not in the main YAML config:

```xml

<sources>
    <source path="./data/storage/documents/hr" type="markdown">
        <domains>
            <domain>hr</domain>
        </domains>
    </source>
    <source path="./data/storage/site/example.com" type="webpages">
        <domains>
            <domain>product</domain>
        </domains>
    </source>
</sources>
```

Supported `type` values: `markdown`, `json`, `mediawiki`, `webpages`, `unstructured`. Each source can carry multiple domains; omitting `<domains>` defaults to `["default"]`.

### NER pipeline stages

The active extraction stages are listed in the `<ner>` block of `global.xml`; valid methods are `regex` (rule-based patterns from entity definitions), `prose` (statistical NER, see `ingestion.ner.prose` thresholds), and `llm` (LLM-based entity/relation/fact extraction, configured under `ingestion.ner.llm`). Provider prompt templates can be overridden via `.tmpl` files in `paths.prompts_path`.

### Cross-domain linking

Configured in the `<cross-domain-links>` block of `global.xml`, applied in priority order:

- **`expression`** — CEL expressions evaluated against entity pairs from different domains (variables for name, type, domain, metadata; functions like `facts()`, `chunks()`, `neighbors()`)
- **`equals`** — automatic matching by normalized name similarity (minimum word count configurable)
- **`llm`** — LLM-based entity resolution between domains, using the `linker.llm` settings from the main config

Links carry provenance (method, confidence, evidence) retrievable via the `get_entity_links` MCP tool.

## MCP Tools

The server exposes 12 tools over the Model Context Protocol:

### Search

**`search`** — hybrid lexical + semantic search fused with RRF. Returns ranked chunks with full metadata (`document_id`, `chunk_id`, `sequence_num`, offsets, domains, score) and associated entities. An unknown `domain` filter produces a warning in the response instead of an error.

| Name     | Type   | Required | Description                           |
|----------|--------|----------|---------------------------------------|
| `query`  | string | yes      | Search query string                   |
| `top_k`  | number | no       | Max results (default 10, range 1–100) |
| `domain` | string | no       | Filter by domain; empty = all domains |

**Response:** `{ results: [{ document_id, chunk_id, text, sequence_num, start_offset, end_offset, document_path, score, source_type, domains[], entities[{id,name,type}] }], total_count, search_time_ms, warning? }`

### Catalog browsing (cursor pagination)

All catalog tools accept `page_size` (1–200, default 20) and a base64 `cursor`; responses include `total_count` and `next_cursor` when more pages exist.

**`catalog_overview`** — aggregate statistics: document/chunk/entity/fact counts, breakdowns by source type and entity type/domain, known domains, graph node/edge counts. No parameters.

**`catalog_documents`** — list documents with metadata (id, source_type, original_path, domain, created_at, updated_at). Filters: `domain`, `source_type`, `name` (substring on path).

**`catalog_entities`** — list entities with metadata (id, name, type, domain, description, confidence). Filters: `type`, `domain`, `name` (substring).

### Entities & facts

**`search_entities_by_type`** — entities of a given type. Parameters: `entity_type` (required), `domain`, plus pagination.

**`search_facts`** — subject–predicate–object facts with entity names and status. Filters: `predicate` (substring), `entity_name` (matches subject or object), `status` (default `approved`), `domain`, plus pagination.

**`get_entity_dossier`** — complete dossier for one entity: approved facts, source documents, related entities via BFS, cross-domain links with provenance. Parameters: `entity_id` **or** `entity_name` (exactly one; `type`/`domain` disambiguate name lookups), `depth` (1–5, default 2), `include_facts`, `include_sources`.

**`get_entity_relations`** — BFS traversal of the knowledge graph from an entity. Parameters: `entity_id` **or** `entity_name`, `domain`, `depth` (1–10, default 2), `include_cross_domain` (follow cross-domain links, default false). Returns nodes, edges, and counts.

**`get_entity_links`** — cross-domain links for an entity with full provenance (method: `expression`/`equals`/`llm`, confidence, evidence). Parameters: `entity_id` **or** `entity_name`, `domain`.

### Document & chunk retrieval

**`get_document_context`** — document metadata plus all chunks (offsets, sequence numbers), associated entities, and fact IDs. Parameters: `document_id` (required), `include_chunks` (default true), `include_entities` (default true), `include_facts` (default false).

**`get_chunk_by_id`** — a single chunk with full text, offsets, document metadata, and entities. Parameter: `chunk_id` (required).

**`get_fact_by_id`** — a single fact with subject/object entity details and source document quotes. Parameter: `fact_id` (required).

## Connecting an MCP Client

### Claude Desktop

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "synopsis": {
      "type": "sse",
      "url": "http://localhost:8080/sse"
    }
  }
}
```

### Cursor / Other MCP Clients

Use a URL-based (SSE) MCP server configuration pointing at `http://<host>:<port>/sse`. The default port is `8080`, configurable via `server.port` in the config or `--port`.

## Docker

Build and run with Docker Compose:

```bash
docker compose up --build
```

The service listens on port 8080. Volumes mount `./configs` (read-only) for configuration files and `./data` for the database, models, ontology, and ONNX Runtime. An optional Ollama service is included in `docker-compose.yml` (commented out) for API-based embeddings or LLM NER.

## Troubleshooting

| Problem                                                        | Solution                                                                                                                                                                                                        |
|----------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `CGO_ENABLED` error on build                                   | Ensure gcc/clang is installed: `apt install build-essential` or `brew install gcc`. CGO is mandatory — `go-sqlite3` and sqlite-vec are compiled in (Makefile passes `-DSQLITE_ENABLE_FTS5 -DSQLITE_ENABLE_VEC`) |
| Vector dimension mismatch on startup                           | Run with `--auto-rebuild-vectors` (or set `embeddings.auto_rebuild_vectors: true`) to re-embed chunks automatically, or delete `data/knowledge.db` and re-ingest                                                |
| Model file not found / slow first start                        | Models auto-download when `model_name` is configured; download manually with `synopsis model download <name>`, or set the legacy explicit `model_path`                                                          |
| ONNX Runtime missing on this platform                          | Run `synopsis onnx-runtime install`, then check with `onnx-runtime status`                                                                                                                                      |
| Slow ingestion                                                 | Increase `ingestion.batch_size`; ensure WAL journal mode is active (`database.pragma`)                                                                                                                          |
| MCP client can't connect                                       | Verify the server is running and `curl http://localhost:8080/health` returns JSON; ensure the client points at `http://<host>:<port>/sse`                                                                       |
| Port already in use                                            | Change `server.port` in config or use `serve --port <N>`                                                                                                                                                        |
| Domain XML file not found / domain discovery failed at startup | Ensure `paths.global_config_path` points to the ontology directory containing `global.xml` and `domains/*.xml`                                                                                                  |
| Facts not appearing in search expansion                        | Check `facts.status` — only `approved` facts are included. Inspect with the `search_facts` MCP tool (it also lists `pending`)                                                                                   |
| Search warns "unknown domain"                                  | The requested domain is not assigned to any document; check `<domains>` on sources in `global.xml` and the domains of indexed documents (`catalog_documents`)                                                   |

## License

Apache 2.0
