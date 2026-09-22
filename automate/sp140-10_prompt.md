# SP-140-10: Design-Mode Chat UX — Autonomous Implementation

You are an autonomous Coordinator agent implementing SP-140-10 (design-mode
chat UX) in the `feat-design-workspace` worktree. Your CWD is the workspace
root (`/Users/alanp/dev/sprout-foundry/`). ALL work — edits, builds, tests,
commits — happens inside the worktree directory `feat-design-workspace/`
(a worktree of the sprout repo, branch `feat-design-workspace`). Never
touch the main `sprout/` checkout or any other repo.

**Read `feat-design-workspace/roadmap/SP-140-10-chat-ux.md` first — it is
the spec.** Items SP140-10a → 10e, in order; each item is implemented,
tested, reviewed, and committed before the next starts. The spec's
"Current-state pointers" section names the exact files and line pointers —
trust them, but verify against the code (the tree has moved since the
pointers were written).

**Do not stop until all five items are complete or you hit a genuinely
unrecoverable error. Budget warnings are not stop conditions.**

## Per-item workflow

1. Read the item in the spec (mechanism + acceptance criteria).
2. Implement it yourself, or delegate to the `coder` subagent with the
   spec item text + file pointers. Verify the result by evidence (build,
   tests, diff) — the subagent's `files_modified` manifest is the
   authoritative record of what it touched.
3. Prove it: targeted vitest (rules below) + `cd feat-design-workspace && make build`.
4. Review:
   - 10a, 10b: self-review of the diff is fine.
   - 10c, 10d: delegate to the `reviewer` subagent — 10c touches
     persistence + boot-restore (state/compatibility risk), 10d changes
     the shared `packages/ui` chat contract (cross-mode regression risk).
   Fix MUST_FIX findings before committing.
5. Commit via the `commit` tool with `repo_dir: feat-design-workspace`:
   stage the specific item files (never `git add .` or `git add -A`),
   and include the spec's checkbox edit for that item (flip
   `- [ ] SP140-10x` to `- [x]`) in the same commit. Subject:
   `feat(webui): SP-140-10a — <title>` (10d may be
   `feat(webui+packages/ui): ...`); body = what changed + test evidence
   (commands + outcomes).
6. NEVER push.

## Test safety (hard rules)

1. **Vitest is bounded only**: `cd feat-design-workspace/webui &&
   npx vitest run <specific files>` — never watch mode, never a bare
   full-suite `vitest run`, never chained with a go build in the same
   window. For packages/ui: `cd feat-design-workspace/packages/ui &&
   npx vitest run <specific files>` (plus `npm run type-check` when
   types change).
2. **Go**: this work is webui + packages/ui only. No `go test` is
   expected. If a Go file somehow changes, do NOT run bare
   `go test ./...` — use the repo Makefile gate with bounded
   `-p`/`-parallel`. One go command at a time, never overlapping a
   build.
3. `make build` (the loop's build gate) is the full pipeline
   (React + WASM + Go binary). Run it serially, one at a time.

## Conventions

- House commit style: item-scoped subject, evidence in the body, plain
  text (the commit tool is shell-safe, but prefer plain text).
- CSS: no raw hex (the no-raw-hex house rule) — tokens only.
- 500-line-per-file rule: keep `ToolDetailInline` and any ChatView
  growth under the limit (extract if not).
- 10e's dogfood checklist: do the mechanical parts (build, targeted
  tests, status flip, index/umbrella row updates). The manual-browser
  lines are marked "awaiting manual verification" in the spec — do not
  claim them.
- When the gate reports "ALL SP140-10 ITEMS COMPLETE", stop: final
  summary with the item commit hashes, test evidence, and any
  punt/deferred line.
