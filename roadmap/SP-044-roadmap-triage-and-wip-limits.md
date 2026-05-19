# SP-044: Roadmap Triage & WIP Limits

**Status:** 📋 Proposed
**Date:** 2026-05-19
**Priority:** LOW (process discipline; structural rather than urgent)
**Depends on:** None
**Related:** every other roadmap spec; SP-043 (Documentation & Bus-Factor Resilience)

## Problem

The roadmap is growing faster than it's being completed. Today:

- **37 spec files** in `roadmap/` (SP-001 through SP-035 plus this batch).
- **22 marked `Proposed`** by `grep -l "Status.*Proposed\|📋 Proposed"`.
- **21 marked `Complete` / Done.**
- **9 new specs added by this commit** (SP-036 through SP-044), all `Proposed`.

That's **~31 open specs** for a single committer working at ~30 commits/day. Even at exceptional throughput, that's a multi-month-to-multi-year backlog with no explicit prioritization. The roadmap is high-quality (per the earlier audit) but volume is itself a problem:

### Concrete failure modes of unmanaged WIP

1. **No "what's next" signal.** A new contributor (or a returning author after a context-switch) sees 31 open specs with no ordering. Choice paralysis.
2. **Specs decay.** A spec written six months ago against then-current code drifts as the code moves. By the time anyone implements it, the file:line refs are stale (SP-029 already shows this — it tracks files that have since been split).
3. **No closure of dead ideas.** Some `Proposed` specs may have been superseded, deprioritized, or quietly abandoned. They sit in `roadmap/` as if they were live, polluting the signal.
4. **TODO.md and roadmap can drift.** TODO.md lists tracked specs; if a spec is deferred but TODO entries linger, they accumulate as zombie tasks.
5. **No throughput visibility.** "How many specs do we close per week?" is currently unknowable without manual git archaeology.

### Why "just ignore it" is fine but suboptimal

The author manages this implicitly today. The system works because there is exactly one person making decisions. **But**:
- It is invisible to anyone else.
- It cannot scale to a second contributor.
- It loses information when context-switching (which spec was I working on, which had I deprioritized?).
- It conflicts with SP-043's onboarding goal: a new contributor cannot reasonably pick a spec to work on.

## Goals / Non-Goals

**Goals**
- Every spec in `roadmap/` has one of: `Active`, `Proposed`, `Deferred`, `Superseded`, `Done`. No ambiguous states.
- The number of `Active` specs is bounded (initial cap: 3).
- A `roadmap/README.md` shows the current state at a glance: what's Active, what's Proposed and prioritized, what's Deferred and why, what's Done.
- Each `Deferred` and `Superseded` spec has a `Deferred-Reason` or `Superseded-By` field so future readers know the disposition.
- A periodic (suggested cadence: end of each phase / milestone) triage pass either promotes Proposed → Active, defers Active → Deferred, or closes Active → Done.

