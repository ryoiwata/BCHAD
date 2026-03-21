# BCHAD Tech Stack

**SF-2026-05 · Rev. 2 · Athena Digital**

*Technology selections for every component of the BCHAD software factory. Each choice maps to a specific architectural requirement from the BCHAD framework (SF-2026-03 Rev. 2). Rev. 2 of the tech stack reduces operational surface area by consolidating services (pgvector replaces Qdrant, Valkey replaces both Redis and NATS, ECS Fargate replaces EKS), shifts the core runtime to Go for faster iteration and ecosystem alignment, and moves the LLM gateway from a standalone service to an in-process library.*

---

## Stack at a Glance

| Layer | Technology | Role |
|---|---|---|
| **Core Runtime** | Go | Control plane, DAG engine, LLM gateway library, CLI |
| **API & Web Layer** | Go (Chi) + TypeScript/Next.js | Internal API + Review UI |
| **Orchestration** | Temporal | DAG execution, retries, human-in-the-loop workflows |
| **State Store** | PostgreSQL 16 | Pipeline state, audit trail, trust scores |
| **Vector Store** | pgvector (in PostgreSQL) | Code pattern embeddings for retrieval |
| **Object Storage** | S3 (MinIO for local dev) | Codebase profiles, generated artifacts, prompt logs |
| **Cache & Message Broker** | Valkey 8 | Retrieval cache, token counts, rate limiting, event streaming |
| **LLM Gateway** | In-process Go library over Anthropic API | Model routing, token budgeting, cost tracking |
| **Code Analysis** | go-tree-sitter + language-specific LSPs | AST parsing, convention extraction, import resolution |
| **Git Operations** | go-git + GitHub API (go-github) | Repo cloning, diffing, branch management, PR creation |
| **Containerization** | Docker | Tier 1 and Tier 2 verification gates |
| **Container Orchestration** | ECS Fargate | Service deployment, verification task scheduling |
| **Secrets** | AWS Secrets Manager | API keys, credentials, SOC 2 audit trail |
| **Observability** | OpenTelemetry + Grafana + Loki + Tempo | Metrics, logs, traces across the full pipeline |
| **Security Scanning** | Semgrep + Trivy | Generated code SAST, container image scanning |
| **Schema Validation** | JSON Schema (Draft 2020-12) + Zod | BCHADSpec/BCHADPlan validation at every boundary |
| **Infrastructure** | Terraform + AWS | Reproducible, auditable infrastructure |

---

## 1. Core Runtime: Go

**What it runs:** The control plane, DAG execution engine, spec parser, plan generator, context budget allocator, error classifier, LLM gateway (as an in-process library), and CLI. Everything except the web UI.

**Why Go:**

BCHAD's core loop is: retrieve context → assemble a prompt within a token budget → call an LLM → parse output → run verification → persist state → repeat. This is overwhelmingly I/O-bound. The dominant latency in every pipeline run is waiting for LLM API calls (seconds to tens of seconds per stage), waiting for CI verification, and waiting for humans to approve things. The CPU-intensive bursts — token counting, AST parsing, diff computation — are measured in milliseconds. The runtime's job is to manage concurrent I/O efficiently, not to optimize CPU throughput.

Go's goroutines and channels are a natural fit for the DAG engine: parallel stage dispatch maps to goroutines, fan-out/fan-in maps to `sync.WaitGroup` or channel select, and the concurrency model is explicit and debuggable. There is no async coloring problem — every function can block without infecting its callers with `async`.

Go is one of Athena's three production languages. Engineers across the seven product squads can read, review, and contribute to the factory without learning a new language. This matters because BCHAD is an internal tool that the people using it also need to trust, inspect, and occasionally fix.

Temporal's Go SDK is the project's primary SDK — the most mature, best documented, and most battle-tested. The BCHAD pipeline workflow, stage activities, signal handlers, and query handlers are all idiomatic Go with full type safety and native integration. There is no FFI boundary between the orchestration layer and the rest of the control plane.

Go compiles fast (sub-second incremental builds), produces a single static binary (simple container images, simple deploys), and has a small runtime footprint. The garbage collector's sub-millisecond pauses are irrelevant at BCHAD's scale — the system processes dozens of features per day, not millions of events per second.

**Key modules:**

| Module | Purpose |
|---|---|
| `github.com/go-chi/chi/v5` | HTTP router for the control plane API (plan submission, approvals, status queries) |
| `encoding/json` + `github.com/invopop/jsonschema` | Serialization and schema validation for BCHADSpec, BCHADPlan, stage artifacts |
| `github.com/jackc/pgx/v5` | Native PostgreSQL driver with connection pooling, COPY support, and pgvector integration |
| `net/http` (stdlib) | HTTP client for LLM API calls, GitHub API, external integrations |
| `github.com/smacker/go-tree-sitter` | Multi-language AST parsing for codebase indexing and convention extraction (CGo bindings) |
| `github.com/go-git/go-git/v5` | Pure-Go Git implementation — clone, diff, branch, commit without shelling out to `git` |
| `github.com/pkoukk/tiktoken-go` | Token counting for context budget allocation (cl100k_base encoding) |
| `github.com/docker/docker/client` | Docker Engine API client for spinning up verification containers |
| `github.com/santhosh-tekuri/jsonschema/v6` | JSON Schema Draft 2020-12 validation at every component boundary |
| `go.opentelemetry.io/otel` | Distributed tracing and metrics across the pipeline |
| `log/slog` (stdlib) | Structured logging with OpenTelemetry span context injection |
| `github.com/spf13/cobra` | CLI framework for the `bchad` command-line interface |
| `github.com/valkey-io/valkey-go` | Valkey client for caching, rate limiting, and event streaming |
| `go.temporal.io/sdk` | Temporal workflow and activity SDK (primary Go SDK) |

