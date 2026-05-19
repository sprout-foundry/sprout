# SP-042: Self-Review Quality Gates — Tooling in Place of Human Review

**Status:** 📋 Proposed
**Date:** 2026-05-19
**Priority:** MEDIUM (mitigates the solo-committer risk by automating what a second reviewer would catch)
**Depends on:** None
**Related:** SP-028 (Test Suite Stabilization), SP-043 (Documentation & Bus-Factor Resilience)

## Problem

The repo has one human committer. `git shortlog -sne --all` returns essentially a single author (1627+ commits as Alan Price across four name aliases; the other entries are GitHub Action / Smoke Test / auto). The implication: **no PR ever receives independent human review.**

A reviewer typically catches:
- Subtle correctness issues (off-by-one, nil deref, error swallowing).
- Architectural drift (a new function in the wrong package, a circular import waiting to happen).
- Security smells (string-built SQL, unvalidated input, hardcoded credentials).
- Complexity creep (a 400-line function that should be three).
- Convention drift (different error-wrapping styles in adjacent files).
- Dead code (unreferenced functions, unused parameters).

Sprout's CI today catches some of this via Go's strict-by-default toolchain (`unused`, `errcheck` partially via vet, race tests, goleak). But several major categories are not gated:

### Concrete gaps in the current CI

1. **No `golangci-lint`.** `.github/workflows/build.yml` runs `go test`, race tests, and an ESLint step marked `continue-on-error`. There is no aggregate Go linter that runs `gosec` (security), `errcheck` (full error-check), `gocyclo` (complexity), `ineffassign`, `unparam`, `unconvert`, `revive`, etc.

2. **ESLint is non-blocking.** Per the existing build.yml, the frontend lint step is allowed to fail without breaking CI. This is a known compromise but it means style and correctness drift accumulate.

3. **No complexity cap.** A function can grow without bound; sprout has several 1000+-line files (per SP-029's own analysis: `seed_tool_registry.go` 1223, `scripted_client.go` 1068, `tool_definitions.go` 1007, `seed_integration.go` 906, `persistence.go` 803). SP-029 is decomposing them after the fact; a cyclomatic-complexity gate would have prevented the growth.

4. **No import-cycle / layering check.** A reviewer would notice if a low-level package starts importing a high-level one. There is no automated check (`go-cleanarch` / equivalent).

5. **No coverage delta gate.** Coverage drift goes unnoticed unless someone manually compares.

6. **No secret-scanning.** `gitleaks` or equivalent does not run in CI. A committed `.env` or API key would not be caught at PR time.

7. **No SAST.** Beyond `gosec` (covered by golangci-lint), there's no semgrep or CodeQL.

8. **No "two pairs of eyes" simulation.** A custom pre-commit / pre-merge step that runs the same model against the diff and produces a review comment — purely advisory — could surface the kind of issues a human reviewer would, without blocking velocity.

### Why this matters now

Velocity is ~30 commits/day. With no review pressure, the only forces preventing drift are (a) the author's discipline (high but not unlimited) and (b) the test suite (good but tests don't catch architectural drift). Adding tooling here is the **only structural mitigation** for the bus-factor risk that doesn't require slowing down the author.

## Goals / Non-Goals

**Goals**
- `golangci-lint` runs in CI as a hard gate, covering `gosec`, `errcheck`, `ineffassign`, `unparam`, `unconvert`, `revive`, `gocyclo` (with a documented threshold).
- Frontend ESLint becomes a hard gate (`continue-on-error: false`); existing violations are either fixed or explicitly waivered with an `.eslintignore` / per-line comment.
- A complexity gate fails any new function over a threshold (start lenient — 30 — and ratchet down).
- Import-layering check enforces the package architecture diagrams in `roadmap/SP-001-agent-core.md` and similar.
- Coverage delta: PRs that drop overall coverage below current floor (currently 40% per `CLAUDE.md`) fail.
- Secret scanning (`gitleaks`) runs on every push.
- Optional but desirable: a "Claude-as-reviewer" advisory step that posts a review comment on every PR with high-signal suggestions — purely informational, never blocking.

**Non-Goals**
- Replacing human review for releases or security-sensitive merges (no spec can do that; this is a velocity-mitigation, not a substitute).
- Mandating that every existing file pass every new lint immediately. Phase-in via `//nolint` comments where needed, with a TODO to clean up.
- Adopting every linter under the sun. The list above is the curated minimum.
- Adding pre-commit hooks that block local commits — those are a personal preference, not project policy.

## Current State

