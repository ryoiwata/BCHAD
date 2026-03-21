# BCHAD Implementation Plan

**SF-2026-06 · Athena Digital · March 2026**

*Phased implementation plan for BCHAD across three six-month increments. Based on the PRD (SF-2026-04), Framework (SF-2026-03), and Tech Stack (SF-2026-05). Team: 3–5 engineers, full-time dedicated. Rev. 2 incorporates feedback from design interview.*

---

## Planning Principles

**Intelligence first, execution second.** Codebase intelligence is the highest-risk component and the one whose failure mode is silent: bad patterns produce plausible-looking code that fails CI or fails code review. The indexer and retrieval service must be built and validated against both v1 products before the generation pipeline runs a single real feature.

**Build the test harness before the system.** Every component has well-defined inputs and outputs (versioned JSON schemas). Table-driven tests, snapshot tests, and Temporal replay tests are written alongside the implementation, not after. The 60-feature validation protocol is the acceptance test for the entire v1 system.

**Strictly sequential execution in v1.** The DAG runs stages one at a time in dependency order: migrate → config → api → frontend → tests. No parallel dispatch, even for independent stages (migrate and config). This simplifies debugging, eliminates concurrency-related test failures, and the timing math shows it's comfortable within the 15-minute NFR (~9–14 minutes sequential). Parallel dispatch of independent stages (saving 15–20 seconds per run) ships in v2 Phase A.

**Highest-risk = earliest start.** The ordering of workstreams within each phase reflects risk, not complexity. Codebase intelligence starts in week 1 and gets the most ongoing iteration time.

---

## v1: Days 1–60 — CRUD+UI Pattern, Two Products

**Goal:** A working software factory that transforms CRUD+UI feature specifications into PRs that pass CI at ≥80% on Payments Dashboard (TypeScript) and Claims Portal (Python), with a full CLI-based engineer workflow.

**Success metrics:** CI pass rate ≥80%, median cleanup time <30 min, per-feature cost <$5, ≥50% of engineers reach Phase 2 within 5 runs.

---

### Phase 0: Foundation (Days 1–8)

**Objective:** Local dev stack running, all interface schemas defined, Go module structure in place. Every subsequent workstream builds on this.

#### Deliverables

**Infrastructure**
- `docker-compose.yml` with Postgres 16 + pgvector, Valkey 8, MinIO, Temporal dev server
- `justfile` with all standard targets (`dev-up`, `dev-down`, `migrate`, `seed`, `test`, `lint`, `fmt`, `build`)
- `go.mod` with all pinned dependencies (pgx/v5, Temporal SDK, Chi, Cobra, valkey-go, go-git, go-tree-sitter, tiktoken-go, santhosh-tekuri/jsonschema/v6, docker/client, aws-sdk-go-v2, otel)

**Schemas** (all four versioned JSON schemas, Draft 2020-12)
- `schemas/bchadspec.v1.json` — BCHADSpec with all fields: product, pattern, entity, fields, permissions, audit, integrations, UI, compliance
- `schemas/bchadplan.v1.json` — BCHADPlan with DAG stages, dependencies, model selection, cost estimates, approval gates
- `schemas/stage_artifact.v1.json` — StageArtifact with generated files, outputs map, gate result, cost
- `schemas/gate_result.v1.json` — GateResult with tier, checks, error category, duration

**Database migrations** (`migrations/` with golang-migrate)
- `001_create_pipeline_tables.sql` — `bchad_runs`, `bchad_stages`, `bchad_artifacts`
- `002_create_verification_tables.sql` — `bchad_gate_results`, `bchad_error_log`
- `003_create_human_interaction_tables.sql` — `bchad_approvals`, `bchad_prompt_log`
- `004_create_trust_metrics_tables.sql` — `bchad_trust_scores`, `bchad_metrics`
- `005_create_vector_store_tables.sql` — `bchad_code_patterns`, `bchad_file_structures`, `bchad_arch_decisions`, all HNSW indexes

**Package skeletons** (`pkg/`, `internal/` directories with interface definitions, no implementation)
- `pkg/bchadspec/types.go` — Go structs matching the JSON schema
- `pkg/bchadplan/types.go` — Go structs for the DAG plan
- `pkg/artifacts/types.go` — StageArtifact, GateResult, GeneratedFile

**End-to-end "hello world" skeleton** (Days 7–8)
- Hardcoded BCHADSpec → hardcoded BCHADPlan → single Anthropic API call with a minimal prompt ("Generate a Go file that prints hello world") → parse output → commit to feature branch via go-git on the `payments-dashboard-test` repo
- Proves every integration point works before any component is built properly: Anthropic API key and headers, go-git authentication with GITHUB_TOKEN, pgx connection pooling to Postgres, MinIO S3 bucket access, Temporal dev server connectivity
- This is a throwaway script (`scripts/e2e-smoke/main.go`), not production code — its purpose is to catch infrastructure issues (API key scoping, git auth, connection strings) that would otherwise surface mid-Phase 2

#### Milestone
`just dev-up` starts all services. `just migrate` applies all migrations cleanly. `go build ./...` compiles with zero errors. JSON schemas validate against their corresponding Go struct fixtures. **A feature branch exists on the `payments-dashboard-test` repo with one LLM-generated file committed to it** (hello world skeleton proves all integration points).

#### Dependencies
None — this is the foundation.

---

### Phase 1: Codebase Intelligence (Days 5–22)