---

## 2. Orchestration: Temporal

**What it runs:** The DAG execution workflow — stage ordering, parallel dispatch, retry policies, human approval gates, timeout handling, and crash recovery.

**Why Temporal over a hand-built state machine:**

The DAG execution engine is the most complex component in BCHAD. It needs to: run stages in parallel where dependencies allow, pause indefinitely at human approval gates (migration approval might sit for hours), retry with category-specific limits and backoff, resume after crashes without re-running completed stages, and inject upstream outputs into downstream prompts. This is exactly what Temporal is built for.

Temporal workflows are durable — the workflow state survives process crashes, deployment rollouts, and infrastructure failures. A BCHAD pipeline that's halfway through generation doesn't lose work when the host restarts. Each stage is a Temporal activity with its own retry policy derived from the error taxonomy: syntax errors get 3 retries with immediate backoff, logic errors get 2 retries with longer backoff, specification errors get 0 retries and signal the workflow to pause for human input. Human approval gates are Temporal signals — the workflow blocks until the approval signal arrives, with no polling.

The alternative is building a custom state machine backed by Postgres. This is what most teams do, and it works until you need durable timers (timeout a stage after 10 minutes), parallel fan-out/fan-in (run migrate and config simultaneously, wait for both before tests), and saga-style compensation (if the frontend stage fails after the API stage succeeded, don't roll back the API stage but do mark it for potential re-run). Temporal provides all of these out of the box.

**Temporal architecture within BCHAD:**

| Temporal Concept | BCHAD Mapping |
|---|---|
| Workflow | One workflow per BCHAD pipeline run (spec → PR) |
| Activity | One activity per stage (migrate, api, frontend, tests, config) + PR assembly + Tier 2 gate |
| Signal | Human approvals (plan review, migration approval, sensitive stage approval) |
| Query | Pipeline status checks (for the CLI and web UI) |
| Child Workflow | Tier 2 integration gate (runs as a child workflow with its own retry loop) |
| Timer | Stage timeouts, approval deadlines, cost-cap enforcement |
| Search Attributes | product, pattern, engineer, trust_phase — for filtering and dashboarding |

**SDK:** Temporal Go SDK (`go.temporal.io/sdk`). This is Temporal's primary and most mature SDK. The Go worker processes activities natively with goroutines, sharing the same process as the rest of the control plane. Workflow definitions use standard Go control flow — `if`, `for`, `select` — with Temporal handling durability transparently. The SDK's test framework supports deterministic workflow replay for unit testing the DAG execution logic without a running Temporal server.

---

## 3. State Store: PostgreSQL 16

**What it stores:** Every pipeline run, every stage transition, every approval decision, every error classification, every trust score update, every cost measurement. The complete audit trail. Also serves as the vector store for codebase intelligence (via pgvector).

**Why Postgres:**

BCHAD's state is relational: runs contain stages, stages produce artifacts, artifacts have verification results, results feed error classifications, classifications trigger retries or approvals. Foreign keys enforce these relationships. Postgres JSONB columns store the semi-structured data (BCHADSpec, BCHADPlan, stage artifacts, prompt logs) alongside the relational structure without requiring a separate document store.

SOC 2 compliance requires an auditable trail of every factory action. Postgres's MVCC and WAL provide the durability guarantees. Row-level security can restrict access to compliance-regulated product data.

With the pgvector extension, Postgres also serves as the vector store for codebase intelligence embeddings, eliminating the need for a dedicated vector database. This consolidation is feasible because BCHAD's embedding corpus is small — hundreds to low thousands of code patterns per product, tens of thousands total — well within pgvector's performant range with HNSW indexing.

**Schema design:**

```sql
-- Core pipeline state
bchad_runs          (id, product_id, pattern, spec_json, plan_json, status, 
                     projected_cost, actual_cost, created_at, completed_at)
bchad_stages        (id, run_id, stage_type, status, model, attempt_count, 
                     input_artifact_ids, output_artifact_id, cost, started_at, 
                     completed_at)
bchad_artifacts     (id, stage_id, artifact_type, content_hash, s3_path, 
                     token_count, created_at)

-- Verification and errors
bchad_gate_results  (id, stage_id, attempt_number, tier, passed, 
                     checks_json, error_output, duration_ms)
bchad_error_log     (id, stage_id, attempt_number, category, raw_error, 
                     recovery_strategy, resolved, created_at)

-- Human interaction
bchad_approvals     (id, stage_id, engineer_id, decision, guidance_note, 
                     decided_at)

-- Trust and metrics
bchad_trust_scores  (id, engineer_id, product_id, score, phase, 
                     signal_weights_json, last_10_runs_json, updated_at)
bchad_metrics       (id, run_id, stage_id, metric_name, metric_value, 
                     recorded_at)

-- Prompt audit
bchad_prompt_log    (id, stage_id, attempt_number, prompt_version, 
                     model, input_tokens, output_tokens, cost, 
                     prompt_hash, response_hash, latency_ms, created_at)

-- Vector store (codebase intelligence)
bchad_code_patterns (id, product_id, stage_type, language, entity_type,
                     has_permissions, has_audit, has_integrations,
                     pr_quality_score, content_text, metadata_json,
                     embedding vector(1024), last_updated)

bchad_file_structures (id, product_id, framework, language,
                       structure_text, embedding vector(1024))

bchad_arch_decisions  (id, product_id, decision_category, content_text,
                       embedding vector(1024))
```

**Extensions:**

| Extension | Purpose |
|---|---|
| `pgvector` | HNSW-indexed vector similarity search for codebase intelligence retrieval |
| `pg_cron` | Weekly codebase re-index trigger |
| `pg_stat_statements` | Query performance monitoring |

**pgvector indexing:**

```sql
-- HNSW indexes for fast approximate nearest neighbor search
CREATE INDEX ON bchad_code_patterns 
  USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 128);

CREATE INDEX ON bchad_file_structures 
  USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);

CREATE INDEX ON bchad_arch_decisions 
  USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);

-- Composite indexes for filtered vector search
CREATE INDEX ON bchad_code_patterns (product_id, stage_type);
CREATE INDEX ON bchad_code_patterns (product_id, language, entity_type);
```

Filtered vector search — the primary retrieval pattern — uses a standard Postgres query combining WHERE clauses with vector similarity ordering:

```sql
SELECT id, content_text, metadata_json,
       1 - (embedding <=> $1) AS similarity
FROM bchad_code_patterns
WHERE product_id = $2 
  AND stage_type = $3
  AND has_permissions = $4
ORDER BY embedding <=> $1
LIMIT 5;
```

At BCHAD's corpus size (tens of thousands of rows), Postgres executes this in single-digit milliseconds with the HNSW index. The query planner combines the B-tree filter with the vector index efficiently. If the corpus grows by an order of magnitude, a dedicated vector store can be introduced later without changing the retrieval service interface.

**Embedding model:** Voyage Code 3 (via Voyage AI API). Purpose-built for code retrieval with strong cross-language understanding. Embeddings are 1024-dimensional. The Context Budget Allocator uses the similarity score as the ranking signal for priority-based context filling.

---

## 4. Object Storage: S3 (MinIO for Local Dev)

**What it stores:** Codebase structural profiles (JSON), generated file artifacts, prompt logs (full prompt text for audit), cloned repo snapshots, and large stage outputs that exceed Postgres JSONB practical limits.

**Why S3:**

BCHAD generates a lot of artifacts that need to be durable but don't need to be queried relationally: the full text of every prompt sent to the LLM (for audit and prompt debugging), the generated code files before they're assembled into a PR, the codebase structural profiles (which can be multi-MB JSON files), and the linter/formatter configs copied from target repos. S3's durability (11 nines), versioning (for codebase profile history), and lifecycle policies (auto-archive old prompt logs) are the right fit.

**Bucket structure:**

```
bchad-codebase-profiles/
  {product_id}/
    structural_profile.json
    dependency_graph.json
    arch_decisions.json
    style_configs/                 # copied linter, formatter, tsconfig files
    
bchad-artifacts/
  {run_id}/
    {stage_id}/
      attempt_{n}/
        generated_files/           # the actual code output
        prompt.txt                 # full prompt text (audit)
        response.txt               # full LLM response (audit)
        gate_results.json          # verification output
        
bchad-pattern-snapshots/
  {product_id}/
    {stage_type}/
      {pattern_id}.annotated.json  # canonical code examples with metadata
```

**MinIO** provides an S3-compatible local development experience so engineers can run BCHAD locally without an AWS dependency. The application code uses the same `aws-sdk-go-v2` S3 client for both.

---

## 5. Cache & Message Broker: Valkey 8

**What it handles:** Token count caching, retrieval result caching, rate limiting, distributed locking, and asynchronous event streaming — combining the responsibilities that would otherwise require separate cache and message broker services.

**Why Valkey (and why one service instead of two):**

Valkey is the Linux Foundation fork of Redis, wire-compatible with Redis clients and commands, with an open-source license and active development. It provides the same sub-millisecond key-value operations for caching and rate limiting, plus Valkey Streams for durable, consumer-group-based event processing.

BCHAD's messaging needs are modest: dozens of features per day, with events flowing between the control plane, the web UI, and integration endpoints (Slack, GitHub webhooks). This doesn't require a dedicated message broker. Valkey Streams provides durable, at-least-once delivery with consumer groups, which covers every messaging use case BCHAD has: stage completion events, approval notifications, real-time UI updates, and re-index scheduling.

Consolidating cache and messaging into a single service eliminates an entire component from the deployment topology — one fewer service to provision, monitor, back up, upgrade, and debug at 2am.

**Caching (key-value):**

| Key Pattern | Type | TTL | Purpose |
|---|---|---|---|
| `tokens:{text_hash}` | String (integer) | 24h | Cached token counts |
| `retrieval:{product}:{stage}:{features_hash}` | String (JSON) | Until next re-index | Cached vector retrieval results |
| `rate:{model}:{window}` | String (counter) | 1 minute | LLM API rate limiting |
| `cost:{run_id}` | String (float) | Run lifetime | Running cost accumulator |
| `lock:stage:{stage_id}` | String | 30 min | Distributed lock for stage execution |

**Event streaming (Valkey Streams):**

| Stream | Consumer Group | Purpose |
|---|---|---|
| `bchad:run:{run_id}:events` | `webui`, `slack` | Stage status changes, approval requests, completion — the web UI and Slack bot each consume independently |
| `bchad:index:events` | `indexer` | Post-merge re-index triggers from GitHub webhooks |
| `bchad:metrics` | `metrics-writer` | Async metric recording to Postgres (decoupled from the hot path) |

**Event structure:**

Each event in the stream is a hash with a `type` field and event-specific payload fields:

```
XADD bchad:run:{run_id}:events * type stage.started stage_id migrate
XADD bchad:run:{run_id}:events * type gate.passed stage_id api tier 1
XADD bchad:run:{run_id}:events * type approval.requested stage_id migrate
XADD bchad:run:{run_id}:events * type run.completed status success
```

The web UI subscribes to `bchad:run:{run_id}:events` via XREAD with a blocking read, bridged to WebSocket connections via the control plane API. Slack integration uses XREADGROUP on the `slack` consumer group to process approval requests.

For real-time UI updates where guaranteed delivery isn't required (e.g., progress animations), Valkey Pub/Sub channels complement Streams:

```
SUBSCRIBE bchad:run:{run_id}:progress
```

---

## 6. LLM Gateway: In-Process Go Library

**What it does:** Routes LLM calls to the correct model per stage configuration, enforces the context budget, tracks token consumption and cost per call, handles retries on transient API errors, logs every prompt/response pair for audit, and applies rate limiting.

**Why an in-process library instead of a separate service:**

Every stage executor in BCHAD calls the LLM, but the calling conventions differ: the migrate stage uses Haiku 3.5 with a 25K-token context, the frontend stage uses Sonnet 4 with a 70K-token context, and retries inject additional error context layers. The gateway centralizes model routing, token accounting, and cost tracking so that stage executors focus on prompt content, not API mechanics.

The gateway's throughput is bounded by the pipeline's parallelism — at most a handful of concurrent LLM calls. It doesn't need independent scaling, independent deployment, or its own failure domain. Running it as a Go package within the control plane eliminates a network hop, simplifies deployment (one fewer ECS service), and removes a failure mode (gateway unavailability can't independently break the pipeline).

