# BCHAD: A Software Factory Framework for Multi-Product Engineering

**SF-2026-03 · Rev. 2 · Athena Digital**

*BCHAD is a software factory framework designed for engineering organizations operating multiple products with existing codebases. It synthesizes structured planning from BMAD, iterative verification from Ralph, and introduces codebase intelligence, dependency-aware orchestration, cross-language generation, and differentiated error recovery as first-class components.*

---

## 1. Framework Overview

### 1.1 Design Principles

BCHAD is built on seven principles derived from the strengths and failure modes of BMAD and Ralph:

**Plan before you generate.** Every feature flows through a structured decomposition phase before any code is written. This is BMAD's core insight — front-loaded planning produces richer context and fewer generation errors. But unlike BMAD, BCHAD's planning phase is automated and pattern-aware, not a manual persona-driven conversation.

**Verify at every stage, not at the end.** Each generation step includes its own verification loop — lint, type-check, test, self-correct — before output passes to the next stage. This is Ralph's core insight. A stage doesn't produce "output" until that output passes its verification gate.

**Learn the codebase, don't assume it.** Neither BMAD nor Ralph indexes existing codebases. BCHAD treats codebase intelligence as infrastructure: every target repo is indexed, conventions are extracted, and relevant patterns are retrieved at generation time. The factory generates code that looks like the team wrote it because it has seen what the team writes.

**Orchestrate with dependencies, not sequences.** BMAD enforces a rigid linear handoff. Ralph picks tasks from a flat list. BCHAD uses a directed acyclic graph (DAG) where stages declare dependencies and execute in parallel where possible, with mid-pipeline failure recovery at the individual stage level.

**Show the work, earn the trust.** Engineers reject black boxes. Every stage produces a human-readable artifact. Engineers can inspect, override, and re-run any stage independently. The factory earns trust by being transparent, not by being autonomous.

**One system, many languages.** Seven repos across TypeScript, Python, and Go cannot mean seven separate generation systems. BCHAD uses language adapters that translate pattern-level intent into language-specific generation and verification, so the orchestration layer stays language-agnostic while output is native to each codebase.

**Budget every token, track every dollar.** LLM context windows are finite and LLM calls cost money. BCHAD manages both explicitly: a context budget allocator partitions the prompt window across retrieval results, upstream outputs, and generation instructions; a cost model projects per-feature spend before execution begins.

### 1.2 How BCHAD Differs from BMAD and Ralph

| Dimension | BMAD | Ralph | BCHAD |
|---|---|---|---|
| Planning | Manual persona handoff (Analyst → Architect → PM) | Flat PRD, agent self-selects tasks | Automated pattern-aware decomposition into a dependency DAG |
| Codebase awareness | None; relies on manual prompt context | None; agent reads files ad hoc during execution | Structured codebase index with convention extraction and retrieval |
| Execution model | Sequential, one-shot per phase | Iterative loop, nondeterministic task selection | DAG-ordered stages, each with bounded verification loops |
| Verification | QA agent at the end of the pipeline | Feedback loop per iteration (lint, test, commit) | Two-tier: per-stage gates + full-pipeline integration gate |
| Failure recovery | Restart from the failed phase manually | Agent retries in next loop iteration | Classified error taxonomy routes failures to differentiated recovery strategies |
| Multi-product support | One project at a time | One repo at a time | Product-agnostic patterns specialized by codebase intelligence + language adapters |
| Multi-language support | Manual prompt context per language | None | Language adapter abstraction: one pipeline, per-language prompt and verification toolchains |
| Parallelism | None — strict sequential | Agent decides (unpredictable) | Explicit — independent stages run concurrently |
| Transparency | Heavy documentation artifacts | Git history only | Structured artifacts at each stage with diff previews |
| Cost visibility | None | None | Per-stage cost estimation with projected totals in every plan |
| Scalability | Breaks at scale (verbose, prompt-heavy) | Simple but nondeterministic | Pattern templates reduce per-feature overhead; DAG scales to complex features |

---

## 2. Architecture

### 2.1 System Architecture

BCHAD is organized into four planes: the **integration plane** (external services), the **control plane** (orchestration and state), the **intelligence plane** (codebase indexing and retrieval), and the **execution plane** (generation, verification, and assembly). Every interface between components uses a versioned JSON schema (`BCHADSpec`, `BCHADPlan`, stage artifacts), and every state transition is persisted for auditability and crash recovery.

```
┌─────────────────────────────── INTEGRATION PLANE ────────────────────────────────┐
│                                                                                   │
│  GitHub API         CI Runner         LaunchDarkly      Vault         Slack        │
│  (repo read/write,  (lint, typecheck, (feature flag     (secret       (notifica-   │
│   PR creation,       test execution,   registration     retrieval,    tions,       │
│   webhook receive)   coverage report)  verification)    audit check)  approvals)   │
│                                                                                   │
└───────┬──────────────────┬──────────────────┬──────────────┬─────────────┬────────┘
        │                  │                  │              │             │
┌───────┴──────────────────┴──────────────────┴──────────────┴─────────────┴────────┐
│                              CONTROL PLANE                                         │
│                                                                                    │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────────────────────────────┐  │
│  │ Spec Parser   │    │ Plan         │    │ DAG Execution Engine                 │  │
│  │              │    │ Generator    │    │                                      │  │
│  │ IN:  JSON or │    │ IN: Norma-   │    │ IN:  Approved DAG Plan               │  │
│  │   NL brief   │───▶│   lized spec │───▶│ OUT: Stage results + assembled PR    │  │
│  │ OUT: Norma-  │    │   + codebase │    │                                      │  │
│  │   lized spec │    │   profile    │    │ Manages: stage ordering, parallel    │  │
│  │   (BCHADSpec │    │ OUT: DAG     │    │   dispatch, retry policy, approval   │  │
│  │   schema)    │    │   Plan       │    │   gates, failure isolation, error     │  │
│  └──────────────┘    │   (BCHADPlan │    │   classification + routing            │  │
│                      │   schema)    │    └──────────┬───────────────────────────┘  │
│                      └──────────────┘               │                              │
│                                                     │                              │
│  ┌──────────────────────────────────────────────────┴──────────────────────────┐   │
│  │                          STATE STORE (Postgres)                              │   │
│  │  Tables: bchad_runs, bchad_stages, bchad_artifacts, bchad_approvals,         │   │
│  │          bchad_metrics, bchad_error_log, bchad_trust_scores                  │   │
│  │  Every stage transition persisted. Full audit trail. Resumable on crash.     │   │
│  └─────────────────────────────────────────────────────────────────────────────┘   │
│                                                                                    │
└────────────────────────────────────────────────────────────────────────────────────┘
        │                                          │
        ▼                                          ▼
┌──────────────────────────────┐   ┌────────────────────────────────────────────────┐
│     INTELLIGENCE PLANE       │   │              EXECUTION PLANE                    │
│                              │   │                                                 │
│ ┌──────────────────────────┐ │   │ ┌──────────────────────────────────────────┐   │
│ │ Codebase Index (per repo)│ │   │ │ Stage Executor                           │   │
│ │ Storage: S3 + vector DB  │ │   │ │                                          │   │
│ │                          │ │   │ │ For each stage:                          │   │
│ │ - Structural profile     │◀┼───┼─│  1. Query Intelligence Plane             │   │
│ │   (JSON, file tree map)  │ │   │ │  2. Assemble prompt via Context Budget   │   │
│ │ - Code patterns          │ │   │ │     Allocator (see §5)                   │   │
│ │   (annotated snippets    │─┼───┼▶│  3. Call LLM (model per stage config)   │   │
│ │    in vector store)      │ │   │ │  4. Write files to workspace            │   │
│ │ - Arch decisions (JSON)  │ │   │ │  5. Run Verification Gate (Tier 1)      │   │
│ │ - Dependency graph (JSON)│ │   │ │  6. Classify errors, route recovery     │   │
│ │ - Style config (copied   │ │   │ │  7. Report result to Control Plane      │   │
│ │   linter/formatter files)│ │   │ └──────────────────────────────────────────┘   │
│ └──────────────────────────┘ │   │                                                │
│                              │   │ ┌──────────────────────────────────────────┐   │
│ ┌──────────────────────────┐ │   │ │ Verification Gates                      │   │
│ │ Pattern Library           │ │   │ │                                          │   │
│ │ Storage: Git repo         │ │   │ │ Tier 1 (per-stage): Runs in disposable  │   │
│ │                           │ │   │ │   Docker container. Lint, typecheck,     │   │
│ │ - DAG templates (YAML)   │ │   │ │   unit test, security scan.             │   │
│ │ - Prompt templates        │ │   │ │                                          │   │
│ │   (per stage × per lang) │ │   │ │ Tier 2 (integration): Runs full CI      │   │
│ │ - Language adapters       │ │   │ │   pipeline after PR assembly against    │   │
│ │ - Verification configs   │ │   │ │   product's complete test environment.  │   │
│ │ - Prompt version history │ │   │ └──────────────────────────────────────────┘   │
│ └──────────────────────────┘ │   │                                                │
│                              │   │ ┌──────────────────────────────────────────┐   │
│ ┌──────────────────────────┐ │   │ │ PR Assembler                             │   │
│ │ Retrieval Service         │ │   │ │                                          │   │
│ │                           │ │   │ │ Collects stage artifacts, creates       │   │
│ │ Accepts: stage type,     │ │   │ │ branch, generates per-stage commits,    │   │
│ │   entity, product ID     │ │   │ │ writes PR description + generation      │   │
│ │ Returns: ranked code     │ │   │ │ report + cost summary.                  │   │
│ │   examples + arch notes  │ │   │ │ Pushes via GitHub API.                  │   │
│ │   within token budget    │ │   │ └──────────────────────────────────────────┘   │
│ └──────────────────────────┘ │   │                                                │
└──────────────────────────────┘   └────────────────────────────────────────────────┘
```

