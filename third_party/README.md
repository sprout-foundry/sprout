# third_party/

Vendored copies of upstream modules that this repository needs patched.
Each entry is a full copy of the upstream module plus a local patch.

## seed (github.com/sprout-foundry/seed)

Patched from upstream v1.9.0. The local patch pins the run's opening brief
(the first user message of a session) verbatim through every compaction
path — substitution, checkpoint compaction, the LLM summarizer,
continuation compaction and every pruner strategy. Until the patch ships
in a seed release and this directory can be deleted, do not remove it:
any `replace github.com/sprout-foundry/seed => ./third_party/seed` in
`go.mod` must stay.

Patch details (files touched):

- `core/structural_compaction.go` — `spliceSummary` no longer demotes the
  anchor's opening user message to a bounded excerpt; the brief passes
  through verbatim (the anchored assistant reply stays demoted).
- `core/pruner.go` — the sliding-window strategy keeps the opening anchor
  span instead of only the system message; the importance-based strategies
  (including the strict tool-call-aware variant) mark the anchor span keep;
  `fallbackKeepSet` keeps the anchor span; `ensureRequiredHeadroom` drops
  whole oldest turns outside the anchor and the recent window instead of
  always dropping index 1.

Tests: `core/brief_preservation_test.go` pins the brief through the four
paths (substitution, LLM summarizer, pruner, continuation compaction).

Upstream is unvendored (no `vendor/` directory at the repo root); this
copy is the local `replace` target in go.mod.
