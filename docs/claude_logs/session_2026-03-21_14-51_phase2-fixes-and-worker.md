# Session Log: Phase 2 Post-Implementation Fixes — Lint, Worker, and Infra

**Date:** 2026-03-21 14:51
**Duration:** ~2 hours
**Focus:** Fix golangci-lint issues, wire up the Temporal worker, and resolve a chain of local dev infrastructure failures that blocked end-to-end pipeline execution

---

## What Got Done

### Lint fixes (`fix(lint)` commit)
- Replaced all 5 bare `defer resp.Body.Close()` calls with `defer func() { _ = resp.Body.Close() }()` to satisfy `errcheck` in:
  - `internal/gateway/client.go`
  - `internal/gateway/stream.go` (×2)
  - `internal/gateway/client_test.go`
  - `cmd/bchad/run.go`
- Replaced deprecated `workflow.UpsertSearchAttributes` with `workflow.UpsertTypedSearchAttributes` using `temporal.NewSearchAttributeKeyKeyword("key").ValueSet(value)` in `workflows/pipeline.go` — looked up the correct API via Context7 MCP
- `golangci-lint run` confirmed 0 issues after fixes

### Worker registration fix (`fix(worker)` commit)
- Updated `cmd/worker/main.go` to register `PipelineWorkflow`, `ExecuteStageActivity`, `AssemblePRActivity`, and `Tier2GateActivity` — all were commented out as placeholders
- Worker now starts cleanly and polls the `bchad-pipeline` task queue

### Temporal namespace creation (manual + `dev-up` fix)
- Manually registered the `bchad` namespace with 72h retention using `tctl` inside the container via its container IP (`172.24.0.5:7233`)
- Added the registration to `just dev-up` so it's automatic going forward