**State Store.** Every pipeline run, stage transition, approval, and error is persisted to a Postgres database (`bchad_runs`, `bchad_stages`, `bchad_artifacts`, `bchad_approvals`, `bchad_metrics`, `bchad_error_log`, `bchad_trust_scores`). This makes runs resumable after crashes, enables the metrics dashboard, and provides the audit trail that SOC 2 requires.

**Storage locations.** Codebase profiles live in S3 (structural JSON) plus a vector database (code pattern embeddings for retrieval). The pattern library — DAG templates, prompt templates, language adapters, and verification configs — lives in its own Git repo with version history. Style configs (linter files, formatter settings) are copied directly from the target repo at index time and mounted into the verification container.

**Integration plane.** Every external service the factory touches is enumerated: GitHub (repo read/write, PR creation, webhook triggers), the CI runner (for full-pipeline integration verification), LaunchDarkly (feature flag registration), Vault (credential patterns), and Slack (engineer notifications and approval flows).

**Interface schemas.** The Spec Parser outputs a `BCHADSpec` schema. The Plan Generator outputs a `BCHADPlan` schema. These are versioned JSON schemas so that components can evolve independently. Every interface is typed and documented.

### 2.2 Data Flow: Spec to Pull Request

```
Feature Spec (JSON or natural language brief)
        │
        ▼
   ┌──────────────┐
   │  NL TRANSLATE │──▶ Draft BCHADSpec (if NL input)
   │  (optional)   │    presented for engineer confirmation
   └──────────────┘
        │
        ▼
   ┌─────────┐
   │  PARSE   │──▶ Normalized BCHADSpec + Pattern Classification
   └─────────┘
        │
        ▼
   ┌─────────┐     ┌──────────────────┐
   │  PLAN    │◀───│ Codebase Profile  │
   └─────────┘     │ + Language Adapter │
        │          └──────────────────┘
        ▼
   Generation DAG (with projected cost)
   ┌────────────────────────────────────────┐
   │                                        │
   │   migrate ──────▶ api ──────▶ frontend │
   │       │                          │     │
   │       └──────────┬───────────────┘     │
   │                  ▼                     │
   │   config ───▶  tests                  │
   │                                        │
   └────────────────────────────────────────┘
        │
        │  (each stage runs independently)
        │
        ▼  PER STAGE:
   ┌──────────────────────────────────────┐
   │  1. Retrieve codebase context        │
   │  2. Assemble prompt via Context      │
   │     Budget Allocator (5 layers,      │
   │     within token budget)             │
   │  3. Generate code via LLM            │
   │     (model selected per stage type)  │
   │  4. Run Tier 1 verification gate     │
   │     ├─ PASS → commit, advance        │
   │     └─ FAIL → classify error:        │
   │        ├─ Syntax/Style → auto-fix    │
   │        │    or direct retry           │
   │        ├─ Type/Logic → retry with    │
   │        │    targeted context          │
   │        ├─ Context → re-retrieve,     │
   │        │    then retry                │
   │        ├─ Security → retry once,     │
   │        │    then escalate             │
   │        └─ Specification → surface    │
   │             to engineer immediately   │
   └──────────────────────────────────────┘
        │
        ▼
   ┌──────────────────┐
   │  PR ASSEMBLY      │──▶ Branch + Commits + Diffs + Explanation + Cost Report
   └──────────────────┘
        │
        ▼
   ┌──────────────────┐
   │  TIER 2:          │──▶ Full CI pipeline against assembled PR
   │  INTEGRATION GATE │    (targeted fix loop on failure, max 2 attempts)
   └──────────────────┘
        │
        ▼
   Engineer Review (approve / intervene / re-run stage)
```

---

## 3. Cross-Language Generation

### 3.1 The Problem

Seven repos, three languages (TypeScript, Python, Go), four databases, two frontend frameworks. Building seven separate generation systems is unscalable. The orchestration layer — DAG execution, verification gates, plan generation — must be language-agnostic. Only the generation prompts and verification commands need to change per language.

### 3.2 Language Adapter Abstraction

A **Language Adapter** sits between the pattern template (language-agnostic) and the generation prompt (language-specific). Each adapter maps abstract stage operations to language-specific prompt fragments and verification toolchains.

```yaml
# Pattern template (language-agnostic)
stage: api
type: rest_endpoints
produces: [endpoint_contracts, route_paths, request_response_types]

# Language adapter — one per language
adapter: typescript
  framework_map:
    rest_endpoints: "Express route handlers using the existing controller pattern"
    db_migration: "Prisma migration SQL"
    test_suite: "Jest with supertest for API, React Testing Library for components"
  verification_toolchain:
    typecheck: "npx tsc --noEmit"
    lint: "npx eslint --config .eslintrc.js"
    test: "npx jest --passWithNoTests"
    format_check: "npx prettier --check"
  import_style: "ES modules (import/export)"
  type_system: "TypeScript strict mode"

adapter: python
  framework_map:
    rest_endpoints: "FastAPI router with Pydantic models"
    db_migration: "Alembic migration script"
    test_suite: "pytest with httpx for API, pytest-asyncio for async"
  verification_toolchain:
    typecheck: "mypy --strict"
    lint: "ruff check"
    test: "pytest -x"
    format_check: "ruff format --check"
  import_style: "absolute imports from package root"
  type_system: "Python type hints (PEP 484)"

adapter: go
  framework_map:
    rest_endpoints: "net/http handlers with chi router"
    db_migration: "goose SQL migration"
    test_suite: "go test with testify assertions"
  verification_toolchain:
    typecheck: "go vet ./..."
    lint: "golangci-lint run"
    test: "go test ./..."
    format_check: "gofmt -l"
  import_style: "go module imports"
  type_system: "Go static typing (no generics pre-1.18 patterns)"
```

### 3.3 How the Adapter Feeds into Prompting

When the generation engine prepares to execute a stage, it combines three layers:

1. **Pattern template** — defines the stage's purpose, inputs, outputs, and dependencies (language-agnostic).
2. **Language adapter** — maps the stage type to framework-specific instructions and provides the verification command set.
3. **Codebase intelligence** — provides actual code examples from the target repo, which implicitly carry the language, framework version, and team conventions.

The prompt construction order is: system prompt → language adapter context → codebase brief (retrieved examples) → upstream stage outputs → generation instruction. The adapter ensures the generation instruction says "create a FastAPI router with Pydantic models" instead of "create REST endpoints" when targeting a Python product.

### 3.4 Verification Gate Adaptation

Each language adapter declares its verification toolchain. The verification gate runner reads the adapter config for the target product and executes the corresponding commands. The gate logic itself is language-agnostic — it runs whatever commands the adapter specifies and interprets exit codes and error output. Adding a new language means writing a new adapter config, not modifying the gate runner.

### 3.5 v1 Implication

v1 ships with TypeScript and Python adapters (covering Payments Dashboard and Claims Portal). The Go adapter ships in v2 when the first Go product is onboarded. The adapter abstraction means adding Go doesn't require changes to the plan generator, execution engine, or verification gate runner.

---

## 4. Codebase Intelligence

This is the component that neither BMAD nor Ralph provides, and it's the one that determines whether generated code looks like a team member wrote it or like a generic AI sample.

### 4.1 What Gets Indexed

For each product repo, the codebase intelligence layer extracts and indexes five categories:

**Structural conventions.** Directory layout, file naming patterns, module organization. Where do migrations live? How are API routes organized? What's the component directory structure? This is extracted by static analysis of the file tree and confirmed by analyzing the most recent 20 merged PRs.