The cost guardrails are enforced in-process: if a run's accumulated cost exceeds the plan's projected cost by more than 2×, the gateway rejects further calls and the Temporal workflow pauses.

**Gateway interface:**

```go
type Gateway struct {
    anthropicClient *anthropic.Client
    voyageClient    *voyage.Client
    costTracker     *CostTracker      // backed by Valkey + Postgres
    rateLimiter     *RateLimiter      // backed by Valkey
    promptLogger    *PromptLogger     // writes to S3 + Postgres
    tokenCounter    *TokenCounter     // with Valkey cache
}

type GenerateRequest struct {
    RunID       string
    StageID     string
    Model       ModelConfig          // model, max tokens, temperature
    Messages    []Message
    Budget      TokenBudget          // max input tokens, reserved output tokens
}

type GenerateResponse struct {
    Content      string
    InputTokens  int
    OutputTokens int
    Cost         float64
    Latency      time.Duration
}
```

**Gateway responsibilities:**

| Responsibility | Implementation |
|---|---|
| Model routing | Stage config specifies model; gateway resolves to API endpoint and parameters |
| Token budgeting | Gateway validates that the assembled prompt is within the model's effective ceiling before sending |
| Cost tracking | Input/output token counts × model pricing, accumulated per stage and per run, written to Postgres via Valkey accumulator |
| Prompt logging | Full prompt text and response hashed and stored in S3, references stored in `bchad_prompt_log` |
| Retry on transient errors | Exponential backoff on 429 (rate limit) and 5xx errors, separate from the error taxonomy retries |
| Rate limiting | Valkey-backed per-model rate counters to stay within API quotas |
| Streaming | Streams responses to the stage executor for long generations (frontend stage), with incremental token counting |

