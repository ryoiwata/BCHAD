# API Contracts & Schemas

## General Principles

- The Go control plane serves a REST API consumed by the Next.js web UI and the CLI.
- All interface schemas are versioned JSON Schema (Draft 2020-12): BCHADSpec, BCHADPlan, StageArtifact, GateResult.
- The web UI validates using Zod schemas generated from JSON Schema definitions (via `json-schema-to-zod`).
- The Anthropic API is called directly via HTTP — no Go SDK. Voyage AI API for code embeddings.
- All timestamps are RFC3339 format.
- PostgreSQL is the source of truth for all pipeline state. The web UI never connects directly to Postgres, Valkey, or S3.

## Core Data Types

### BCHADSpec (pkg/bchadspec/types.go)

The normalized input to the factory — what the engineer specifies:

```go
type BCHADSpec struct {
    Product      string          `json:"product"`
    Pattern      string          `json:"pattern"`       // "crud_ui", "integration", "workflow", "analytics"
    Entity       EntitySpec      `json:"entity"`
    Permissions  string          `json:"permissions"`    // e.g. "payment_methods:manage"
    Audit        bool            `json:"audit"`
    Integrations []string        `json:"integrations"`   // e.g. ["vault"]
    UI           UISpec          `json:"ui"`
    Compliance   ComplianceFlags `json:"compliance"`     // auto-detected from product profile
}

type EntitySpec struct {
    Name   string      `json:"name"`       // PascalCase: "PaymentMethod"
    Fields []FieldSpec `json:"fields"`
}

type FieldSpec struct {
    Name               string   `json:"name"`
    Kind               string   `json:"kind"`                // "string", "enum", "boolean", "integer", "float", "date"
    Values             []string `json:"values,omitempty"`     // for enums
    Sensitive          bool     `json:"sensitive,omitempty"`
    Required           bool     `json:"required,omitempty"`
    NeedsClarification bool     `json:"needs_clarification,omitempty"`
    Reason             string   `json:"reason,omitempty"`     // why clarification is needed
}

type UISpec struct {
    List   bool `json:"list"`
    Detail bool `json:"detail"`
    Form   bool `json:"form"`
}

type ComplianceFlags struct {
    SOC2  bool `json:"soc2"`
    HIPAA bool `json:"hipaa"`
}
```

### BCHADPlan (pkg/bchadplan/types.go)

The generation plan — a DAG of stages:

```go
type BCHADPlan struct {
    ID             string        `json:"id"`              // e.g. "pf-20260315-001"
    Product        string        `json:"product"`
    Pattern        string        `json:"pattern"`
    Entity         string        `json:"entity"`
    ProjectedCost  float64       `json:"projected_cost"`
    Stages         []PlanStage   `json:"stages"`
    TotalFiles     int           `json:"estimated_total_files"`
    ApprovalGates  []string      `json:"human_approval_gates"`
    SecurityReview bool          `json:"security_review"`
}

type PlanStage struct {
    ID            string   `json:"id"`              // "migrate", "api", "frontend", "tests", "config"
    Type          string   `json:"type"`            // "db_migration", "rest_endpoints", "react_components", etc.
    DependsOn     []string `json:"depends_on"`
    HumanApproval bool     `json:"human_approval"`
    Model         string   `json:"model"`           // "claude-haiku-3.5", "claude-sonnet-4"
    Description   string   `json:"description"`
    EstFiles      int      `json:"estimated_files"`
    EstCost       float64  `json:"estimated_cost"`
    CodebaseRefs  []string `json:"codebase_refs"`   // file paths in target repo
}
```

### StageArtifact (pkg/artifacts/types.go)

The output of a completed stage, consumed by downstream stages:

```go
type StageArtifact struct {
    StageID          string            `json:"stage_id"`
    StageType        string            `json:"stage_type"`
    Status           string            `json:"status"`          // "passed", "failed", "awaiting_approval"
    GeneratedFiles   []GeneratedFile   `json:"generated_files"`
    Outputs          map[string]string `json:"outputs"`         // schema_definition, endpoint_contracts, component_paths, etc.
    GateResult       GateResult        `json:"gate_result"`
    Attempts         int               `json:"attempts"`
    Cost             float64           `json:"cost"`
}

type GeneratedFile struct {
    Path     string `json:"path"`      // relative to repo root
    Action   string `json:"action"`    // "create", "modify"
    Language string `json:"language"`
}
```