**Code patterns.** For each stage type (migration, API endpoint, React component, test, config), the indexer extracts 3–5 canonical examples from the existing codebase. These are selected by recency and review quality (PRs that were approved without revision are preferred). Patterns are stored as annotated code snippets with metadata: what product, what entity, what pattern elements are present (permissions, audit logging, integrations).

**Architectural decisions.** How does this product handle authentication? What's the permission model? How are feature flags managed? What ORM or query builder is used? How are environment variables structured? These are extracted from a combination of config files, middleware chains, and a one-time onboarding questionnaire completed by the product's tech lead.

**Dependency graph.** What internal packages and external libraries are used? What versions? What import patterns? This prevents the factory from introducing dependencies that conflict with the existing stack or using deprecated internal APIs.

**Style and formatting.** Linter config, Prettier/formatter settings, TypeScript strictness level, test framework and assertion style. These are extracted directly from config files and applied as hard constraints during generation.

### 4.2 How Context Is Retrieved

At generation time, each stage retrieves context specific to what it's generating:

| Stage | Retrieved Context |
|---|---|
| Migration | Last 3 migrations in this repo, DB engine config, ORM conventions, naming patterns for tables/columns |
| API Endpoints | Existing route structure, controller/handler patterns, middleware chain, error handling conventions, auth/permission wiring |
| Frontend Components | Existing component library usage, form patterns, table/list patterns, state management approach, styling conventions |
| Tests | Test framework config, assertion patterns, mock/fixture conventions, test file naming and location |
| Config | Feature flag system, permission registration, environment variable patterns, deploy config |

Context is assembled into a structured prompt section called the **Codebase Brief** — a concise, machine-readable summary that sits between the system prompt and the generation instruction. It includes annotated code examples, not just descriptions. The Retrieval Service returns examples ranked by relevance and token-counted, and the Context Budget Allocator (see §5) fills the prompt within the model's token budget.

### 4.3 Cold Start: Onboarding a New Product

When the factory encounters a repo it has never indexed:

**Step 1 — Automated scan (minutes).** The indexer runs static analysis: file tree, config files, package manifests, linter settings, framework detection. This produces a structural profile immediately. The language adapter is selected automatically based on detected language.

**Step 2 — Pattern extraction (hours).** The indexer analyzes the most recent 20 merged PRs to extract canonical code patterns for each stage type. It looks for CRUD-shaped features (entity + endpoints + UI) and extracts the migration, API, frontend, and test patterns used.

**Step 3 — Tech lead questionnaire (30 minutes of human time).** A structured form asks the product's tech lead to confirm or correct the automated findings and fill in architectural decisions that can't be reliably inferred: permission model, feature flag strategy, sensitive data handling, compliance requirements.

**Step 4 — Validation generation.** The factory generates a small, throwaway CRUD feature using the extracted conventions and presents it to the tech lead for review. Corrections from this review are fed back into the codebase profile.

Total cold-start time: under 4 hours, of which 30 minutes is human effort.

### 4.4 Staying Current

The codebase intelligence layer re-indexes incrementally:

**Post-merge hook.** Every merged PR triggers a lightweight re-index of the files it touched. If the PR introduced a new pattern (e.g., a new way of handling permissions), the indexer flags it for review.

**Weekly full re-index.** A scheduled job re-runs the full extraction pipeline to catch drift. Differences from the previous index are surfaced in a summary report.

**Convention override.** Tech leads can manually update the codebase profile at any time — for example, when the team decides to adopt a new testing approach going forward. Manual overrides take precedence over extracted patterns.

---

## 5. Prompt Architecture & Context Management

### 5.1 Prompt Structure

Every generation prompt follows a five-layer structure. This is the template that every stage executor follows; the content of each layer varies by stage type, language, and product.

```
┌─────────────────────────────────────────────────┐
│ Layer 1: System Prompt (fixed per BCHAD version) │
│                                                  │
│ "You are a code generator in the BCHAD software  │
│ factory. You generate production-quality code     │
│ that conforms exactly to the conventions of the   │
│ target codebase. You never invent patterns —      │
│ you follow the examples provided. Output only     │
│ the requested files, no explanations."            │
│                                                  │
│ + Output format instructions (file markers,       │
│   structured JSON for metadata)                   │
│ + Hard constraints (no hardcoded secrets,          │
│   no new dependencies without declaration)        │
└─────────────────────────────────────────────────┘
                      │
┌─────────────────────────────────────────────────┐
│ Layer 2: Language Adapter Context                │
│                                                  │
│ Framework-specific rules, import style, type      │
│ system notes, common pitfalls for this language.  │
│ (From the language adapter — see §3.)             │
└─────────────────────────────────────────────────┘
                      │
┌─────────────────────────────────────────────────┐
│ Layer 3: Codebase Brief                          │
│                                                  │
│ Retrieved examples (annotated):                   │
│   "Here is how this product writes [stage type].  │
│    Follow this pattern exactly."                  │
│                                                  │
│ Architectural notes:                              │
│   "This product uses [auth model]. Permission     │
│    checks are applied via [middleware pattern]."   │
│                                                  │
│ (From the Retrieval Service, within token budget.)│
└─────────────────────────────────────────────────┘
                      │
┌─────────────────────────────────────────────────┐
│ Layer 4: Upstream Context                        │
│                                                  │
│ Outputs from completed stages:                    │
│   "The migration created table payment_methods    │
│    with columns: [schema]. The API contract is:   │
│    [endpoint definitions]. Use these exactly."     │
│                                                  │
│ (Injected by the DAG executor.)                   │
└─────────────────────────────────────────────────┘
                      │
┌─────────────────────────────────────────────────┐
│ Layer 5: Generation Instruction                  │
│                                                  │
│ The specific task:                                │
│   "Generate CRUD API endpoints for the            │
│    PaymentMethod entity. Include: [list of        │
│    operations]. Apply permission gate:             │
│    payment_methods:manage. Add audit logging       │
│    to create, update, delete operations."          │
│                                                  │
│ (Parameterized from the BCHADPlan stage config.)  │
└─────────────────────────────────────────────────┘
```

On retry, two additional sections are injected between Layer 4 and Layer 5:

**Layer 4.5a — Error Context:** The exact error output from the verification gate, including file paths, line numbers, and error messages.

**Layer 4.5b — Corrective Example (if available):** An additional retrieved example from the codebase showing how the specific failure pattern is handled correctly. This is fetched by the error classifier (see §7.3) when the failure category is Context or Logic.

### 5.2 Context Budget Allocator

The generation engine needs to fit into each prompt: a system prompt, the language adapter context, the codebase brief, upstream stage outputs, and the generation instruction. For complex stages like `frontend`, where retrieved context includes list patterns, form patterns, shared components, the API contract, and type definitions, this can exceed model limits if unmanaged.

The **Context Budget Allocator** partitions the available context window across prompt sections, using a priority system for retrieval results that adapts to model capabilities.

For a model with a 200K token context window, the budget is allocated as follows:

| Prompt Section | Token Budget | Priority | Notes |
|---|---|---|---|
| System prompt | 2,000 | Fixed | BCHAD instructions, output format, constraints |
| Language adapter context | 1,000 | Fixed | Framework-specific generation rules |
| Upstream stage outputs | 5,000–15,000 | High | Schema definitions, API contracts — these are the actual inputs |
| Codebase brief: primary examples | 10,000–30,000 | High | The 2–3 most relevant canonical examples, annotated |
| Codebase brief: secondary examples | 5,000–15,000 | Medium | Additional context (utility patterns, shared components) |
| Codebase brief: architectural notes | 2,000 | Medium | Auth model, permission system, env var patterns |
| Generation instruction | 3,000 | Fixed | The specific task for this stage |
| Output buffer | 20,000–40,000 | Reserved | Space for the model to generate |
| **Total ceiling** | **~100,000** | | Stays well within 200K limit for safety margin |

The Retrieval Service returns examples ranked and token-counted. The allocator fills sections in priority order:

1. Fixed sections are always included (system prompt, adapter, generation instruction, output buffer).
2. Upstream stage outputs are included in full (small and critical — without the API contract, the frontend stage cannot function).
3. Primary codebase examples are included up to their budget.
4. Secondary examples fill remaining space.
5. Architectural notes are included if space remains.

If the total exceeds the budget, the allocator applies these strategies in order: truncate secondary examples by removing method bodies and keeping signatures and structural patterns; summarize architectural notes into bullet points; as a last resort, drop the least-relevant primary example.

### 5.3 Model Selection per Stage

Not all stages require the same model. Simpler, more constrained stages benefit from faster, cheaper models; complex stages with more creative latitude benefit from more capable ones.