**Models configured:**

| Model | API Identifier | Primary Use |
|---|---|---|
| Claude Haiku 3.5 | `claude-haiku-3-5-sonnet-latest` | migrate, config stages |
| Claude Sonnet 4 | `claude-sonnet-4-20250514` | api, frontend, tests stages, NL spec translation |
| Voyage Code 3 | `voyage-code-3` (Voyage AI API) | Code pattern embedding for codebase intelligence |

---

## 7. Code Analysis: go-tree-sitter + Language Server Protocol

**What it does:** Parses source code into ASTs for convention extraction (during indexing), import resolution (during pre-generation validation), route conflict detection (during API stage verification), and structural pattern matching (during retrieval ranking).

**Why Tree-sitter:**

BCHAD's codebase intelligence layer needs to parse code in TypeScript, Python, and Go without requiring each language's full compiler toolchain. Tree-sitter provides incremental, error-tolerant parsing across all three languages from a single library. It produces concrete syntax trees that BCHAD can query for structural patterns (e.g., "find all Express route handler registrations in this file"), and handles incomplete or syntactically invalid code — which is important because the factory needs to analyze generated code that might have syntax errors before routing to the correct error recovery strategy.

**Go bindings:** The `go-tree-sitter` package (`github.com/smacker/go-tree-sitter`) provides CGo bindings to the Tree-sitter C library. The CGo overhead is negligible for BCHAD's use case — AST parsing runs during indexing (background) and verification (not latency-critical). Each language grammar is a separate Go module (`github.com/smacker/go-tree-sitter/typescript`, `/python`, `/golang`).

