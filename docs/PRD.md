# BCHAD: Product Requirements Document

**SF-2026-04 · Rev. 2 · Athena Digital**

*Product requirements for BCHAD, a software factory that transforms feature specifications into complete, tested, deployable pull requests across Athena Digital's product portfolio.*

---

## 1. Problem Statement

Athena Digital's 180 engineers use AI coding tools daily, but measured cycle time has improved only 11%. A time study across three squads found that writing code accounts for 22% of engineering time — the phase AI tools accelerate. The remaining 78% (requirements decomposition, architecture decisions, environment setup, CI configuration, review, and integration) moves at human speed.

The last 100 features shipped across all seven products follow four repeating patterns: CRUD + UI (38%), Integration (24%), Workflow (22%), and Analytics (16%). Each pattern has a predictable anatomy — database changes, API layer, frontend, tests, configuration — but every feature is built from scratch. Engineers re-make decisions that have been made before on other products.

BCHAD is a production system that takes a high-level feature specification and outputs a complete, tested, deployable pull request conforming to the target product's existing conventions. It addresses the 78% of work that current AI tools don't touch: decomposing requirements into ordered generation tasks, retrieving relevant codebase conventions, generating code stage-by-stage with verification at each step, and assembling the output into a reviewable PR.

### What BCHAD Is Not

BCHAD is not a code completion tool, not an autonomous agent, and not a replacement for engineers. It is a pipeline that automates the structured, repeatable work of turning a well-understood feature pattern into code — with human review at every critical decision point. Nothing ships without an engineer's approval.

---

## 2. Goals

### Primary Goals (v1)

**G1 — Reduce CRUD+UI feature cycle time from 3.5 days to under 1 hour of engineer time** (specification + review), with the factory handling generation, verification, and PR assembly.

