# Code Style Rules

## Go (Backend)

### General
- Go 1.22+ features are fine (range-over-func, etc.)
- All exported functions and types get godoc comments. Skip for obvious unexported helpers.
- Use explicit error returns everywhere — no panics for expected conditions.
- Prefer returning `(result, error)` over logging and continuing silently.
- Use `context.Context` as the first parameter for any function that does I/O, calls the LLM, or queries a database.

### Formatting
- `gofumpt` is the standard (stricter than `gofmt`). No exceptions.
- Imports: stdlib → third-party → local, separated by blank lines. `goimports` handles this.
- Line length: no hard limit, but break long function signatures and struct literals for readability.

### Naming
- `camelCase` for unexported functions, variables.
- `PascalCase` for exported functions, types, constants.
- Acronyms stay consistent case: `LLM`, `API`, `SSE`, `DAG`, `PR`, `CI` (not `Llm`, `Api`, `Dag`).
- Package names are short, singular, lowercase: `spec`, `plan`, `engine`, `gateway`, `retrieval`, `verify`, `budget`, `trust`, `assembly`.
- Interface names describe behavior: `StageExecutor`, `Retriever`, `GateRunner`, `ErrorClassifier`, `CostTracker` — not `IStageExecutor`.
- Struct names match their purpose: `BCHADSpec`, `BCHADPlan`, `StageArtifact`, `GateResult`, `TokenBudget`, `TrustScore`.

### Error Handling
- Always wrap errors with context: `fmt.Errorf("stage %s attempt %d: %w", stageID, attempt, err)`.
- Never swallow errors silently. If you don't return it, log it with context.
- Use sentinel errors (`var ErrBudgetExceeded = errors.New(...)`) for conditions callers need to check.
- Use `errors.Is` and `errors.As` for error checking, never string comparison.
- Return early on errors — no deep nesting.

### Database (pgx/v5)
- Use `pgx/v5` with connection pooling via `pgxpool`. No database/sql wrapper.
- Use typed struct scanning with `pgx.CollectRows` and `pgx.RowToStructByName`.
- Use `pgx.Batch` for multi-query operations within a pipeline run.
- JSONB columns use `pgtype.JSONB` with explicit marshal/unmarshal to typed Go structs.
- pgvector embeddings use the `pgvector-go` extension types.
- All queries use parameterized placeholders (`$1`, `$2`), never string interpolation.

### Temporal Workflows
- Workflow functions are deterministic — no I/O, no random, no time.Now() inside workflows.
- All I/O happens in activities. Activities receive context and return `(result, error)`.
- Use `workflow.Go` for parallel stage dispatch inside workflows, not raw goroutines.
- Signals for human approvals: define typed signal channels with `workflow.GetSignalChannel`.
- Queries for status checks: define typed query handlers with `workflow.SetQueryHandler`.
- Activity retry policies are derived from the error taxonomy — different retries per error category.
- Use search attributes (`product`, `pattern`, `engineer`, `trust_phase`) for filtering in the dashboard.

### HTTP Client (LLM API)
- Use `net/http` standard library client. No external HTTP frameworks or SDKs.
- Set explicit timeouts: `http.Client{Timeout: 120 * time.Second}` for LLM calls.
- Use `json.NewEncoder` / `json.NewDecoder` for request/response bodies.
- Check `resp.StatusCode` explicitly — don't rely on error-only flow.
- Close response bodies: `defer resp.Body.Close()` immediately after error check.
- All request/response types are explicit Go structs with `json` tags. No `map[string]interface{}`.
- Handle streaming responses (SSE) with buffered line-by-line reading for LLM streaming output.

### HTTP Server (Chi)
- Use `chi` router for the control plane API. Standard `net/http` handler signatures.
- Route handlers are thin: parse request, call service, write response. No business logic in handlers.
- Use middleware for: CORS, logging, request ID, panic recovery, OpenTelemetry trace injection.
- Return JSON with `json.NewEncoder(w).Encode()` and set `Content-Type: application/json`.
- Use proper HTTP status codes: 400 for bad input, 404 for not found, 500 for internal errors, 202 for accepted (async pipeline run).
- WebSocket connections for real-time pipeline updates bridge from Valkey Streams via XREAD.

### Concurrency
- Use goroutines for parallel stage execution within the DAG (via Temporal `workflow.Go`, not raw goroutines in the control plane).
- Use `chan` for Valkey Stream event consumption and SSE bridging to WebSocket clients.
- Use `sync.Mutex` only where absolutely necessary — prefer Temporal's built-in concurrency model for workflow state.
- Use `context.WithCancel` to propagate cancellation from HTTP requests to LLM calls.

### Logging
- Use `log/slog` structured logger (Go 1.21+).
- JSON output in production, text output in dev (controlled by `BCHAD_LOG_LEVEL` env var).
- Inject OpenTelemetry span context into log entries for trace correlation.
- Log: pipeline start/complete, stage transitions, LLM API calls (model, token count, cost, duration), verification gate results, approval decisions, error classifications.
- **Never log:** API keys (`ANTHROPIC_API_KEY`, `VOYAGE_API_KEY`, `GITHUB_TOKEN`), full prompt/response bodies, raw codebase file contents.
- Use structured fields: `slog.Info("stage complete", "run_id", runID, "stage", stageType, "cost", cost, "attempts", attempts, "duration", elapsed)`.