| Stage Type | Recommended Model | Rationale |
|---|---|---|
| migrate | Haiku 3.5 | Highly constrained output (SQL/ORM schema matching field definitions). Low ambiguity. |
| config | Haiku 3.5 | Minimal generation — registering flags and permissions in existing config files. |
| api | Sonnet 4 | Moderate complexity — must compose auth, validation, audit, and Vault integration correctly. |
| frontend | Sonnet 4 | Highest complexity — UI components, state management, multiple files, style matching. |
| tests | Sonnet 4 | Must understand the API contract and component behavior to write meaningful assertions. |
| NL spec translation | Sonnet 4 | Needs strong reasoning to extract structured data from ambiguous natural language. |

Model selection is configurable per product and per stage. If a team finds that Haiku produces insufficient quality for their migration patterns (e.g., complex DynamoDB schemas), they can upgrade that stage to Sonnet without changing anything else. The allocator adjusts the token budget based on the selected model's effective context ceiling.

### 5.4 Prompt Versioning

Prompts are stored in the Pattern Library Git repo alongside the DAG templates. Each prompt template has a version number, and the generation engine logs which prompt version produced each stage's output. This enables: A/B testing of prompt variants (run the same feature with two prompt versions and compare CI pass rates), rollback if a prompt change degrades quality, and audit trail for compliance (which prompt generated which code).

---

## 6. Orchestration & Decomposition

### 6.1 Natural Language Spec Input

BCHAD accepts both structured JSON and natural language product briefs. When the input is natural language, the Spec Parser's NL Translator extracts a structured BCHADSpec as a pre-processing step.

```
Natural Language Brief
        │
        ▼
┌─────────────────────────────────────────────────┐
│ NL Spec Translator (single LLM call)             │
│                                                  │
│ System prompt:                                   │
│   "You are a spec parser for BCHAD. Given a      │
│    product brief, extract a BCHADSpec JSON.       │
│    Use only the fields defined in the schema.     │
│    Mark anything ambiguous as                     │
│    'needs_clarification = true' with a reason."   │
│                                                  │
│ Context:                                         │
│   - BCHADSpec JSON schema (with field docs)       │
│   - Target product's entity list (from codebase   │
│     intelligence) so it can resolve references    │
│   - Available integrations for this product       │
│                                                  │
│ Output: Draft BCHADSpec JSON                      │
└─────────────────────────────────────────────────┘
        │
        ▼
┌─────────────────────────────────────────────────┐
│ Spec Confirmation UI                             │
│                                                  │
│ Shows the engineer:                              │
│   - The extracted spec in a readable format       │
│   - Any fields marked needs_clarification         │
│   - Side-by-side: original brief ↔ extracted spec │
│                                                  │
│ Engineer can: confirm, edit fields, or reject     │
└─────────────────────────────────────────────────┘
        │
        ▼
  Confirmed BCHADSpec → standard pipeline continues
```

The NL translator doesn't need to be perfect — it needs to produce a draft that the engineer confirms. Ambiguities are flagged, not silently resolved. The engineer always approves the structured spec before any code generates, so NL parsing errors are caught at the cheapest possible point. Cost is negligible: one additional LLM call of approximately 5,000 tokens. Engineers can still provide JSON directly if they prefer; the NL path is additive.

### 6.2 Pattern Templates

Each pattern type defines a DAG template. Here is the CRUD+UI template:

```yaml
pattern: crud_ui
description: "New entity with API endpoints, database table, and management interface"
stages:
  - id: migrate
    type: db_migration
    depends_on: []
    human_approval: true   # migrations always require approval
    produces: [schema_definition, table_name, column_types]
    
  - id: api
    type: rest_endpoints
    depends_on: [migrate]
    consumes: [schema_definition]
    produces: [endpoint_contracts, route_paths, request_response_types]
    
  - id: frontend
    type: ui_components
    depends_on: [api]
    consumes: [endpoint_contracts, request_response_types]
    produces: [component_paths, page_routes]
    
  - id: config
    type: feature_flags_and_permissions
    depends_on: []          # independent — runs in parallel with migrate
    consumes: []
    produces: [flag_names, permission_keys]
    
  - id: tests
    type: test_suite
    depends_on: [api, frontend, config]
    consumes: [endpoint_contracts, component_paths, flag_names]
    produces: [test_file_paths]

verification:
  migrate: [sql_syntax_valid, no_data_loss_risk, rollback_exists]
  api: [type_check_pass, lint_pass, route_conflicts_none]
  frontend: [type_check_pass, lint_pass, component_renders]
  config: [flag_registered, permission_registered]
  tests: [all_tests_pass, coverage_threshold_met]
```

### 6.3 Plan Generation

When a feature spec arrives, the Plan Generator:

1. **Classifies the pattern.** Matches the spec to one of the four pattern templates. If the spec spans multiple patterns (e.g., a CRUD feature with an external integration), it composes templates.

2. **Parameterizes the template.** Fills in entity names, field definitions, permissions, integrations, and UI requirements from the spec. Each stage gets its specific generation parameters.

3. **Queries codebase intelligence.** Retrieves the target product's conventions for each stage type. Confirms that the planned stages align with how this product actually structures features (e.g., if this product uses GraphQL instead of REST, the `api` stage adapts). Selects the appropriate language adapter.

4. **Sets human approval gates.** Migrations always require approval. Stages touching sensitive data (fields marked `sensitive: true`) require approval. Stages involving compliance-regulated products (fintech, healthtech) get additional security review flags.

5. **Estimates cost.** Each stage's projected cost is calculated from the selected model, estimated input/output tokens, and expected retry rate (see §9). The total projected cost is included in the plan.

6. **Outputs the generation plan.** A concrete DAG with estimated file counts, projected cost, stage-level descriptions of what will be generated, and a dependency-ordered execution schedule.

The engineer reviews the plan — including projected cost — before generation begins. This is the first trust checkpoint.

### 6.4 DAG Execution

The execution engine processes the DAG with these rules:

**Parallel execution of independent stages.** Stages with no unresolved dependencies run concurrently. In the CRUD+UI template, `migrate` and `config` start simultaneously.

**Upstream output feeds downstream prompts.** When a stage completes, its output artifacts (schema definitions, endpoint contracts, component paths) are injected into the prompts of dependent stages. The frontend generator doesn't guess the API contract — it receives the actual contract from the API stage.

**Classified retry on failure.** If a stage's verification gate fails, the error classifier categorizes the failure (see §7.3) and routes it to the appropriate recovery strategy. This replaces the original undifferentiated retry loop. Maximum retry attempts depend on error category (1 for security, 2 for logic and conflicts, 3 for syntax and style).

**Stage-level failure isolation.** If a stage exhausts its retries and still fails, the pipeline pauses at that stage. Successful upstream stages are preserved. The engineer is notified with the failure context and can: (a) manually fix and resume, (b) adjust the spec and re-run the failed stage, or (c) abort the pipeline. Downstream stages that depend on the failed stage wait; independent stages continue.

**No cascading restarts.** Unlike BMAD (where you restart the whole sequence) or Ralph (where the next loop iteration may redo work), BCHAD never re-runs a stage that already passed. If the `api` stage fails, the `migrate` stage's output is preserved and doesn't re-execute.

### 6.5 Why This Is Better Than BMAD or Ralph

**vs. BMAD:** BMAD's sequential handoff means the Architect can't start until the Analyst finishes, the Developer can't start until the Architect finishes, and so on. BCHAD's DAG allows `config` to generate while `migrate` generates. More importantly, BMAD's phases are generic (any Analyst → any Architect), while BCHAD's stages are pattern-specific and codebase-aware. The migration stage for a Postgres product behaves differently than for a DynamoDB product — automatically, via language adapters, without changing the pipeline.

**vs. Ralph:** Ralph's agent picks tasks nondeterministically from a flat PRD. It might try to build the frontend before the API exists, then spend iterations self-correcting. BCHAD's DAG guarantees the frontend stage has the API contract before it starts. Ralph's retry loop operates on the entire feature with undifferentiated error handling; BCHAD's retry loop operates on individual stages with classified errors routed to appropriate recovery strategies, which is cheaper (fewer tokens), more debuggable (the error is scoped to one generation step), and more effective (context errors trigger re-retrieval, not blind re-generation).

---

## 7. Quality & Verification

### 7.1 Two-Tier Verification Model

BCHAD uses a two-tier verification model that catches errors at the earliest and cheapest point while ensuring the assembled PR works as a whole.

**Tier 1 — Stage gates (fast, scoped).** Every stage has a verification gate that output must pass before advancing. Gates run inside a lightweight disposable Docker container cloned from the target product's CI environment. They catch the majority of issues and enable the classified retry loop. Stage gates run in seconds to low minutes.

**Tier 2 — Integration gate (slow, comprehensive).** After all stages pass and the PR is assembled, the integration gate spins up the target product's full CI environment — database, test fixtures, dependent services — and runs the complete CI pipeline against the generated branch. This catches issues that only surface when all components interact: foreign key violations in migrations that reference existing tables, API endpoints that conflict with existing routes in ways the static check missed, frontend components that fail to render when integrated with the real API, and environment configuration that works in isolation but breaks in the full stack.

