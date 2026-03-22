# Session Log: PR URL Fix, GitHub Integration, and Worker Infrastructure

**Date:** 2026-03-21 19:56
**Duration:** ~3 hours (continued from previous session via context compaction)
**Focus:** Fix the pipeline's PR URL to point to a real, accessible GitHub PR; add `just worker` target to eliminate stale-worker operational friction.

---

## What Got Done

- **Fixed `loadRepoURL` defaulting to empty** — `BCHAD_S3_ENDPOINT` is only exported by `just`, not the shell; `loadRepoURL` silently returned `""` when run via `./bin/bchad` directly. Added `http://localhost:9000` as the default, matching the justfile's own default.
- **Added `RepoURL` field to `StructuralProfile`** (`internal/intelligence/types.go`) and wired `resolveRepoURL()` into `Scanner.Scan()` (`internal/intelligence/scanner.go`) using `go-git` to read the git remote URL and normalise SSH → HTTPS.
- **Added `RepoURL` to `BCHADPlan`** (`pkg/bchadplan/types.go`) and plumbed it through `cmd/bchad/run.go` → `workflows/pipeline.go` → `workflows/activities.go` (`PRInput`).
- **Implemented real GitHub PR creation** in `AssemblePRActivity` (`workflows/activities.go` + new `workflows/github.go`):
  - Parses `owner/repo` from the HTTPS repo URL.
  - Fetches the default branch and its HEAD SHA via GitHub REST API.
  - Creates branch `bchad/{planID}` (no-op if it already exists).
  - Commits a `.bchad/{planID}.json` manifest file to produce one commit ahead of base.
  - Opens a draft pull request.
  - Falls back to a stub URL if `GITHUB_TOKEN` is unset.
- **Added `just worker` target** (`justfile`):
  - Kills any stale process named `worker` with `pkill -x worker`.
  - Runs `just build` to guarantee the binary is current.
  - Starts `./bin/worker` in the background with Temporal env vars inherited from the justfile's exports and `.env` via `set dotenv-load`.
- **Rebuilt both binaries** (`bin/bchad`, `bin/worker`) after every change set.
- **Committed four changesets** on `phase2/core-pipeline`:
  - `fix(assembly): resolve correct github repo url from indexed structural profile`
  - `feat(assembly): implement real github pr creation in AssemblePRActivity`
  - `feat(infra): add worker target to justfile with temporal env defaults`

---

## Issues & Troubleshooting

- **Problem:** Pipeline PR URL was `https://github.com/athena-digital/node-express-prisma-v1/pull/stub-1234` — wrong org, wrong repo, fake PR number.
  - **Cause (1):** `AssemblePRActivity` stub hardcoded `athena-digital/{productID}` for the repo URL.
  - **Cause (2):** `loadRepoURL` returned `""` because `BCHAD_S3_ENDPOINT` was not set in the shell environment (only exported inside `just` tasks). The early-return `if s3Endpoint == "" { return "" }` silently skipped the S3 fetch.
  - **Fix:** Default `s3Endpoint` to `http://localhost:9000` when unset. Confirmed via a unit test (`cmd/bchad/repourl_test.go`, deleted after passing) that the function returns `https://github.com/ryoiwata/node-express-prisma-v1-official-app`.

- **Problem:** After fixing `loadRepoURL`, the PR URL still showed `stub-1234`.
  - **Cause:** The worker binary (`bin/worker`) was built at 17:18, before the `RepoURL` changes were applied in this session. The old binary's `AssemblePRActivity` didn't know about `PRInput.RepoURL` and returned the stub. Additionally, a stale worker process (PID 467383, started in an earlier test) was still connected to Temporal and stealing activity tasks.
  - **Fix:** Rebuilt `bin/worker`. Killed the stale worker with `ps aux | grep bin/worker | xargs kill -9`. Restarted the new binary with `GITHUB_TOKEN` set.

- **Problem:** `just worker` terminated with "Recipe `worker` was terminated on line 102 by signal 15".
  - **Cause:** The original implementation used `pkill -f bin/worker`. The `-f` flag matches the full command line, which includes the shell `just` uses to run the recipe (`sh -c 'pkill -f bin/worker || true'`). That shell's argv contains `bin/worker`, so `pkill` sent SIGTERM to its own parent shell, propagating up to the `just` process.
  - **Fix:** Switched to `pkill -x worker` (`-x` matches exact process name, not full cmdline). The running worker binary has process name `worker`; the `just` and `sh` processes do not.

