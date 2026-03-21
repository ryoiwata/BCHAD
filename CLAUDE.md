# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

BCHAD (Batch Code Harvesting, Assembly, and Deployment) is a software factory that transforms feature specifications into complete, tested, deployable pull requests — matching existing codebase conventions across multiple products, languages, and frameworks. It is an internal tool for Athena Digital, a 180-person product studio operating seven SaaS products across fintech, healthtech, and logistics.

**Stack:** Go 1.22 · Chi router · Temporal · PostgreSQL 16 + pgvector · Valkey 8 · S3/MinIO · Anthropic Claude API (direct HTTP, no SDK) · Voyage AI API · Next.js/React · Docker · ECS Fargate · Terraform

**Do not suggest switching frameworks or languages.** Go is the core runtime. Next.js is the web UI. Temporal is the orchestrator. These choices are deliberate and documented in `docs/bchad-techstack.md`.

## Commands

### Build & Run
```bash
go build -o bin/bchad ./cmd/bchad              # Build the CLI binary
go build -o bin/worker ./cmd/worker             # Build the Temporal worker binary
./bin/bchad --help                              # CLI usage
./bin/bchad run --spec examples/payment-methods.json  # Run a pipeline
./bin/bchad index --repo <repo-url>             # Index a codebase
./bin/bchad onboard --product <name>            # Interactive product onboarding
./bin/bchad validate --product <name> --pattern crud_ui  # Validation generation
```

### Testing
```bash
go test ./...                                   # All unit tests (no API key needed)
go test -v ./internal/spec/...                  # Spec parser tests
go test -v ./internal/plan/...                  # Plan generator tests
go test -v ./internal/retrieval/...             # Retrieval service tests
go test -v ./internal/verify/...                # Verification gate tests
go test -v ./internal/budget/...                # Context budget allocator tests
go test -tags=integration ./...                 # Integration tests (needs ANTHROPIC_API_KEY)
go test -run TestPipelineE2E ./workflows/...    # End-to-end pipeline test (needs API keys + local stack)
go test ./workflows/...                         # Temporal workflow replay tests (deterministic, no server)
```

### Linting & Formatting
```bash
golangci-lint run                               # Lint
gofumpt -w .                                    # Format (gofumpt, not gofmt)
go vet ./...                                    # Vet
```

### Frontend
```bash
cd web && npm ci                                # Install frontend deps
cd web && npm run dev                           # Dev server with hot reload
cd web && npm run build                         # Production build
```

### Local Development Stack
```bash
just dev-up                                     # Start Postgres+pgvector, Valkey, MinIO, Temporal dev server
just dev-down                                   # Stop local infrastructure
just migrate                                    # Run database migrations
just migrate-down                               # Rollback last migration
just seed                                       # Seed test codebase profile (payments-dashboard-test)
just test                                       # Run all tests
just test-unit                                  # Unit tests only
just test-int                                   # Integration tests (requires local stack)
just test-e2e                                   # Full pipeline end-to-end
just test-snapshot                              # Prompt template snapshot tests
just lint                                       # golangci-lint
just fmt                                        # gofumpt + prettier
just build                                      # Build CLI and worker binaries
just snapshot-update                            # Update snapshot tests
```

