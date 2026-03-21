# Testing Rules

## Philosophy

Test what matters for a production system that must achieve an 80% CI pass rate on generated code and maintain SOC 2 audit compliance. The test suite validates that: specs parse correctly, plans decompose correctly, context budgets stay within limits, verification gates catch real errors, error classification routes to the right recovery strategy, and Temporal workflows execute the DAG correctly.

**Test rigorously:** Spec parsing and validation, plan generation and DAG template parameterization, context budget allocation, error classification, retrieval service (filtered vector search ranking), verification gate runners, Temporal workflow execution (deterministic replay), trust score computation, prompt template construction.
**Test lightly:** HTTP handler wiring, CLI entry points, React components, Terraform configs.
**Don't test:** Postgres query planning, pgvector indexing internals, Valkey protocol, Temporal SDK internals, LLM output quality (test the pipeline's handling of LLM output, not the LLM itself).

Target: Every spec parser edge case is covered. Every error category routes correctly. Every DAG template produces a valid plan. Workflow tests replay deterministically without a Temporal server. All deterministic tests pass without API keys.

## Framework

- **Go:** Standard `testing` package + `gotestsum` for output formatting. No testify. No external test frameworks.
- **Assertions:** Use `if got != want { t.Errorf(...) }` pattern. Use `t.Fatalf` for setup failures.
- **Subtests:** Use `t.Run(tt.name, ...)` for table-driven cases.
- **Workflow tests:** Temporal Go test framework (`go.temporal.io/sdk/testsuite`) for deterministic replay.
- **Integration tests:** `testcontainers-go` for real Postgres/Valkey containers.
- **Snapshot tests:** `cupaloy` for prompt templates, plan output, PR descriptions.
- **Run all:** `go test ./...`
- **Run specific:** `go test -v ./internal/spec/...`
- **Integration only:** `go test -tags=integration ./...`
- **E2E only:** `go test -tags=e2e ./...`

## Directory Structure

```
internal/
├── spec/
│   ├── parser_test.go           # JSON spec parsing, field normalization, convention resolution
│   ├── nl_test.go               # NL-to-BCHADSpec translation (integration, needs API key)
│   └── validation_test.go       # JSON Schema validation of BCHADSpec
├── plan/
│   ├── generator_test.go        # DAG template parameterization, dependency ordering
│   ├── cost_test.go             # Cost estimation per stage and per plan
│   └── templates_test.go        # CRUD+UI template produces valid plans
├── engine/
│   └── dispatcher_test.go       # Stage dispatch ordering, parallel group identification
├── gateway/
│   ├── client_test.go           # LLM request/response serialization
│   ├── cost_test.go             # Token counting, cost accumulation
│   ├── ratelimit_test.go        # Rate limiting via Valkey counters
│   └── stream_test.go           # SSE parsing for streaming responses
├── retrieval/
│   ├── search_test.go           # Filtered vector search query construction
│   ├── ranking_test.go          # Context ranking by similarity + token budget
│   └── cache_test.go            # Retrieval result caching in Valkey
├── intelligence/
│   ├── scanner_test.go          # Repo structural profile extraction
│   ├── extractor_test.go        # Code pattern extraction from merged PRs
│   └── indexer_test.go          # Embedding generation and pgvector storage
├── verify/
│   ├── gate_test.go             # Tier 1 gate execution (Docker container dispatch)
│   ├── classifier_test.go       # Error classification into eight categories
│   └── security_test.go         # Semgrep rule execution for security checks
├── assembly/
│   ├── branch_test.go           # Branch creation, per-stage commits
│   ├── pr_test.go               # PR description generation with cost summary
│   └── diff_test.go             # Diff computation for review interface
├── budget/
│   ├── allocator_test.go        # Token partitioning across prompt sections
│   ├── truncation_test.go       # Graceful truncation strategies
│   └── counting_test.go         # Token counting with cache (tiktoken-go)
├── trust/
│   ├── score_test.go            # Trust score computation from signal weights
│   ├── phase_test.go            # Phase transition logic (Supervised → Gated → Monitored)
│   └── regression_test.go       # Automatic downgrade on consecutive low scores
├── adapters/
│   ├── typescript_test.go       # TypeScript adapter: framework map, toolchain commands
│   └── python_test.go           # Python adapter: framework map, toolchain commands
├── llm/
│   └── client_test.go           # Request/response serialization, tool-use type handling

workflows/
├── pipeline_test.go             # Full pipeline workflow replay (deterministic, no server)
├── stage_test.go                # Individual stage activity tests
├── approval_test.go             # Signal-based approval gate blocking/unblocking
├── parallel_test.go             # Parallel stage dispatch (migrate + config simultaneously)
└── retry_test.go                # Category-specific retry policies

testdata/
├── specs/                       # BCHADSpec fixture files
│   ├── payment-methods.json     # Valid CRUD+UI spec
│   ├── minimal.json             # Minimum valid spec
│   ├── invalid-missing-entity.json
│   ├── nl-brief.txt             # Natural language input for NL translator test
│   └── ambiguous-fields.json    # Spec with needs_clarification fields
├── plans/                       # BCHADPlan fixture files
│   ├── payment-methods-plan.json
│   └── expected-dag.json        # Expected DAG structure for template tests
├── profiles/                    # Codebase profile fixtures
│   ├── payments-dashboard/
│   │   ├── structural_profile.json
│   │   ├── code_patterns/       # Annotated code examples per stage type
│   │   └── arch_decisions.json
│   └── claims-portal/
├── gate-results/                # Verification gate output fixtures
│   ├── syntax-error.json
│   ├── type-error.json
│   ├── lint-failure.json
│   ├── test-failure.json
│   ├── route-conflict.json
│   ├── security-violation.json
│   └── all-pass.json
├── prompts/                     # Prompt snapshot files
│   ├── migrate-stage.snapshot
│   ├── api-stage.snapshot
│   ├── frontend-stage.snapshot
│   └── tests-stage.snapshot
└── fixtures/
    ├── llm/                     # LLM response fixtures
    │   ├── text-response.json
    │   ├── tool-use-response.json
    │   ├── streaming-events.txt
    │   ├── error-401.json
    │   └── error-rate-limit.json
    └── embeddings/              # Voyage Code 3 embedding fixtures
        └── sample-patterns.json
```