**Objective:** The indexer can scan both v1 products, extract high-quality patterns, and the retrieval service returns stage-relevant context within the token budget. This phase runs earliest because it's the highest-risk component — wrong patterns cascade into every pipeline run.

*Starts on Day 5, overlapping with Phase 0 completion.*

#### Deliverables

**Manual codebase exploration** (Days 5–7, before writing indexer code)
- One engineer spends 2–3 days reading Payments Dashboard and Claims Portal codebases by hand
- Document expected patterns for each stage type: what does a good migration look like (naming, rollback, index strategy), where are the route handlers (file path, middleware chain, error handling pattern), how are tests organized (framework, fixtures, assertion style), what does the component directory structure look like
- Output: `docs/codebase-exploration/payments-dashboard.md` and `docs/codebase-exploration/claims-portal.md` — these become the acceptance criteria for the indexer. If automated extraction doesn't match what a human found by reading the code, the indexer is wrong
- This calibration step is critical: codebase intelligence is the highest-risk component and the indexer can only be evaluated against known-good patterns

**Indexer** (`internal/intelligence/`)
- `scanner.go` — structural profile extraction: file tree, config files, package manifests, framework detection, linter/formatter config copy to S3
- `extractor.go` — code pattern extraction from last 20 merged PRs via GitHub GraphQL API; pattern scoring: recency decay (0.3) + review quality from PR data (0.3) + Tree-sitter structural completeness check (0.4); stores top 3–5 per stage type in `bchad_code_patterns`
- `indexer.go` — Voyage Code 3 embedding generation (1024-dim) via Voyage AI API; batched upserts to pgvector; stores structural profiles in S3
- Tree-sitter query files for TypeScript and Python: route handler extraction, type definition extraction, import graph extraction, middleware chain detection, test pattern detection

**Retrieval service** (`internal/retrieval/`)
- `search.go` — filtered vector search: SQL WHERE on `(product_id, stage_type)` + vector cosine distance ordering + LIMIT; returns results with similarity scores and token counts
- `ranking.go` — context ranking: fills within the stage's token budget allocation, prioritizes by similarity × quality score, truncates gracefully
- `cache.go` — Valkey cache keyed on `retrieval:{product}:{stage}:{features_hash}`, 24h TTL per index cycle; cache invalidation on re-index

**Onboarding CLI commands** (`cmd/bchad/`)
- `bchad index --repo <url>` — runs automated scan + pattern extraction; outputs structural profile to S3 and embeddings to pgvector
- `bchad index --repo <url> --extract-patterns` — pattern extraction pass only (for re-extraction without full re-index)
- `bchad onboard --product <name>` — interactive tech lead questionnaire (30-question structured form); outputs editable YAML codebase profile
- `bchad validate --product <name> --pattern crud_ui` — validation generation: runs a throwaway CRUD feature through the indexer output and presents the result to the tech lead for profile correction

**Tests** (`internal/intelligence/`, `internal/retrieval/`)
- `scanner_test.go` — structural profile extraction from test fixtures; convention field detection
- `extractor_test.go` — pattern scoring with fixture PR data; quality ranking correctness
- `indexer_test.go` — embedding generation with mocked Voyage API; pgvector storage (testcontainers)
- `search_test.go` — filtered vector search query construction; SQL parameterization
- `ranking_test.go` — token budget filling; truncation strategies
- `cache_test.go` — cache hit/miss; TTL behavior

**Embedding validation** (Day 20–21, after indexer runs on both products)
- Embed 20–30 real code patterns from each product using Voyage Code 3 against live pgvector
- Run filtered similarity queries for all five stage types on both products
- Manually evaluate whether the top-3 results per stage type are the correct canonical examples (compare against the patterns documented in the manual codebase exploration step)
- This is a half-day of hands-on validation per product that catches embedding quality issues before Phase 2 builds on retrieval results
- If similar patterns don't cluster or the top results are wrong examples, investigate: is the embedding model misranking, is the filter too broad/narrow, or is the extractor selecting wrong patterns?

#### Milestone
Both product profiles indexed. Tech leads for Payments Dashboard and Claims Portal complete onboarding questionnaires and validate the throwaway CRUD output. Profile corrections applied. Retrieval queries return relevant patterns for all five CRUD+UI stage types for both products. **Embedding validation confirms top-3 retrieval results match the patterns documented in the manual codebase exploration** — if they don't, profile corrections are applied before Phase 2 depends on retrieval.

#### Dependencies
- Phase 0 complete (database schema, S3 buckets, package types)
- Access to Payments Dashboard and Claims Portal repos
- Voyage AI API key configured
- Tech lead time: ~30 min each for questionnaire + ~60 min for validation generation review

---

### Phase 2: Core Pipeline (Days 15–35)

**Objective:** Spec parsing, plan generation, Temporal workflow skeleton, and LLM gateway are functional end-to-end. A spec enters and a plan with cost estimates exits. The workflow can execute stub activities.

*Starts on Day 15, overlapping with Phase 1. Note: Phase 1 retrieval service may not be complete until Day 22. The plan generator uses stub codebase refs initially (hardcoded file paths from the manual codebase exploration docs) and wires in real retrieval results once Phase 1 delivers. The plan generator's core logic — template parameterization, dependency ordering, cost estimation — is testable without live retrieval.*

#### Deliverables