| Gate | Status | Evidence |
|------|--------|----------|
| `go test -race` (pkg/agent, pkg/webui) | ✅ Hard gate | SP-028 landed |
| `goleak.VerifyNone` | ⚠️ With allowlist | SP-036 will tighten |
| Frontend ESLint | ❌ Soft (continue-on-error) | `.github/workflows/build.yml` |
| Frontend type-check | ✅ Hard | tsc step |
| `golangci-lint` | ❌ Absent | No workflow runs it |
| `gosec` | ❌ Absent | Subset of golangci-lint |
| `gocyclo` complexity cap | ❌ Absent | Files routinely exceed 1000 lines |
| Import layering | ❌ Absent | SP-038 spec notes the "acyclic by discipline" risk |
| Coverage delta | ⚠️ Floor only | 40% minimum per CLAUDE.md, no drop-protection |
| Secret scanning | ❌ Absent | No gitleaks/trufflehog step |
| Advisory LLM review | ❌ Absent | Not implemented |

## Proposed Solution

### Track A — `golangci-lint`

A1. **Add `.golangci.yml`** to repo root. Linters enabled (starting curated set): `errcheck`, `gosec`, `govet`, `ineffassign`, `unconvert`, `unparam`, `revive`, `gocyclo`, `misspell`, `bodyclose`, `noctx`, `nilerr`.

A2. **Initial baseline.** Run locally; fix what's cheap to fix; for the rest, add `//nolint:<linter>` with a one-line reason. Goal: zero open violations on day one.

A3. **CI workflow.** New step in `.github/workflows/build.yml`: `golangci-lint run --timeout=5m`. Hard gate.

A4. **Ratchet plan.** Each subsequent PR cannot add new violations; over time, the `//nolint` waivers are paid down.

### Track B — Frontend ESLint as a hard gate

B1. **Audit existing ESLint output.** Run `npm run lint --workspaces` (or whatever exists today). Fix violations or waiver them explicitly.

B2. **Flip `continue-on-error: false`** in the relevant frontend lint workflow step.

B3. **Add Prettier check** alongside ESLint (formatting violations as a hard gate prevents drift).

### Track C — Complexity cap

C1. **`gocyclo --over=30`** in golangci-lint config initially. Identify violators; for each: either refactor in this spec's scope (no — too much), or add `//nolint:gocyclo // SP-029-tracked: split planned in <phase>` with explicit ticket reference.

C2. **Ratchet plan.** As SP-029 decomposes files, complexity drops; reduce the threshold to 20, then 15. Goal end state: cap at 15 with zero waivers.

C3. **Frontend complexity.** Add `complexity` rule in ESLint config with threshold 20; ratchet similarly.

### Track D — Import-layering check

D1. **Tool selection.** `go-cleanarch` (declarative layer config in YAML) or `dep` (depguard via golangci-lint, simpler). Start with `depguard` since it composes with the linter already added.

D2. **Layer declarations.** Encode the architecture:
  - `pkg/configuration` — no domain imports.
  - `pkg/agent_tools` — may import `pkg/configuration`, `pkg/events`, `pkg/logging`; may NOT import `pkg/agent`.
  - `pkg/agent` — may import everything in `pkg/agent_*`, `pkg/configuration`, etc.; may NOT import `pkg/webui`.
  - `pkg/webui` — top-level; may import everything else.
  - `cmd/*` — top-level binary entry points; may import everything.

D3. **CI hard gate.** Layering violations fail the build.

### Track E — Coverage drop-protection

E1. **`gcov2lcov` + `lcov-summary`** (or whatever tool fits) to compute total coverage per PR.

E2. **Coverage gate.** PRs that drop overall coverage below the rolling baseline (initial: 40%) fail. Add a `make coverage-baseline` to update the baseline intentionally.

E3. **Optional: package-level floors.** `pkg/agent` and `pkg/webui` get tighter floors (e.g., 55%) since they're critical.

### Track F — Secret scanning

F1. **`gitleaks` step** in CI on every push. Configuration: standard ruleset + custom rules for sprout-specific config patterns (look at `~/.config/sprout/api_keys.json` shape).

F2. **`.gitleaks.toml`** at repo root with documented allow-list for known false positives (e.g., test fixtures with fake tokens).

F3. **Pre-existing commits.** Run `gitleaks detect` historically; document findings; rotate any real leaks; add commits to the historical-allow-list.

### Track G — Optional: LLM advisory review

G1. **GitHub Action that runs sprout against the PR diff** and posts a review comment with findings. Purely informational. Skip if the PR is from a bot or the diff is trivial (<20 lines).

G2. **Cost cap.** Per-PR token budget (e.g., 50k) enforced via `pkg/agent`'s existing budget system. Skip PR if it exceeds the diff-size threshold.

G3. **Suppression label.** A `skip-llm-review` PR label skips the step (for emergencies or low-signal PRs).

G4. **Quality calibration.** Track the signal-to-noise of LLM suggestions for a defined period; if SNR is poor, gate the review's surfaced findings on a confidence-based filter.

## Implementation Phases

### Phase 1: golangci-lint baseline
[ ] SP-042-1a: Add `.golangci.yml` with the curated linter set. Commit with the documented config.
[ ] SP-042-1b: Run locally; fix cheap violations.
[ ] SP-042-1c: Add `//nolint:<linter> // <reason>` waivers where fix is out-of-scope; each waiver references a tracking ticket where appropriate.
[ ] SP-042-1d: Add the `golangci-lint run --timeout=5m` step to `.github/workflows/build.yml` as a hard gate.