## Required Test Cases

### Spec Parsing (Deterministic)

#### 1. JSON Spec Validation
```go
func TestParseSpec(t *testing.T) {
    tests := []struct {
        name        string
        fixtureFile string
        wantErr     bool
        wantEntity  string
        wantPattern string
    }{
        {"valid-crud-ui", "testdata/specs/payment-methods.json", false, "PaymentMethod", "crud_ui"},
        {"minimal-valid", "testdata/specs/minimal.json", false, "Widget", "crud_ui"},
        {"missing-entity", "testdata/specs/invalid-missing-entity.json", true, "", ""},
    }
}
```

#### 2. Field Normalization
- Table name inferred from entity name (`PaymentMethod` → `payment_methods`).
- DB types inferred from field kinds (`string` → `VARCHAR(255)`, `enum` → `VARCHAR(20)`).
- Sensitive fields detected and flagged for Vault integration.
- Product-specific conventions resolved from codebase profile.

#### 3. JSON Schema Validation
- Valid BCHADSpec passes Draft 2020-12 validation.
- Invalid specs produce actionable error messages referencing the specific field.
- Cross-field constraints enforced (e.g., `audit: true` requires a state-changing entity).

### Plan Generation (Deterministic)

#### 4. CRUD+UI Template Parameterization
```go
func TestGeneratePlan(t *testing.T) {
    tests := []struct {
        name           string
        specFile       string
        profileDir     string
        wantStages     int
        wantParallel   []string // stages that should have no dependencies
        wantSequential []string // stages that must follow others
    }{
        {
            "payment-methods-standard",
            "testdata/specs/payment-methods.json",
            "testdata/profiles/payments-dashboard",
            5,
            []string{"migrate", "config"},
            []string{"api", "frontend", "tests"},
        },
    }
}
```

#### 5. Dependency Ordering
- `api` depends on `migrate`. `frontend` depends on `api`. `tests` depends on `api`, `frontend`, `config`.
- `migrate` and `config` are independent — can run in parallel.
- No circular dependencies.

#### 6. Cost Estimation
- Each stage has a projected cost based on model + estimated tokens.
- Total plan cost is the sum of stage costs × expected retry rate.
- Plans exceeding `BCHAD_COST_THRESHOLD` are flagged for human review.

#### 7. Approval Gates
- Migrations always require human approval.
- Sensitive stages (SOC 2/HIPAA products, fields with `sensitive: true`) require approval.
- Approval gates set correctly in the BCHADPlan output.

### Context Budget Allocation (Deterministic, Critical)