**Spec parser** (`internal/spec/`)
- `parser.go` — JSON spec validation against bchadspec.v1.json schema (santhosh-tekuri/jsonschema/v6); convention resolution from codebase profile (table naming, route prefix, component directory); field normalization (string → VARCHAR(255), enum → VARCHAR(20))
- `nl.go` — NL-to-BCHADSpec translator: single Sonnet 4 call with product context injected; returns draft spec with `needs_clarification` fields flagged; CLI presents draft in readable format with highlighted fields for engineer confirmation (one round-trip, not interactive)
- `validation.go` — JSON Schema validation with actionable error messages referencing specific fields

**Plan generator** (`internal/plan/`)
- `generator.go` — CRUD+UI DAG template parameterization from BCHADSpec + codebase profile; produces BCHADPlan with per-stage model selection (Haiku 3.5 for migrate/config, Sonnet 4 for api/frontend/tests), estimated token counts and costs, dependencies, human approval flags
- `cost.go` — per-stage cost estimation using token budget models: migrate (25K in, 5K out, $0.04), config (15K in, 3K out, $0.02), api (60K in, 15K out, $0.41), frontend (70K in, 20K out, $0.51), tests (50K in, 15K out, $0.38); blended with expected retry rates (1.0–1.3× per stage)
- `templates_test.go` — CRUD+UI template produces valid BCHADPlan with correct dependency ordering: api depends on migrate, frontend depends on api, tests depends on api+frontend+config, migrate and config are independent

**LLM gateway** (`internal/gateway/`)
- `client.go` — direct HTTP calls to Anthropic API (`POST /v1/messages`); no SDK; explicit headers (x-api-key, anthropic-version, content-type); explicit timeouts (120s for generation calls); streaming SSE response parsing for frontend stage
- `cost.go` — token counting with Valkey cache (key: `tokens:{text_hash}`, 24h TTL); cost accumulation per stage and per run in Valkey; 2× projected cost guardrail triggers workflow pause
- `ratelimit.go` — per-model rate counters in Valkey with 1-minute TTL windows; exponential backoff on 429/5xx (separate from error taxonomy retries)
- `stream.go` — SSE line-by-line parser; content_block_delta event extraction; incremental token counting

**Temporal workflow skeleton** (`workflows/`)
- `pipeline.go` — PipelineWorkflow: accepts approved BCHADPlan; executes stages in dependency order (sequential in v1); blocks on human approval signals (migrate, sensitive stages); handles stage failure (pause, notify, preserve completed stages); executes child workflow for Tier 2 gate; calls PR assembly activity on completion
- `pipeline_test.go` — deterministic replay tests: correct stage ordering, approval signal blocking/resuming, approval timeout handling, rejection signal handling, crash recovery (workflow replays correctly after simulated crash mid-pipeline)

**CLI: pipeline submission** (`cmd/bchad/`)
- `bchad run --spec <file>` — submits BCHADSpec, triggers NL translation if needed, presents plan for review, sends approval signal to start execution, streams stage status to terminal
- `bchad status <run-id>` — shows pipeline status via Temporal query handler
- `bchad approve <run-id> --stage migrate` — sends approval signal for migration stage
- GitHub identity resolution on startup: `/user` endpoint call with GITHUB_TOKEN, caches login for session

**Tests** (`internal/spec/`, `internal/plan/`, `internal/gateway/`)
- `parser_test.go` — JSON spec validation, field normalization, convention resolution; fixtures: payment-methods.json, minimal.json, invalid-missing-entity.json
- `nl_test.go` — NL translation with mocked gateway; ambiguous field flagging (integration test, needs API key)
- `generator_test.go` — DAG template parameterization, dependency ordering, cost estimation
- `client_test.go` — request serialization matching Anthropic API spec; header validation
- `cost_test.go` — token counting, cost accumulation; guardrail threshold trigger
- `stream_test.go` — SSE event parsing for all event types

#### Milestone
`bchad run --spec examples/payment-methods.json` submits a spec, translates if NL, presents a plan with per-stage costs, accepts approval, and executes the workflow with stub activities (stubs return fixture stage artifacts). The Temporal workflow replays deterministically in tests.

#### Dependencies
- Phase 0 complete (schemas, database, Go modules)
- Phase 1 retrieval service (for plan generation to include real codebase refs — stub refs used until retrieval is ready; real refs wired in by Day 22)
- ANTHROPIC_API_KEY configured
- GITHUB_TOKEN configured (with `repo` + `read:user` scopes for identity resolution)

---

### Phase 3: Execution Engine (Days 28–48)

**Objective:** Real code generation: context budget allocator fills prompts correctly, stage executors call the LLM with the five-layer structure, verification gates run in Docker containers, error classifier routes failures to the right recovery strategies.

*Starts on Day 28, overlapping with Phase 2 completion.*

#### Deliverables

**Context Budget Allocator** (`internal/budget/`)
- `allocator.go` — token partitioning across five prompt layers; priority fill order: fixed sections (Layer 1 system prompt + Layer 2 adapter + Layer 5 instruction + output buffer) always included → Layer 4 upstream outputs (full, high priority) → Layer 3 primary examples (fill to budget) → Layer 3 secondary examples (remaining space) → Layer 3 architectural notes (last); total never exceeds model context limit
- `truncation.go` — graceful truncation: secondary examples truncated first (remove method bodies, keep signatures); architectural notes summarized to bullet points; least-relevant primary example dropped as last resort
- `counting.go` — tiktoken-go token counting with Valkey cache (24h TTL, key: `tokens:{text_hash}`)
- Tests: token partitioning correctness; truncation order; cache hit behavior; all from `testing.md` §Context Budget Allocation cases