### GateResult (pkg/artifacts/types.go)

The output of a verification gate:

```go
type GateResult struct {
    Passed       bool              `json:"passed"`
    Tier         int               `json:"tier"`           // 1 or 2
    Checks       []GateCheck       `json:"checks"`
    ErrorOutput  string            `json:"error_output"`   // raw error text (stored in S3)
    ErrorCategory string           `json:"error_category"` // syntax, style, type, logic, context, conflict, security, specification
    DurationMS   int               `json:"duration_ms"`
}

type GateCheck struct {
    Name   string `json:"name"`    // "typecheck", "lint", "route_conflicts", "auth_middleware", etc.
    Passed bool   `json:"passed"`
    Output string `json:"output"`  // brief error or success message
}
```

### TrustScore (internal/trust/types.go)

```go
type TrustScore struct {
    EngineerID  string  `json:"engineer_id"`
    ProductID   string  `json:"product_id"`
    Score       float64 `json:"score"`
    Phase       string  `json:"phase"`       // "supervised", "gated", "monitored"
    Signals     TrustSignals `json:"signals"`
}

type TrustSignals struct {
    CIPassRate     float64 `json:"ci_pass_rate"`      // weight: 0.30
    EditVolume     float64 `json:"edit_volume"`        // weight: 0.25
    RetryRate      float64 `json:"retry_rate"`         // weight: 0.15
    OverrideCount  float64 `json:"override_count"`     // weight: 0.15
    TimeToMerge    float64 `json:"time_to_merge"`      // weight: 0.15
    CompletedRuns  int     `json:"completed_runs"`
}
```

## Claude API Request Format

Direct HTTP calls to `POST https://api.anthropic.com/v1/messages`.

### Headers
```
x-api-key: <ANTHROPIC_API_KEY>
anthropic-version: 2023-06-01
content-type: application/json
```

### Request Body (Stage Generation)
```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 8192,
  "system": "<five-layer prompt: system + adapter + codebase brief + upstream + instruction>",
  "messages": [
    {
      "role": "user",
      "content": "<generation instruction parameterized from BCHADPlan stage>"
    }
  ]
}
```

### Request Body (Stage Generation — Retry with Error Context)
```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 8192,
  "system": "<five-layer prompt>",
  "messages": [
    {
      "role": "user",
      "content": "<original generation instruction>"
    },
    {
      "role": "assistant",
      "content": "<previous generation output>"
    },
    {
      "role": "user",
      "content": "<error context layer: exact error output + corrective example (if Context/Logic category)>"
    }
  ]
}
```

### Models Configured

| Model | API Identifier | Primary Use |
|---|---|---|
| Claude Haiku 3.5 | `claude-haiku-3-5-sonnet-latest` | migrate, config stages (constrained, low ambiguity) |
| Claude Sonnet 4 | `claude-sonnet-4-20250514` | api, frontend, tests stages, NL spec translation |
| Voyage Code 3 | `voyage-code-3` (Voyage AI API) | Code pattern embedding for codebase intelligence |

### Streaming (SSE)
```
POST with "stream": true

Events:
  event: message_start     → { "type": "message_start", "message": { ... } }
  event: content_block_start → { "type": "content_block_start", "index": 0, "content_block": { "type": "text" } }
  event: content_block_delta → { "type": "content_block_delta", "delta": { "type": "text_delta", "text": "..." } }
  event: content_block_stop  → { "type": "content_block_stop" }
  event: message_stop        → { "type": "message_stop" }
```

## LLM Prompt Templates

### Stage Generation System Prompt (Layer 1 — Fixed)

```
You are a code generator in the BCHAD software factory. You generate production-quality code that conforms exactly to the conventions of the target codebase. You never invent patterns — you follow the examples provided. Output only the requested files, no explanations.

## Output Format
Each generated file must be delimited with markers:
--- FILE: <path relative to repo root> ---
<file contents>
--- END FILE ---

## Hard Constraints
- Do not hardcode secrets, API keys, or credentials.
- Do not introduce new dependencies without explicit declaration.
- Follow the import style and naming conventions shown in the codebase examples exactly.
- If the spec includes audit: true, every state-changing operation must include an audit log call.
- If a field is marked sensitive: true, it must use the Vault integration pattern shown in the examples.
```

