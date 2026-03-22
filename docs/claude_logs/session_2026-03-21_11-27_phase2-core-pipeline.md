# Session Log: Phase 2 — Core Pipeline Implementation

**Date:** 2026-03-21 11:27
**Duration:** ~2.5 hours
**Focus:** Implement all Phase 2 deliverables: spec parser, plan generator, LLM gateway, Temporal workflow, and CLI commands

---

## What Got Done

### `internal/spec`
- **`validation.go`** — JSON Schema Draft 2020-12 validator using `santhosh-tekuri/jsonschema/v6`; two constructors: `NewValidator` (file path) and `NewValidatorFromBytes` (embedded bytes for tests); `Validate()` surfaces the full error tree via `ve.Error()`
- **`parser.go`** — Convention resolution from `BCHADSpec`: snake_case table names, REST route prefixes, frontend component directories, PostgreSQL type mapping; pluralization helpers (`pluralizeSnake`, `pluralizeKebab`, `pluralWord`)
- **`nl.go`** — NL-to-BCHADSpec translator calling Claude Sonnet 4 via the gateway; strips markdown fences from LLM output; returns `NLTranslationResult` with `ClarificationFields` for ambiguous inputs
- All three files fully tested (`parser_test.go`, `validation_test.go`, `nl_test.go`)

### `internal/plan`
- **`generator.go`** — Produces a `BCHADPlan` from a `ParsedSpec` using the CRUD+UI DAG template; 5 stages: `migrate → config → api → frontend → tests`; sets `HumanApproval: true` on `migrate` and any stage with sensitive fields; generates plan IDs in `pf-YYYYMMDD-NNN` format
- **`cost.go`** — Per-stage cost estimation based on model pricing and expected token usage (Haiku for migrate/config, Sonnet for api/frontend/tests); `EstimateStageCost` and `EstimatePlanCost`; total ~$1.66 for a standard CRUD+UI run

### `internal/gateway`
- **`client.go`** — Direct HTTP client for `POST https://api.anthropic.com/v1/messages`; typed `GenerateRequest`/`GenerateResponse`/`Message`/`ContentBlock`/`Usage` structs; `APIError` with `IsRateLimit()` and `IsServerError()` helpers
- **`cost.go`** — Token counting via `tiktoken-go` (cl100k_base), results cached in Valkey by SHA-256 hash with 24h TTL; per-run cost accumulation with `ErrCostGuardrail` sentinel when accumulated > 2× projected
- **`ratelimit.go`** — Valkey-based sliding window rate limiter per model; `GenerateWithRetry` wraps `Generate` with 429/5xx exponential backoff (max 5/3 retries)
- **`stream.go`** — SSE parser for Anthropic streaming API; channel-based `StreamEvent` delivery; `parseSSEStream` goroutine; `CollectStreamText` helper; `GenerateStream` entry point

### `workflows`
- **`signals.go`** — `ApprovalDecision` signal type, `PipelineStatus` query type, constants: `ApprovalSignalName`, `StatusQueryName`, `PipelineTaskQueue`
- **`activities.go`** — Stub implementations of `ExecuteStageActivity`, `AssemblePRActivity`, `Tier2GateActivity` with fully typed `StageInput`/`StageOutput`/`PRInput`/`PROutput`/`Tier2Input`/`Tier2Output` structs
- **`pipeline.go`** — `PipelineWorkflow` executing stages sequentially; blocks on `workflow.GetSignalChannel` for human-gated stages (`waitForApproval`); registers `StatusQueryName` query handler; calls `workflow.UpsertSearchAttributes` for Temporal dashboard filtering; returns `PipelineOutput` with PR URL, accumulated cost, and status
- **`pipeline_test.go`** — 5 deterministic replay tests using `testsuite.WorkflowTestSuite`: `StageOrdering`, `ApprovalBlocking`, `ApprovalRejection`, `StatusQuery`, `CompleteOutput`; `fixtureArtifact` helper

### `cmd/bchad`
- **`run.go`** — `bchad run --spec <file|json|nl>` command; `loadAndParseSpec` handles file path, inline JSON, and NL brief; `printPlan` renders the plan table; `monitorPipeline` polls status and handles interactive approval gates; `resolveEngineerID` resolves GitHub login via API or falls back to `$USER`; `resolveSchemaPath` finds `schemas/bchadspec.v1.json` relative to CWD or binary
- **`status.go`** — `bchad status <run-id>` command; queries Temporal via `QueryWorkflow` and prints stage status table
- **`approve.go`** — `bchad approve <run-id> --stage <id>` command; sends `ApprovalDecision` signal; supports `--reject` and `--guidance` flags