### Docker
```bash
docker build -t bchad .                         # Build image (multi-stage: Go + Node)
docker compose up                               # Local dev stack (Postgres, Valkey, MinIO, Temporal)
```

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `BCHAD_DATABASE_URL` | Yes | — | Postgres connection string |
| `BCHAD_VALKEY_URL` | Yes | — | Valkey host:port |
| `BCHAD_S3_ENDPOINT` | Yes | — | S3/MinIO endpoint |
| `BCHAD_S3_BUCKET_PROFILES` | Yes | `bchad-codebase-profiles` | Codebase profiles bucket |
| `BCHAD_S3_BUCKET_ARTIFACTS` | Yes | `bchad-artifacts` | Pipeline artifacts bucket |
| `BCHAD_TEMPORAL_HOST` | Yes | — | Temporal server host:port |
| `BCHAD_TEMPORAL_NAMESPACE` | Yes | `bchad` | Temporal namespace |
| `ANTHROPIC_API_KEY` | Yes | — | Claude API key for LLM calls |
| `VOYAGE_API_KEY` | Yes | — | Voyage AI API key for code embeddings |
| `GITHUB_TOKEN` | Yes | — | GitHub token for repo access and PR creation |
| `BCHAD_LOG_LEVEL` | No | `info` | Logging level (debug, info, warn, error) |
| `BCHAD_COST_THRESHOLD` | No | `10.00` | Pause plan if projected cost exceeds this |
| `BCHAD_TIER2_TIMEOUT` | No | `15m` | Integration gate timeout |

## Project Structure

```
bchad/
├── cmd/
│   ├── bchad/                  # CLI entrypoint (Cobra)
│   └── worker/                 # Temporal worker entrypoint
├── internal/
│   ├── spec/                   # Spec parser, NL translator, BCHADSpec validation
│   ├── plan/                   # Plan generator, DAG templates, cost estimator
│   ├── engine/                 # DAG execution engine, stage dispatcher
│   ├── gateway/                # LLM gateway (model routing, token budgeting, cost tracking)
│   ├── retrieval/              # Retrieval service (pgvector queries, context ranking)
│   ├── intelligence/           # Codebase indexer, pattern extractor, convention scanner
│   ├── verify/                 # Verification gate runner, error classifier
│   ├── assembly/               # PR assembler (branch, commits, description)
│   ├── budget/                 # Context Budget Allocator (token partitioning)
│   ├── trust/                  # Trust score computation, phase transitions
│   └── adapters/               # Language adapters (TypeScript, Python, Go)
├── pkg/
│   ├── bchadspec/              # BCHADSpec schema types and validation
│   ├── bchadplan/              # BCHADPlan schema types and validation
│   └── artifacts/              # Stage artifact types
├── workflows/                  # Temporal workflow and activity definitions
├── patterns/                   # Pattern library (DAG templates, prompt templates)
├── schemas/                    # JSON Schema definitions (Draft 2020-12)
├── migrations/                 # Postgres schema migrations (golang-migrate)
├── semgrep/                    # Custom Semgrep rules for security verification
├── docker/                     # Verification gate container images
├── web/                        # Next.js review UI
├── terraform/                  # Infrastructure as code (AWS ECS Fargate)
├── docs/                       # Architecture docs, tech stack, framework, PRD
├── docker-compose.yml          # Local development stack
├── justfile                    # Task runner for dev workflows
└── go.mod
```

## Architecture

### Four Planes

**Control Plane** — Spec parser, plan generator, DAG execution engine (Temporal workflows), state store (PostgreSQL). Manages the lifecycle of every pipeline run.

**Intelligence Plane** — Codebase index (structural profiles in S3, code pattern embeddings in pgvector), pattern library (DAG templates, prompt templates, language adapters in Git repo), retrieval service (filtered vector search within token budgets).

**Execution Plane** — Stage executor (prompt assembly via Context Budget Allocator, LLM calls via in-process gateway), verification gates (Tier 1 per-stage in Docker containers, Tier 2 integration via full CI), error classifier, PR assembler.

**Integration Plane** — GitHub API (repo operations, PR creation, webhooks), CI runner, Vault (credential patterns), Slack (notifications, approvals).

### Pipeline Flow

```
Feature spec → Parse → Plan (DAG) → Generate per stage → Verify per stage → Assemble PR
                                          ↑                      |
                                          |                      ↓
                                    Codebase intelligence    Error classify → retry or escalate
```

Each stage: retrieve context → assemble prompt within token budget → call LLM → parse output → run Tier 1 verification gate → classify errors → retry or advance → persist state.

### Temporal Workflow Mapping