**Language Adapters** (`internal/adapters/`)
- `typescript.go` — framework map: rest_endpoints → "Express route handlers using the existing controller pattern"; verification toolchain: `npx tsc --noEmit`, `npx eslint`, `npx jest --passWithNoTests`, `npx prettier --check`; import style: ES modules; type system: TypeScript strict
- `python.go` — framework map: rest_endpoints → "FastAPI router with Pydantic models"; verification toolchain: `mypy --strict`, `ruff check`, `pytest -x`, `ruff format --check`; import style: absolute imports from package root
- `adapters/typescript.yaml`, `adapters/python.yaml` — YAML adapter definitions in `patterns/adapters/`

**Stage Executor** (`internal/engine/`)
- `executor.go` — for each stage: retrieve context from retrieval service → assemble five-layer prompt via Context Budget Allocator → call LLM gateway → parse file-delimited output (`--- FILE: <path> ---` markers) → write files to workspace → run Tier 1 gate → classify errors → route recovery → report result
- Guidance note injection: on re-run with engineer guidance note, inject as additional user message after original output (conversation history pattern: user→assistant→user), not into system layers
- Re-run with error context: Syntax/Type/Style errors add error output as new user message; Logic errors add error + corrective codebase example; Context errors re-retrieve with tighter parameters then re-inject

**Verification Gates** (`internal/verify/`)
- `gate.go` — dispatches Docker container via Docker Engine API client; mounts generated files + product toolchain files (from S3 codebase profile) + generated code; runs verification commands from language adapter; parses exit code + stdout/stderr into structured GateResult JSON; 60-second timeout (NFR-3)
- `classifier.go` — hybrid error classification: rule-based first (TS error codes → Type; ESLint rule IDs → Style; Prettier diff → Style; Semgrep rule IDs → Security; syntax parse error class → Syntax; route conflict check output → Conflict; spec compliance check → Specification); LLM fallback for ambiguous 20% (Haiku 3.5 call with gate output + generated code); classification drives retry routing
- `security.go` — custom Semgrep rule execution + Trivy dependency scan on manifests
- Docker images for Tier 1 gates (`docker/verify-ts/`, `docker/verify-py/`, `docker/security-scan/`): base runtime images with common tools pre-installed; product-specific deps mounted at runtime from codebase profile S3 path

**Custom Semgrep rules** (`semgrep/`) — written and tested early in Phase 3, not as an afterthought. A missing auth check that slips through verification is a SOC 2 finding.
- `bchad-sensitive-field-exposure` — detect sensitive field (vault_ref, etc.) returned in API response without masking
- `bchad-missing-auth` — detect route handler registered without auth middleware
- `bchad-hardcoded-secret` — detect hardcoded credentials (api_key, secret, password, token patterns with string values ≥8 chars)
- Additional rules discovered during Phase 1 manual codebase exploration (e.g., product-specific audit logging patterns, Vault integration patterns)
- Each rule has a corresponding test fixture in `testdata/semgrep/` with both passing and failing code samples

**Docker layer caching for gate containers** — critical for meeting the 60-second NFR-3
- Gate container Dockerfiles use lockfile-hash-based cache layers: `COPY package-lock.json . && RUN npm ci` as a separate layer before code mount
- When the product's lockfile hasn't changed since the last gate run (the common case), the cached layer is reused and `npm ci` completes in seconds instead of 30–45 seconds
- Base images (`bchad-verify-ts`, `bchad-verify-py`) pre-install common tools (ESLint, TypeScript, Jest, Prettier / Ruff, mypy, pytest) — `npm ci` only installs product-specific dependencies that differ from the base
- Time budget within 60s NFR: ~5s container startup + ~5–15s dependency install (cached) + ~30s verification checks + ~10s margin

**Pattern Library** (`patterns/crud_ui/`)
- `dag.yaml` — CRUD+UI DAG template with stage definitions, dependencies, defaults
- `prompts/migrate.tmpl`, `api.tmpl`, `frontend.tmpl`, `tests.tmpl`, `config.tmpl` — Go text/template files for five-layer prompt construction; snapshot tests via cupaloy lock initial good state

**Tests** (`internal/budget/`, `internal/verify/`, `internal/engine/`)
- `allocator_test.go` — all eight budget test cases from testing.md; within-limit assertion; truncation order
- `gate_test.go` — Docker container dispatch with mocked Docker client; GateResult parsing
- `classifier_test.go` — all eight error categories routing correctly; fixtures in `testdata/gate-results/`
- `security_test.go` — Semgrep rule execution for credential detection, auth enforcement
- Snapshot tests: `cupaloy` for all five prompt templates with fixture inputs

#### Milestone
A complete pipeline run on `payments-dashboard-test` (seeded test codebase): `bchad run --spec examples/payment-methods.json` executes all five stages with real LLM calls, real Tier 1 gates, and produces a feature branch with committed stage outputs. Pipeline completes in under 15 minutes. At least 3 of 5 stages pass Tier 1 on first attempt.

#### Dependencies
- Phase 1 retrieval service functional (codebase profiles indexed)
- Phase 2 LLM gateway functional
- Phase 2 Temporal workflow functional
- Docker Engine running locally for Tier 1 gate tests
- Voyage API key for embedding generation during retrieval