#### 8. Token Partitioning
```go
func TestAllocateBudget(t *testing.T) {
    tests := []struct {
        name              string
        modelContextLimit int
        upstreamTokens    int
        primaryExamples   int
        secondaryExamples int
        wantWithinLimit   bool
        wantTruncated     []string // which sections were truncated
    }{
        {"fits-comfortably", 200000, 5000, 15000, 10000, true, nil},
        {"needs-secondary-truncation", 100000, 15000, 30000, 20000, true, []string{"secondary"}},
        {"needs-primary-drop", 50000, 15000, 40000, 20000, true, []string{"secondary", "primary"}},
    }
}
```

#### 9. Priority Ordering
- Fixed sections (system prompt, adapter, instruction, output buffer) always included.
- Upstream outputs included in full — they are critical inputs.
- Primary examples filled up to budget.
- Secondary examples fill remaining space.
- Architectural notes included if space remains.

#### 10. Graceful Truncation
- Secondary examples truncated first (method bodies removed, signatures kept).
- Architectural notes summarized to bullet points.
- Least-relevant primary example dropped as last resort.
- Total never exceeds the model's context window.

### Error Classification (Deterministic, Critical)

#### 11. Category Routing
```go
func TestClassifyError(t *testing.T) {
    tests := []struct {
        name          string
        gateResult    string // fixture file
        wantCategory  string
        wantMaxRetries int
        wantRecovery  string
    }{
        {"syntax-error", "testdata/gate-results/syntax-error.json", "syntax", 3, "direct_retry"},
        {"type-error", "testdata/gate-results/type-error.json", "type", 3, "retry_with_type_context"},
        {"route-conflict", "testdata/gate-results/route-conflict.json", "conflict", 2, "retry_with_conflict"},
        {"security-violation", "testdata/gate-results/security-violation.json", "security", 1, "retry_then_escalate"},
        {"test-failure", "testdata/gate-results/test-failure.json", "logic", 2, "retry_with_corrective"},
    }
}
```

#### 12. Recovery Strategy Selection
- Syntax/Style errors: direct retry or auto-fix via linter.
- Type errors: retry with correct type definition from upstream output.
- Logic errors: retry with failing test output + corrective example from codebase.
- Context errors: re-retrieve from vector store with more specific parameters, then retry.
- Security errors: retry once with security pattern, then escalate to human.
- Specification errors: surface to engineer immediately, zero retries.

### Verification Gates (Deterministic + Integration)

#### 13. Tier 1 Gate Execution
- Gate runner dispatches Docker container with correct image for language.
- Product-specific configs (linter settings, tsconfig) mounted at runtime.
- Gate output parsed into structured `GateResult` JSON.
- Exit code 0 = pass, non-zero = fail with error output captured.

#### 14. Security Scanning
- Custom Semgrep rules detect: hardcoded credentials, missing auth middleware, sensitive field exposure.
- Trivy scans dependency manifests for known vulnerabilities.
- Security failures classified as `security` category with max 1 retry.

### Temporal Workflows (Deterministic — replay tests)

#### 15. Pipeline Workflow Replay
```go
func TestPipelineWorkflow(t *testing.T) {
    suite := testsuite.WorkflowTestSuite{}
    env := suite.NewTestWorkflowEnvironment()

    // Register activities with mock implementations
    env.RegisterActivity(executeStage)
    env.RegisterActivity(runTier2Gate)
    env.RegisterActivity(assemblePR)

    // Execute workflow
    env.ExecuteWorkflow(PipelineWorkflow, pipelineInput)

    // Assert: workflow completed, stages executed in dependency order
    require.True(t, env.IsWorkflowCompleted())
    require.NoError(t, env.GetWorkflowError())
}
```

#### 16. Approval Gate Blocking
- Workflow blocks on migration approval signal.
- Workflow resumes when approval signal received.
- Workflow times out if approval deadline exceeded.
- Workflow handles rejection signal (pause, notify engineer).

#### 17. Parallel Stage Dispatch
- `migrate` and `config` start simultaneously.
- `api` waits for `migrate` to complete before starting.
- `tests` waits for `api`, `frontend`, and `config` to all complete.

#### 18. Category-Specific Retry
- Syntax error activity retried 3 times with immediate backoff.
- Logic error activity retried 2 times with longer backoff.
- Security error activity retried 1 time, then pauses workflow for human.
- Specification error activity pauses workflow immediately (0 retries).

#### 19. Crash Recovery
- Workflow replays correctly after simulated crash mid-pipeline.
- Completed stages are not re-executed.
- Failed stages resume from last attempt.

