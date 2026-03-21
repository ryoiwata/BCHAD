# Session Log: Phase 0 Foundation — Full Infrastructure Scaffold

**Date:** 2026-03-21, ~01:14
**Duration:** ~1.5 hours
**Focus:** Implement all four Phase 0 deliverables: infrastructure, schemas, migrations, Go package skeletons, and e2e smoke script

---

## What Got Done

### Infrastructure
- Created `docker-compose.yml` with 5 services: `pgvector/pgvector:pg16` (Postgres + pgvector), `valkey/valkey:8`, `minio/minio:latest` (with `minio-init` sidecar), `temporalio/auto-setup:latest`, `temporalio/ui:latest` — all on a shared `bchad` Docker network with healthchecks on postgres and valkey
- Created `docker/init/postgres/01-extensions.sql` — enables `vector` and `uuid-ossp` extensions at container init time
- Created `justfile` with all standard targets: `dev-up`, `dev-down`, `migrate`, `migrate-down`, `seed`, `test`, `test-unit`, `test-int`, `test-e2e`, `test-snapshot`, `snapshot-update`, `lint`, `fmt`, `build`, `clean`, `smoke`; default env vars for local dev baked in
- Initialized `go.mod` as `github.com/athena-digital/bchad` at Go 1.22 with all required dependencies (pgx/v5, Temporal SDK, Chi, Cobra, valkey-go, go-git, go-tree-sitter, tiktoken-go, jsonschema/v6, aws-sdk-go-v2, otel)
- Created `tools.go` with `//go:build tools` blank imports to keep future-use dependencies pinned in `go.mod` after `go mod tidy`
- Created `.env.example` with all required and optional env vars documented
- Updated `.gitignore` with `!.env.example` exception, `bin/`, `terraform/`, `node_modules/`, etc.
- Created `.golangci.yml` enabling `errcheck`, `govet`, `staticcheck`, `unused`, `gosimple`, `ineffassign`, `goimports`; disabling `fieldalignment`

### JSON Schemas (Draft 2020-12)
- Created `schemas/bchadspec.v1.json` — BCHADSpec with `$id`, `$defs`, enum pattern for `pattern` field, if/then constraint requiring `values` when `kind=enum`
- Created `schemas/bchadplan.v1.json` — BCHADPlan with `id` pattern `^pf-\d{8}-\d{3}$`, `stages` minItems:1, model enum
- Created `schemas/stage_artifact.v1.json` — StageArtifact with cross-schema `$ref` to gate_result (full URL)
- Created `schemas/gate_result.v1.json` — GateResult with 8-category error taxonomy enum, tier enum [1, 2]

### Database Migrations (5 up/down pairs using golang-migrate naming)
- `000001_create_pipeline_tables` — `bchad_runs`, `bchad_stages`, `bchad_artifacts` with composite indexes
- `000002_create_verification_tables` — `bchad_gate_results`, `bchad_error_log`
- `000003_create_human_interaction_tables` — `bchad_approvals`, `bchad_prompt_log`
- `000004_create_trust_metrics_tables` — `bchad_trust_scores` (unique on engineer_id+product_id), `bchad_metrics`
- `000005_create_vector_store_tables` — `CREATE EXTENSION IF NOT EXISTS vector`, `bchad_code_patterns` with `embedding vector(1024)`, `bchad_file_structures`, `bchad_arch_decisions`; HNSW indexes (`m=16, ef_construction=128`) on all three; composite B-tree indexes for filtered vector search

### Go Package Skeletons
- `pkg/bchadspec/types.go` — `BCHADSpec`, `EntitySpec`, `FieldSpec`, `UISpec`, `ComplianceFlags` with json tags and godoc
- `pkg/bchadplan/types.go` — `BCHADPlan`, `PlanStage` with json tags and godoc
- `pkg/artifacts/types.go` — `StageArtifact`, `GeneratedFile`, `GateResult`, `GateCheck`, `TrustScore`, `TrustSignals` with json tags and godoc
- `internal/{spec,plan,engine,gateway,retrieval,intelligence,verify,assembly,budget,trust,adapters}/doc.go` — 11 package stubs with descriptive package comments
- `workflows/doc.go` — Temporal workflow package stub
- `cmd/bchad/main.go` — minimal Cobra root command printing "BCHAD: Batch Code Harvesting, Assembly, and Deployment"
- `cmd/worker/main.go` — Temporal worker entrypoint; fails fast if `BCHAD_TEMPORAL_HOST` unset; registers no workflows yet
- Schema validation tests: `pkg/bchadspec/types_test.go`, `pkg/bchadplan/types_test.go`, `pkg/artifacts/types_test.go` — 15 tests total covering valid structs, missing required fields, invalid enum values, cross-schema references
- `testdata/specs/` — `payment-methods.json`, `minimal.json`, `invalid-missing-entity.json`
- `testdata/gate-results/` — `all-pass.json`, `syntax-error.json`, `type-error.json`, `security-violation.json`, `route-conflict.json`