### 7.2 Stage Gate Checks

**Universal checks (all stages):**
- Syntax validity (parseable, no syntax errors)
- Lint pass (using the target repo's linter config, via language adapter toolchain)
- No new dependencies introduced without explicit declaration in the spec
- No hardcoded credentials, API keys, or secrets
- Generated files are in the correct directories per the repo's conventions

**Stage-specific checks:**

| Stage | Additional Checks |
|---|---|
| Migration | SQL/ORM syntax valid; rollback migration exists; no destructive operations on existing tables without explicit approval; column types match field definitions |
| API | Type-check pass; route paths don't conflict with existing routes; auth middleware applied; permission checks present for gated endpoints; request/response types consistent with schema |
| Frontend | Type-check pass; components render without errors (via lightweight headless check); existing component library used (no re-implementations of existing UI primitives); accessibility basics (labels, ARIA attributes) |
| Config | Feature flag registered in the correct system; permission keys follow naming conventions; environment variables use the correct prefix |
| Tests | All generated tests pass; coverage meets minimum threshold; test assertions are meaningful (not just "expect(true).toBe(true)"); mocks use existing fixtures where available |

### 7.3 Error Taxonomy and Differentiated Recovery

When a verification gate fails, the error classifier categorizes the failure and routes it to the appropriate recovery strategy. This replaces the original undifferentiated "inject error and retry" loop.

| Category | Description | Example | Recovery Strategy | Max Retries |
|---|---|---|---|---|
| **Syntax** | Code doesn't parse. | `Unexpected token at line 42` | Direct retry — include the exact error. Alternatively, auto-fix with the formatter. | 3 |
| **Style** | Violates linter or formatter rules. | `Prefer const over let (no-let)` | Auto-fix with the linter if possible (many lint errors are auto-fixable), else direct retry. | 3 |
| **Type** | Type errors from the language's type checker. | `Property 'vaultRef' does not exist on type 'PaymentMethod'` | Retry with the type error and the correct type definition from upstream output. If the type error originates in upstream output, escalate to that stage. | 3 |
| **Logic** | Tests fail due to incorrect behavior. | `Expected 403 for unauthorized request, got 200` | Retry with the failing test output and a corrective example from the codebase showing the correct pattern. | 2 |
| **Context** | Code uses patterns, imports, or APIs that don't exist in the target codebase. | `Module 'src/utils/oldHelper' not found` | Do **not** retry with the same context. Re-query the Retrieval Service with more specific parameters, replace the incorrect example, then retry. | 2 |
| **Specification** | Generated code doesn't match the spec. | `Spec requires 'audit: true' but no audit logging calls found` | Surface to engineer immediately. No retry — this indicates a spec ambiguity or planning error. | 0 |
| **Conflict** | Conflicts with existing code in the repo. | `Route /api/v1/payments/methods conflicts with existing wildcard` | Retry with the conflict information and the existing code. | 2 |
| **Security** | Credential leak, missing auth, sensitive data exposure. | `Field vault_ref returned in API response without masking` | Retry once with the security violation highlighted and the product's security pattern. Escalate immediately if retry fails. | 1 |

**Recovery routing flow:**

```
Verification gate failure
        │
        ▼
  Classify error (match error output against category patterns)
        │
        ├─── Syntax/Style → Auto-fix if possible, else direct retry
        │
        ├─── Type → Retry with correct type context from upstream
        │
        ├─── Logic → Retry with failing test + corrective example
        │
        ├─── Context → Re-retrieve codebase examples, then retry
        │                  (retrieval failure, not generation failure)
        │
        ├─── Specification → Surface to engineer immediately (no retry)
        │
        ├─── Conflict → Retry with conflict context
        │
        └─── Security → Retry once with security pattern, then escalate
```

### 7.4 Integration Gate (Tier 2)

After PR assembly, the integration gate runs the target product's complete CI pipeline:

```yaml
integration_gate:
  environment:
    source: "target product's docker-compose.test.yml"
    databases:
      - spin up fresh Postgres/Mongo/Dynamo from product's test fixtures
      - apply all existing migrations + the generated migration
    services:
      - start the API server with the generated code
      - start dependent services from the product's test compose file

  execution:
    - run: "product's full CI script"
      timeout: 15 minutes
      on_pass: "mark PR as factory-verified"
      on_fail: "classify failure, attempt targeted fix"
  
  targeted_fix_strategy:
    - identify which files the failing test touches
    - re-generate only those files with the integration error as context
    - re-run the full CI pipeline
    - max 2 targeted fix attempts before surfacing to engineer
```

**Impact on the 80% target.** Tier 1 stage gates alone are estimated to achieve a 65–70% first-run CI pass rate. The Tier 2 integration gate's targeted fix loop adds 10–15%, bringing the expected first-run pass rate to 75–85%. The "first run" in the 80% target is measured after the full pipeline (including the integration gate's fix attempts) but before any human intervention.

### 7.5 Pre-Generation Validation

Some errors are cheaper to prevent than to detect. Before each stage generates code, a pre-generation check verifies:

- **Schema consistency:** If the frontend stage is about to generate, confirm the API contract it's consuming actually exists and is valid.
- **Naming conflicts:** Confirm the planned file paths don't collide with existing files in the repo (unless the spec explicitly declares an override).
- **Import availability:** Confirm that internal packages and external libraries referenced in the codebase patterns are actually available in the target repo's dependency graph.

### 7.6 Security-Specific Verification

For products under SOC 2 or HIPAA, additional verification applies:

- **Sensitive field handling:** Fields marked `sensitive: true` in the spec must use the product's designated encryption/vault integration. The verification gate checks that raw sensitive values are never stored, logged, or returned in API responses.
- **Auth enforcement:** Every generated endpoint must include the product's authentication middleware. The gate checks the middleware chain, not just the presence of an auth import.
- **Audit logging:** If the spec includes `"audit": true`, the gate verifies that every state-changing operation (create, update, delete) includes an audit log call matching the product's existing audit pattern.
- **Migration safety:** Destructive operations (DROP, ALTER with data loss risk) require explicit human approval and are flagged in the plan, not silently generated.

---

## 8. Human Interface & Trust

### 8.1 Trust Checkpoints

BCHAD inserts explicit trust checkpoints at key moments in the pipeline:

**Checkpoint 1: Plan Review.** After the Plan Generator produces the DAG, the engineer reviews: what stages will run, what each stage will generate, what dependencies exist, what human approvals are required, and what the projected cost is. The engineer can modify the plan (add a stage, change dependencies, remove a component) before execution begins.

**Checkpoint 2: Migration Approval.** Database migrations always pause for human approval. The engineer sees the exact SQL/ORM migration, the rollback migration, and a diff showing the schema change. This is non-negotiable — the factory never auto-applies a schema change.

**Checkpoint 3: Sensitive Stage Approval.** Stages touching sensitive data, compliance-regulated products, or external integrations pause for approval. The engineer sees the generated code with sensitive-field handling highlighted.

**Checkpoint 4: PR Review.** The final PR includes clean diffs, commit messages (one per stage), and a generation report explaining what was produced and why, including cost summary and error recovery actions taken. The engineer reviews as they would any PR — but with the advantage of knowing exactly which parts were generated and the reasoning behind each decision.

### 8.2 What the Engineer Sees

At every point in the pipeline, the engineer can view:

**The generation plan** — the full DAG with stage statuses (pending, running, passed, failed, awaiting approval) and projected vs. actual cost.

**Stage-level artifacts** — for each completed stage, the generated code, the codebase context that was retrieved, the prompt that was constructed, and the verification gate results. Full transparency into what the factory "thought" when generating.

**Diff previews** — each stage's output as a diff against the current repo state. The engineer sees exactly what files are being added or modified, and can read them in context.

**Error trails** — if a stage required retries, the engineer can see each attempt, what went wrong, what error category was assigned, how the recovery strategy addressed it, and the final result. If a stage failed after all retries, the engineer sees all attempts and the error output.

### 8.3 Intervention Points

The engineer can intervene at any point:

**Edit and resume.** If a stage's output is 90% correct but has a small issue, the engineer can manually edit the generated files and mark the stage as passed. Downstream stages will use the edited output.

**Re-run with guidance.** If a stage produced the wrong approach (e.g., used the wrong component pattern), the engineer can add a guidance note (plain text instruction) and re-run that stage. The guidance note is prepended to the generation prompt.

**Override codebase context.** If the retrieved codebase examples are outdated or wrong, the engineer can point the stage at specific files to use as reference instead.

**Skip a stage.** If the engineer wants to write a particular stage manually (e.g., they know the migration is tricky and want to hand-write it), they can skip the stage and provide their own files. Downstream stages will consume the manually-provided output.