---

### Phase 4: Assembly, Approvals, and Trust (Days 42–54)

**Objective:** Complete pipeline: PR assembler creates reviewable PRs with generation reports, approval flows work in the CLI, trust scores are computed and stored, Tier 2 gate delegates to GitHub Actions CI.

*Starts on Day 42.*

#### Deliverables

**PR Assembler** (`internal/assembly/`)
- `branch.go` — create feature branch (`bchad/pf-{plan_id}-{entity}`), one commit per completed stage with conventional commit messages; push via go-github
- `pr.go` — PR description generation: summary, generation report (pattern, stages, files, retries by category, tier 1/tier 2 results, approvals, actual vs. projected cost), codebase references used, per-stage review guidance, structured TODOs for skipped/non-CRUD elements; push PR via GitHub API
- `diff.go` — diff computation for review interface (stage output vs. baseline)

**Trust scoring** (`internal/trust/`)
- `score.go` — trust score computation from five signals: CI pass rate (0.30), line-level edit volume from git diff between factory commit SHA and merge commit (0.25), stage retry rate (0.15), engineer override count (0.15), time-to-merge (0.15); blocked_by_baseline outcomes excluded from CI pass rate signal
- `phase.go` — phase transition logic: Phase 1 (score <60 OR <5 runs), Phase 2 (score 60–85 AND ≥5 runs), Phase 3 (score >85 AND ≥15 runs); automatic downgrade after 3 consecutive low-score runs with notification
- `regression_test.go` — downgrade scenarios; consecutive threshold detection

**Tier 2 integration gate** (`workflows/`)
- `tier2.go` — child workflow: pushes assembled branch → monitors GitHub Checks API for CI result → on failure, checks main branch CI status (single `/repos/{owner}/{repo}/commits/{sha}/check-runs` API call) → if main is also failing on same tests: mark `blocked_by_baseline`, surface to engineer, skip fix loop → if genuinely failing: classify error, re-generate affected files with integration error context, push commit, re-trigger CI, max 2 fix attempts; blocks on `BCHAD_TIER2_TIMEOUT` (default 15 min)

**CLI: full engineer workflow** (`cmd/bchad/`)
- `bchad approve <run-id> --stage <stage>` — approve stage (sends Temporal signal); shows migration SQL + rollback SQL + schema diff for migration approval
- `bchad reject <run-id> --stage <stage> --guidance <note>` — reject with guidance note; triggers stage re-run with guidance injected as conversation history message
- `bchad rerun <run-id> --stage <stage> --guidance <note>` — re-run stage with guidance
- `bchad edit <run-id> --stage <stage>` — opens generated files in $EDITOR; on save, marks stage as passed (human edit = implicit gate pass), advances to next stage via Temporal signal
- `bchad skip <run-id> --stage <stage> --files <dir>` — engineer provides manual files; BCHAD auto-extracts StageArtifact outputs via Tree-sitter AST parsing; advances pipeline
- `bchad abort <run-id>` — abort pipeline, keep partial output

**Slack approval integration** (`internal/adapters/slack.go`)
- Valkey Streams consumer on `bchad:run:{run_id}:events` for `slack` consumer group
- On `approval.requested` event: post Slack message with migration SQL preview + Approve/Reject buttons
- Approval/rejection triggers Temporal signal via REST API call to control plane
- Note: This is OQ-1. The answer is: v1 ships both CLI and Slack approvals. The CLI is sufficient for terminal workflows; Slack covers engineers who prefer async notification.
- *Scope risk: Slack integration requires a Slack app, OAuth flow, message formatting, interactive button handling, and Valkey Streams consumer wiring. If this threatens Phase 4 timeline, defer to first week of v2. CLI + GitHub PR notifications are sufficient for v1's pilot engineers.*

**Tests**
- `branch_test.go` — per-stage commit format, branch naming convention
- `pr_test.go` — PR description generation with cost summary; snapshot test via cupaloy
- `score_test.go` — all three test cases from testing.md §Trust Score section
- `phase_test.go` — phase transition correctness; downgrade on 3 consecutive low scores
- `tier2_test.go` (workflow replay) — fix loop execution; main-branch-failing bypass; timeout handling

**Tier 2 test fixtures** (`testdata/fixtures/github/`)
- `check-runs-main-passing.json` — mock GitHub Checks API response showing green main branch (normal path: proceed with fix loop)
- `check-runs-main-failing-same-test.json` — mock response showing main branch failing on the same test as the PR (baseline path: `blocked_by_baseline`, skip fix loop)
- `check-runs-main-failing-different-test.json` — mock response showing main branch failing on different tests (mixed path: enter fix loop for PR-specific failures only)
- Without these fixtures, the baseline check logic is untestable without a real GitHub repo with a failing main branch

#### Milestone
Full end-to-end pipeline run produces a real GitHub PR on `payments-dashboard-test` with a generation report, per-stage commits, correct cost summary, and a trust score recorded for the engineer. CLI approval flow for migration stage works. PR passes Tier 2 gate (GitHub Actions CI on test repo).

#### Dependencies
- Phase 3 stage executor and verification gates functional
- Phase 2 Temporal workflow functional
- Phase 1 codebase profiles complete and validated
- GITHUB_TOKEN with `repo` + `read:user` scope

---

### Phase 5: Validation and Launch (Days 52–60)

**Objective:** Run the full 60-feature validation protocol, hit ≥80% CI pass rate, fix systematic failures (primarily through profile corrections and prompt tuning), and ship v1.

