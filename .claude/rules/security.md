# Security Rules

## Secrets Management

- **Never hardcode API keys.** `ANTHROPIC_API_KEY`, `VOYAGE_API_KEY`, and `GITHUB_TOKEN` come from environment variables only.
- Local development: load secrets via `.env` file (gitignored). In Docker Compose, use `env_file`.
- Production (ECS Fargate): secrets injected via AWS Secrets Manager at task launch — no application-level client code needed.
- Required env vars: `ANTHROPIC_API_KEY`, `VOYAGE_API_KEY`, `GITHUB_TOKEN`, `BCHAD_DATABASE_URL`, `BCHAD_VALKEY_URL`.
- Never log API key values. Log only that the variable "is set" or "is missing".
- Fail fast on startup if required secrets are empty — don't let the user discover this mid-pipeline.
- CloudTrail provides the audit trail for secret access in production (SOC 2 requirement).

## .gitignore

The following must always be gitignored:
```
.env
.env.*
*.pem
*.key
*.tar.gz
bin/
/tmp/
node_modules/
web/.next/
web/node_modules/
.vscode/
.idea/
terraform/.terraform/
terraform/*.tfstate*
terraform/*.tfvars
```

## SOC 2 / HIPAA Compliance

BCHAD generates code for products under SOC 2 and HIPAA. The factory itself must maintain compliance:

### Audit Trail
- Every pipeline run, stage transition, approval decision, error classification, and cost measurement is persisted to PostgreSQL.
- Every prompt sent and response received is hashed and stored in S3, with references in `bchad_prompt_log`.
- Distributed tracing via OpenTelemetry spans the full pipeline — each LLM call, retrieval query, and verification gate as a span tied to the pipeline run ID.
- AWS CloudTrail logs all secret access.

### Generated Code Security
- Generated code for SOC 2/HIPAA products must pass Semgrep security scanning rules as part of the verification gate:
  - Sensitive fields must use Vault integration (never stored raw).
  - Every endpoint must include auth middleware.
  - Audit logging must be present on state-changing operations.
  - No hardcoded credentials in generated code.
- Security verification failures are classified as `security` category with max 1 retry, then immediate escalation to human.

### Data Isolation
- ECS Fargate tasks run on Firecracker microVMs — hardware-enforced task isolation.
- All data-plane components run in private subnets. Only the ALB is internet-facing.
- RDS PostgreSQL uses encryption at rest and Multi-AZ for durability.
- Verification gates for different products run in separate Fargate tasks — a compromised integration test for the fintech product cannot affect the healthtech product's test environment.

## Codebase Data Handling

BCHAD indexes and retrieves code from target product repositories. Treat all codebase contents as confidential:

### What Not to Log
- **Never log:** raw source code file contents, full prompt/response bodies, API keys, GitHub tokens, database credentials.
- **Safe to log:** file paths, file sizes, token counts, embedding dimensions, similarity scores, stage statuses, cost amounts, latency measurements, error categories.
- When logging LLM interactions, log: model name, input/output token count, cost, latency. Not the prompt or response content.

### What Not to Send to the LLM Unnecessarily
- Never send entire repository files to the LLM in bulk. Always retrieve specific patterns via filtered vector search.
- Context Budget Allocator enforces token limits — prompts are assembled within budget, not dumped wholesale.
- Codebase profiles (structural JSON) can be multi-MB — store in S3, retrieve only the relevant sections.
- The file index (paths + metadata) goes in the prompt so the LLM knows what's available without reading everything.

### Prompt Logging
- Full prompt text and LLM response are hashed (SHA-256) and stored in S3 under `bchad-artifacts/{run_id}/{stage_id}/attempt_{n}/`.
- References (hash, token counts, cost, latency) stored in `bchad_prompt_log` table.
- Prompt logs are subject to S3 lifecycle policies — auto-archive after the SOC 2 retention period.
- Prompt logs are used for: audit compliance, prompt debugging, A/B testing prompt variants.

## API Key Handling in Code

```go
// CORRECT: Read from environment, pass as parameter
apiKey := os.Getenv("ANTHROPIC_API_KEY")
gateway := gateway.New(apiKey, voyageKey, costTracker, rateLimiter, promptLogger)

// CORRECT: Set header from stored key
req.Header.Set("x-api-key", g.apiKey)

// WRONG: Log the key
slog.Info("using api key", "key", apiKey)  // NEVER DO THIS

// WRONG: Include in error messages
return fmt.Errorf("API call failed with key %s: %w", apiKey, err)  // NEVER DO THIS

// CORRECT: Log that key is present
slog.Info("gateway initialized", "model", model, "api_key_set", apiKey != "", "voyage_key_set", voyageKey != "")
```