- **Problem:** `just worker` ran successfully but the worker process disappeared immediately when invoked in background by Claude Code's bash harness.
  - **Cause:** The bash harness backgrounded the `just worker` command itself; when that harness process exited, it killed the child process tree before the worker could detach.
  - **Fix:** Not a real problem — when run normally in the foreground, `./bin/worker &` correctly orphans the worker process. Verified by running `just worker` then checking `pgrep -a -x worker`.

- **Problem:** GitHub API integration test created PR #1 with a test branch (`bchad/test-pr-creation`) and a test file (`.bchad/test-run.json`) in the target repo.
  - **Cause:** This was intentional during testing.
  - **Fix:** No code fix needed; PR #1 and its branch remain in the repo as a side effect of the integration test.

---

## Decisions Made

- **Default `BCHAD_S3_ENDPOINT` to `http://localhost:9000` in `loadRepoURL`** rather than requiring it to be set in the shell. The justfile already uses this same default for all targets; matching it removes a sharp edge when running `./bin/bchad` directly.

- **Use `pkill -x worker` instead of `pkill -f bin/worker`** for the `just worker` target. The `-x` flag is safe because it matches only the process name (`worker`), not the full command line, so it cannot accidentally kill the `just` or shell processes running the recipe.

- **Draft PR by default** — `AssemblePRActivity` opens PRs with `"draft": true`. Since stage outputs are still stubs in Phase 2, a draft signals to reviewers that the PR is not yet ready for merge review.

- **Fallback to stub URL when `GITHUB_TOKEN` is unset** — rather than hard-failing, `AssemblePRActivity` logs a warning and returns a `stub-no-token` URL. This keeps the pipeline runnable in token-less CI environments or sandboxes.

- **Commit a `.bchad/{planID}.json` manifest file** to the branch before opening the PR. GitHub requires at least one commit ahead of the base branch to create a PR; the manifest file serves that purpose while also providing useful metadata about the pipeline run.

- **`just worker` echoes the Temporal host/namespace** at startup so engineers can confirm which environment the worker connected to.

---

## Current State

**Working end-to-end:**
- `just dev-up` starts the full local stack (Postgres, Valkey, MinIO, Temporal) and registers the `bchad` namespace and custom search attributes idempotently.
- `just index-repo` scans the target repo, resolves the git remote URL via `go-git`, and uploads the structural profile (including `repo_url`) to MinIO.
- `just worker` kills stale workers, rebuilds binaries, and starts a fresh worker connected to Temporal.
- `./bin/bchad run --spec examples/payment-methods.json` generates a plan, prompts for approval at `migrate` and `api` gates, executes all five stages (stubs), assembles a real GitHub PR, and prints the PR URL.
- Pipeline output: `https://github.com/ryoiwata/node-express-prisma-v1-official-app/pull/2` — a real open draft PR on the correct repo.

**Still stubbed (Phase 2 scope):**
- `ExecuteStageActivity` — returns fixture artifacts, does not call Claude API.
- `Tier2GateActivity` — always returns `passed: true`, does not run real CI.
- Per-stage commits — the PR branch contains only the `.bchad/{planID}.json` manifest; actual generated files are not committed.

**All unit and replay tests passing** (`go test ./...`).

---

## Next Steps

1. **Phase 3: implement `ExecuteStageActivity`** — wire the LLM gateway, context budget allocator, and retrieval service to generate real code for each stage type (`migrate`, `config`, `api`, `frontend`, `tests`).
2. **Per-stage commits** — after each `ExecuteStageActivity` returns generated files, commit them to the PR branch before advancing to the next stage.
3. **`Tier2GateActivity`** — dispatch a real CI run (GitHub Actions or ECS Fargate) against the assembled PR and poll for completion.
4. **Error classification and retry** — implement the eight-category error taxonomy; wire category-specific retry policies into the workflow's activity options.
5. **Add `GITHUB_TOKEN` guidance to `README` / `CLAUDE.md`** — document that `GITHUB_TOKEN` must be present in `.env` for `just worker` to create real PRs.
6. **Clean up test PR #1** — close or delete `https://github.com/ryoiwata/node-express-prisma-v1-official-app/pull/1` and the `bchad/test-pr-creation` branch created during the GitHub API integration test.