### Phase 2: ESLint hard gate
[ ] SP-042-2a: Run `npm run lint` (or equivalent) for both `packages/ui` and `webui`. Capture the violation count.
[ ] SP-042-2b: Fix or explicitly waiver every violation.
[ ] SP-042-2c: Flip `continue-on-error` to `false` for the frontend lint step.
[ ] SP-042-2d: Add Prettier check step.

### Phase 3: Complexity cap
[ ] SP-042-3a: Enable `gocyclo --over=30` in `.golangci.yml`. Add waivers for known SP-029-tracked files.
[ ] SP-042-3b: Add ESLint `complexity: ["error", 20]` rule. Apply waivers where SP-029 / SP-038 / SP-039 will refactor.

### Phase 4: Import layering
[ ] SP-042-4a: Add `depguard` to `.golangci.yml` with the layer rules.
[ ] SP-042-4b: Resolve any violations surfaced by the initial run.

### Phase 5: Coverage gate
[ ] SP-042-5a: Add coverage-computation step to CI producing a single percentage per package + overall.
[ ] SP-042-5b: Store rolling baseline (in repo or as a GitHub Action artifact); fail if PR drops below baseline minus tolerance (e.g., 0.5pp).
[ ] SP-042-5c: Add `make coverage-baseline` to update the baseline intentionally.

### Phase 6: Secret scanning
[ ] SP-042-6a: Add `gitleaks` step to CI.
[ ] SP-042-6b: Create `.gitleaks.toml` with custom rules + allowlist.
[ ] SP-042-6c: Run `gitleaks detect` on full history; document/rotate findings.

### Phase 7: LLM advisory review (optional)
[ ] SP-042-7a: Create `.github/workflows/llm-review.yml` that runs sprout against the PR diff.
[ ] SP-042-7b: Implement diff-size gating and budget caps.
[ ] SP-042-7c: Add `skip-llm-review` label support.
[ ] SP-042-7d: Calibrate signal-to-noise; tune confidence filter.

### Phase 8: Documentation
[ ] SP-042-8a: Add `docs/QUALITY_GATES.md` covering: every active gate, threshold, how to add a waiver, how to update the baseline.
[ ] SP-042-8b: Update `CONTRIBUTING.md` with a "How to handle a CI gate failure" section.

## Success Criteria

| Metric | Target |
|--------|--------|
| `golangci-lint` open violations | 0 (waivers explicit and tracked) |
| Frontend ESLint `continue-on-error` | `false` |
| `gocyclo` ceiling | 30 (ratchet plan to 15) |
| `depguard` layering violations | 0 |
| Coverage delta gate | Active, floor = 40% |
| `gitleaks` step | Passing on main |
| LLM advisory comments per PR (Phase 7) | At least one substantive finding on 60%+ of non-trivial PRs |

## Files Reference

| File | Action |
|------|--------|
| `.golangci.yml` | Create: linter config |
| `.gitleaks.toml` | Create: secret-scan config |
| `.github/workflows/build.yml` | Modify: add lint, gocyclo, depguard, gitleaks, coverage steps |
| `.github/workflows/llm-review.yml` | Create (Phase 7): LLM advisory review |
| `packages/ui/.eslintrc` / `webui/.eslintrc` | Modify: add complexity rule; tighten as needed |
| `docs/QUALITY_GATES.md` | Create |
| `CONTRIBUTING.md` | Modify: "How to handle a CI gate failure" section |
| `Makefile` | Modify: add `lint`, `coverage-baseline` targets |
| (multiple `*.go` files) | Modify: add `//nolint:<linter> // <reason>` waivers as needed |

## Risks

- **Linter floods.** Initial run may produce hundreds of violations. Mitigation: triage by linter; fix cheap categories wholesale; waiver the rest explicitly with reasons. Track waivers as a debt list.
- **Velocity hit during baseline.** The first PR after enablement is expensive. Mitigation: do the baseline cleanup as its own PR; subsequent PRs see only their own deltas.
- **False positives drive cynicism.** A linter that cries wolf gets ignored. Mitigation: configure each linter explicitly (no defaults); document why each is enabled.
- **LLM review is noise.** Bad suggestions waste reader attention. Mitigation: advisory only, never blocking; confidence filter; ability to suppress per-PR via label; calibration metric tracked.
- **Coverage gate punishes risky refactors.** Big refactor PRs may legitimately reduce coverage temporarily. Mitigation: `make coverage-baseline` lets the author update the floor intentionally with a justification commit.
- **The author still has to maintain all of this.** Adding tooling without a maintainer creates its own debt. Mitigation: the tooling is well-documented, off-the-shelf, and runs in CI without manual operation; failures route through standard build-failure handling.