| Temporal Concept | BCHAD Mapping |
|---|---|
| Workflow | One workflow per pipeline run (spec → PR) |
| Activity | One activity per stage (migrate, api, frontend, tests, config) + PR assembly + Tier 2 gate |
| Signal | Human approvals (plan review, migration approval, sensitive stage approval) |
| Query | Pipeline status checks (CLI and web UI) |
| Child Workflow | Tier 2 integration gate (own retry loop) |
| Timer | Stage timeouts, approval deadlines, cost-cap enforcement |

### Error Taxonomy

Eight error categories with differentiated recovery — this is not a simple retry loop:

| Category | Recovery | Max Retries |
|---|---|---|
| Syntax | Direct retry with error | 3 |
| Style | Auto-fix via linter, else retry | 3 |
| Type | Retry with correct type context | 3 |
| Logic | Retry with failing test + corrective example | 2 |
| Context | Re-retrieve codebase examples, then retry | 2 |
| Conflict | Retry with conflict information | 2 |
| Security | Retry once with security pattern, then escalate | 1 |
| Specification | Surface to engineer immediately | 0 |

### Data Storage

| Store | Technology | Contents |
|---|---|---|
| State store | PostgreSQL 16 | Pipeline runs, stage transitions, approvals, errors, trust scores, metrics, prompt audit log |
| Vector store | pgvector (in PostgreSQL) | Code pattern embeddings (Voyage Code 3, 1024-dim), file structure embeddings, arch decision embeddings |
| Object storage | S3 (MinIO local) | Codebase profiles, generated artifacts, prompt logs, cloned repo snapshots |
| Cache & messaging | Valkey 8 | Token count cache, retrieval result cache, rate limiting, event streaming (Valkey Streams) |

## Key Design Decisions

- **Temporal for orchestration** — durable workflows with crash recovery, parallel fan-out/fan-in, approval signals, per-activity retry policies. Don't build a custom state machine.
- **In-process LLM gateway** — Go library, not a separate service. Throughput is bounded by pipeline parallelism (handful of concurrent calls). Eliminates a network hop and failure mode.
- **pgvector in PostgreSQL** — corpus is small (tens of thousands of embeddings). Consolidation eliminates a service. Filtered vector search via standard SQL WHERE + vector distance.
- **Valkey Streams for messaging** — replaces a dedicated message broker. Forge's messaging volume is dozens of features/day, not millions of events/second.
- **Five-layer prompt structure** — system prompt → language adapter context → codebase brief (retrieved examples) → upstream stage outputs → generation instruction. Every stage follows this template.
- **Context Budget Allocator** — partitions token budget across prompt sections by priority. Fills fixed sections first, then high-priority (upstream outputs, primary examples), then medium (secondary examples, arch notes). Truncates gracefully.
- **Two-tier verification** — Tier 1 (per-stage, fast, scoped: lint, typecheck, tests, security scan in Docker containers) + Tier 2 (integration, slow: full CI pipeline against assembled PR in ECS Fargate tasks).
- **Data-driven trust model** — trust score per-engineer per-product, based on CI pass rate, edit volume, retry rate, override count, time-to-merge. Phases: Supervised → Gated → Monitored.

## Testing

- **Framework:** Go `testing` package + `gotestsum` for output formatting. No testify.
- **Pattern:** Table-driven tests with `t.Run` subtests.
- **Workflow tests:** Temporal Go test framework — deterministic workflow replay without a running server.
- **Integration tests:** `testcontainers-go` for real Postgres/Valkey. LLM gateway with mocked API.
- **Snapshot tests:** `cupaloy` for prompt templates, plan generation output, PR descriptions.
- **End-to-end tests:** Full pipeline runs against a test repo (`payments-dashboard-test`), measuring CI pass rate.
- **Deterministic tests run on every commit. Integration tests before PR.**

## Git Workflow

### Conventional Commits

```
<type>(<scope>): <description>
```

**Types:** feat, fix, test, docs, refactor, chore, perf