### E2E Smoke Script
- `scripts/e2e-smoke/main.go` — 5-step integration probe:
  1. Postgres: `SELECT 1` + pgvector extension check
  2. Valkey: `SET`/`GET`/`DEL` round-trip
  3. MinIO: `PutObject`/`GetObject` round-trip on `bchad-artifacts` bucket
  4. Temporal: starts no-op workflow on dev server, waits for completion
  5. Anthropic API: minimal prompt ("Generate a Go file that prints Hello from BCHAD"), writes output to `/tmp/bchad-smoke-generated.go` — skipped if `ANTHROPIC_API_KEY` not set
- `scripts/seed/main.go` — seeds a test trust score record for `payments-dashboard-test`
- Added `smoke` target to `justfile`

### Git Commits (4 total)
1. `chore(docker): add docker-compose with postgres+pgvector, valkey, minio, temporal`
2. `feat(schemas): add four versioned JSON Schema Draft 2020-12 definitions`
3. `feat(schemas): add five golang-migrate SQL migration pairs`
4. `feat(spec): add Go package skeletons with types, stubs, and CLI entrypoints`

---

## Issues & Troubleshooting

### 1. `.env.example` blocked by `.gitignore`
- **Problem:** `git add .env.example` failed — the existing `.gitignore` had `.env.*` which matched `.env.example`
- **Cause:** Overly broad glob pattern in `.gitignore` for secrets
- **Fix:** Added `!.env.example` exception line immediately after `.env.*` in `.gitignore`

### 2. `github.com/docker/docker/client` module path conflict
- **Problem:** `go mod tidy -tags tools` failed with `module declares its path as: github.com/moby/moby/client but was required as: github.com/docker/docker/client`
- **Cause:** The Docker SDK has been reorganized; the client package was moved to `github.com/moby/moby`. The `github.com/docker/docker` top-level module still exists but individual sub-packages were split off
- **Fix:** Removed the docker import from `tools.go` entirely. The docker dependency (`github.com/docker/docker`) will be added to `go.mod` when `internal/verify` is implemented, using the correct import path at that time

### 3. `jsonschema/v6` v6.0.1 panic with nil pointer on `Compile`
- **Problem:** All 15 schema tests panicked with `SIGSEGV: nil pointer dereference at addr=0x20` inside `(*roots).validate` at `roots.go:241`
- **Cause:** v6.0.1 had a bug where `AddResource` was called with `bytes.NewReader(data)` (an `io.Reader`), but the library treated it as a raw JSON value (not as JSON to be read), causing the parsed document to be nil. The schema could not be validated against its own metaschema, leading to a nil dereference
- **Fix (partial):** Upgraded to v6.0.2 (`go get github.com/santhosh-tekuri/jsonschema/v6@v6.0.2`). This changed the error from a panic to an explicit error message: `invalid jsonType *bytes.Reader`
- **Fix (complete):** Changed all `c.AddResource(id, bytes.NewReader(data))` calls to `c.AddResource(id, raw)` where `raw` is the already-decoded `map[string]any`. In v6, `AddResource` requires pre-decoded JSON (a Go map/slice/primitive), not an `io.Reader`

### 4. Relative `$ref` in `stage_artifact.v1.json`
- **Problem:** The schema used `"$ref": "gate_result.v1.json"` (relative ref). This resolved against the schema's `$id` base URL (`https://bchad.athena.internal/schemas/`) to produce `https://bchad.athena.internal/schemas/gate_result.v1.json`, but the test compiler didn't look up cross-schema refs by base-relative resolution initially
- **Cause:** The original relative ref was ambiguous without confirming the compiler was loading all schemas under their `$id` URLs first
- **Fix:** Changed the `$ref` to the full absolute URL: `"$ref": "https://bchad.athena.internal/schemas/gate_result.v1.json"`. This is unambiguous regardless of how the compiler resolves base URLs