### Project-Specific
- **BCHADSpec and BCHADPlan are versioned JSON Schemas** — validate at every component boundary using `santhosh-tekuri/jsonschema/v6`.
- **The LLM gateway is an in-process Go library** — it shares the control plane's `pgxpool` and Valkey client. No separate service, no network hop.
- **Verification gates run in Docker containers** — the control plane dispatches them via the Docker Engine API client, never runs linters/typecheckers in its own process.
- **Error classification drives retry routing** — the error classifier categorizes gate failures into eight categories, each with a different recovery strategy. Don't use undifferentiated retry.
- **Context Budget Allocator fills by priority** — fixed sections always included, then high-priority (upstream outputs, primary examples), then medium (secondary examples, arch notes). Truncate gracefully, never exceed the model's context window.
- **Token counts are cached in Valkey** — token counting is on the hot path. Cache by text hash with 24h TTL.
- **Codebase patterns are embedded with Voyage Code 3** — 1024-dimensional embeddings stored in pgvector. Retrieval uses filtered vector search: SQL WHERE clause + vector distance ordering.
- **Stage artifacts flow downstream** — when a stage completes, its output (schema definitions, API contracts, component paths) is injected into dependent stage prompts. The frontend stage receives the API contract from the API stage, not a guess.

## TypeScript / React (Frontend — Next.js Web UI)

### General
- TypeScript strict mode. No `any` — use `unknown` and narrow.
- Functional components with hooks only. No class components.
- Use named exports, not default exports.
- Next.js App Router for pages. API routes proxy to the Go control plane.

### Formatting
- Prettier for formatting. No debate.
- Imports: react → next → third-party → local components → local utils → types.

### Naming
- `PascalCase` for components and types: `DAGView`, `StagePanel`, `DiffViewer`, `TrustBadge`, `ApprovalGate`.
- `camelCase` for functions, variables, hooks: `usePipelineStatus`, `handleApproval`, `stageResult`.
- File names match component names: `DAGView.tsx`, `StagePanel.tsx`.

### Styling
- Tailwind utility classes. No custom CSS files.
- Avoid inline styles. If Tailwind doesn't cover it, use a `<style>` block in the component file.

### State Management
- React state (`useState`, `useReducer`) for component-level state.
- Use `useRef` for WebSocket connections to the control plane.
- Use `useEffect` cleanup to close WebSocket connections on unmount.
- Server-side rendering (Next.js SSR) for initial page load; client-side updates via WebSocket.

### API Communication
- Use `fetch` for REST calls to the Go control plane API (proxied through Next.js API routes).
- Use WebSocket for real-time pipeline updates (bridged from Valkey Streams by the Go control plane).
- Handle errors explicitly: check `response.ok`, parse error JSON, display to user.
- Validate responses against BCHADPlan/BCHADSpec Zod schemas on the client side.

### Key Components
- `DAGView`: interactive pipeline graph with stage status, timing, and cost overlays (React Flow / @xyflow/react).
- `DiffViewer`: side-by-side and unified diff views for generated code vs. repo state (react-diff-viewer-continued).
- `StagePanel`: stage-level artifact inspection — generated code, codebase context retrieved, prompt constructed, gate results.
- `ApprovalGate`: migration approval UI with SQL preview, rollback preview, schema diff.
- `TrustBadge`: trust phase indicator (Supervised/Gated/Monitored) per product.
- `CostTracker`: projected vs. actual cost per stage and per run.
- `ErrorTrail`: error classification history — each attempt, what went wrong, what category, what recovery.
- `TerminalOutput`: embedded terminal for verification gate output and error logs (@xterm/xterm).

### Key Libraries
- `@xyflow/react` — DAG visualization
- `react-diff-viewer-continued` — code diff views
- `Shiki` — syntax highlighting
- `@tanstack/react-table` — data tables (metrics, run history, trust scores)
- `Recharts` — charts (cost over time, CI pass rates)
- `React Hook Form` + `Zod` — spec input and plan editing forms
- `@xterm/xterm` — terminal rendering for gate output

## Justfile

Standard targets:
```makefile
dev-up:          docker compose up -d           # Start local infrastructure
dev-down:        docker compose down            # Stop local infrastructure
build:           go build -o bin/bchad ./cmd/bchad && go build -o bin/worker ./cmd/worker
test:            go test ./...
test-unit:       go test -short ./...
test-int:        just dev-up && go test -tags=integration ./...
test-e2e:        just dev-up && go test -tags=e2e ./...
test-snapshot:   go test -run TestSnapshot ./...
snapshot-update: UPDATE_SNAPSHOTS=true go test -run TestSnapshot ./...
lint:            golangci-lint run
fmt:             gofumpt -w . && cd web && npx prettier --write .
migrate:         go run github.com/golang-migrate/migrate/v4/cmd/migrate@latest -path migrations -database "$BCHAD_DATABASE_URL" up
migrate-down:    go run github.com/golang-migrate/migrate/v4/cmd/migrate@latest -path migrations -database "$BCHAD_DATABASE_URL" down 1
seed:            go run ./scripts/seed/main.go
clean:           rm -rf bin/
```