**Scopes:** spec, plan, engine, gateway, retrieval, intelligence, verify, assembly, budget, trust, adapters, workflows, patterns, schemas, web, cli, docker, terraform, docs, tests

**Rules:**
- Lowercase type and description. No period at end.
- Imperative mood: "add", "fix", "update" — not "added", "fixes", "updated".
- Keep the first line under 72 characters.

**Examples:**
```
feat(spec): add NL-to-BCHADSpec translator with confirmation flow
feat(engine): implement DAG execution with parallel stage dispatch
feat(verify): add error classifier with eight-category taxonomy
feat(retrieval): implement filtered vector search with token budget ranking
feat(gateway): add cost tracking and rate limiting via Valkey
test(budget): add table-driven tests for context budget allocator
test(workflows): add deterministic replay test for approval gate blocking
fix(assembly): handle merge conflicts in per-stage commit strategy
chore(docker): add verification gate container images for TypeScript
docs: update bchad-framework-v2.md with revised error taxonomy
```

### Commit Cadence

One logical unit of work = one commit. Don't batch unrelated changes. Don't commit half-finished features.

### Auto-Commit Behavior

**After every meaningful change, Claude Code MUST `git add` all relevant files and `git commit` with a conventional commit message.** Do not wait for the user to ask. Do not accumulate uncommitted changes across multiple tasks.

**Commit workflow:**
1. Complete the logical unit of work
2. Run `go vet ./...` and `golangci-lint run` — fix any issues before committing
3. Run relevant tests (`go test -v ./internal/<package>/...`) — do not commit failing tests
4. `git add` all changed files related to this unit of work
5. `git commit -m "<type>(<scope>): <description>"`

**Do NOT commit:**
- Files that should be gitignored (see .gitignore rules)
- Failing tests or code that doesn't pass vet/lint
- Unrelated changes bundled into one commit
- Temporary debug code or fmt.Println statements

## Rules

- Read `docs/bchad-framework-v2.md` before implementing any component — it defines the architecture, data flows, and interface schemas
- Read `docs/bchad-techstack.md` for technology choices and Go module dependencies
- Read `docs/PRD.md` for functional requirements (FR-1 through FR-42) and non-functional requirements
- Every interface between components uses a versioned JSON schema (BCHADSpec, BCHADPlan, StageArtifact, GateResult) — validate at every boundary
- Temporal workflows define the pipeline lifecycle — stage ordering, retry policies, approval gates, crash recovery are all Temporal concerns, not hand-built state machines
- The LLM gateway is an in-process Go library, not a separate service — it shares the control plane's Postgres and Valkey connections
- Use `pgx/v5` for Postgres, `valkey-go` for Valkey, `go-git/v5` for Git, `go-tree-sitter` for AST parsing
- Use `context.Context` as the first parameter for any function that does I/O, calls the LLM, or queries a database
- All errors are wrapped with context: `fmt.Errorf("stage %s attempt %d: %w", stageID, attempt, err)`
- Use `log/slog` with structured fields and OpenTelemetry span context injection
- Never log API keys, full prompt/response bodies, or raw codebase contents — log token counts, costs, latencies, stage statuses
- Verification gates run in disposable Docker containers using the target product's toolchain — never run verification in the control plane process
- Error classification drives retry strategy — don't use undifferentiated retry loops
- The Context Budget Allocator fills prompt sections in priority order and truncates gracefully — never exceed the model's context window
- Generated code must pass Semgrep security rules before advancing — credential leaks, missing auth, sensitive data exposure are gate failures
- Trust scores are per-engineer per-product — a senior engineer can be Phase 3 on one product and Phase 1 on another
- Database migrations always require human approval, regardless of trust phase
- Keep CLAUDE.md under 50 instructions — put details in reference docs
- Always use Context7 MCP to look up library/API documentation when doing code generation, setup, configuration, or referencing external dependencies — do not rely on training data for docs that may be stale