**G2 — Achieve an 80% first-run CI pass rate** on factory-generated PRs, measured after the full pipeline (including the integration gate's fix attempts) but before any human intervention.

**G3 — Produce code indistinguishable from human-written code** in blind review, scoring within 0.5 points on a 1–5 scale across convention adherence, readability, completeness, test quality, and security practices.

**G4 — Build engineer trust through transparency**, giving engineers full visibility into what the factory generated, why, and how — with the ability to intervene, correct, and re-run at any stage.

### Secondary Goals

**G5 — Keep per-feature cost under $5.00** in LLM API spend, with cost projected before execution and tracked per stage.

**G6 — Design for incremental extension** to all four patterns and all seven products without architectural changes.

**G7 — Maintain SOC 2 and HIPAA compliance** with a full audit trail of every factory action, every prompt, and every approval decision.

---

## 3. Users and Personas

### Primary: Product Squad Engineers

The 4–8 engineers on each product squad who currently build features manually. They will write feature specifications (natural language or structured JSON), review generation plans, approve migrations, and review final PRs. Their trust must be earned through demonstrated reliability — they start in supervised mode and graduate to less oversight as the factory proves itself on their product.

### Secondary: Tech Leads

One per product squad. They onboard their product to BCHAD by validating extracted conventions, completing the architectural questionnaire, and reviewing the codebase intelligence profile quarterly. They set product-specific policies (which stages require approval, which models to use per stage).

### Tertiary: Engineering Leadership

VP of Engineering and CTO. They consume aggregate metrics: CI pass rates, cost per feature, time savings, trust score progression across squads. They do not interact with the factory directly.

---

## 4. Functional Requirements

### 4.1 Specification Input

**FR-1.** The system shall accept feature specifications as structured JSON conforming to the BCHADSpec schema.

**FR-2.** The system shall accept natural language product briefs and translate them into structured BCHADSpec JSON via a single LLM call, presenting the result for engineer confirmation before proceeding.

**FR-3.** The NL translator shall mark ambiguous fields as `needs_clarification: true` with a human-readable reason, rather than guessing.

**FR-4.** The system shall validate all specifications against the BCHADSpec JSON Schema (Draft 2020-12) before proceeding to plan generation.

**FR-5.** The system shall resolve product-specific conventions (table naming, route prefixes, component directory structure) from the codebase intelligence profile during spec parsing.

### 4.2 Codebase Intelligence

**FR-6.** The system shall index each target product repository, extracting: structural conventions (directory layout, file naming), code patterns (3–5 canonical examples per stage type), architectural decisions (auth model, permission system, feature flags), dependency graph (internal packages, external libraries, versions), and style/formatting configuration (linter, formatter, TypeScript strictness).

**FR-7.** The system shall embed code patterns using Voyage Code 3 and store embeddings in pgvector for filtered vector similarity search.

**FR-8.** The system shall retrieve stage-specific context at generation time: migration stage retrieves recent migrations and DB conventions; API stage retrieves route patterns, middleware chains, and error handling; frontend stage retrieves component patterns, state management approach, and shared component library usage; test stage retrieves test framework config, assertion patterns, and fixture conventions.

**FR-9.** The system shall support cold-start onboarding of a new product in under 4 hours total (automated scan + pattern extraction + 30-minute tech lead questionnaire + validation generation).

**FR-10.** The system shall re-index incrementally on every merged PR (lightweight, files touched only) and fully on a weekly schedule.

**FR-11.** Tech leads shall be able to manually override extracted conventions at any time, with manual overrides taking precedence over automated extraction.

### 4.3 Plan Generation and Orchestration

**FR-12.** The system shall decompose each feature specification into an ordered DAG of generation stages based on the detected pattern, with explicit dependencies between stages.

**FR-13.** The CRUD+UI pattern shall decompose into: migrate, config, api (depends on migrate), frontend (depends on api), and tests (depends on api, frontend, config).

**FR-14.** Each stage in the plan shall specify: the generation model, estimated cost, estimated file count, required codebase references, and whether human approval is required before execution.

**FR-15.** The plan shall include a projected total cost. If the projected cost exceeds a configurable threshold (default: $10), the plan shall pause for human review before execution.

**FR-16.** Independent stages (e.g., migrate and config) shall execute in parallel where dependencies allow.

**FR-17.** Upstream stage outputs (schema definitions, API contracts, component paths) shall be injected into downstream stage prompts automatically.

**FR-18.** If a stage fails after exhausting retries, the pipeline shall pause at that stage. Successfully completed upstream stages shall be preserved and not re-executed. Independent stages shall continue. The engineer shall be notified with failure context.

### 4.4 Code Generation

**FR-19.** The system shall generate code using a five-layer prompt structure: system prompt (fixed per BCHAD version), language adapter context (framework-specific rules), codebase brief (retrieved examples), upstream context (outputs from completed stages), and generation instruction (parameterized from the plan).

**FR-20.** The Context Budget Allocator shall partition the available context window across prompt sections using a priority system, ensuring prompts stay within model limits.

**FR-21.** The system shall support language adapters for TypeScript and Python (v1), mapping pattern-level intent to language-specific generation prompts and verification toolchains. The Go adapter shall ship in v2.

**FR-22.** Model selection shall be configurable per stage and per product. Default: Haiku 3.5 for migrate and config stages; Sonnet 4 for api, frontend, tests, and NL spec translation.

**FR-23.** On retry, the prompt shall include error context (exact error output) and, for Context and Logic errors, a corrective example retrieved from the codebase.

### 4.5 Verification

**FR-24.** Every stage shall pass a Tier 1 verification gate before its output advances. Gates shall run in disposable Docker containers using the target product's toolchain configuration.

**FR-25.** Tier 1 universal checks (all stages): syntax validity, lint pass (target repo's config), no undeclared dependencies, no hardcoded credentials, files in correct directories.

**FR-26.** Tier 1 stage-specific checks: migration (SQL valid, rollback exists, no unacknowledged destructive operations); API (type-check, no route conflicts, auth middleware, permission checks, audit logging); frontend (type-check, components render, existing component library used); config (flag/permission naming conventions, no duplicates); tests (all pass, coverage threshold met, meaningful assertions).

**FR-27.** After PR assembly, the system shall run a Tier 2 integration gate: spin up the target product's full CI environment and run the complete CI pipeline against the generated branch. On failure, attempt a targeted fix (re-generate only affected files with integration error as context), with a maximum of 2 fix attempts.

**FR-28.** The system shall classify verification failures into eight categories (Syntax, Style, Type, Logic, Context, Specification, Conflict, Security) and route each to a differentiated recovery strategy with category-specific retry limits.

**FR-29.** Security-specific verification for SOC 2/HIPAA products: sensitive fields must use Vault integration (never stored raw); every endpoint must include auth middleware; audit logging must be present on state-changing operations; destructive migrations require explicit human approval.

### 4.6 PR Assembly

**FR-30.** The system shall create a feature branch, generate one commit per completed stage with descriptive commit messages, and push via the GitHub API.

**FR-31.** The PR description shall include: a summary of what was generated, a generation report (pattern, stages, files, retries with error categories, tier 1/tier 2 results, human approvals, total cost vs. projected cost), codebase references used, and review guidance per stage.

### 4.7 Human Interface and Trust

**FR-32.** The system shall present the generation plan for engineer review before execution, including the DAG, per-stage details, codebase references, and projected cost. The engineer shall be able to modify the plan (change dependencies, swap codebase references, remove stages).

**FR-33.** Database migrations shall always pause for human approval. The engineer shall see the exact migration SQL, rollback SQL, and schema diff.

**FR-34.** Stages touching sensitive data or compliance-regulated products shall pause for approval, with sensitive-field handling highlighted.

**FR-35.** At any point, the engineer shall be able to: edit generated files and resume (downstream stages consume edited output); re-run a stage with a guidance note prepended to the prompt; override codebase context by pointing to specific reference files; skip a stage and provide manual files; abort and keep partial output.

**FR-36.** The system shall compute a trust score per engineer per product, based on: CI pass rate (weight 0.30), human edit volume (0.25), stage retry rate (0.15), engineer override count (0.15), and time to merge (0.15).

**FR-37.** Trust phases: Phase 1 (Supervised, score < 60 or < 5 runs) — every stage pauses for approval; Phase 2 (Gated, score 60–85, ≥ 5 runs) — only checkpoints pause; Phase 3 (Monitored, score > 85, ≥ 15 runs) — pipeline runs end-to-end, engineer reviews only final PR.

**FR-38.** If the trust score drops below a phase threshold for 3 consecutive runs, the system shall automatically downgrade to the lower phase with a notification.

### 4.8 Observability and Audit

**FR-39.** Every pipeline run, stage transition, approval decision, error classification, and cost measurement shall be persisted to the state store (PostgreSQL).

**FR-40.** Every prompt sent and response received shall be logged (full text hashed and stored in S3, references in Postgres) for audit and debugging.

**FR-41.** Distributed tracing shall span the full pipeline: each LLM call, retrieval query, and verification gate execution as a span in a single trace tied to the pipeline run ID.

**FR-42.** The system shall expose metrics for: pipeline duration, stage duration, token consumption, cost per stage/run, gate pass rate, error category counts, retrieval latency, and trust scores.

---

## 5. Non-Functional Requirements

### 5.1 Performance

**NFR-1.** End-to-end pipeline for a CRUD+UI feature (5 stages) shall complete in under 15 minutes wall-clock time, excluding human approval wait time.

**NFR-2.** Codebase retrieval queries (filtered vector search) shall return results in under 100ms at corpus sizes up to 100,000 rows.

**NFR-3.** Tier 1 verification gates shall complete in under 60 seconds per stage.

### 5.2 Reliability

**NFR-4.** Pipeline state shall be durable across process crashes, deployment rollouts, and infrastructure failures. A pipeline that is halfway through generation shall resume from the last completed stage, not restart.

**NFR-5.** The system shall handle LLM API transient errors (429, 5xx) with exponential backoff, separate from the error taxonomy retry logic.

### 5.3 Security and Compliance

**NFR-6.** All data-plane components shall run in private subnets. Only the load balancer shall be internet-facing.

**NFR-7.** Secrets (API keys, database credentials, GitHub tokens) shall be managed via AWS Secrets Manager with CloudTrail audit logging.

**NFR-8.** Generated code for SOC 2/HIPAA products shall pass Semgrep security scanning rules (credential detection, auth enforcement, sensitive data handling) as part of the verification gate.

**NFR-9.** The full audit trail (every prompt, every approval, every error, every cost) shall be retained for the period required by SOC 2 compliance.

### 5.4 Scalability

**NFR-10.** The system shall support up to 7 products and 4 patterns without architectural changes.

**NFR-11.** The system shall handle up to 50 concurrent pipeline runs (well above the expected dozens per day).

### 5.5 Cost

**NFR-12.** Per-feature LLM API cost shall remain under $5.00 for CRUD+UI features, with cost projected before execution and tracked per stage.

**NFR-13.** Infrastructure cost for the full BCHAD deployment shall remain under $3,000/month (RDS, ElastiCache, ECS Fargate, S3, Temporal Cloud, Grafana stack).

---

## 6. Scope

### v1: 60 Days — CRUD+UI Pattern, Two Products

| Area | v1 Scope |
|---|---|
| Patterns | CRUD + UI only (38% of features) |
| Products | Payments Dashboard (TypeScript/Postgres), Claims Portal (Python/Postgres) |
| Language adapters | TypeScript, Python |
| Codebase intelligence | Full index for 2 products; automated scan + pattern extraction + tech lead questionnaire |
| Spec input | NL-to-BCHADSpec translator with confirmation; JSON direct input |
| Plan generation | CRUD+UI DAG template with parameterization and cost estimation |
| Generation engine | Stage execution with five-layer prompts, context budget allocator, model selection per stage |
| Verification | Tier 1 stage gates + Tier 2 integration gate; classified error taxonomy |
| Human interface | CLI-based: plan review, migration approval, diff preview, stage re-run, edit-and-resume |
| Trust model | Data-driven trust score with phase transitions from day 1 |
| Cost tracking | Projected cost in plans, actual cost logged per stage |
| PR assembly | Auto-branch, one commit per stage, generated PR description with generation report |

### v2: Days 61–120

| Area | v2 Scope | Rationale |
|---|---|---|
| Patterns | Add Integration (24% of features) | Second most common; requires external service mocking |
| Products | Add 2 more (total 4 of 7) | First Go product and first MongoDB product |
| Language adapters | Add Go | Required for Go product onboarding |
| Parallel execution | DAG-based parallel stage dispatch | Reduces generation time; deferred from v1 for debuggability |
| Web UI | Review interface with visual DAG, inline diffs, one-click approval | Replaces CLI for non-terminal workflows |
| Codebase sync | Post-merge hooks for incremental re-index | Keeps intelligence current without manual intervention |

### v3: Days 121–180

| Area | v3 Scope | Rationale |
|---|---|---|
| Patterns | Add Workflow and Analytics | Completes four-pattern coverage; Workflow is most complex |
| Products | All 7 onboarded | Full portfolio |
| Self-improvement | Feedback loop from engineer corrections into codebase intelligence | Factory learns from corrections |
| Metrics dashboard | CI pass rate, intervention rate, cost per feature/product | Quantifies performance |
| Template composition | Features spanning multiple patterns | Handles the 15–20% that don't fit a single pattern |

### Deferred Indefinitely

| Item | Rationale |
|---|---|
| Fully autonomous mode | Violates human-in-the-loop constraint |
| Infrastructure/Terraform generation | Different risk profile; separate system |
| Cross-product features | Requires inter-service coordination beyond scope |

---

## 7. Success Metrics

| Metric | v1 Target | Measurement Method |
|---|---|---|
| CI pass rate on first run (Tier 2) | ≥ 80% | Factory-generated PRs run through full CI pipeline |
| Human cleanup time per feature | < 30 minutes | Time from PR creation to merge, minus review deliberation |
| Engineer time per CRUD+UI feature | < 1 hour (spec + review) | Compared to 3.5-day baseline |
| Stage retry rate | < 30% of stages require retry | Logged per stage, categorized by error type |
| Cost per CRUD+UI feature | < $5.00 | API spend tracked per stage, per run |
| Engineer trust progression | ≥ 50% reach Phase 2 within 5 runs | Trust score, not calendar time |
| Codebase intelligence accuracy | ≥ 90% rated "correct" by tech leads | Quarterly profile review |
| Error classification accuracy | ≥ 85% correctly categorized | Spot-check by engineers reviewing error trails |
| Blind review quality | Factory output within 0.5 points of human-written | Blind code review during validation |

---

## 8. Validation Protocol

Before v1 launch, the factory shall be validated against 60 features (30 per product) drawn from the last 6 months of shipped CRUD+UI features. Features are selected by a validation team (not the BCHAD engineers) with a proportional mix: 40% simple, 40% medium, 20% complex.

Each feature undergoes three assessments: (1) CI pass — does the PR pass the full CI pipeline on first run? (2) Cleanup time — an engineer who did not build the original feature reviews and brings the output to merge-ready; target under 30 minutes. (3) Blind code review — a senior engineer rates factory and human code side-by-side (without knowing which is which) on a 1–5 scale for convention adherence, readability, completeness, test quality, and security.

Results are reported with confidence intervals. Systematic failures are categorized by the error taxonomy and fed back into prompt tuning and codebase profile updates.

---

## 9. Dependencies and Risks

### External Dependencies

| Dependency | Risk | Mitigation |
|---|---|---|
| Anthropic API (Claude Haiku 3.5, Sonnet 4) | Rate limits, pricing changes, API instability | Rate limiting via Valkey; cost guardrails in plan; model selection is configurable |
| Voyage AI API (Code 3 embeddings) | Service availability | Embeddings are cached; re-index is async; retrieval degrades to keyword fallback |
| GitHub API | Rate limits, webhook reliability | go-git for local Git ops; GitHub API only for PR creation and webhook ingestion |
| Temporal Cloud | Service availability | Workflow state is durable; Temporal resumes on reconnect |

### Key Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Codebase intelligence extracts wrong conventions | Medium | High — factory generates non-idiomatic code | Tech lead validation during onboarding; quarterly profile review; manual override capability |
| 80% CI pass rate not achieved in v1 | Medium | High — undermines trust and adoption | Classified error taxonomy targets the specific failure modes; validation protocol catches issues before launch |
| Engineers reject factory output as "not my code" | Medium | High — adoption failure | Blind code review in validation proves quality; trust model starts supervised; engineers can intervene at every stage |
| LLM model quality regression after update | Low | Medium — pass rates drop | Prompt versioning tracks which version produced which output; model selection is configurable per stage |
| Context window insufficient for complex stages | Low | Medium — frontend stage for large products | Context Budget Allocator with priority-based filling; truncation strategies; model upgrade path |

---

## 10. Open Questions

**OQ-1.** Should the v1 CLI support Slack-based approval workflows, or is terminal-only sufficient for the initial three engineers plus early adopters?

**OQ-2.** What is the retention policy for prompt audit logs in S3? SOC 2 requires a defined period; the default lifecycle policy should be set before launch.

**OQ-3.** How should the factory handle features that are 80% CRUD+UI but include a small non-CRUD element (e.g., a CRUD entity with one custom aggregation endpoint)? Should v1 generate the CRUD portion and flag the rest for manual implementation, or defer the entire feature?

**OQ-4.** Should the trust score be visible to engineering leadership, or only to the individual engineer and their tech lead? Visibility affects whether it's perceived as a quality signal or a performance metric.

---

*SF-2026-04 · Rev. 2 · March 2026*