### Trust Score (Deterministic)

#### 20. Score Computation
```go
func TestComputeTrustScore(t *testing.T) {
    tests := []struct {
        name      string
        signals   TrustSignals
        wantScore float64
        wantPhase string
    }{
        {"new-engineer", TrustSignals{Runs: 3, CIPassRate: 0.8, EditVolume: 0.1}, 0, "supervised"},
        {"reliable-user", TrustSignals{Runs: 10, CIPassRate: 0.9, EditVolume: 0.05}, 78, "gated"},
        {"expert-user", TrustSignals{Runs: 20, CIPassRate: 0.95, EditVolume: 0.02}, 92, "monitored"},
    }
}
```

#### 21. Phase Transitions
- Phase 1 → Phase 2: score ≥ 60 AND ≥ 5 completed runs.
- Phase 2 → Phase 3: score > 85 AND ≥ 15 completed runs.
- Automatic downgrade after 3 consecutive runs below threshold.
- Phase is per-engineer per-product (independent).

### Retrieval Service (Integration — needs Postgres/pgvector)

#### 22. Filtered Vector Search
- Retrieval returns examples filtered by `product_id`, `stage_type`, and feature tags.
- Results ranked by vector similarity.
- Results token-counted and fit within the allocated budget.
- Cached results returned for repeated queries (Valkey cache hit).

### LLM Gateway (Deterministic + Integration)

#### 23. Request Serialization
- Construct a stage generation request, serialize to JSON, verify structure matches Anthropic API spec.
- Verify headers: `x-api-key`, `anthropic-version`, `content-type`.

#### 24. Cost Tracking
- Token counts accumulated per stage and per run in Valkey.
- Cost computed from input/output tokens × model pricing.
- Cost guardrail triggers when accumulated cost exceeds 2× projected.

#### 25. Rate Limiting
- Per-model rate counters enforced via Valkey with TTL windows.
- 429 responses trigger exponential backoff (separate from error taxonomy retries).

### Prompt Regression (Deterministic)

#### 26. Prompt Snapshot Tests
- Construct the generation prompt for each stage type using fixture data.
- Compare against stored snapshot (`cupaloy`).
- If the prompt template changes, the snapshot diff shows exactly what changed.
- Prevents accidental prompt regression.

### Assembly (Deterministic)

#### 27. PR Description Generation
- PR description includes: summary, generation report (stages, files, retries, cost), codebase references, review guidance.
- Cost summary matches accumulated stage costs.
- Error recovery actions listed with categories.

#### 28. Per-Stage Commits
- One commit per completed stage with descriptive message.
- Commit messages follow conventional format.
- Branch name follows pattern: `bchad/pf-{plan_id}-{entity}`.

## Mocking Strategy

### LLM Gateway Mock (for workflow and stage tests)

```go
type mockGateway struct {
    responses []gateway.GenerateResponse
    callIndex int
}

func (m *mockGateway) Generate(ctx context.Context, req gateway.GenerateRequest) (*gateway.GenerateResponse, error) {
    if m.callIndex >= len(m.responses) {
        return nil, fmt.Errorf("unexpected call %d", m.callIndex)
    }
    resp := m.responses[m.callIndex]
    m.callIndex++
    return &resp, nil
}
```

### Testcontainers (for integration tests)

```go
func setupTestPostgres(t *testing.T) *pgxpool.Pool {
    ctx := context.Background()
    container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: testcontainers.ContainerRequest{
            Image:        "pgvector/pgvector:pg16",
            ExposedPorts: []string{"5432/tcp"},
            WaitingFor:   wait.ForListeningPort("5432/tcp"),
        },
        Started: true,
    })
    // ... connect, run migrations, return pool
}
```

## What Not to Test

- Don't test Postgres query planning or pgvector index behavior — trust the database.
- Don't test Temporal SDK internals — test your workflow and activity logic.
- Don't test `go-git` or `go-github` library correctness — mock them and test your Git operation logic.
- Don't test Docker Engine API — mock the client and test your gate dispatch logic.
- Don't test `tiktoken-go` token counting accuracy — trust the library, test your budget allocation.
- Don't test LLM output quality directly — test the pipeline's handling of LLM output (parsing, validation, error classification).
- Don't test React components for an internal tool — the Go backend tests cover correctness.
- Don't aim for 100% coverage — aim for "every spec validates, every error classifies correctly, every workflow replays deterministically, and every budget stays within limits."