**Abort and keep partial output.** If the pipeline hits a fundamental issue, the engineer can abort. All successfully generated stages are preserved as a partial branch. The engineer can use the partial output as a starting point for manual completion.

### 8.4 Data-Driven Trust Escalation

Trust is earned through demonstrated reliability, not demanded on a calendar. BCHAD computes a **trust score** per engineer per product, based on observed factory performance over the last 10 runs.

**Trust score signals:**

| Signal | Weight | Measurement |
|---|---|---|
| CI pass on first run | 0.30 | Binary — did the PR pass CI without human edits? |
| Human edit volume | 0.25 | Lines changed by engineer after generation / total lines generated |
| Stage retry rate | 0.15 | Fraction of stages that required retries |
| Engineer override count | 0.15 | How many times did the engineer re-run a stage with guidance? |
| Time to merge | 0.15 | Time from PR creation to merge (shorter = less rework) |

**Phase transitions:**

**Phase 1 — Supervised:** Trust score below 60 OR fewer than 5 completed runs. Every stage pauses for engineer approval before advancing.

**Phase 2 — Gated:** Trust score 60–85 AND at least 5 completed runs. Only checkpoint stages pause (plan, migration, PR review). Non-checkpoint stages auto-advance on gate pass.

**Phase 3 — Monitored:** Trust score above 85 AND at least 15 completed runs. Pipeline runs end-to-end. Engineer reviews only the final PR. Stage details available on demand.

**Automatic regression.** If the trust score drops below a phase's threshold for 3 consecutive runs, the system automatically downgrades to the lower phase and notifies the engineer: "Factory reliability for [product] has dropped — reverting to gated mode. Recent issues: [summary]." This prevents a single bad run from triggering a downgrade while catching genuine reliability regressions.

**Per-product independence.** A senior engineer might be in Phase 3 for Payments Dashboard (familiar product, well-indexed) and Phase 1 for a newly onboarded product. Trust is earned per-product because reliability depends on the quality of the codebase intelligence profile.

---

## 9. Cost Model

### 9.1 Per-Stage Cost Estimation

Based on the context budget allocator and model profiles, each stage has a predictable cost envelope:

| Stage | Model | Avg Input Tokens | Avg Output Tokens | Cost/Attempt | Avg Attempts | Est. Total |
|---|---|---|---|---|---|---|
| migrate | Haiku 3.5 | 25,000 | 5,000 | $0.04 | 1.1 | $0.04 |
| config | Haiku 3.5 | 15,000 | 3,000 | $0.02 | 1.0 | $0.02 |
| api | Sonnet 4 | 60,000 | 15,000 | $0.41 | 1.3 | $0.53 |
| frontend | Sonnet 4 | 70,000 | 20,000 | $0.51 | 1.2 | $0.61 |
| tests | Sonnet 4 | 50,000 | 15,000 | $0.38 | 1.2 | $0.46 |
| **CRUD+UI total** | | | | | | **~$1.66** |

### 9.2 Cost Comparison

An engineer spending 3.5 days on the same feature costs approximately $2,800–$4,200 in salary (at $160K–$240K/yr fully loaded). The factory's marginal cost of approximately $1.66 in API spend plus roughly 1 hour of engineer time for spec + review (~$100–$150) yields approximately $100–$150 per feature versus $2,800–$4,200. Even with generous overhead assumptions (infrastructure, indexing, maintenance), the unit economics are compelling.

### 9.3 Cost Guardrails

The plan generator includes a `projected_cost` field in the BCHADPlan. If the projected cost exceeds a configurable threshold (e.g., $10 for a single feature), the plan pauses for human review before execution. The classified retry limits per error category (see §7.3) act as natural cost caps — security errors get at most 1 retry, logic errors at most 2, syntax errors at most 3.

### 9.4 Monthly Cost Tracking

The metrics dashboard tracks cost per feature, cost per pattern type, cost per product, and cost per retry. This lets the team identify which stages or products are consuming disproportionate resources and optimize accordingly (e.g., upgrading codebase examples for a product whose `api` stage retries frequently).

---

## 10. Worked Example: Payment Methods Feature

### 10.1 Input: Natural Language Brief

> *"Add a 'Payment Methods' management page to the merchant dashboard. Merchants should be able to add, edit, delete, and set a default payment method. Each payment method has a type (credit card, ACH, wire), a label, and credentials stored via our Vault integration. List view with sorting and filtering, detail/edit form. Audit log every change. Gate behind `payment_methods:manage`."*

### 10.2 Step 1 — NL Spec Translation

The NL Translator extracts a BCHADSpec and presents it for confirmation:

```json
{
  "product": "payments-dashboard",
  "pattern": "crud_ui",
  "entity": "PaymentMethod",
  "fields": [
    {"name": "type", "kind": "enum", "values": ["credit_card", "ach", "wire"]},
    {"name": "label", "kind": "string"},
    {"name": "is_default", "kind": "boolean"},
    {"name": "vault_ref", "kind": "string", "sensitive": true,
     "needs_clarification": true,
     "reason": "Brief says 'credentials stored via Vault' — inferred vault_ref as the field name. Please confirm or rename."}
  ],
  "permissions": "payment_methods:manage",
  "audit": true,
  "integrations": ["vault"],
  "ui": {"list": true, "detail": true, "form": true}
}
```

The engineer confirms the vault_ref field name. Total time: under 2 minutes.

### 10.3 Step 2 — Spec Parsing

The Spec Parser normalizes the confirmed input:

```yaml
parsed_spec:
  product: payments-dashboard
  language: TypeScript
  language_adapter: typescript
  database: Postgres
  orm: Prisma
  frontend_framework: React
  pattern: crud_ui
  entity:
    name: PaymentMethod
    table_name: payment_methods     # inferred from convention
    fields:
      - name: type
        db_type: VARCHAR(20)
        enum_values: [credit_card, ach, wire]
        required: true
      - name: label
        db_type: VARCHAR(255)
        required: true
      - name: is_default
        db_type: BOOLEAN
        default: false
      - name: vault_ref
        db_type: VARCHAR(255)
        sensitive: true              # triggers Vault integration
        required: true
  permissions:
    key: payment_methods:manage
    applies_to: [create, read, update, delete]
  audit: true
  integrations: [vault]
  ui:
    pages: [list, detail, form]
  compliance_flags:
    soc2: true                       # auto-detected from product profile
    hipaa: false
```

### 10.4 Step 3 — Plan Generation

The Plan Generator consults the CRUD+UI template, the payments-dashboard codebase profile, and the TypeScript language adapter:

```yaml
plan:
  id: pf-20260315-001
  product: payments-dashboard
  pattern: crud_ui
  entity: PaymentMethod
  projected_cost: $1.72
  
  stages:
    - id: migrate
      type: db_migration
      depends_on: []
      human_approval: true
      model: claude-haiku-3.5
      description: >
        Create payment_methods table with type (enum), label, is_default,
        vault_ref columns. Add indexes on merchant_id and type.
        Generate rollback migration.
      estimated_files: 2
      estimated_cost: $0.04
      codebase_refs:
        - src/db/migrations/20260301_create_invoices.ts    # recent example
        - src/db/migrations/20260212_create_webhooks.ts    # enum example
    
    - id: config
      type: feature_flags_and_permissions
      depends_on: []
      human_approval: false
      model: claude-haiku-3.5
      description: >
        Register payment_methods:manage permission in auth config.
        Add payment-methods feature flag to LaunchDarkly config.
      estimated_files: 2
      estimated_cost: $0.02
      codebase_refs:
        - src/config/permissions.ts
        - src/config/feature-flags.ts
    
    - id: api
      type: rest_endpoints
      depends_on: [migrate]
      human_approval: false
      model: claude-sonnet-4
      description: >
        Generate CRUD endpoints for PaymentMethod at /api/v1/payment-methods.
        Include Vault integration for vault_ref field. Apply permission gate.
        Add audit logging to create/update/delete operations.
      estimated_files: 4
      estimated_cost: $0.53
      codebase_refs:
        - src/api/routes/invoices.ts                       # CRUD pattern
        - src/api/middleware/permissions.ts                 # permission check
        - src/services/vault-client.ts                     # Vault integration
        - src/services/audit-logger.ts                     # audit pattern
    
    - id: frontend
      type: react_components
      depends_on: [api]
      human_approval: false
      model: claude-sonnet-4
      description: >
        Generate list view (with sort/filter), detail view, and form
        (create/edit with Vault-masked credential field). Use existing
        DataTable component for list. Add route to merchant dashboard nav.
      estimated_files: 5
      estimated_cost: $0.61
      codebase_refs:
        - src/components/invoices/InvoiceList.tsx           # list pattern
        - src/components/invoices/InvoiceForm.tsx           # form pattern
        - src/components/shared/DataTable.tsx               # table component
        - src/components/shared/ConfirmModal.tsx            # delete confirm
    
    - id: tests
      type: test_suite
      depends_on: [api, frontend, config]
      human_approval: false
      model: claude-sonnet-4
      description: >
        Generate API integration tests (CRUD operations, permission
        enforcement, audit logging verification). Generate component
        tests for list, detail, and form views. Use existing test
        fixtures and factory patterns.
      estimated_files: 3
      estimated_cost: $0.46
      codebase_refs:
        - tests/api/invoices.test.ts                       # API test pattern
        - tests/components/invoices/InvoiceList.test.tsx    # component test
        - tests/factories/index.ts                         # test factories
  
  estimated_total_files: 16
  human_approval_gates: [migrate]
  security_review: true                                    # SOC 2 product
  
  execution_order:
    parallel_group_1: [migrate, config]
    then: [api]                                            # after migrate
    then: [frontend]                                       # after api
    then: [tests]                                          # after api + frontend + config
```

