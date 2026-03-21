# BCHAD: Batch Code Harvesting, Assembly, and Deployment

A software factory that transforms feature specifications into complete, tested, deployable pull requests — matching existing codebase conventions across multiple products, languages, and frameworks.

BCHAD takes a feature description (natural language or structured JSON), decomposes it into ordered generation stages, generates code using codebase-aware prompts, verifies each stage against the target product's CI toolchain, and assembles the output into a reviewable PR. Nothing ships without human review.

---

## How It Works

```
Feature spec → Parse → Plan (DAG) → Generate per stage → Verify per stage → Assemble PR
                                          ↑                      |
                                          |                      ↓
                                    Codebase intelligence    Error classify → retry or escalate
```

1. **Spec input.** An engineer writes a feature description — either a natural language brief ("Add payment methods management to the merchant dashboard...") or a structured JSON spec. If natural language, BCHAD translates it to a structured BCHADSpec and asks the engineer to confirm.

2. **Plan generation.** BCHAD decomposes the spec into a DAG of generation stages (migrate → api → frontend → tests, with config running in parallel). The plan includes per-stage model selection, cost estimates, codebase references, and approval gates. The engineer reviews and can modify the plan before execution.

3. **Stage execution.** Each stage retrieves relevant code patterns from the target product's codebase intelligence index, assembles a prompt within a managed token budget, generates code, and runs a Tier 1 verification gate (lint, typecheck, tests, security scan). Failures are classified by error type and routed to differentiated recovery strategies.

4. **PR assembly.** Completed stages are committed (one per stage) to a feature branch. A Tier 2 integration gate runs the target product's full CI pipeline against the assembled PR. The final PR includes a generation report with what was produced, why, cost summary, and review guidance.

5. **Engineer review.** The engineer reviews the PR as they would any PR — with full visibility into the generation process, the ability to re-run stages, and the option to edit and resume.

---

## Architecture

BCHAD is organized into four planes:

**Control Plane** — Spec parser, plan generator, DAG execution engine (Temporal workflows), and state store (PostgreSQL). Manages the lifecycle of every pipeline run.

**Intelligence Plane** — Codebase index (structural profiles in S3, code pattern embeddings in pgvector), pattern library (DAG templates, prompt templates, language adapters in a Git repo), and retrieval service (filtered vector search within token budgets).

**Execution Plane** — Stage executor (prompt assembly via Context Budget Allocator, LLM calls via in-process gateway), verification gates (Tier 1 per-stage in Docker containers, Tier 2 integration via full CI), error classifier, and PR assembler.

**Integration Plane** — GitHub API (repo operations, PR creation, webhooks), CI runner, Vault (credential patterns), Slack (notifications, approvals).

---

## Tech Stack

| Layer | Technology |
|---|---|
| Core runtime | Go |
| API | Go (Chi) |
| Web UI | TypeScript / Next.js |
| Orchestration | Temporal (Go SDK) |
| State store | PostgreSQL 16 |
| Vector store | pgvector (in PostgreSQL) |
| Object storage | S3 (MinIO for local dev) |
| Cache & messaging | Valkey 8 |
| LLM gateway | In-process Go library (Anthropic API) |
| Code analysis | go-tree-sitter + LSPs |
| Git operations | go-git + go-github |
| Containerization | Docker on ECS Fargate |
| Secrets | AWS Secrets Manager |
| Observability | OpenTelemetry + Grafana + Loki + Tempo |
| Security scanning | Semgrep + Trivy |
| Infrastructure | Terraform + AWS |

See [bchad-techstack.md](docs/bchad-techstack.md) for detailed rationale behind each choice.

---

## Project Structure