### Justfile Docker template escaping (`fix(infra)` commit — two iterations)
- Added `docker inspect`-based IP lookup to `dev-up` to find the container IP before calling `tctl`
- First attempt used `{{{{...}}}}` (justfile's brace escape) — failed with `Syntax error: "(" unexpected` at runtime
- Second attempt used `FMT='...'` shell variable — failed with `error: Unknown start of token '.'` at justfile parse time
- Final fix: replaced `docker inspect` entirely with `docker exec bchad-temporal sh -c 'IP=$(hostname -i | tr -d " "); tctl ...'` — no template syntax, runs entirely inside the container

### Custom search attribute registration (`fix(infra)` commit)
- Registered `product`, `engineer`, `trust_phase` as `Keyword` type search attributes at the cluster level via `tctl admin cluster add-search-attributes`
- Added registration to `just dev-up` (idempotent — "Search attributes already exist" on re-run)

### End-to-end pipeline verified
- `just build` produces `bin/bchad` (50M) and `bin/worker` (26M)
- `./bin/bchad run --spec examples/payment-methods.json` parses spec, generates plan, starts Temporal workflow, reaches `awaiting_approval` for `migrate` stage
- Worker processes the workflow task without errors after search attribute registration

---

## Issues & Troubleshooting

### 1. `golangci-lint` — `errcheck` on `resp.Body.Close()`
- **Problem:** 5 lint failures for unhandled error return from `resp.Body.Close()`
- **Cause:** `errcheck` linter requires all error returns to be handled or explicitly discarded
- **Fix:** Wrapped each with `defer func() { _ = resp.Body.Close() }()`

### 2. `golangci-lint` — deprecated `workflow.UpsertSearchAttributes`
- **Problem:** 1 lint failure for use of deprecated Temporal API
- **Cause:** `workflow.UpsertSearchAttributes(ctx, map[string]interface{}{...})` was deprecated in favor of typed search attributes
- **Fix:** Used `workflow.UpsertTypedSearchAttributes(ctx, temporal.NewSearchAttributeKeyKeyword("key").ValueSet(value), ...)` per Context7 MCP docs for `/temporalio/sdk-go`

### 3. Worker: "Namespace bchad is not found"
- **Problem:** Worker crashed immediately with `Namespace bchad is not found`
- **Cause:** The `temporalio/auto-setup` image only creates the `default` namespace; `bchad` was never registered
- **Fix:** Ran `tctl --address <container-ip>:7233 --namespace bchad namespace register --retention 72h` from inside the container; added to `dev-up`

### 4. `tctl` inside container can't connect to `localhost:7233`
- **Problem:** `docker exec bchad-temporal tctl ... localhost:7233` failed — the Temporal server binds to `172.24.0.5:7233` (the container's bridge IP), not to `127.0.0.1`
- **Cause:** The `auto-setup` container binds to its Docker network interface, not loopback
- **Fix:** Used `hostname -i` inside the container to get the actual binding IP, then passed it to `tctl --address`

### 5. Worker: "unable to find workflow type: PipelineWorkflow"
- **Problem:** Worker started but every workflow task failed with `unable to find workflow type: PipelineWorkflow. Supported types: []`
- **Cause:** `cmd/worker/main.go` had all `RegisterWorkflow`/`RegisterActivity` calls commented out as Phase 2 placeholders
- **Fix:** Uncommented and wired in `workflows.PipelineWorkflow`, `workflows.ExecuteStageActivity`, `workflows.AssemblePRActivity`, `workflows.Tier2GateActivity`

### 6. Justfile `{{` template syntax — two failed attempts
- **Problem:** `just dev-up` crashed with `Syntax error: "(" unexpected` (or `Unknown start of token '.'`) when the `dev-up` recipe contained `docker inspect -f '{{range ...}}'`
- **Cause:** Justfile parses `{{...}}` as its own variable interpolation syntax in recipe bodies, even inside single-quoted strings. `{{{{` (the documented escape) produced `sh: 1: Syntax error: "(" unexpected` at runtime on this system's version of just/sh
- **Fix:** Eliminated `docker inspect` entirely. Used `docker exec bchad-temporal sh -c 'IP=$(hostname -i | tr -d " "); tctl --address $IP:7233 ...'` — the IP lookup happens inside the container with no template syntax visible to justfile

### 7. Workflow retry loop: `BadSearchAttributes`
- **Problem:** Worker logged `Task processing failed with error ... BadSearchAttributes: Namespace bchad has no mapping defined for search attribute product` on every workflow task attempt, causing infinite replays
- **Cause:** `UpsertTypedSearchAttributes` sends commands to the server as part of the workflow task. Even though the returned error was discarded with `_ =` in Go code, the server rejected the entire workflow task when the attributes weren't registered — causing the task to fail and replay at the Temporal SDK level
- **Fix:** Registered `product`, `engineer`, `trust_phase` as `Keyword` type at cluster level via `echo y | tctl admin cluster add-search-attributes --name product --type Keyword --name engineer --type Keyword --name trust_phase --type Keyword`; added to `dev-up`

---

## Decisions Made

### Register search attributes at cluster level vs. removing the call
The `UpsertTypedSearchAttributes` call is genuinely useful — it enables filtering by product, engineer, and trust phase in the Temporal dashboard. Rather than removing it (which would lose the feature), we registered the attributes at the cluster level and added it to `dev-up`. This is a one-time setup step that's now automated.

### Use `hostname -i` instead of `docker inspect` in justfile
`docker inspect -f '{{...}}'` requires Go template syntax (`{{range}}`, `{{.IPAddress}}`, `{{end}}`). Every approach to escaping or quoting these braces for justfile either failed at parse time or at runtime. The simpler approach — running `hostname -i` from inside the container — avoids the problem entirely and is more robust across justfile versions and shell implementations.

### `_ = resp.Body.Close()` pattern over silently ignoring
The `defer func() { _ = resp.Body.Close() }()` pattern is the standard Go idiom for satisfying `errcheck` while acknowledging that the error is intentionally discarded. In HTTP response body close, there's no useful recovery action, so discarding is correct — but it needs to be explicit.

---

## Current State

### Working end-to-end (local dev)
- `just dev-up` starts all 6 services, registers the `bchad` namespace, and registers the 3 custom search attributes — all idempotently
- `just build` produces `bin/bchad` and `bin/worker`
- `./bin/worker` (or `go run ./cmd/worker`) connects to Temporal, registers `PipelineWorkflow` + 3 activities, and polls `bchad-pipeline`
- `./bin/bchad run --spec examples/payment-methods.json` generates the CRUD+UI plan and starts the Temporal workflow
- The workflow reaches `awaiting_approval` for the `migrate` stage and waits for a signal — this is correct behavior (Phase 2 activities are stubs)
- `golangci-lint run` → 0 issues
- `go test ./...` → all 9 packages pass

### What's still stubbed
- `ExecuteStageActivity` returns a placeholder artifact — no real LLM calls, no prompt assembly, no Tier 1 gate
- `AssemblePRActivity` and `Tier2GateActivity` return hardcoded success — no GitHub API, no CI runner
- The pipeline can't actually complete a run end-to-end without manual approval signals for each gated stage

### Branch status
All fixes committed to `phase2/core-pipeline`. No PR opened yet.

---

## Next Steps

1. **Open PR** for `phase2/core-pipeline` → `main`
2. **Manually test the full approval flow** — run the worker and CLI in two terminals, send an approve signal via `./bin/bchad approve <run-id> --stage migrate` to verify the workflow advances through all stages with stub activities
3. **Phase 3 — Stage Executor**: Implement real `ExecuteStageActivity`:
   - 5-layer prompt assembly via Context Budget Allocator (`internal/budget`)
   - Codebase context retrieval via `internal/retrieval`
   - LLM call via `internal/gateway` with error-category-specific retry
   - Parse LLM output into `GeneratedFile` list
4. **Phase 4 — Verification Gates**: Implement `internal/verify`:
   - Tier 1 gate: Docker container dispatch per language
   - 8-category error classifier + retry routing
5. **Phase 5 — PR Assembler**: Implement real `AssemblePRActivity` using `go-git` and GitHub API
6. **Parallel stage dispatch**: Fan out `migrate` + `config` concurrently in `PipelineWorkflow` using `workflow.Go`
