# Session Log: BCHAD Architecture Deep-Dive Interview and Implementation Planning

**Date:** 2026-03-21 00:50
**Duration:** ~2 hours
**Focus:** Conducted a 20-question architecture interview covering every major BCHAD subsystem, then produced a phased v1/v2/v3 implementation plan based on the answers.

---

## What Got Done

- Read all four core project documents: `README.md`, `docs/PRD.md`, `docs/bchad-framework-v2.md`, `docs/bchad-techstack.md`
- Conducted a structured multi-round interview (20 questions across 5 rounds) covering: trust model, NL translator UX, codebase intelligence, error classification, Tier 2 gate design, verification containers, cost model, Temporal sizing, and all four open questions from the PRD (OQ-1 through OQ-4)
- Resolved all four PRD open questions (OQ-1: Slack + CLI in v1; OQ-2: 7-year S3 retention; OQ-3: CRUD + structured TODOs; OQ-4: aggregate-only to leadership)
- Created `docs/implementation-plan.md` — a full phased plan for v1 (Days 1–60), v2 (Days 61–120), and v3 (Days 121–180) with milestones, dependencies, deliverables, a risk register, and a cross-cutting decisions table mapping every interview answer to a specific file and phase

---

## Issues & Troubleshooting

No bugs or deployment issues — this was a pure design/planning session. The "troubleshooting" was surfacing and resolving design ambiguities:

- **Problem:** The PRD left OQ-1 (Slack vs. CLI approvals) unresolved.
  **Cause:** The v1 scope section only mentioned "CLI-based" approvals; Slack was listed as a risk.
  **Fix:** Decision: ship both in v1. CLI covers terminal workflows; Slack covers async engineers. `internal/adapters/slack.go` added to Phase 4 scope.

- **Problem:** The PRD left OQ-2 (S3 prompt log retention) unresolved.
  **Cause:** SOC 2 requires a defined period but no duration was specified.
  **Fix:** Decision: 7-year retention (SOC 2 Type II standard). S3 lifecycle policy applied to `bchad-artifacts/` in Phase 5.

- **Problem:** OQ-3 (mixed-pattern features that are 80% CRUD+UI) had no v1 answer.
  **Cause:** The framework only described pure CRUD+UI generation; partial patterns were out of scope but unanswered.
  **Fix:** Decision: generate CRUD portion fully, emit structured TODOs for non-CRUD elements with codebase reference pointers. PR description explicitly lists what was generated vs. what needs manual work.

- **Problem:** OQ-4 (trust score visibility to engineering leadership) raised a gaming risk.
  **Cause:** If engineers see individual scores are visible upward, they avoid hard features to protect their score, undermining adoption and trust model accuracy.
  **Fix:** Decision: individual trust scores visible only to engineer + tech lead. Leadership sees aggregate-only metrics (CI pass rate, cost, adoption) via the v3 metrics dashboard.

- **Problem:** The Tier 2 gate description ("spin up the target product's full CI environment") was ambiguous about what "full environment" meant and who provisions it.
  **Cause:** The framework doc described it abstractly; the tech stack doc listed ECS Fargate tasks without clarifying which gate ran where.
  **Fix:** Clarification: Tier 1 gates run in BCHAD-controlled ephemeral Docker containers (per-language base images + mounted product toolchain). Tier 2 delegates entirely to the product's existing GitHub Actions CI by pushing the assembled branch and monitoring via the GitHub Checks API.

- **Problem:** The framework said guidance notes are "prepended to the prompt" without specifying where in the five-layer structure.
  **Cause:** The prompt architecture section described layers 1–5 but not how re-run guidance fits in.
  **Fix:** Clarification: guidance notes are injected as a new user message in conversation history after the previous failed output (pattern: user → assistant: [failed output] → user: [guidance + error context]). This gives the model the corrective context it needs rather than treating the guidance as a static constraint.

---

## Decisions Made

**Trust scoring — edit volume measurement**
Line-level diff (normalized as % of generated lines) via `git diff` between the factory's committed stage SHA and the final merge commit. Cheaper than AST-level diffing, far more precise than file-level. A typo fix on 1 line of a 500-line file scores ~0.2% edit volume; a full rewrite scores ~100%. Implementation uses `go-git` diff output, which is already in the stack.

**NL translator UX**
Single-pass best-guess + spec edit, one round-trip. The translator makes its best inference for every field, marks uncertain ones `needs_clarification: true` with a human-readable reason. The engineer sees the full draft spec with flagged fields highlighted, edits what they want, confirms. Not interactive question-by-question — that's the BMAD failure mode.