*Time allocation within Phase 5: Days 52–53 validation infrastructure. Day 54 first validation run. Days 55–58 tuning iterations (profile corrections + prompt adjustments). Day 59 final validation run. Day 60 launch gate review.*

#### Deliverables

**Validation infrastructure** (`cmd/bchad/`) — Days 52–53
- `bchad validate --suite validation/v1 --products payments-dashboard,claims-portal` — runs validation suite: 60 features (30 per product), proportional mix (40% simple, 40% medium, 20% complex); measures CI pass rate, cleanup time, error category breakdown; outputs confidence intervals
- `testdata/` fully populated: specs, profiles, gate-results, prompt snapshots
- Note: running 60 features = ~300 LLM calls in a batch. Run the validation suite overnight or in batches of 10 to stay within Anthropic API rate limits (see Risk Register).

**First validation run** — Day 54
- Run the full 60-feature suite
- Expected: first run will not hit 80%. The purpose is to surface systematic failures, not to pass.

**Prompt tuning loop** — Days 55–58 (3–4 days explicitly budgeted for iteration)
- Analyze error category breakdown from first validation run
- Classify each systematic failure: profile error (wrong examples → tech lead fixes profile) vs. prompt error (correct examples but wrong generation → update prompt template via validation-protocol-gated PR) vs. gate error (verification check is too strict/loose → adjust gate configuration)
- Priority order: fix profile errors first (they cascade into every run), then prompt errors, then gate tuning
- Each tuning iteration: fix → re-run affected subset of validation features → measure delta → decide next fix
- Snapshot tests updated as prompt templates are tuned
- The validation suite will almost certainly surface failures that require this iteration — that's its purpose. This time is not padding; it is the core of Phase 5.

**Final validation run** — Day 59
- Full 60-feature suite with all corrections applied
- This is the launch gate measurement

**Observability baseline**
- OpenTelemetry traces configured for all pipeline components (each LLM call, retrieval query, Tier 1 gate, Tier 2 gate as spans tied to run ID)
- Grafana dashboard with baseline metrics: pipeline duration, stage duration, token consumption, cost per run, gate pass rate by stage and error category
- Structured logging (log/slog) with OTel span context injection; all log fields from code-style.md guidelines

**S3 retention policy**
- Prompt audit log lifecycle: auto-archive after SOC 2 retention period (OQ-2 resolution: 7 years per SOC 2 Type II standard)
- Apply lifecycle policy to `bchad-artifacts/` bucket

#### Milestone (v1 Launch Gate)
- ≥80% CI pass rate across 60 validation features
- Median cleanup time <30 minutes for passing features
- Per-feature cost <$5 (measured on validation suite)
- All tests pass (`go test ./...`)
- Security review: no secrets in logs, all Semgrep rules passing on BCHAD's own codebase

---

## v2: Days 61–120 — Integration Pattern, Web UI, 4 Products

**Goal:** Extend the factory to the Integration pattern (24% of features), add the visual review interface, bring in 2 more products (first Go product + first MongoDB product), and enable parallel stage execution.

### v2 Phase Breakdown

**Phase A: Parallel Execution (Days 61–68)**
- Enable parallel stage dispatch in the Temporal workflow for independent stages (migrate + config simultaneously)
- Update DAG execution engine to use `workflow.Go` for parallel dispatch with `sync.WaitGroup`-equivalent fan-out/fan-in
- Deterministic replay tests for the parallel dispatch cases (these are complex to get right — isolated focus period)
- Expected time saving: 30–60 seconds per run