### Fixtures & Examples
- `examples/payment-methods.json` — Full CRUD+UI BCHADSpec for `node-express-prisma-v1`
- `examples/payment-methods-nl.txt` — Natural language version of the same spec
- `testdata/fixtures/llm/text-response.json` — Standard Anthropic response fixture
- `testdata/fixtures/llm/streaming-events.txt` — SSE stream fixture (content_block_delta events)
- `testdata/fixtures/llm/error-401.json`, `error-rate-limit.json` — Error response fixtures

### Git Commits (7 commits on `phase2/core-pipeline`)
1. `test(workflows)`: fix temporal activity mock registrations
2. `feat(spec)`: parser, validation, NL translator
3. `feat(plan)`: generator and cost estimator
4. `feat(gateway)`: LLM gateway with cost, rate limiting, streaming
5. `feat(workflows)`: pipeline workflow with approval signals
6. `feat(cli)`: run, status, approve commands
7. `chore(tests)`: example specs and LLM fixtures

---

## Issues & Troubleshooting

### 1. `jsonschema/v6` API mismatches
- **Problem:** Initial `validation.go` used `bytes.NewReader` for `AddResource`, `ve.InstanceLocation` as a string, and `ve.Message` field — all of which don't exist in the actual library API
- **Cause:** Library API differs significantly from training data assumptions; `AddResource` takes `any` (parsed JSON), `InstanceLocation` is `[]string`, there is no `Message` field
- **Fix:** Parse the schema JSON with `json.Unmarshal` before passing to `AddResource`; use `ve.Error()` to format the full validation error tree (handles all internal formatting internally)

### 2. `pluralWord` — "Status" → "status" instead of "statuses"
- **Problem:** `toTableName("Status")` returned `"status"` (already plural, no-op) instead of `"statuses"`
- **Cause:** The generic `case strings.HasSuffix(word, "s")` matched before the `"us"` suffix case could be evaluated
- **Fix:** Added `case strings.HasSuffix(word, "us"): return word + "es"` before the generic `"s"` catch-all

### 3. `workflow.UpsertTypedSearchAttributes` unavailable
- **Problem:** Build error — `workflow.UpsertTypedSearchAttributes` does not exist in SDK v1.32.0
- **Cause:** That API was added in a later SDK version than what's in go.mod
- **Fix:** Changed to `workflow.UpsertSearchAttributes(ctx, map[string]interface{}{...})` which is available in v1.32.0

### 4. `c.QueryWorkflow` return value mismatch
- **Problem:** `err := c.QueryWorkflow(...)` — compiler error, function returns 2 values
- **Cause:** Temporal client's `QueryWorkflow` returns `(client.Value, error)`, not just `error`
- **Fix:** `qresp, qerr := c.QueryWorkflow(...); qerr = qresp.Get(&status)`

### 5. Temporal workflow test mock registration failure (primary issue this session)
- **Problem:** All 5 workflow tests failed with: `mock: Unexpected Method Call — ExecuteStageActivity(*context.timerCtx, workflows.StageInput) — The closest call I have is: ExecuteStageActivity() — Provided 0 arguments, mocked for 2 arguments`
- **Cause (first attempt):** `env.OnActivity(ExecuteStageActivity, context.Background())` passed `context.Background()` as an exact argument matcher for the context. The Temporal test suite passes a real `*context.timerCtx` (with deadline, values, etc.) not `context.Background()`. The mock registration expected 1 arg but the call provided 2 (context + StageInput).
- **Cause (second attempt):** Removing all args — `env.OnActivity(ExecuteStageActivity)` — registers with 0 expected args, but testify mock still fails when called with 2 actual args ("Provided 0 arguments, mocked for 2 arguments")
- **Fix:** Use `mock.Anything` matchers: `env.OnActivity(ExecuteStageActivity, mock.Anything, mock.Anything)` — first for the context (any type), second for `StageInput`. Also converted all static `.Return(&PROutput{...}, nil)` forms to function callbacks `func(_ context.Context, input PRInput) (*PROutput, error)` to avoid type mismatch issues with testify's return value dispatch.