**Trust checkpoint 1:** The engineer reviews this plan, including the $1.72 projected cost and the specific codebase references. They can say "don't use the invoices pattern for the API — use the subscriptions pattern instead, it's newer" and point the `api` stage at different reference files.

### 10.5 Step 4 — Stage Execution

**Stage: migrate (runs in parallel with config)**

The generation engine retrieves the two referenced migration files, extracts their patterns (Prisma migration format, naming convention, index strategy), and generates:

```
Generated files:
  prisma/migrations/20260315120000_create_payment_methods/migration.sql
  prisma/migrations/20260315120000_create_payment_methods/rollback.sql

Verification gate (Tier 1):
  ✓ SQL syntax valid
  ✓ Rollback migration exists
  ✓ No destructive operations on existing tables
  ✓ Column types match field definitions
  ✓ Table name follows convention (snake_case plural)
  ✓ Indexes present on foreign key and enum columns

Status: PASSED — awaiting human approval
Cost: $0.04 (1 attempt)
```

**Trust checkpoint 2:** The engineer reviews the migration SQL, confirms it's correct, and approves. The pipeline advances.

**Stage: config (ran in parallel with migrate)**

```
Generated files:
  src/config/permissions.ts  (modified — added payment_methods:manage)
  src/config/feature-flags.ts  (modified — added payment-methods flag)

Verification gate (Tier 1):
  ✓ Permission key follows naming convention
  ✓ Feature flag follows naming convention
  ✓ No duplicate keys
  ✓ TypeScript compiles

Status: PASSED
Cost: $0.02 (1 attempt)
```

**Stage: api (starts after migrate passes)**

The engine retrieves the invoice CRUD routes, vault client, audit logger, and permission middleware. It generates endpoints that follow the exact same structure — same error handling, same middleware chain, same response envelope.

```
Generated files:
  src/api/routes/payment-methods.ts
  src/api/validators/payment-methods.ts
  src/api/services/payment-method-service.ts
  src/types/payment-method.ts

Verification gate (Tier 1) — attempt 1:
  ✓ TypeScript compiles
  ✓ Lint passes
  ✗ Route path conflicts with existing /api/v1/payments/* wildcard

Error classification: CONFLICT
Recovery: Retry with conflict information and existing route structure.

  "Route /api/v1/payment-methods conflicts with existing wildcard route
   at /api/v1/payments/*. Nest under /api/v1/merchants/:merchantId/payment-methods
   per the existing merchant-scoped resource pattern."

Verification gate (Tier 1) — attempt 2:
  ✓ TypeScript compiles
  ✓ Lint passes
  ✓ No route conflicts
  ✓ Permission middleware applied
  ✓ Vault integration used for vault_ref
  ✓ Audit logging on create/update/delete
  ✓ Auth middleware present

Status: PASSED (after 1 retry — Conflict category)
Cost: $0.79 (2 attempts)
```

**Stage: frontend (starts after api passes)**

```
Generated files:
  src/components/payment-methods/PaymentMethodList.tsx
  src/components/payment-methods/PaymentMethodForm.tsx
  src/components/payment-methods/PaymentMethodDetail.tsx
  src/pages/merchant/payment-methods.tsx
  src/nav/merchant-nav.ts  (modified — added payment methods link)

Verification gate (Tier 1):
  ✓ TypeScript compiles
  ✓ Lint passes
  ✓ Uses existing DataTable component (no re-implementation)
  ✓ Uses existing ConfirmModal for delete
  ✓ Vault ref field masked in display
  ✓ Form validation present

Status: PASSED
Cost: $0.51 (1 attempt)
```

**Stage: tests (starts after api + frontend + config all pass)**

```
Generated files:
  tests/api/payment-methods.test.ts
  tests/components/payment-methods/PaymentMethodList.test.tsx
  tests/components/payment-methods/PaymentMethodForm.test.tsx

Verification gate (Tier 1):
  ✓ All 24 tests pass
  ✓ Coverage: 87% (above 80% threshold)
  ✓ Tests use existing factory patterns
  ✓ Permission enforcement tested (unauthorized returns 403)
  ✓ Audit logging assertions present

Status: PASSED
Cost: $0.38 (1 attempt)
```

### 10.6 Step 5 — PR Assembly & Integration Gate

```
Branch: bchad/pf-20260315-001-payment-methods

Commits (one per stage):
  1. feat(db): create payment_methods table and indexes
  2. feat(config): register payment_methods permission and feature flag
  3. feat(api): add CRUD endpoints for payment methods with Vault integration
  4. feat(ui): add payment methods list, form, and detail views
  5. test: add API and component tests for payment methods

Tier 2 — Integration Gate:
  Environment: payments-dashboard docker-compose.test.yml
  Database: Fresh Postgres with all migrations applied (including generated)
  Full CI pipeline: PASSED (all existing + generated tests pass)

PR Description:
  ## Generated by BCHAD — Plan pf-20260315-001
  
  ### What this PR does
  Adds Payment Methods management to the merchant dashboard. Merchants can
  add, edit, delete, and set a default payment method. Each payment method
  stores credentials via Vault integration. All changes are audit logged.
  Gated behind `payment_methods:manage` permission.
  
  ### Generation report
  - Pattern: CRUD + UI
  - Stages: 5 (migrate, config, api, frontend, tests)
  - Files generated: 16
  - Retries: 1 (api stage — route conflict auto-corrected, category: Conflict)
  - Tier 1 verification: all stages passed
  - Tier 2 integration gate: passed
  - Human approvals: migration reviewed and approved
  - Total cost: $1.74 (projected: $1.72)
  
  ### Codebase references used
  - Migration pattern: 20260301_create_invoices
  - API pattern: src/api/routes/invoices.ts
  - Frontend pattern: src/components/invoices/
  - Test pattern: tests/api/invoices.test.ts
  
  ### Review guidance
  - Migration: approved pre-generation
  - API: note merchant-scoped routing (auto-corrected from flat route)
  - Frontend: uses DataTable and ConfirmModal from shared components
  - Vault: vault_ref stored and retrieved via existing VaultClient service
```

**Trust checkpoint 4:** The engineer reviews the PR as they would any PR. Total engineer time: approximately 20 minutes (NL spec confirmation, plan review, migration approval, PR review) versus the 3.5 days the same feature took when built manually. Total API cost: $1.74.

---

## 11. Scope Table: v1 vs. Later Phases

### v1: 60 Days, Three Engineers

| Component | v1 Scope | Rationale |
|---|---|---|
| **Patterns supported** | CRUD + UI only | 38% of all features; most structurally predictable; proves the architecture |
| **Products supported** | Payments Dashboard, Claims Portal (2 of 7) | One TypeScript/Postgres, one Python/Postgres; tests cross-language support without cross-database complexity |
| **Language adapters** | TypeScript, Python | Covers the two v1 products; Go adapter deferred to v2 |
| **Codebase Intelligence** | Full index for 2 products; automated scan + pattern extraction + tech lead questionnaire | Must be solid for v1 — this is the differentiator |
| **Plan Generator** | CRUD+UI template with parameterization for the 2 target products, including cost estimation | Single template, well-tested |
| **NL Spec Input** | NL-to-BCHADSpec translator with engineer confirmation | Low cost (single LLM call), high usability payoff |
| **Generation Engine** | Sequential stage execution with verification gates, classified error recovery, and model selection per stage | Parallel execution deferred to v2 — sequential is simpler to debug |
| **Prompt Architecture** | Five-layer prompt structure with context budget allocator and prompt versioning | Foundational for generation quality |
| **Verification** | Tier 1 (stage gates) + Tier 2 (integration gate); security-specific checks for Payments product | Two-tier verification included from v1 |
| **Error Recovery** | Classified error taxonomy with differentiated recovery strategies | Improves pass rate and reduces wasted retries |
| **Human Interface** | CLI-based: plan review, migration approval, diff preview, stage re-run | Web UI deferred to v2 |
| **Trust Model** | Data-driven trust score with phase transitions | Score tracking from day 1; phases unlock by demonstrated reliability |
| **Cost Tracking** | Projected cost in plans, actual cost logged per stage | Dashboard deferred to v3 |
| **PR Assembly** | Auto-branch, one commit per stage, generated PR description with cost summary | Integrated into existing GitHub workflow |

