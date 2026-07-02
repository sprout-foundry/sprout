# SP-016b: Expanded Embedding Index — Full Workspace Semantic Search

**Status:** ✅ Shipped (backend complete 2026-06; minor UI gap)

The original embedding index only stored code-unit-level vectors
(functions, methods, classes). Whole-file content (which often has the
"what is this repo about" semantic signal that code units miss) wasn't
indexed. SP-016b added a `pkg/embedding/extractor_file.go` that produces
file-level records, merged them into the query API alongside code-unit
results, and added a `detectDuplicateClusters()` pass that flags
semantically-near-duplicate files. Trivial functions are filtered
(`pkg/embedding/check.go`) so the index doesn't fill with false-positive
hits. The WebUI `SearchView.tsx` has CSS for rendering
`duplicate_clusters` hints but the TSX wiring is incomplete.

## Key decisions

- **Two extractors, one index.** File-level and code-unit records coexist
  in the same index with `type: "file"` vs `type: "code_unit"`. One
  query path merges and ranks them.
- **Duplicate cluster detection at index-build time**, not query time.
  The clusters change rarely; recomputing per query is wasteful.
- **Trivial function filter** (lines < N or no real logic) before
  embedding to reduce false positives.
- **Type-aware ranking**: file-level records weight higher for
  repository-level queries, code-unit for "find this function" queries.
- **Open WebUI gap**: `SearchView.tsx` should render `duplicate_clusters`
  hints in results. CSS exists; TSX wiring deferred.

## Artifacts

- code: `pkg/embedding/extractor_file.go` — file-level extractor
- code: `pkg/embedding/index.go` — `BuildIndex` includes file records
- code: `pkg/webui/search_semantic_api.go` — merged query response
- code: `pkg/embedding/check.go` — trivial function filter
- tests: `pkg/embedding/extractor_file_test.go`
- known gap: `SearchView.tsx` `duplicate_clusters` rendering (CSS ready)

Full specification archived — see git history for original content.