### 6. `go vet` — context leak in stream_test.go
- **Problem:** `go vet` reported: `the cancel function is not used on all paths (possible context leak)` at `stream_test.go:125`
- **Cause:** `cancel()` was only called inside a `for range` loop when `count >= 3` — if the loop exited early for other reasons, `cancel` was never called
- **Fix:** Added `defer cancel()` immediately after `ctx, cancel := context.WithCancel(...)` so cancel is guaranteed to run

---

## Decisions Made

### Sequential stage execution in v1
The CRUD+UI DAG template executes stages sequentially (migrate → config → api → frontend → tests) rather than in parallel groups (migrate+config concurrently, etc.). The DAG dependency data is present in `PlanStage.DependsOn` for future use, but the workflow executes sequentially for v1 simplicity. Parallel dispatch via `workflow.Go` is a Phase 3 concern.

### `mock.Anything` over `mock.MatchedBy` for context matching
Used `mock.Anything` for both the context and input args in `OnActivity` rather than more specific matchers. The tests are replay tests — the exact input values are determined by the fixture plan, not by matcher logic. The `fixtureArtifact` helper in the return function provides per-stage correct output without needing input-specific matchers.

### Activity stubs in `activities.go`
`ExecuteStageActivity` is a stub that logs and returns a placeholder artifact. The real implementation (prompt assembly, LLM call, Tier 1 gate) is a Phase 3 deliverable. The stub is sufficient for workflow replay tests to validate the DAG execution logic, approval gating, and PR assembly flow.

### `UpsertSearchAttributes` over typed variant
SDK v1.32.0 only supports `workflow.UpsertSearchAttributes(ctx, map[string]interface{}{})`. The typed variant (`UpsertTypedSearchAttributes`) was not yet available. Kept the untyped form rather than upgrading the SDK to avoid destabilizing Phase 1 components.

---

## Current State

### Passing
- `go build ./...` — clean build, no errors
- `go vet ./...` — clean, no issues
- `go test ./...` — all 9 packages pass:
  - `internal/gateway` ✅
  - `internal/intelligence` ✅
  - `internal/plan` ✅
  - `internal/retrieval` ✅
  - `internal/spec` ✅
  - `pkg/artifacts` ✅
  - `pkg/bchadplan` ✅
  - `pkg/bchadspec` ✅
  - `workflows` ✅ (5 deterministic replay tests)

### Deployed / Committed
All Phase 2 code is committed to `phase2/core-pipeline` in 7 commits. No PR opened yet.

### What's a Stub
- `ExecuteStageActivity` — returns a placeholder artifact; real implementation needs prompt assembly, LLM call, Tier 1 gate
- `cmd/worker/main.go` — not yet updated to register `PipelineWorkflow` and Phase 2 activities
- `AssemblePRActivity` / `Tier2GateActivity` — stubs returning hardcoded success; real implementation needs GitHub API integration and CI runner

### What's Not Started
- Phase 3: Stage Executor (prompt assembly via Context Budget Allocator, LLM calls per stage)
- Phase 4: Verification Gates (Tier 1 Docker containers, error classifier, retry routing)
- Phase 5: PR Assembler (GitHub branch/PR creation)
- Web UI (`web/`) — not touched in Phase 2

---

## Next Steps

1. **Open PR** for `phase2/core-pipeline` → `main` and get it merged
2. **Update `cmd/worker/main.go`** to register `PipelineWorkflow`, `ExecuteStageActivity`, `AssemblePRActivity`, `Tier2GateActivity` so the worker binary actually serves the task queue
3. **Phase 3 — Stage Executor**: Implement real `ExecuteStageActivity`:
   - Assemble 5-layer prompt via Context Budget Allocator (`internal/budget`)
   - Retrieve codebase examples via `internal/retrieval`
   - Call LLM via `internal/gateway` with retry on error categories
   - Parse LLM output into `GeneratedFile` list
4. **Phase 4 — Verification Gates**: Implement `internal/verify`:
   - Tier 1: dispatch Docker containers per language/stage type
   - Error classifier with 8-category taxonomy → retry routing
   - Security scanning via Semgrep rules
5. **Phase 5 — PR Assembler**: Implement real `AssemblePRActivity`:
   - `go-git` for branch creation and per-stage commits
   - GitHub API for PR creation with cost summary in description
6. **Parallel stage dispatch**: Update `PipelineWorkflow` to fan out `migrate` and `config` concurrently using `workflow.Go`, then fan in before starting `api`