### 5. `go mod tidy` removing unused dependencies from `go.mod`
- **Problem:** After initial `go mod tidy`, several planned dependencies (chi, go-git, pgvector-go, tiktoken-go, etc.) were removed because no Go files imported them yet
- **Cause:** Standard `go mod tidy` behavior — removes any module not transitively imported by current source
- **Fix:** Created `tools.go` with `//go:build tools` build tag and blank imports for all planned-but-not-yet-used packages. Run `GOFLAGS="-tags=tools" go mod tidy` to pull these in. Note: `go mod tidy` does not support a `-tags` flag directly; the workaround is `GOFLAGS`

---

## Decisions Made

### Use `map[string]any` (not `io.Reader`) with `jsonschema/v6` `AddResource`
The v6 library changed its API from v5: `AddResource` now expects a pre-decoded Go value, not a reader. The test helpers were updated to unmarshal JSON files first, then pass the resulting map. This is cleaner for test code anyway since it avoids double-parsing.

### All schemas use full absolute `$ref` URLs
Rather than relying on relative reference resolution (which depends on `$id` base URL processing working correctly), all cross-schema `$ref` values use the full `https://bchad.athena.internal/schemas/...` URL. More explicit, less fragile.

### Temporal namespace defaults to `default` in smoke script (not `bchad`)
The Temporal dev server (`temporalio/auto-setup`) creates a default namespace called `"default"`. The `bchad` namespace is provisioned by the production setup. For the smoke test (which just runs a no-op workflow to prove connectivity), `default` is sufficient. The worker in `cmd/worker/main.go` correctly reads `BCHAD_TEMPORAL_NAMESPACE` from env with `bchad` as default.

### Docker dependency deferred
The `github.com/docker/docker` package (needed by `internal/verify` for dispatching verification gate containers) was removed from `tools.go` due to module path conflicts with the reorganized moby project. This is a Phase 1 concern when `internal/verify` is actually implemented. The correct import path and version will be determined then.

### `go 1.22` in `go.mod` despite running Go 1.26
The project targets Go 1.22 as specified in the implementation plan and CLAUDE.md. `go mod tidy` updated the directive to `go 1.22.0` (fully qualified patch version), which is equivalent.

---

## Current State

**Working:**
- `go build ./...` compiles with zero errors
- `go vet ./...` passes with zero warnings
- `go test ./...` passes — 15/15 schema validation tests green across all three packages (bchadspec, bchadplan, artifacts)
- All JSON schemas validate their corresponding Go struct fixtures and correctly reject invalid inputs
- Full directory structure matches the spec in `README.md`

**Ready but not yet exercised (requires Docker):**
- `just dev-up` — all 5 services defined and should start
- `just migrate` — all 5 migration files present and ready to apply
- `just smoke` — steps 1–4 (Postgres, Valkey, MinIO, Temporal) will run against local stack; step 5 (Anthropic) will run if `ANTHROPIC_API_KEY` is set

**Not yet implemented (Phase 1+):**
- No CLI subcommands (`run`, `index`, `onboard`, `validate`) — just the root Cobra command
- No Temporal workflows or activities registered
- No `internal/` package implementations — all are `doc.go` stubs
- No `web/` frontend scaffolding
- No `terraform/` infrastructure code
- No `docker/` verification gate container images
- No `semgrep/` custom rules
- No `patterns/` DAG templates or prompt templates

**Branch:** `phase0/foundation` — 4 commits ahead of `main`

---

## Next Steps

1. **Run `just dev-up && just migrate`** to verify all services start and all 5 migrations apply cleanly. Fix any Docker networking or migration issues before Phase 1 work begins.

2. **Run `just smoke`** with a live `ANTHROPIC_API_KEY` to validate the full integration chain end-to-end. The Temporal step requires the dev server to be running (from step 1).

3. **Manual codebase exploration** (Phase 1, Days 5–7) — Read Payments Dashboard and Claims Portal codebases by hand to document expected patterns per stage type. Output: `docs/codebase-exploration/payments-dashboard.md` and `docs/codebase-exploration/claims-portal.md`. These become the acceptance criteria for the indexer.

4. **Implement `internal/intelligence/`** — scanner, extractor, indexer. Start with `scanner.go` (structural profile extraction) since it doesn't require LLM calls or pgvector.

5. **Implement `internal/retrieval/`** — search, ranking, cache. Integration tests with testcontainers-go require `just dev-up`.

6. **Implement `internal/spec/`** — JSON spec parser, validation against `schemas/bchadspec.v1.json`. NL-to-spec translation deferred until after the base parser is solid.

7. **Implement `internal/plan/`** — CRUD+UI DAG template parameterization, cost estimation.

8. **Add `bchad index` and `bchad onboard` CLI subcommands** once the intelligence layer is functional.

9. **Add `golangci-lint` to CI** (GitHub Actions workflow) when the repo is pushed to the remote.
