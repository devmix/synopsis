# AGENTS.md — Synopsis

Go 1.25 RAG + knowledge-graph MCP server (`github.com/devmix/synopsis`) with a Docusaurus site in `site/`. **CGO is mandatory**: SQLite FTS5 + sqlite-vec are compiled into the binary via go-sqlite3.

## Layout

- `cmd/app` — CLI entrypoint, subcommand dispatch
- `internal/<area>` — one package per area: search, ingestion (chunkers/parsers/ner/sources), mcp (+handlers), onnx, embedding, graph, database, domain, watcher, scheduler...
- `migrations/` — numbered SQL applied at startup; add new files, never edit shipped ones
- `data/` — runtime state only (SQLite DBs, models, ONNX Runtime, ontology XML); gitignored, never commit it
- `configs/` — YAML config + prompts; model registry lives in `configs/onnx.yaml`, not Go code
- `site/` — Docusaurus 3 docs site; separate npm project, unrelated to the Go build. **All project documentation lives in `site/docs`** (main content)
- `openspec/` — spec-driven workflow config (`config.yaml`) plus local specs/changes; agent skills in `.agents/skills/`

## Commands (Go, repo root)

| Command | What it does |
|---|---|
| `make build` | builds `bin/synopsis` with required CGO flags |
| `make run` / `make sync` / `make rebuild` | serve / one-shot re-index / clear DB + re-ingest |
| `make test` | `go test -race ./...` — full suite ~5s, no services or network needed |
| `make lint` | golangci-lint **v2.12** — pinned via `go run ...@v2.12` to match CI; no `.golangci.yml`, defaults only |

- Single package: `go test -race ./internal/search/...` · single test: `go test -race -run TestName ./pkg/...`
- Integration e2e vector tests are build-tagged: `go test -tags=integration ./internal/database/` (self-contained, uses `/tmp` DB)
- Tests conventionally use `t.Parallel()` and `t.Run(...)` subtests.
- Cross-compilation requires Zig 0.14+: `make build-all` (→ `bin/`) or `./scripts/build.sh <platform>` (→ `dist/`, what CI uses). macOS targets link against `scripts/darwin-stubs`; don't hand-roll those flags.

**Build gotcha:** use `make build`, not a bare `go build`. The Makefile passes `CGO_CFLAGS="-DSQLITE_ENABLE_FTS5 -DSQLITE_ENABLE_VEC"`; without them the migration runner silently skips fts5/vec0 tables (`isModuleError` path in `internal/database`) and search degrades with no error. `make test` sets the same flags — a bare `go test ./...` compiles **without** FTS5, so every fts5-backed test silently `t.Skip`s (green suite, zero FTS coverage). Native gcc targets also set `CGO_LDFLAGS=-lm`: FTS5 bm25 calls `log()`, which needs explicit libm on glibc where it is not merged into libc.

## CLI

```
./bin/synopsis [--config PATH] [--preset NAME] [--db PATH] <serve|sync|model|onnx-runtime> [flags]
```

- Global flags must precede the subcommand; per-command flags follow it (`sync --rebuild`, `serve --port N --no-initial-sync`).
- Config resolution: `--config` > `config.{preset}.yaml` (default preset `default`) auto-searched relative to executable, then CWD.
- `serve` = initial sync + MCP over HTTP/SSE (`GET /sse`, `POST /message`, `GET /health` on :8080) + file watching. This is the only long-running process.
- Ontology lives in `data/ontology/` (`global.xml` + `domains/*.xml`). Ingestion **sources are declared there, not in YAML config**. Missing/malformed domain XML fails startup; a domain entity with an ID matching the global pool overrides it (warning logged).

## Website (`site/`)

```bash
npm install && npm run start   # dev server
npm run build                  # static build → site/build (gitignored)
npm run typecheck              # tsc (noEmit via @docusaurus/tsconfig)
```

- Content root is `site/docs` (MDX), i18n en + ru, mermaid enabled.
- `onBrokenLinks: 'throw'` — broken links fail the production build; keep cross-doc anchors valid when editing.

## Domain gotchas

- Only facts with `status=approved` appear in search expansion (`pending` is queryable via MCP but never auto-expanded).
- Embedding vector-dimension mismatch at startup → rerun with `--auto-rebuild-vectors` or delete `data/knowledge.db` and re-sync.
- New MCP tool: define schema + register in `internal/mcp/tools.go`, implement handler under `internal/mcp/handlers/`.
- Commits follow Conventional Commits (`feat(scope): ...`, `fix(onnx): ...`) per history and CI.