**Non-Goals**
- Forcing a calendar-based cadence on the author (no "weekly" or "monthly" — those don't apply to agent work).
- Migrating the roadmap into an external tool (GitHub Projects, Linear, etc.). Markdown in `roadmap/` is sufficient.
- Reformatting existing spec files. Add a header field; don't rewrite the content.
- Killing genuinely valuable Proposed specs to hit a WIP target. The triage is *information*; the author still decides.

## Current State

| Status (counted by `grep` of spec headers) | Count |
|--------------------------------------------|-------|
| `Proposed` | 22 (+ 9 from this batch = 31) |
| Complete / Done | 21 |
| `Active` (in-flight) | not tracked; implicit |
| `Deferred` | not tracked; not used |
| `Superseded` | not tracked; not used |

`TODO.md` (currently 386 lines) lists per-spec sub-tasks but doesn't track high-level status; the spec file header is the source of truth.

## Proposed Solution

### Track A — Define the status taxonomy

A1. **Five statuses, mutually exclusive:**
  - `Active` — currently being worked on, capped at N (initial: 3).
  - `Proposed` — fully spec'd, prioritized in the queue, waiting for an Active slot.
  - `Deferred` — spec is valid but not now; has a `Deferred-Reason`.
  - `Superseded` — replaced by another spec; has a `Superseded-By: SP-XXX`.
  - `Done` — implementation merged; spec retained for historical reference.

A2. **Optional priority for `Proposed`**: `Critical / High / Medium / Low`. Defaults to `Medium`.

A3. **Add status to spec frontmatter** in each spec file's "Status:" line. Use the existing line; just enforce the taxonomy.

### Track B — Triage the current backlog

B1. **Walk every Proposed spec.** For each, decide:
  - Active? (rare; cap is 3)
  - Stay Proposed at what priority?
  - Defer with what reason? (e.g., "deprecated by SP-038", "depends on Foundry decision", "needs more product context")
  - Supersede with which replacement? (write the link explicitly)

B2. **Track the decisions in `roadmap/TRIAGE-LOG.md`** so reasoning is preserved. One line per spec per triage pass.

B3. **For specs that have aged poorly** (file:line refs stale, code reorganized since), either refresh the spec or mark as `Superseded` with a pointer to the new context.

### Track C — `roadmap/README.md` as the status dashboard

C1. **A single table** showing every spec with: ID, Title, Status, Priority (if Proposed), short hook. Sorted by Status (Active → Proposed by priority → Deferred → Superseded → Done by most-recent).

C2. **`Active` section** with a one-line hook for each: what's the current sub-phase, what's blocked on what.

C3. **`Proposed` section** ordered by priority. Critical → High → Medium → Low → unset.

C4. **`Deferred` section** with each spec's Deferred-Reason visible.

C5. **`Done` section** as a flat list with link to the spec; serves as a historical changelog.

C6. **Auto-update or hand-update?** Initial: hand-update at triage time. If volume grows, a small script (`scripts/roadmap-status.sh`) can regenerate the table from spec frontmatter — write it only if hand-maintenance proves painful.

### Track D — WIP discipline

D1. **Cap Active specs at 3** initially. The cap is a soft target; exceeding it is fine but triggers a triage pass.

D2. **Status transition rules**:
  - `Proposed → Active` allowed when a slot is free.
  - `Active → Done` requires the implementation phases to be checked off in TODO.md.
  - `Active → Proposed` (de-promotion) allowed; record the reason.
  - `Active → Deferred` allowed; record the reason.
  - `* → Superseded` requires the replacement spec to exist.

D3. **TODO.md consistency check.** When a spec moves to `Deferred` / `Superseded`, mark its TODO.md entries with a `[deferred]` or `[superseded]` prefix so they don't look like live work.

### Track E — Periodic triage trigger

E1. **Triage triggers (any of):**
  - An Active spec moves to Done — re-evaluate the next pick.
  - A spec has been Active for "too long" — definition is fuzzy in agent-time; suggest: every time a triage opportunity presents itself, run the pass.
  - The Proposed list grows past a threshold (e.g., 30 — current).
  - Before a major release cut.
  - When onboarding a new contributor (SP-043 dependency).

E2. **Triage output:** updated spec headers, updated `roadmap/README.md`, updated `TRIAGE-LOG.md` entries.

### Track F — Tooling (optional)

F1. **`scripts/roadmap-status.sh`** — bash or Go: scans `roadmap/SP-*.md`, parses Status and Priority from headers, regenerates `roadmap/README.md`'s status table.

F2. **CI lint**: every spec file has a valid Status value. Add to SP-042's golangci-lint-adjacent script suite (or a standalone CI step).

F3. **A "stale Active" check**: any spec marked Active for N commits without a checked-off phase emits a warning. Optional and possibly more noise than signal.

## Implementation Phases

### Phase 1: Taxonomy + dashboard skeleton
[ ] SP-044-1a: Define the five-status taxonomy in `roadmap/README.md`'s header section.
[ ] SP-044-1b: Audit every existing spec file; ensure its `Status:` line uses one of the five values. Convert legacy values (`📋 Proposed` is fine; other variants need normalization).
[ ] SP-044-1c: Write the initial `roadmap/README.md` status table by hand.

### Phase 2: Triage pass
[ ] SP-044-2a: Walk every `Proposed` spec. Per-spec decision: keep / defer / supersede / promote to Active. Cap Active at 3.
[ ] SP-044-2b: For specs marked `Deferred`, add a `Deferred-Reason:` line to the spec header.
[ ] SP-044-2c: For specs marked `Superseded`, add a `Superseded-By: SP-XXX` line and a one-paragraph explanation at the top of the spec body.
[ ] SP-044-2d: Create `roadmap/TRIAGE-LOG.md` and record the decisions made in this pass.

### Phase 3: TODO.md consistency
[ ] SP-044-3a: For every spec moved to `Deferred` or `Superseded`, prefix its TODO.md section with `[deferred]` or `[superseded]` so live work is visually distinct.
[ ] SP-044-3b: Remove (or visually demote) TODO entries for `Superseded` specs whose successor has its own TODO entries.

### Phase 4: Tooling (optional)
[ ] SP-044-4a: Write `scripts/roadmap-status.sh` to regenerate the status table from spec headers.
[ ] SP-044-4b: Add a CI lint that fails on invalid Status values.
[ ] SP-044-4c: Optional: stale-Active warning.

### Phase 5: Documentation
[ ] SP-044-5a: Update `CONTRIBUTING.md` with a "Roadmap status model" subsection.
[ ] SP-044-5b: Cross-link from `docs/ONBOARDING.md` (SP-043 dependency) so a new contributor sees `roadmap/README.md` as the entry point.
[ ] SP-044-5c: Document the triage trigger conditions in `roadmap/README.md` header.

## Success Criteria

| Metric | Target |
|--------|--------|
| Spec files with a valid `Status:` value | 100% |
| `roadmap/README.md` status table | Exists, accurate |
| `Active` count | ≤ 3 (soft cap) |
| `Deferred` specs without a `Deferred-Reason` | 0 |
| `Superseded` specs without a `Superseded-By` | 0 |
| TODO.md entries for non-`Active`/`Proposed` specs visually distinct | Yes |
| `TRIAGE-LOG.md` records each triage pass | Yes |

## Files Reference

| File | Action |
|------|--------|
| `roadmap/README.md` | Modify: define taxonomy, add status table |
| `roadmap/TRIAGE-LOG.md` | Create: append-only triage decisions |
| `roadmap/SP-*.md` (all) | Modify: normalize `Status:` line; add `Deferred-Reason` / `Superseded-By` where applicable |
| `TODO.md` | Modify: prefix Deferred/Superseded sections; consider removing Superseded entries |
| `scripts/roadmap-status.sh` | Create (Track F, optional) |
| `CONTRIBUTING.md` | Modify: add "Roadmap status model" subsection |
| `docs/ONBOARDING.md` (when SP-043 lands) | Modify: link to `roadmap/README.md` |

## Risks

- **Triage is itself work.** Spending engineering time on roadmap meta could displace shipping. Mitigation: keep the triage cheap (header edit + log line); avoid rewriting specs.
- **Status becomes a vanity metric.** "We have 3 Active" sounds good but doesn't mean anything is shipping. Mitigation: SP-042's coverage-delta and PR cadence are the actual throughput metrics; status is for *prioritization clarity*, not progress reporting.
- **Premature Defer is information loss.** A Deferred spec might have been the right next thing. Mitigation: triage pass is reversible; re-promote freely.
- **Solo author doesn't need this.** True today; that's why priority is LOW. The value is in (a) inviting a successor, (b) reducing the author's own context-switch cost, (c) preventing rot. Mitigation: do enough to capture the current state, defer Tracks F/E if they feel like overhead.
- **`Active` cap encourages serial work over parallel exploration.** Sometimes the right pattern is to spike on three things in parallel. Mitigation: cap is soft; the goal is awareness, not enforcement.