**Cold-start feedback loop**
Tech lead edits the codebase profile directly (editable YAML). Not LLM-interpreted corrections (lossy at the most critical point) and not re-extraction (can't capture forward-looking intent). Manual overrides take precedence over extracted patterns per FR-11.

**Flaky CI handling in Tier 2**
Before entering the targeted fix loop on any CI failure, BCHAD checks if the same tests are failing on the main branch via the GitHub Checks API (one API call, milliseconds). If main is also failing: mark `blocked_by_baseline`, skip fix loop, surface to engineer. Excludes these outcomes from CI pass rate in the trust score so engineers aren't penalized for their squad's flaky tests.

**Error classifier design**
Hybrid: rule-based first (TS error code → Type, ESLint rule ID → Style, Semgrep rule ID → Security, syntax parse error class → Syntax, etc.), LLM fallback for ambiguous 20% (primarily Type vs. Logic edge cases and Context vs. Specification edge cases). Context/Specification ambiguity is resolved deterministically first: if `permissions` is set in the BCHADSpec and auth middleware is missing → Context; if `permissions` is absent → Specification.

**Skip-stage contract extraction**
When an engineer skips a stage and provides manual files, BCHAD auto-extracts the StageArtifact outputs (endpoint contracts, types, route paths) via Tree-sitter AST parsing — same queries used by the codebase indexer. Not structured JSON input from the engineer (too much friction), not an LLM pass (non-deterministic for a deterministic problem). If extraction fails, BCHAD surfaces a specific error asking the engineer to match the product's route registration pattern.

**Route conflict detection**
Two-step: route inventory lives in the structural profile (extracted and indexed by Tree-sitter during indexing, updated on every post-merge re-index). Conflict check runs post-generation: Tree-sitter parses generated files to extract route definitions, diffs against indexed inventory. Can't run pre-generation because the exact paths the LLM will generate aren't known until it generates them.

**Pattern quality scoring**
Hybrid composite: recency (decay function, weight ~0.3) + review quality from GitHub PR data — zero revision-requested reviews preferred (weight ~0.3) + structural completeness via Tree-sitter (all expected elements present for stage type, weight ~0.4). Stored as `pr_quality_score` in `bchad_code_patterns`, used as secondary ranking signal alongside vector similarity in retrieval.

**Migration concurrency**
Deliberately not built. Timestamp-based migration filenames + normal PR merge ordering handle it the same way they handle concurrent human-written migrations. Distributed locking would serialize pipeline throughput at the most approval-heavy stage for a problem the existing CI pipeline already solves.

**Prompt template ownership**
Layer 1 (system prompt), Layer 2 (language adapter), Layer 5 (generation instruction): owned by BCHAD engineers; changes gated by validation protocol (60-feature validation suite must not regress CI pass rate). Layer 3 (codebase brief): owned by tech leads via direct profile edits. Snapshot tests (`cupaloy`) catch accidental template changes immediately without LLM calls.

**Temporal worker sizing**
2 ECS Fargate tasks, single task queue, no auto-scaling. Stage activities are >99% I/O wait (LLM calls + Docker gate polling). 2 tasks provides availability (one can restart without stalling all in-flight activities), not capacity. The real concurrency bottleneck is the Anthropic API rate limit, handled by the Valkey-backed rate limiter in the gateway.

**Verification container toolchain**
Three base language images (`bchad-verify-ts`, `bchad-verify-py`, `bchad-verify-go`) with common tools pre-installed. Product-specific `package.json`/`requirements.txt`/`go.mod` + linter/formatter configs mounted at runtime from S3 codebase profile. `npm ci`/`pip install` runs at container start with lockfile-hash-keyed cache layer. This ensures gates run against the product's exact toolchain without maintaining per-product images that drift.

**BCHAD engineer authentication (v1)**
GitHub identity via `GITHUB_TOKEN`. On CLI startup, one `/user` endpoint call resolves the GitHub login as the stable `engineer_id`. Zero additional setup — the token is already required for repo/PR operations. Trust scores and approval audit records key on the GitHub login. v2 web UI migrates to SSO (Okta/IdP browser OAuth flow).

**Highest-risk component**
Codebase intelligence and retrieval. Unlike every other component (which have deterministic correctness conditions testable without LLM calls), intelligence correctness is subjective and only verifiable by generating code and seeing if it looks right to the team. Bad patterns → silently wrong generation that may pass CI but fail code review. Mitigation: start first (Phase 1, Day 5), tech lead validation generation before any real features, manual override capability.

---

## Current State

The project has:
- Complete architecture documentation (README, PRD, framework, tech stack, CLAUDE.md with code style and testing rules)
- All interface schemas defined in docs (BCHADSpec, BCHADPlan, StageArtifact, GateResult)
- Complete database schema defined in docs
- Full API contract documentation (REST endpoints, Valkey streams, event formats)
- A resolved set of design decisions covering every major ambiguity
- A phased implementation plan with concrete deliverables, milestones, and a risk register

What does **not** exist yet:
- No Go source files have been written
- No database migrations have been applied
- No Docker images have been built
- No product repositories have been indexed
- No prompt templates exist in `patterns/`

The project is at the architecture-complete, implementation-not-started state.

---

## Next Steps

Prioritized for the next session (start of Phase 0 + early Phase 1):

1. **Initialize Go module** — `go mod init github.com/athena-digital/bchad`, add all dependencies from the tech stack doc, verify `go build ./...` compiles cleanly
2. **Write `docker-compose.yml`** — Postgres 16 + pgvector, Valkey 8, MinIO, Temporal dev server; verify `just dev-up` starts all services
3. **Write all five database migrations** — `migrations/001` through `005` covering all tables and HNSW indexes from the schema; verify `just migrate` applies cleanly
4. **Define all four JSON schemas** — `schemas/bchadspec.v1.json`, `bchadplan.v1.json`, `stage_artifact.v1.json`, `gate_result.v1.json` with Draft 2020-12 validation
5. **Create Go type stubs** — `pkg/bchadspec/types.go`, `pkg/bchadplan/types.go`, `pkg/artifacts/types.go` matching the schemas; compile-clean
6. **Start Phase 1 in parallel** — reach out to Payments Dashboard and Claims Portal tech leads to schedule onboarding questionnaire sessions (30 min each); request repo read access; this is the critical path item for the entire v1 timeline since everything depends on validated codebase profiles
7. **Write `internal/intelligence/scanner.go`** — structural profile extraction from repo file tree, framework detection, config file copy to S3; table-driven tests with fixture repos

The Phase 1 intelligence work should start no later than Day 5 even if the foundation isn't 100% complete, because the tech lead scheduling and profile validation cycle has a human dependency that can't be parallelized away.
