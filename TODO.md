# TODO

Active work tracked here. Each item is scoped for a single agent run
via the workflow automation (~1-4 hours of focused work). Only items that are
approved and ready to assign are listed.

---

## R-0: webui decoupling audit + feature-flag seams (Track R)

Context: Sprout Studio's Track R (`../sprout-studio/roadmap/Track-R-selective-replacement.md`,
ratified 2026-08-29) replaces webui portions with native iOS/Android
implementations one at a time, gated on full parity. This repo owns the
seams: build-time feature flags and a capability manifest so a shell can
serve a dist whose excluded portions are provided natively.

- [x] **R-0 — decoupling audit + build flags**: (1) audit and document the
      webui's swappable subsystems — file system/workspace ops, terminal,
      chat loop, git — with their module boundaries and call sites under
      `webui/src`; (2) add feature flags to `scripts/build-webui-dist.mjs`
      (start with `--native-fs`) that exclude a portion's module set from
      the dist and emit a capability manifest (JSON listing excluded
      portions); (3) the default no-flag build must remain functionally
      unchanged — no behavior change, same modules; (4) short ADR in `docs/`
      defining the flag set and manifest format (this is the contract
      sprout-studio's bridge handshake will consume). Exit: default dist
      build unchanged, `--native-fs` dist build succeeds with manifest
      emitted, webui test suite green, all committed.

---


## test-isolation: TestRunAgentWorkflowLoop suite hang — RESOLVED 2026-08-20

Original hypothesis (live git against the dev worktree) was disproven:
an instrumented full run left `git status`/HEAD/reflog byte-identical.
The actual root cause of the 10-minute suite hang was a hardcoded
`/tmp/test_sprout_retry_count` counter in the retry scenario — on hosts
where `/tmp` is not writable (Termux) the build step failed on every
invocation, the ScriptedClient exhausted its scripted responses and
returned nil, and the loop spun to MaxIterations until the `go test`
timeout killed the suite.

Shipped fixes:
- `pkg/workflow/run_loop_test.go`: counter now lives in `t.TempDir()`
  via a `__COUNTER_FILE__` placeholder substituted per-subtest. Suite
  runtime 422s-timeout-hang → ~5s.
- `pkg/workflow/git_leak_test.go`: `TestMain` guard snapshots
  `git status --porcelain` + `git rev-parse HEAD` before/after the
  package run and fails the run if either changed (regression guard
  for the original incident class; skip with `SPROUT_SKIP_GIT_LEAK_CHECK`).

---

## R-2w: webui native-FS deferral (Track R, second half of the first swap)

Context: Studio side is DONE 2026-08-30 (sprout-studio `484436e`:
WebUIServeGate enforces the ADR-0008 servability gate; `484436e`/`0e2256d`
also shipped the `bridge.capabilities` handshake). The contract is
`docs/adr-0008-webui-native-seams.md` + `docs/WEBUI_DECOUPLING_AUDIT.md`
(both in this repo) — consume as written, do not invent new formats.

- [ ] **R-2w — manifest-driven FS deferral**: when the dist's
      `capabilities.json` declares portion `fs` with `status: "ratified"`
      AND the runtime `bridge.capabilities` op confirms the shell provides
      `fs`, the webui's workspace FS operations (file tree open/browse/
      read/write/save — call sites per the decoupling audit) route through
      the existing bridge fs/workspace channel to the shell's native
      FilesHandler instead of the WASM FS modules. When the manifest is
      absent / `seam-only` / unratified, behavior is exactly today's
      (WASM FS path, byte-identical). Exit: default build unchanged and
      full webui suite green; a ratified-manifest build exercises the
      bridge deferral in component tests (mocked bridge); seam-only still
      throws via the stubs; ADR-0008 gains a short deferral-wiring
      addendum.

---


Status: ✅ All 5 phases shipped 2026-08-07 (P0 file-lock index writes,
P1 multi-workspace hardening, P2 lazy daemon auto-start, P3 embedding
service via Unix socket, P4 full CLI-on-daemon). See `git log --grep=SP-136`
for the per-phase commits; spec body deleted from `roadmap/` per index
policy.

The daemon deduplicates model loads, inference permits, and index writers
across CLI processes; the CLI routes embedding + one-shot agent work through
it with in-process fallback and `SPROUT_DAEMON=0` / `SPROUT_DAEMON_AGENT=0`
escape hatches. Follow-on workstream (open): `roadmap/SP-136-cross-platform-local-llm.md`.