```
bchad/
├── cmd/
│   ├── bchad/              # CLI entrypoint
│   └── worker/             # Temporal worker entrypoint
├── internal/
│   ├── spec/               # Spec parser, NL translator, BCHADSpec validation
│   ├── plan/               # Plan generator, DAG templates, cost estimator
│   ├── engine/             # DAG execution engine, stage dispatcher
│   ├── gateway/            # LLM gateway (model routing, token budgeting, cost tracking)
│   ├── retrieval/          # Retrieval service (pgvector queries, context ranking)
│   ├── intelligence/       # Codebase indexer, pattern extractor, convention scanner
│   ├── verify/             # Verification gate runner, error classifier
│   ├── assembly/           # PR assembler (branch, commits, description)
│   ├── budget/             # Context Budget Allocator (token partitioning)
│   ├── trust/              # Trust score computation, phase transitions
│   └── adapters/           # Language adapters (TypeScript, Python, Go)
├── pkg/
│   ├── bchadspec/          # BCHADSpec schema types and validation
│   ├── bchadplan/          # BCHADPlan schema types and validation
│   └── artifacts/          # Stage artifact types
├── workflows/              # Temporal workflow and activity definitions
├── patterns/               # Pattern library (DAG templates, prompt templates)
│   ├── crud_ui/
│   │   ├── dag.yaml
│   │   └── prompts/
│   │       ├── migrate.tmpl
│   │       ├── api.tmpl
│   │       ├── frontend.tmpl
│   │       ├── tests.tmpl
│   │       └── config.tmpl
│   └── adapters/
│       ├── typescript.yaml
│       ├── python.yaml
│       └── go.yaml
├── schemas/                # JSON Schema definitions (Draft 2020-12)
│   ├── bchadspec.v1.json
│   ├── bchadplan.v1.json
│   ├── stage_artifact.v1.json
│   └── gate_result.v1.json
├── migrations/             # Postgres schema migrations (golang-migrate)
├── semgrep/                # Custom Semgrep rules for security verification
├── docker/                 # Verification gate container images
│   ├── verify-ts/
│   ├── verify-py/
│   ├── verify-go/
│   └── security-scan/
├── web/                    # Next.js review UI
├── terraform/              # Infrastructure as code
│   ├── modules/
│   ├── environments/
│   │   ├── dev/
│   │   ├── staging/
│   │   └── production/
│   └── main.tf
├── scripts/                # Development and operational scripts
├── docs/                   # Architecture docs, tech stack, framework
├── docker-compose.yml      # Local development stack
├── justfile                # Task runner for dev workflows
└── go.mod
```

---

## Getting Started

### Prerequisites