**Language-specific analysis tools (used alongside Tree-sitter):**

| Language | Tool | Purpose |
|---|---|---|
| TypeScript | `typescript-language-server` | Type checking (`tsc --noEmit`), import resolution, definition lookup |
| Python | `pyright` | Static type checking, import resolution, type inference |
| Go | `gopls` | Type checking (`go vet`), import resolution, unused dependency detection |
| SQL | `pganalyze/pg_query_go` | Postgres SQL parsing for migration validation, destructive operation detection |

The LSP servers run as sidecar processes in the verification containers, not in the control plane. Tree-sitter runs in-process for fast analysis. The split keeps the control plane lightweight while giving verification gates access to full language tooling.

**Tree-sitter queries used by BCHAD:**

| Query | Language | Purpose |
|---|---|---|
| Route handler extraction | TypeScript, Python, Go | Find all registered API routes for conflict detection |
| Import graph | TypeScript, Python, Go | Map dependencies for import availability checks |
| Middleware chain extraction | TypeScript, Python | Identify auth and permission middleware for security verification |
| Test assertion patterns | TypeScript, Python, Go | Classify test quality (meaningful vs. trivial assertions) |
| Component structure | TypeScript (JSX/TSX) | Extract React component hierarchy for UI pattern matching |
| Migration operations | SQL | Detect CREATE, ALTER, DROP for safety classification |

---

## 8. Git Operations: go-git + GitHub API

**What it does:** Clones target repos for indexing, creates branches for generated code, computes diffs for the review interface, assembles per-stage commits, and pushes PRs to GitHub.

**Why go-git:**

BCHAD performs Git operations on every pipeline run: clone the target repo (or fetch latest), create a feature branch, write generated files, commit per stage, and push. It also performs Git operations during indexing: analyze the last 20 merged PRs to extract code patterns. `go-git` (`github.com/go-git/go-git/v5`) is a pure-Go Git implementation that avoids shelling out to the `git` CLI (which is slow for programmatic use), doesn't depend on `libgit2`, and provides typed access to Git objects (commits, trees, diffs) without string parsing. It integrates naturally with Go's concurrency model — multiple repos can be cloned or analyzed concurrently with goroutines.

**GitHub API:** `go-github` (`github.com/google/go-github/v62`) for PR creation, webhook management, and repository metadata. The REST client handles PR creation and status checks. The GraphQL client (`github.com/shurcooL/githubv4`) is used for efficient bulk queries during indexing (fetching PR reviews, merge status, file changes across multiple PRs in a single request).

| Operation | Tool | Context |
|---|---|---|
| Repo clone / fetch | go-git | Indexing, pipeline start |
| Branch creation | go-git | Pipeline execution |
| File write + commit | go-git | Per-stage code generation |
| Diff computation | go-git | Review interface, pre-generation conflict detection |
| PR merge history analysis | GitHub GraphQL API | Codebase intelligence indexing |
| PR creation + description | GitHub REST API (go-github) | PR assembly |
| Webhook ingestion | GitHub Webhooks → Valkey Stream | Post-merge re-indexing trigger |

---

## 9. Containerization: Docker

**What it runs:** Tier 1 verification gates (per-stage lint, typecheck, unit tests, security scans) and Tier 2 integration gates (full CI pipeline against assembled PR).

**Why Docker for both tiers:**

Tier 1 gates run lightweight, scoped checks: lint, typecheck, unit tests, and security scans. They need to execute in seconds, use the target product's toolchain (ESLint config, tsconfig, pytest config), and be disposable. Docker containers are the natural fit — spin up a container with the product's toolchain pre-installed, mount the generated code, run the checks, capture output, tear down.

Tier 2 integration gates run the product's full CI pipeline: databases, API servers, dependent services, end-to-end tests. These are run as ECS Fargate tasks with multi-container task definitions. Fargate provides VM-level isolation under the hood (it runs on Firecracker microVMs) without BCHAD needing to manage Firecracker directly — the isolation boundary comes free with the infrastructure choice. For SOC 2 compliance, Fargate's hardware-enforced task isolation provides a clean audit story: a compromised integration test in the fintech product cannot affect the healthtech product's test environment.

**Container image management:**

| Image | Base | Contents | Used By |
|---|---|---|---|
| `bchad-verify-ts` | `node:20-slim` | TypeScript toolchain, ESLint, Prettier, Jest, product-specific configs mounted at runtime | Tier 1 gates for TypeScript products |
| `bchad-verify-py` | `python:3.12-slim` | Python toolchain, Ruff, mypy, pytest, product-specific configs mounted at runtime | Tier 1 gates for Python products |
| `bchad-verify-go` | `golang:1.22-alpine` | Go toolchain, golangci-lint, product-specific configs mounted at runtime | Tier 1 gates for Go products |
| `bchad-integration-{product}` | Product's own CI image | Full test environment per product's ECS task definition | Tier 2 integration gate |
| `bchad-security-scan` | `semgrep/semgrep:latest` + Trivy | SAST rules for credential detection, auth enforcement, sensitive data handling | Security verification (both tiers) |