**Phase B: Web UI — Core (Days 65–90)**
- Next.js App Router setup, Chi API routes for WebSocket bridging from Valkey Streams
- `DAGView` component: interactive pipeline graph showing stage status, timing, cost (React Flow / @xyflow/react)
- `StagePanel`: artifact inspection — generated files, retrieved codebase context, constructed prompt (read-only prompt.txt from S3 via presigned URL), gate results
- `DiffViewer`: side-by-side diff view for each stage's generated files (react-diff-viewer-continued)
- `ApprovalGate`: migration approval with SQL preview, rollback preview, schema diff
- WebSocket connection to `bchad:run:{run_id}:events` Valkey Stream via XREAD bridged by control plane API
- SSO authentication for web sessions (Athena's IdP via OAuth; CLI retains GITHUB_TOKEN identity)
- *SSO dependency: IdP integration (Okta/Azure AD app registration, redirect URI configuration, client credentials) requires IT department involvement. Start the SSO app registration request in the last week of v1 (Day 55–60) so the IdP configuration is ready when web UI development begins. Without lead time, SSO becomes a blocker at Day 75+.*

**Phase C: Integration Pattern (Days 75–100)**
- New DAG template: `patterns/integration/dag.yaml` with stages for external-service contract, adapter code, mock generation, integration tests
- Prompt templates for integration stage types
- External service mock framework integration (WireMock for TypeScript, pytest-mock for Python)
- Language adapter additions for integration-specific patterns
- Validation suite extended: 60 new features (integration pattern) against 2 products

**Phase D: New Products Onboarding (Days 85–110)**
- Go adapter (`internal/adapters/go.go`, `patterns/adapters/go.yaml`): net/http + Chi handlers, goose migrations, go test assertions
- `docker/verify-go/` Tier 1 gate image
- Onboard Go product: automated scan + extraction + questionnaire + validation generation
- Onboard MongoDB product: requires additional adapter configuration for document-based migration patterns and query builders
- Cold-start onboarding CLI tested against both new products

**Phase E: Post-merge Re-index Hook (Days 95–115)**
- GitHub webhook endpoint in control plane: receives `pull_request` event on `closed` + `merged`
- Publishes to `bchad:index:events` Valkey Stream
- Indexer consumer processes incremental re-index: extract files touched by PR, re-embed changed patterns, update pgvector
- pg_cron weekly full re-index job
- Incremental re-index latency target: <5 minutes from merge to updated embeddings

**v2 Milestones**
- Week 10: Parallel execution in production, web UI serving basic pipeline status
- Week 14: Web UI fully functional with diff views and approval gates; Integration pattern validated at ≥80% CI pass rate on 2 products
- Week 16: 4 products onboarded; Go adapter passing validation suite; post-merge re-index operational
- Week 17: v2 launch

---

## v3: Days 121–180 — All Patterns, All Products, Self-Improvement

**Goal:** Complete four-pattern coverage (Workflow and Analytics), onboard all 7 products, build the self-improvement feedback loop from engineer corrections, and ship the metrics dashboard for engineering leadership.

### v3 Phase Breakdown

**Phase A: Workflow Pattern (Days 121–145)**
- The most complex pattern: spans multiple entities, has state machines, requires event handling
- New DAG template: workflow orchestration stages (state definition, transition handlers, event producers/consumers, compensation logic)
- Workflow pattern requires new verification checks: state machine completeness, deadlock detection, event schema validation
- Estimated complexity: 2× the Integration pattern; plan for extended validation iteration

**Phase B: Analytics Pattern (Days 130–150)**
- Aggregation queries, reporting endpoints, data export, dashboard component generation
- Heavy reliance on existing DB schema conventions extracted from codebase intelligence
- Analytics stages: data model, aggregation layer, API, visualization components, scheduled jobs

**Phase C: Remaining Products (Days 135–160)**
- Onboard remaining 3 products (total: 7 of 7)
- Each product: automated scan + extraction + questionnaire + validation generation + 30-feature validation suite
- Cross-product patterns: ensure TypeScript, Python, and Go adapters cover all product variants

**Phase D: Self-Improvement Feedback Loop (Days 145–165)**
- When engineer makes edits to factory output (tracked via git diff between factory commit SHA and merge commit):
  - Extract changed files and the diff
  - If the diff represents a pattern correction (same type of change repeated ≥3 times across runs), flag for codebase profile update
  - Tech lead reviews flagged corrections; if confirmed, the correction is written back to the canonical code pattern in the profile
  - Re-embed updated patterns via Voyage Code 3
- This closes the v1 manual correction loop with a semi-automated pipeline; humans still approve each profile update
- Target: corrections to profile take <24 hours from merge to updated embeddings

**Phase E: Metrics Dashboard (Days 155–175)**
- Aggregate-only visibility for engineering leadership (per the trust visibility decision: individual scores stay with engineer + tech lead)
- Dashboard panels: CI pass rate by product and by pattern; average engineer time per feature vs. 3.5-day baseline; cost per feature trend; adoption rate (% of CRUD+UI features through factory); error category breakdown over time; trust phase distribution across squads
- Read-only view, no BCHAD operational controls
- Data sourced from `bchad_metrics` and `bchad_runs` tables

**Phase F: Template Composition (Days 165–178)**
- Handle features that span multiple patterns (the 15–20% that v1 generates with structured TODOs)
- Composite DAG templates: CRUD+UI + Integration, CRUD+UI + custom aggregation endpoint
- The structured TODOs emitted in v1 become input specs for v3 composite templates
- This is the path to eliminating the remaining manual work in mixed-pattern features

**v3 Milestones**
- Week 22: Workflow pattern validated at ≥80% CI pass rate
- Week 24: All 7 products onboarded
- Week 26: Self-improvement feedback loop in production; metrics dashboard live for VP Engineering and CTO
- Week 26: v3 launch; full BCHAD portfolio coverage

---

## Cross-Cutting Decisions Incorporated

These decisions made during the design interview are reflected throughout the phases above:

| Decision | Where reflected |
|---|---|
| Edit volume: line-level diff normalized as % of generated lines | Phase 4: `score.go` diff computation |
| NL ambiguity: best-guess + spec edit, one round-trip | Phase 2: `nl.go` confirmation flow |
| Cold-start corrections: tech lead edits profile directly | Phase 1: editable YAML profile output from onboarding |
| Error classifier: rule-based first, LLM fallback for ambiguous 20% | Phase 3: `classifier.go` hybrid design |
| Tier 2 gate: delegates to GitHub Actions CI | Phase 4: `tier2.go` child workflow |
| Edit-and-resume: human edit = implicit gate pass | Phase 3/4: stage executor bypass on `bchad edit` |
| Mixed features: generate CRUD + structured TODOs | Phase 3: stage executor TODO emission |
| Trust visibility: aggregate-only to leadership | Phase 4: `score.go` stores individual; metrics dashboard in v3 is aggregate-only |
| Prompt ownership: validation protocol gates changes | All phases: CI validation suite blocks prompt PRs |
| Pattern quality: hybrid recency + completeness + review signal | Phase 1: `extractor.go` composite scoring |
| Route conflicts: post-generation check vs. indexed inventory | Phase 3: `gate.go` route conflict check |
| Migration concurrency: normal PR process | Explicitly not built; migration timestamps handle ordering |
| Flaky CI: check main branch status before fix loop | Phase 4: `tier2.go` baseline check |
| Skip-stage: AST extraction from provided files | Phase 4: `bchad skip` with Tree-sitter extraction |
| Auth model: GitHub identity via GITHUB_TOKEN | Phase 2: CLI startup identity resolution |
| Guidance note: conversation history (user turn after output) | Phase 3: stage executor retry prompt construction |
| Container toolchain: mount product's manifest from S3 | Phase 3: `docker/verify-ts/`, `verify-py/` design |
| Cost model: ~$1.66 baseline, $5 ceiling with 2× guardrail | Phase 2: `cost.go` with Valkey accumulator |
| Temporal workers: 2 tasks, single queue, I/O bound | Phase 0: deployment config |
| v1 strictly sequential: no parallel even for independent stages | Phase 2: `pipeline.go` sequential dispatch; v2 Phase A adds parallel |
| OQ-1: Slack approvals ship in v1 alongside CLI | Phase 4: `internal/adapters/slack.go` (scope risk flagged — may defer to v2 week 1) |
| OQ-2: S3 retention 7 years (SOC 2 Type II) | Phase 5: lifecycle policy |
| OQ-3: Generate CRUD + structured TODOs for non-CRUD | Phase 3: TODO emission format |
| OQ-4: Aggregate-only trust visibility to leadership | Phase 4 (individual scores) + v3 Phase E (aggregate dashboard) |

---

## Risk Register

| Risk | Likelihood | Impact | Phase | Mitigation |
|---|---|---|---|---|
| Codebase intelligence extracts wrong conventions | Medium | High | Phase 1 | Tech lead validation generation (throwaway CRUD feature before any real runs); direct profile editing; weekly re-index; highest-priority start |
| 80% CI pass rate not achieved in v1 validation | Medium | High | Phase 5 | Error category breakdown identifies systematic vs. random failures; profile corrections vs. prompt tuning is a clear decision tree; validation gate on prompt changes |
| Claims Portal Python patterns are inconsistent (legacy Flask + new FastAPI) | Medium | High | Phase 1 | Structural completeness check in extractor weights patterns with `alembic` migration + FastAPI router + pytest test together; tech lead questionnaire flags migration state |
| Tier 1 gate startup time exceeds 60-second NFR | Low | Medium | Phase 3 | `npm ci` with lockfile hash cache layer in Docker; common tools pre-installed in base image; 60-second NFR with 15s startup + 30s checks + 15s margin |
| LLM model quality regression after Anthropic model update | Low | Medium | All | Prompt version tracking in `bchad_prompt_log`; validation suite as regression test on every prompt change; model selection is per-stage config |
| GitHub Actions CI flakiness in v1 products blocks Tier 2 | Medium | Low | Phase 4 | Tier 2 baseline check (`blocked_by_baseline` outcome); trust score excludes these; surfaces CI reliability data to tech leads |
| Engineer adoption: engineers don't trust factory output | Medium | High | All | Phase 1 (supervised mode, every stage approved) builds trust incrementally; blind code review in validation protocol proves quality before launch |
| Anthropic API rate limits during validation suite | Medium | Medium | Phase 5 | 60 features × ~5 stages × ~1.2 attempts = ~360 LLM calls in a batch. At Sonnet 4's rate limits, this could take longer than expected. Mitigation: run the validation suite overnight or in batches of 10; budget Day 54 as a full-day run, not a quick check |
| SSO IdP integration blocks v2 web UI | Low | Medium | v2 Phase B | Enterprise IdP app registration (Okta/Azure AD) requires IT department involvement and may take 1–2 weeks. Start the request in the last week of v1 (Day 55–60) so configuration is ready when web UI development begins |

---

## Deferred Items (Explicitly Out of Scope)

| Item | Deferred To | Rationale |
|---|---|---|
| Parallel stage execution (including independent stages) | v2 Phase A | v1 is strictly sequential: migrate → config → api → frontend → tests, one at a time. Even independent stages (migrate and config) run sequentially. This simplifies debugging and fits the 15-minute NFR (~9–14 min sequential). Parallel dispatch of independent stages saves 15–20 seconds — meaningful at scale but not worth the concurrency debugging cost in v1 |
| Web review UI | v2 Phase B | CLI sufficient for v1 pilot engineers; web is better UX but not blocking |
| Go language adapter | v2 Phase D | No Go product in v1 scope |
| Post-merge webhook re-index | v2 Phase E | Manual re-index (`bchad index`) sufficient for v1's two products |
| A/B testing for prompt templates | v3 | Insufficient traffic for statistically significant results until v3 volume |
| Self-improvement feedback loop | v3 Phase D | v1 volume too low; manual profile corrections are sufficient and more reliable |
| Metrics dashboard for leadership | v3 Phase E | No aggregate data to show until multiple products and patterns are running |
| Cross-product features | Deferred indefinitely | Different risk profile; requires inter-service coordination |
| Fully autonomous mode | Deferred indefinitely | Human-in-the-loop is a design principle, not a phase |
| Infrastructure / Terraform generation | Deferred indefinitely | Different risk profile; separate system |
| Template composition for mixed patterns | v3 Phase F | v1 handles via structured TODOs; v3 closes remaining gap |

---

*SF-2026-06 · Rev. 2 · March 2026*