### Stage Generation — Layer 2 (Language Adapter Context)

```
## Language: TypeScript
## Framework: Express route handlers using the existing controller pattern
## ORM: Prisma
## Type System: TypeScript strict mode
## Import Style: ES modules (import/export)
## Verification: npx tsc --noEmit && npx eslint --config .eslintrc.js && npx jest --passWithNoTests
```

### Stage Generation — Layer 3 (Codebase Brief)

```
## Codebase Conventions for payments-dashboard

### Migration Pattern (from 20260301_create_invoices):
<annotated migration example>

### API Route Pattern (from src/api/routes/invoices.ts):
<annotated route handler example with auth middleware, audit logging, error handling>

### Architectural Notes:
- Authentication: JWT middleware via src/api/middleware/auth.ts
- Permissions: Role-based, checked via permissionGate('scope:action') middleware
- Audit: AuditLogger.log(action, entity, entityId, userId, changes) on create/update/delete
- Vault: VaultClient.store(key, value) and VaultClient.retrieve(key) for sensitive fields
```

### Stage Generation — Layer 4 (Upstream Context)

```
## Upstream Stage Outputs

### From migrate stage:
Table: payment_methods
Columns: id (UUID, PK), merchant_id (UUID, FK), type (VARCHAR(20), enum: credit_card|ach|wire), label (VARCHAR(255)), is_default (BOOLEAN, default false), vault_ref (VARCHAR(255)), created_at (TIMESTAMPTZ), updated_at (TIMESTAMPTZ)
Indexes: merchant_id, type

### From config stage:
Permission registered: payment_methods:manage
Feature flag registered: payment-methods
```

### NL Spec Translation System Prompt

```
You are a spec parser for BCHAD. Given a product brief, extract a BCHADSpec JSON.
Use only the fields defined in the schema. Mark anything ambiguous as 'needs_clarification = true' with a reason.

## BCHADSpec JSON Schema
<schema definition with field docs>

## Target Product Context
- Product: payments-dashboard
- Known entities: Invoice, Subscription, Merchant, Webhook
- Available integrations: vault, launchdarkly, audit-logger
- Compliance: SOC 2

## Output
Return ONLY valid JSON matching the BCHADSpec schema. No explanation.
```

## REST API Endpoints (Control Plane)

### Submit Pipeline Run

```
POST /api/runs
Content-Type: application/json
Body: { "spec": <BCHADSpec JSON> }

Response 202:
{
  "run_id": "pf-20260315-001",
  "status": "planning",
  "plan": null
}
```

### Get Pipeline Run Status

```
GET /api/runs/{run_id}

Response 200:
{
  "run_id": "pf-20260315-001",
  "status": "executing",        // planning, awaiting_approval, executing, complete, failed
  "plan": <BCHADPlan>,
  "stages": [
    { "id": "migrate", "status": "passed", "cost": 0.04, "attempts": 1 },
    { "id": "config", "status": "passed", "cost": 0.02, "attempts": 1 },
    { "id": "api", "status": "running", "cost": null, "attempts": 1 }
  ],
  "projected_cost": 1.72,
  "actual_cost": 0.06
}
```

### Approve Stage

```
POST /api/runs/{run_id}/stages/{stage_id}/approve
Content-Type: application/json
Body: { "decision": "approve", "guidance_note": "" }

Response 200:
{ "status": "approved" }
```

### Re-run Stage with Guidance

```
POST /api/runs/{run_id}/stages/{stage_id}/rerun
Content-Type: application/json
Body: { "guidance": "Use the subscriptions pattern instead of invoices for the API routes" }

Response 202:
{ "status": "rerunning" }
```

### Get Stage Artifacts

```
GET /api/runs/{run_id}/stages/{stage_id}/artifacts

Response 200:
{
  "stage_id": "api",
  "generated_files": [...],
  "gate_result": <GateResult>,
  "prompt_ref": "s3://bchad-artifacts/pf-20260315-001/api/attempt_1/prompt.txt",
  "codebase_refs_used": [...]
}
```

### Stream Pipeline Events