Images are pre-built and stored in ECR. Product-specific configs (linter settings, tsconfig, formatter rules) are mounted at runtime from S3, not baked into the image, so a config change doesn't require an image rebuild.

---

## 10. Web UI: Next.js + React

**What it provides:** The engineer-facing review interface — plan review with DAG visualization, stage-level artifact inspection, inline diff previews, approval workflows, error trail exploration, trust score dashboard, and cost tracking.

**Why Next.js:**

The review UI is a real-time dashboard that needs: server-side rendering for initial load performance (an engineer opening a pipeline review shouldn't wait for a client-side data fetch), WebSocket connections for live pipeline updates (stage status changes, approval requests), and rich interactive components (DAG visualization, inline code diffs, side-by-side comparisons). Next.js provides the SSR + API routes + WebSocket support in a single framework.

Athena's frontend stack already includes React and Next.js (two of their seven products use these). Using the same framework for BCHAD's UI means the team building the factory can borrow UI patterns from the products it generates code for.

**Key UI components and libraries:**

| Component | Library | Purpose |
|---|---|---|
| DAG visualization | `@xyflow/react` (React Flow) | Interactive pipeline graph with stage status, timing, and cost overlays |
| Code diffs | `react-diff-viewer-continued` | Side-by-side and unified diff views for generated code vs. repo state |
| Syntax highlighting | `Shiki` | Code display in stage artifacts, codebase examples, and prompt inspection |
| Real-time updates | Native WebSocket → Valkey Streams bridge | Live stage status, approval notifications |
| Data tables | `@tanstack/react-table` | Metrics dashboard, run history, trust score tables |
| Charts | `Recharts` | Cost over time, CI pass rates, retry frequency |
| Forms | `React Hook Form` + `Zod` | Spec input, plan editing, approval forms (Zod validates BCHADSpec client-side) |
| Terminal rendering | `@xterm/xterm` | Embedded terminal for verification gate output, error logs |

**Real-time architecture:** The Next.js API routes maintain WebSocket connections to the browser. The server side consumes Valkey Streams via XREAD (blocking read on `bchad:run:{run_id}:events`) and forwards events to connected WebSocket clients. This replaces a dedicated message broker bridge with a simpler pattern — the API route reads directly from the same Valkey instance that the control plane writes to.

**API layer:** The Next.js API routes proxy to the Go control plane's Chi API. The web UI never connects to Postgres, Valkey, or S3 directly — the control plane is the single source of truth.

---

## 11. Security Scanning: Semgrep + Trivy

**What it checks:** Generated code for credential leaks, missing auth middleware, sensitive data exposure, insecure patterns, and dependency vulnerabilities.

**Why Semgrep:**

BCHAD's security verification (§7.6 of the framework) requires checking that: sensitive fields use Vault integration and are never stored in plain text, every endpoint includes auth middleware, audit logging is present on state-changing operations, and no hardcoded credentials appear in generated code. These are pattern-matching problems across multiple languages. Semgrep's pattern syntax is designed for exactly this — write a rule once and it works across TypeScript, Python, and Go.

**Custom Semgrep rules for BCHAD:**

```yaml
# Detect sensitive field returned without masking
- id: bchad-sensitive-field-exposure
  pattern: |
    res.json({ ..., vault_ref: $X, ... })
  message: "Sensitive field vault_ref returned in API response without masking"
  severity: ERROR

# Detect missing auth middleware on route handler
- id: bchad-missing-auth
  patterns:
    - pattern: router.$METHOD($PATH, $HANDLER)
    - pattern-not: router.$METHOD($PATH, authMiddleware, ..., $HANDLER)
  message: "Route handler registered without auth middleware"
  severity: ERROR

# Detect hardcoded credentials
- id: bchad-hardcoded-secret
  pattern-regex: "(api_key|secret|password|token|credential)\\s*[:=]\\s*['\"][^'\"]{8,}['\"]"
  message: "Possible hardcoded credential detected"
  severity: ERROR
```

**Trivy** scans the generated code's dependency manifest (package.json, requirements.txt, go.mod) for known vulnerabilities in any dependencies the generated code introduces. It also scans the verification container images themselves.

---

## 12. Schema Validation: JSON Schema + Zod

**What it validates:** Every data boundary in BCHAD — BCHADSpec (from spec parser to plan generator), BCHADPlan (from plan generator to DAG engine), stage artifacts (between stages), and API payloads (between the web UI and the control plane).

**Why both JSON Schema and Zod:**

JSON Schema (Draft 2020-12) is the canonical schema definition, stored in the Pattern Library Git repo alongside the DAG templates and prompt templates. It's language-agnostic and versioned. The Go control plane validates against JSON Schema using `santhosh-tekuri/jsonschema/v6`. The web UI validates using Zod schemas generated from the JSON Schema definitions (via `json-schema-to-zod`), giving type-safe validation on both sides of the API boundary without maintaining two separate schema definitions.

**Schemas defined:**

| Schema | Boundary | Validates |
|---|---|---|
| `BCHADSpec.v1.json` | Spec Parser output → Plan Generator input | Entity fields, pattern type, permissions, integrations, UI config |
| `BCHADPlan.v1.json` | Plan Generator output → DAG Engine input | Stage DAG, dependencies, models, cost estimates, approval gates |
| `StageArtifact.v1.json` | Stage output → downstream stage input | Schema definitions, endpoint contracts, component paths |
| `GateResult.v1.json` | Verification gate output → error classifier input | Pass/fail, check results, error output, duration |
| `LanguageAdapter.v1.json` | Pattern Library → Generation Engine | Framework map, verification toolchain, import style |
| `CodebaseProfile.v1.json` | Intelligence Plane → Plan Generator / Retrieval Service | Structural conventions, arch decisions, dependency graph |

---

## 13. Observability: OpenTelemetry + Grafana Stack

**What it monitors:** End-to-end pipeline traces, per-stage latency and cost, LLM API call performance, verification gate pass rates, error classification accuracy, retrieval quality, and infrastructure health.

**Why OpenTelemetry:**

BCHAD is a distributed pipeline: a single feature generation touches the control plane, the LLM gateway library, the retrieval queries, multiple verification containers, and GitHub. OpenTelemetry provides a single instrumentation standard across all of these components. The Go control plane uses the OTel Go SDK (`go.opentelemetry.io/otel`) with `log/slog` for structured logging, exporting traces and metrics via the OTLP protocol. Every LLM call, every retrieval query, every verification gate execution appears as a span in the same trace, tied to the pipeline run ID.

**Grafana stack deployment (on ECS Fargate):**

| Component | Role | Key Dashboards |
|---|---|---|
| **Grafana** | Visualization and alerting | Pipeline health, cost tracking, trust score progression, CI pass rate over time |
| **Loki** | Log aggregation | Stage execution logs, verification gate output, error classifier decisions |
| **Tempo** | Distributed tracing | End-to-end pipeline traces, LLM call latency, retrieval performance |
| **Prometheus** | Metrics collection | System metrics (CPU, memory), application metrics (stage duration, retry count, tokens consumed) |

**Key metrics exported:**

| Metric | Type | Labels |
|---|---|---|
| `bchad_pipeline_duration_seconds` | Histogram | product, pattern, status |
| `bchad_stage_duration_seconds` | Histogram | product, stage_type, model, attempt |
| `bchad_llm_tokens_total` | Counter | model, direction (input/output), stage_type |
| `bchad_llm_cost_dollars` | Counter | model, stage_type, run_id |
| `bchad_gate_pass_rate` | Gauge | product, stage_type, tier |
| `bchad_error_category_total` | Counter | category, stage_type, product |
| `bchad_retrieval_latency_seconds` | Histogram | product, stage_type |
| `bchad_trust_score` | Gauge | engineer_id, product_id |

**Alerting:** Grafana alerts fire when the rolling CI pass rate drops below 75% (approaching the 80% target), when per-run cost exceeds 3× the projected cost, when the LLM API error rate exceeds 5%, or when a codebase re-index fails.

---

## 14. Infrastructure: Terraform + AWS (ECS Fargate)

**What it provisions:** The complete BCHAD deployment — ECS Fargate cluster, RDS Postgres (with pgvector), S3 buckets, ElastiCache Valkey, Temporal Cloud, networking, IAM, and secrets management.

**Why ECS Fargate over EKS:**

BCHAD is a handful of long-running services (control plane, web UI, Temporal worker, Grafana stack) plus short-lived burst tasks (verification gates, indexing jobs). This workload doesn't benefit from Kubernetes's scheduling complexity, custom resource definitions, or operator ecosystem. ECS Fargate provides container orchestration with zero node management — no AMI updates, no kubelet debugging, no cluster autoscaler tuning. Fargate tasks run on Firecracker microVMs under the hood, providing hardware-level isolation between tasks without BCHAD managing Firecracker directly.

For verification gates, Fargate's `RunTask` API is a natural fit: spin up a task with the verification image, pass the generated code as an S3 reference, capture the output, tear down. The task inherits its IAM role, secrets, and networking from the task definition — no volume mounts, no init containers, no pod security policies.

Athena already uses Terraform for all product infrastructure. Using it for BCHAD means the infrastructure is reproducible, version-controlled, and reviewable by the same engineers who manage product infrastructure. It also means BCHAD's infrastructure passes the same SOC 2 audit process as the products it generates code for.

**AWS service mapping:**

| BCHAD Component | AWS Service | Config |
|---|---|---|
| Control Plane | ECS Fargate | 2 tasks (1 vCPU, 4 GB), behind ALB, auto-scaling on CPU |
| Temporal Worker | ECS Fargate | 2 tasks (1 vCPU, 2 GB), connected to Temporal Cloud |
| Web UI | ECS Fargate | 2 tasks (0.5 vCPU, 1 GB), behind ALB |
| Verification Gates | ECS Fargate (RunTask) | On-demand tasks (2 vCPU, 4 GB), ephemeral, per-stage |
| Indexing Jobs | ECS Fargate (RunTask) | On-demand tasks (1 vCPU, 2 GB), triggered by webhook or schedule |
| State Store + Vector Store | RDS PostgreSQL 16 + pgvector | db.r6g.large, Multi-AZ, automated backups, encryption at rest |
| Object Storage | S3 | Standard tier for artifacts, Intelligent-Tiering for prompt logs |
| Cache & Message Broker | ElastiCache Valkey 8 | cache.r6g.large, single-node (cluster mode disabled) |
| Orchestration | Temporal Cloud | Managed service — no self-hosted infrastructure |
| Secrets | AWS Secrets Manager | Native ECS integration, automatic rotation, audit via CloudTrail |
| Container Registry | ECR | Verification gate images, BCHAD service images |
| Networking | VPC + private subnets | All data-plane components in private subnets; only the ALB is internet-facing |
| Load Balancer | ALB | Routes to the Next.js web UI and the Go control plane API |
| DNS | Route 53 | `bchad.athena.internal` for internal services via Cloud Map |
| Monitoring | CloudWatch (baseline) + Grafana Stack on Fargate | CloudWatch for AWS-level metrics and ECS task logs; Grafana for application-level |

**Secrets management:** AWS Secrets Manager replaces a self-hosted secrets infrastructure for BCHAD's own secrets (API keys, database credentials, GitHub tokens). ECS Fargate has native Secrets Manager integration — secrets are injected as environment variables at task launch without application-level client code. CloudTrail provides the audit trail for secret access, meeting SOC 2 requirements. For product-level secrets (e.g., the Vault integration patterns that the generated code references), BCHAD still reads from Athena's existing HashiCorp Vault deployment — but BCHAD itself doesn't need to run or manage a Vault cluster.

**Environment parity:** Terraform modules define three environments — `dev` (reduced task sizes, local MinIO and Valkey via Docker Compose instead of AWS services), `staging` (full topology, synthetic data), and `production` (Multi-AZ RDS, encrypted, SOC 2 controls). The staging environment is where the validation protocol runs.

---

## 15. Development Tooling

**What developers working on BCHAD itself use:**

| Tool | Purpose |
|---|---|
| `go` toolchain + `gopls` | Build system and IDE integration |
| `docker compose` | Local development stack (Postgres + pgvector, Valkey, MinIO) |
| `go test` + `gotestsum` | Test runner with readable output, parallel execution |
| `air` | Auto-rebuild on file change during development |
| `golang-migrate/migrate` | Database migration management for the state store schema |
| `just` (justfile) | Task runner for common dev workflows (start stack, run tests, seed data, reset state) |
| `temporal-cli` | Local Temporal server for workflow development |
| `bruno` | API testing for the control plane endpoints (Git-versionable alternative to Postman) |
| `pre-commit` + `golangci-lint` + `gofumpt` | Code quality enforcement on commit |

**Testing strategy for BCHAD itself:**

| Test Level | Framework | What It Covers |
|---|---|---|
| Unit tests | `go test` | Spec parser logic, plan generation, error classification, context budget allocation, token counting |
| Integration tests | `gotestsum` + `testcontainers-go` | Stage executors against real Postgres/Valkey, LLM gateway with mocked API |
| Workflow tests | Temporal Go test framework | DAG execution ordering, parallel dispatch, approval gate blocking, retry policies |
| End-to-end tests | Custom harness | Full pipeline runs against a test repo (payments-dashboard-test), measuring CI pass rate |
| Snapshot tests | `cupaloy` | Prompt templates, plan generation output, PR descriptions — catch unintended changes |

---

## Dependency Summary

Total external dependencies, grouped by criticality:

**Must run (pipeline doesn't function without these):**
PostgreSQL 16 + pgvector, S3/MinIO, Valkey 8, Temporal, Docker, Anthropic API, Voyage AI API, GitHub API

**Should run (functionality degrades gracefully without these):**
Semgrep (security checks become manual), Grafana stack (observability degrades to CloudWatch logs)

**Development only:**
gotestsum, air, golang-migrate, just, temporal-cli, bruno, pre-commit, golangci-lint

---

## Comparison: Rev. 1 → Rev. 2

| Dimension | Rev. 1 | Rev. 2 | Change Rationale |
|---|---|---|---|
| Core runtime | Rust | Go | Faster iteration, native Temporal SDK maturity, existing team expertise, comparable I/O performance |
| Vector store | Qdrant (separate service) | pgvector (in Postgres) | Corpus is small enough; eliminates a service; filtered search via standard SQL |
| Cache | Redis 7 | Valkey 8 | Open-source fork, same capabilities, combined with message broker |
| Message broker | NATS JetStream | Valkey Streams | Modest throughput needs don't justify a dedicated broker; Valkey Streams provides durable consumer groups |
| LLM gateway | Separate Rust service | In-process Go library | Throughput doesn't justify independent scaling; eliminates a network hop and failure mode |
| Container orchestration | EKS (Kubernetes) | ECS Fargate | No node management; native Firecracker isolation; simpler for BCHAD's workload profile |
| Verification isolation | Docker + Firecracker | Docker on Fargate | Fargate provides VM-level isolation (Firecracker) transparently |
| Secrets | HashiCorp Vault (self-hosted) | AWS Secrets Manager | Native ECS integration; no Vault cluster to operate; CloudTrail for audit |
| Stateful services in production | 6 (Postgres, Qdrant, Redis, NATS, Temporal, Vault) | 3 (Postgres, Valkey, Temporal Cloud) | Half the operational surface for the same functional capability |

---

*SF-2026-05 · Rev. 2 · March 2026*