## Input Validation

### Feature Specifications
- Validate all specs against the BCHADSpec JSON Schema (Draft 2020-12) before plan generation.
- NL-to-BCHADSpec translation marks ambiguous fields as `needs_clarification: true` — never silently resolves ambiguity.
- Reject specs with unknown products, invalid pattern types, or unsupported field kinds.

### Codebase Profiles
- Validate structural profiles against CodebaseProfile JSON Schema.
- Manual overrides from tech leads take precedence over automated extraction.
- Re-index incrementally on merged PRs; full re-index weekly.

### Web UI Input
- BCHADSpec and BCHADPlan validated with Zod schemas on the client side (generated from JSON Schema).
- Plan modifications by engineers are re-validated before execution.
- Chat/guidance notes in the review interface are sanitized and length-limited.

### File Path Requests
- When the web UI requests a file via the control plane API, validate the path is within the expected scope.
- Reject paths containing `..` or absolute paths.
- Only serve files that exist in the codebase profile or artifact store.

## Docker Security

### Verification Gate Containers
- Verification gate images use specific base tags (`node:20-slim`, `python:3.12-slim`, `golang:1.22-alpine`), not `latest`.
- Product-specific configs (linter settings, tsconfig, formatter rules) are mounted at runtime from S3, not baked into the image.
- Gate containers are ephemeral — spun up per-stage, torn down after verification completes.
- Images are pre-built and stored in ECR. Image scanning via Trivy.

### Service Containers
- Don't run as root. Use a non-root user in the Dockerfile.
- Multi-stage builds: build stage compiles Go + Node, runtime stage copies only binaries + static assets.
- Don't copy `.env` files into Docker images.
- Only expose necessary ports (8080 for control plane API, 3000 for web UI).

## Network Security

- All data-plane components (Postgres, Valkey, S3, Temporal) run in private subnets.
- Only the ALB is internet-facing, routing to the Next.js web UI and Go control plane API.
- Internal service discovery via Route 53 Cloud Map (`bchad.athena.internal`).
- ECS tasks inherit IAM roles, secrets, and networking from task definitions — no volume mounts with credentials.

## Error Responses

- Never expose stack traces, file system paths, or internal error details to the web UI.
- API errors return JSON: `{"error": "message", "code": "error_code"}`.
- If the LLM returns an error, return a generic "generation failed" message to the UI, log the details server-side with the run/stage ID.
- Verification gate errors are captured in structured `GateResult` JSON — the raw error output is stored in S3, the classification and recovery action are returned to the workflow.

## CORS

- Production: CORS restricted to the ALB domain serving the web UI.
- Development: `*` (allow all origins) for local dev with separate dev servers.
- API keys are server-side only (control plane holds them) — never sent in browser headers.

## Dependencies

- Pin Go dependencies via `go.sum` (committed to git).
- Pin Node dependencies via `package-lock.json` (committed to git).
- Key Go dependencies:
  - `go.temporal.io/sdk` — workflow orchestration
  - `github.com/jackc/pgx/v5` — PostgreSQL driver + pgvector
  - `github.com/go-chi/chi/v5` — HTTP router
  - `github.com/spf13/cobra` — CLI framework
  - `github.com/valkey-io/valkey-go` — Valkey client
  - `github.com/go-git/go-git/v5` — Git operations
  - `github.com/smacker/go-tree-sitter` — AST parsing
  - `github.com/pkoukk/tiktoken-go` — Token counting
  - `github.com/santhosh-tekuri/jsonschema/v6` — JSON Schema validation
  - `go.opentelemetry.io/otel` — Distributed tracing
  - `github.com/docker/docker/client` — Docker Engine API
  - `github.com/aws/aws-sdk-go-v2` — S3 client
- Key Node dependencies:
  - `react`, `react-dom`, `next` — Web UI framework
  - `@xyflow/react` — DAG visualization
  - `react-diff-viewer-continued` — Code diff views
  - `zod` — Client-side schema validation
  - `tailwindcss` — Styling
- Before adding a new dependency, check if the standard library or an existing dependency covers the need. Every new dependency is a maintenance burden and audit surface.

## Git Hygiene

- **Before committing, verify no secrets were accidentally added:**
  ```bash
  git diff --cached | grep -iE "(api_key|secret|password|token|sk-ant|pa-|ghp_)" | grep -v "test\|mock\|example\|getenv\|env\.\|\.md"
  ```
  Review any matches.
- Never commit `.env` files, API keys, Terraform state, or raw bundle archives.
- Terraform `.tfvars` files with environment-specific values are gitignored — use `.tfvars.example` templates.