- Go 1.22+
- Docker and Docker Compose
- Node.js 20+ (for the web UI and TypeScript verification gates)
- [just](https://github.com/casey/just) (task runner)
- [Temporal CLI](https://docs.temporal.io/cli) (local Temporal server)

### Local Development Setup

```bash
# Clone the repo
git clone https://github.com/athena-digital/bchad.git
cd bchad

# Start the local infrastructure stack
# (Postgres + pgvector, Valkey, MinIO, Temporal dev server)
just dev-up

# Run database migrations
just migrate

# Seed a test codebase profile (payments-dashboard-test)
just seed

# Build and run the BCHAD CLI
go build -o bin/bchad ./cmd/bchad
./bin/bchad --help

# In a separate terminal, start the Temporal worker
go run ./cmd/worker

# Run a test pipeline
./bin/bchad run --spec examples/payment-methods.json
```

### Environment Variables

```bash
# Required
BCHAD_DATABASE_URL=postgres://bchad:bchad@localhost:5432/bchad?sslmode=disable
BCHAD_VALKEY_URL=localhost:6379
BCHAD_S3_ENDPOINT=http://localhost:9000        # MinIO
BCHAD_S3_BUCKET_PROFILES=bchad-codebase-profiles
BCHAD_S3_BUCKET_ARTIFACTS=bchad-artifacts
BCHAD_TEMPORAL_HOST=localhost:7233
BCHAD_TEMPORAL_NAMESPACE=bchad

# LLM API keys
ANTHROPIC_API_KEY=sk-ant-...
VOYAGE_API_KEY=pa-...

# GitHub (for PR creation and webhook ingestion)
GITHUB_TOKEN=ghp_...

# Optional (defaults shown)
BCHAD_LOG_LEVEL=info
BCHAD_COST_THRESHOLD=10.00                     # pause plan if projected cost exceeds this
BCHAD_TIER2_TIMEOUT=15m                        # integration gate timeout
```

### Common Development Tasks

```bash
just dev-up          # Start local infrastructure
just dev-down        # Stop local infrastructure
just test            # Run all tests
just test-unit       # Run unit tests only
just test-int        # Run integration tests (requires local stack)
just migrate         # Run database migrations
just migrate-down    # Rollback last migration
just seed            # Seed test data
just lint            # Run golangci-lint
just fmt             # Format code (gofumpt)
just build           # Build CLI and worker binaries
just snapshot-update # Update snapshot tests
```

---

## Key Concepts

### BCHADSpec

The normalized input to the factory. Every feature, whether entered as natural language or JSON, is parsed into a BCHADSpec before plan generation. The schema defines: product, pattern type, entity, fields (with types and constraints), permissions, audit requirements, integrations, and UI configuration.

### BCHADPlan

The generation plan — a DAG of stages with dependencies, model selection, cost estimates, and approval gates. Generated by the Plan Generator from a BCHADSpec + codebase profile + pattern template. Reviewed by the engineer before execution.

### Language Adapters

The abstraction that keeps the pipeline language-agnostic. Each adapter maps pattern-level intent ("create REST endpoints") to language-specific instructions ("create Express route handlers using the existing controller pattern") and declares the verification toolchain (typecheck command, lint command, test command). Adding a new language means writing a new adapter YAML file, not modifying the pipeline.

### Context Budget Allocator

Manages the LLM context window. Partitions available tokens across: system prompt (fixed), language adapter context (fixed), upstream stage outputs (high priority), primary codebase examples (high), secondary examples (medium), architectural notes (medium), generation instruction (fixed), and output buffer (reserved). Fills sections in priority order, truncating gracefully when the budget is tight.

### Error Taxonomy

Eight error categories with differentiated recovery:

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

### Trust Phases

Trust is earned per-engineer per-product through demonstrated reliability, not granted on a schedule:

- **Phase 1 (Supervised):** Score < 60 or < 5 runs. Every stage pauses for approval.
- **Phase 2 (Gated):** Score 60–85, ≥ 5 runs. Only checkpoints pause (plan, migration, PR).
- **Phase 3 (Monitored):** Score > 85, ≥ 15 runs. Pipeline runs end-to-end. Engineer reviews final PR.

---

## Configuration

### Product Onboarding

To onboard a new product:

```bash
# Step 1: Automated scan (produces structural profile)
./bin/bchad index --repo https://github.com/athena-digital/product-repo.git

# Step 2: Pattern extraction (analyzes recent PRs)
./bin/bchad index --repo ... --extract-patterns

# Step 3: Tech lead questionnaire (interactive)
./bin/bchad onboard --product product-name

# Step 4: Validation generation (generates a throwaway feature for review)
./bin/bchad validate --product product-name --pattern crud_ui
```

### Per-Product Configuration

Each product's configuration lives in the codebase profile (stored in S3) and can be overridden by the tech lead:

```yaml
product: payments-dashboard
language: typescript
adapter: typescript
database: postgres
orm: prisma
frontend_framework: react
compliance:
  soc2: true
  hipaa: false
stage_overrides:
  migrate:
    model: claude-haiku-3.5        # default for this stage type
    human_approval: true            # always require approval for migrations
  api:
    model: claude-sonnet-4
  frontend:
    model: claude-sonnet-4
```

---

## Deployment

BCHAD runs on AWS ECS Fargate. Infrastructure is managed via Terraform.

```bash
cd terraform/environments/production
terraform init
terraform plan
terraform apply
```

### Services

| Service | ECS Config | Notes |
|---|---|---|
| Control Plane | 2 tasks, 1 vCPU / 4 GB, behind ALB | Go binary, Chi API |
| Temporal Worker | 2 tasks, 1 vCPU / 2 GB | Connects to Temporal Cloud |
| Web UI | 2 tasks, 0.5 vCPU / 1 GB, behind ALB | Next.js |
| Verification Gates | On-demand RunTask, 2 vCPU / 4 GB | Ephemeral, per-stage |
| Indexing Jobs | On-demand RunTask, 1 vCPU / 2 GB | Triggered by webhook or schedule |

### Managed Services

| Service | AWS Product |
|---|---|
| Database + vector store | RDS PostgreSQL 16 + pgvector |
| Cache & messaging | ElastiCache Valkey 8 |
| Object storage | S3 |
| Orchestration | Temporal Cloud |
| Secrets | AWS Secrets Manager |
| Observability | Grafana stack on Fargate + CloudWatch |

---

## Testing

```bash
# Unit tests
just test-unit

# Integration tests (requires local stack running)
just test-int

# Temporal workflow tests (deterministic replay, no server needed)
go test ./workflows/...

# End-to-end test (full pipeline against test repo)
just test-e2e

# Snapshot tests (prompt templates, plan output, PR descriptions)
just test-snapshot
```

### Validation Protocol

Before any release, run the validation suite against historical features:

```bash
# Run 60 features (30 per product) from the validation set
./bin/bchad validate --suite validation/v1 --products payments-dashboard,claims-portal

# Output: CI pass rate, cleanup time distribution, error category breakdown
```

Target: ≥ 80% CI pass rate, median cleanup under 30 minutes.

---

## Related Documents

| Document | Description |
|---|---|
| [Case Study](docs/software-factory-case-study.md) | The Athena Digital scenario and assignment that motivated BCHAD |
| [Framework](docs/bchad-framework-v2.md) | Architecture, codebase intelligence, orchestration, verification, trust model |
| [Tech Stack](docs/bchad-techstack.md) | Technology selections with rationale for every component |
| [PRD](docs/PRD.md) | Product requirements, success metrics, scope, and risks |

---

## License

Internal to Athena Digital. Not open source.