```
GET /api/runs/{run_id}/events
Upgrade: websocket

Events (via WebSocket, bridged from Valkey Streams):
  {"type": "stage.started", "stage_id": "migrate", "timestamp": "..."}
  {"type": "gate.passed", "stage_id": "migrate", "tier": 1}
  {"type": "approval.requested", "stage_id": "migrate"}
  {"type": "approval.received", "stage_id": "migrate", "decision": "approve"}
  {"type": "stage.started", "stage_id": "api"}
  {"type": "stage.retry", "stage_id": "api", "attempt": 2, "error_category": "conflict"}
  {"type": "gate.passed", "stage_id": "api", "tier": 1}
  {"type": "run.complete", "status": "success", "total_cost": 1.74}
```

### Get Trust Score

```
GET /api/trust/{engineer_id}/{product_id}

Response 200:
{
  "score": 78.5,
  "phase": "gated",
  "signals": <TrustSignals>,
  "last_10_runs": [...]
}
```

### Index a Product Repository

```
POST /api/products/{product_id}/index
Content-Type: application/json
Body: { "repo_url": "https://github.com/athena-digital/payments-dashboard.git", "full": true }

Response 202:
{ "job_id": "idx-001", "status": "indexing" }
```

## Valkey Stream Events

### Pipeline Events (per-run stream)

```
XADD bchad:run:{run_id}:events * type stage.started stage_id migrate
XADD bchad:run:{run_id}:events * type gate.passed stage_id api tier 1
XADD bchad:run:{run_id}:events * type approval.requested stage_id migrate
XADD bchad:run:{run_id}:events * type run.complete status success
```

Consumer groups: `webui` (real-time pipeline view via WebSocket), `slack` (approval notifications), `metrics-writer` (async metric recording to Postgres).

### Index Events

```
XADD bchad:index:events * type index.requested product_id payments-dashboard
XADD bchad:index:events * type index.complete product_id payments-dashboard patterns_extracted 47
```

Consumer group: `indexer` (processes post-merge re-index triggers from GitHub webhooks).

## Valkey Cache Keys

| Key Pattern | Type | TTL | Purpose |
|---|---|---|---|
| `tokens:{text_hash}` | String (integer) | 24h | Cached token counts |
| `retrieval:{product}:{stage}:{features_hash}` | String (JSON) | Until next re-index | Cached vector retrieval results |
| `rate:{model}:{window}` | String (counter) | 1 minute | LLM API rate limiting |
| `cost:{run_id}` | String (float) | Run lifetime | Running cost accumulator |
| `lock:stage:{stage_id}` | String | 30 min | Distributed lock for stage execution |

## Database Schema (PostgreSQL 16)

See `migrations/` for the full schema. Key tables:

```sql
bchad_runs          (id, product_id, pattern, spec_json, plan_json, status, projected_cost, actual_cost, created_at, completed_at)
bchad_stages        (id, run_id, stage_type, status, model, attempt_count, input_artifact_ids, output_artifact_id, cost, started_at, completed_at)
bchad_artifacts     (id, stage_id, artifact_type, content_hash, s3_path, token_count, created_at)
bchad_gate_results  (id, stage_id, attempt_number, tier, passed, checks_json, error_output, duration_ms)
bchad_error_log     (id, stage_id, attempt_number, category, raw_error, recovery_strategy, resolved, created_at)
bchad_approvals     (id, stage_id, engineer_id, decision, guidance_note, decided_at)
bchad_trust_scores  (id, engineer_id, product_id, score, phase, signal_weights_json, last_10_runs_json, updated_at)
bchad_metrics       (id, run_id, stage_id, metric_name, metric_value, recorded_at)
bchad_prompt_log    (id, stage_id, attempt_number, prompt_version, model, input_tokens, output_tokens, cost, prompt_hash, response_hash, latency_ms, created_at)
bchad_code_patterns (id, product_id, stage_type, language, entity_type, has_permissions, has_audit, has_integrations, pr_quality_score, content_text, metadata_json, embedding vector(1024), last_updated)
```

pgvector indexes:
```sql
CREATE INDEX ON bchad_code_patterns USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 128);
CREATE INDEX ON bchad_code_patterns (product_id, stage_type);
CREATE INDEX ON bchad_code_patterns (product_id, language, entity_type);
```