### v2: Days 61–120

| Component | v2 Scope | Rationale |
|---|---|---|
| **Patterns** | Add Integration pattern | 24% of features; next most common; requires external service mocking |
| **Products** | Add 2 more products (total 4 of 7) | Includes first Go product and first MongoDB product — tests cross-language and cross-database |
| **Language adapters** | Add Go adapter | Required for first Go product onboarding |
| **Parallel execution** | DAG-based parallel stage execution | Reduces end-to-end generation time; requires resolved concurrency issues |
| **Web UI** | Review interface with visual DAG, inline diffs, one-click approval | Replaces CLI interface for non-terminal workflows |
| **Codebase sync** | Post-merge hooks for incremental re-index | Keeps intelligence layer current without manual intervention |

### v3: Days 121–180

| Component | v3 Scope | Rationale |
|---|---|---|
| **Patterns** | Add Workflow and Analytics patterns | Completes the four-pattern coverage; Workflow is the most complex |
| **Products** | All 7 products onboarded | Full portfolio coverage |
| **Self-improvement** | Feedback loop from engineer corrections back into codebase intelligence | If an engineer consistently corrects a specific generation pattern, the factory learns |
| **Metrics dashboard** | CI pass rate, retry frequency, engineer intervention rate, time-to-PR per pattern, cost per feature, cost per product | Quantifies factory performance and identifies improvement areas |
| **Template composition** | Features that span multiple patterns (e.g., CRUD + Integration) | Handles the 15–20% of features that don't fit a single pattern |

### Deferred Indefinitely

| Item | Rationale |
|---|---|
| Fully autonomous mode (no human review) | Violates the human-in-the-loop constraint; trust must be earned gradually |
| Infrastructure/Terraform generation | Different risk profile (production infrastructure changes); separate system |
| Cross-product features | Features spanning multiple repos require inter-service coordination beyond v1–v3 scope |

---

## 12. Implementation Plan: 60-Day v1

### Weeks 1–2: Codebase Intelligence & Language Adapters

**Engineer 1:** Build the automated repo scanner — file tree analysis, config extraction, framework detection, dependency graph, language adapter selection. Target: given a repo URL, produce a structural profile in under 10 minutes.

**Engineer 2:** Build the pattern extractor — analyze merged PRs to identify CRUD-shaped features and extract canonical examples for each stage type (migration, API, frontend, test). Build the TypeScript and Python language adapter configs. Target: extract 3–5 annotated examples per stage type per product.

**Engineer 3:** Build the tech lead questionnaire and codebase profile format. Run the onboarding process on Payments Dashboard and Claims Portal. Validate extracted patterns with each product's tech lead. Set up the state store (Postgres schema for `bchad_runs`, `bchad_stages`, etc.).

**Milestone:** Two complete codebase profiles (Payments Dashboard, Claims Portal) with validated conventions and language adapters.

### Weeks 3–4: Plan Generator, Spec Parser, and Prompt Architecture

**Engineer 1:** Build the Spec Parser — including the NL-to-BCHADSpec translator (single LLM call with confirmation UI), JSON validation, field normalization, pattern classification, and product-specific convention resolution from the codebase profile.

**Engineer 2:** Build the CRUD+UI DAG template with parameterization. Define stage inputs, outputs, dependencies, and verification criteria. Build the five-layer prompt template structure and the Context Budget Allocator. Define model selection per stage type.

**Engineer 3:** Build the Plan Generator — compose spec + template + codebase profile + language adapter into a concrete generation plan. Include human approval gate logic, cost estimation, and prompt versioning.

**Milestone:** Given the Payment Methods brief (natural language or JSON), produce a correct generation plan for both Payments Dashboard and Claims Portal.

### Weeks 5–7: Generation Engine with Classified Error Recovery

**All three engineers** work on the Generation Engine, one stage type each:

**Engineer 1:** Migration and config stage generators. Include Tier 1 verification gates (SQL syntax, rollback existence, naming conventions, permission/flag registration). Implement error classification for these stage types.

**Engineer 2:** API stage generator. Include Tier 1 verification gates (type-check, lint, route conflicts, auth middleware, audit logging). Implement the Conflict and Security error categories with differentiated recovery.

**Engineer 3:** Frontend and test stage generators. Include Tier 1 verification gates (type-check, lint, component rendering, test pass, coverage threshold). Implement the Logic and Context error categories with re-retrieval recovery.

Each engineer implements the classified retry loop (not undifferentiated retry) for their stage types, including auto-fix for Style errors and immediate escalation for Specification errors.

**Milestone:** Each stage generator can produce correct output for the Payment Methods feature on both target products, passing Tier 1 verification gates. Error classification routes failures correctly.

### Weeks 8–9: Orchestration, Integration Gate, and PR Assembly

**Engineer 1:** Build the DAG execution engine — sequential execution with dependency resolution, upstream output injection into downstream prompts, stage-level failure handling, trust score tracking.

**Engineer 2:** Build the Tier 2 integration gate — spin up product CI environment, run full pipeline against assembled PR, targeted fix loop (max 2 attempts). Build the PR assembly system — branch creation, per-stage commits, PR description generation with generation report and cost summary.

**Engineer 3:** Build the CLI interface — NL spec confirmation, plan review (with cost), migration approval, diff preview, stage re-run, edit-and-resume. Implement trust score display and phase transitions.

**Milestone:** End-to-end pipeline runs: NL brief → spec confirmation → plan → generation → Tier 1 + Tier 2 verification → PR for the Payment Methods feature on both products.

### Weeks 9–10: Validation and Hardening

**Validation protocol:** Run the factory on 30 features per product (60 total), drawn from the last 6 months of shipped CRUD+UI features. Features selected by a validation team (not the three BCHAD engineers) with a proportional mix of simple (40%), medium (40%), and complex (20%).

**Three-assessment evaluation per feature:**

1. **CI pass (binary):** Does the factory-generated PR pass the full CI pipeline (Tier 2) on first run? Primary metric for the 80% target.

2. **Cleanup time (measured):** An engineer who did *not* build the original feature reviews the factory output and brings it to merge-ready quality. Target: under 30 minutes.

3. **Blind code review (qualitative):** A senior engineer reviews factory-generated code alongside the original human-written code, without knowing which is which. Rates both on a 1–5 scale for convention adherence, readability, completeness, test quality, and security practices.

**Reporting:** CI pass rate with confidence interval, median and p90 cleanup time, mean quality scores (factory vs. human), categorized breakdown of failures by error taxonomy. Fix systematic issues, tune prompts, update codebase profiles.

**Target:** 80% first-run CI pass rate across the 60 test features. Median cleanup time under 30 minutes. Factory code quality scores within 0.5 points of human-written code.

**Milestone:** v1 ready for supervised production use (Phase 1 trust mode).

---

## 13. Success Metrics

| Metric | Target (v1) | How Measured |
|---|---|---|
| CI pass rate on first run (Tier 2) | ≥ 80% | Factory-generated PRs run through full CI pipeline |
| Human cleanup time per feature | < 30 minutes | Time from PR creation to merge, minus review time |
| Engineer time per CRUD+UI feature | < 1 hour (spec + review) | Compared to current baseline of 3.5 days |
| Retry rate per stage | < 30% of stages require retry | Logged by the generation engine, categorized by error type |
| Cost per CRUD+UI feature | < $5.00 | API spend tracked per stage, per run |
| Engineer trust progression | ≥ 50% of engineers reach Phase 2 within 5 completed runs | Measured by trust score, not calendar time |
| Codebase intelligence accuracy | ≥ 90% of retrieved patterns rated "correct" by tech leads | Quarterly review of codebase profiles |
| Error classification accuracy | ≥ 85% of errors correctly categorized | Spot-check by engineers reviewing error trails |
| Validation blind review scores | Factory output within 0.5 points of human-written code | Blind code review during validation phase |

---

*BCHAD is not a code generator — it is an engineering system that treats code generation as one step in a larger pipeline of intelligence, orchestration, verification, and human collaboration. The hard problem is not producing code. It is producing the right code, in the right place, in the right style, with the right dependencies, within a managed context window, at a tracked cost, with errors classified and routed to the right recovery strategy. BCHAD is designed for that.*

---

*SF-2026-03 · Rev. 2 · March 2026*
