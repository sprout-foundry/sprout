# Completed Specs

Specs whose core work has shipped. Kept for historical context — the
implementation lives in the codebase; the spec captures *why* it was
built that way.

## Layout

Each file follows the standard spec template:

- `**Status:**` header — what shipped (Implemented / Shipped / Partially
  Implemented / Active / etc.)
- `## 1. Executive Summary` — one-paragraph framing
- `## ...` — design + phases
- Cross-references to related specs (e.g. SP-016b continues work from
  SP-016; SP-082 supersedes SP-066's key-order concept)

## Conventions

- **Decision docs** (e.g. `SP-039-DECISION.md`) record the choice; the
  code that implements it lives in another spec.
- **Sub-specs** (e.g. `SP-063-4g-panic-key.md`, `SP-087-acceptance.md`)
  are supporting documents; their parent spec explains why they exist.
- **Review docs** (e.g. `SP-059-6a-review.md`) document the audit of a
  shipped feature; not spec proposals.

## Adding a new completed spec

1. Finish the work and land the commits.
2. Update the spec file's `**Status:**` line.
3. `git mv roadmap/SP-XXX-...md roadmap/_completed/`.
4. Re-run the index generator (`/tmp/build_index.py` is in this
   session's scratch; for a permanent helper, see
   `scripts/regen-roadmap-index.py` if/when added).
5. The 00-INDEX.md will pick it up automatically.
