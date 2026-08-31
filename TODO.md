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

## R-4: chat + git webui seams (the last two reserved portions)

Mirror the proven R-3 template exactly, for both remaining portions in
one pass:

- **Chat**: `--native-chat` on `scripts/build-webui-dist.mjs` (sets
  `VITE_SPROUT_NATIVE_CHAT=1`), stub-alias the webui chat transport
  modules (per the R-0 audit's chat section: fetch/SSE agent-turn client
  paths) behind `nativeChatStubs/`, compile-time short-circuits so the
  webui chat transport never initializes, manifest portion `"chat"`
  (`seam-only` default), `--ratify-chat` (requires base flag), runtime
  gate leaf `services/nativeChat/` (both builds, inert by default)
  mirroring `services/nativeTerminal/`.
- **Git**: `--native-git` / `--ratify-git` / `nativeGitStubs/` / portion
  `"git"` / `services/nativeGit/` leaf — same shape, for the audit's git
  modules (status/log/diff/commit client paths).
- Flags additive with each other and with `--native-fs` /
  `--native-terminal`; every `--ratify-*` requires its base; all
  prohibited with `--components`. ADR-0008 flag table + seam subsections;
  audit §2.1 rows; fail-fast validation updated.
- Tests mirroring the native-terminal suites: flag matrix, stub no-op,
  boot short-circuits both states, manifest entries. Default build
  byte-identical; full webui suite green.
- Evidence per R-3: real dist builds, absent-module marker counts,
  fail-fast exits verified. Device verification stays batched (see
  sprout-studio TODO R-4s).

---


## R-3: native terminal swap

Status: **SURFACE COMPLETE 2026-08-31** — the live console pane replaced
the placeholder in sprout main (`843bb7cc8`, gate-gated on ratified
terminal + bridge capability; shells-fetch/toast suppressed in native
mode). Studio-side engine: `EmulatorTerminalTransport` (ShellEmulator
wired behind the §15 channel) on sprout-studio branch
`r3s-emulator-terminal` (`9b6cdb6`, 781 tests / 0 failures), merges to
studio main after the in-flight chat/git item lands. Device-verified on
iPad (console renders, commands run) 2026-08-31. Interactive PTY
sessions remain a documented follow-up (one-shot command semantics).

Previous status: **ACTIVE — unblocked by user 2026-08-30 21:07** ("don't
block on waiting for a battery recharge"). Device verification for this
item and R-2's ratification remain batched into one iPad session when
the device is recharged; all development proceeds now.
Contract: ADR-0008 reserves `--native-terminal` (fail-fast today); the
`--native-fs`/R-2w/R-2f machinery (flag scaffolding, servability gate,
capability handshake, wasm-free boot) is the proven template — mirror it
for the terminal portion: shell provides the terminal channel natively,
webui renders the placeholder ("provided by native shell" — already
shipping since R-2f) as the real UI, WASM terminal chain excluded at
build time. Exit mirrors R-2: seam-only dist refuses to serve, ratified
dist defers, default byte-identical, suites green, device-verified
(jointly with R-2's checklist on the same charge).

- [x] **R-3 — native terminal swap (sprout-side seam)**: implement
      `--native-terminal` on `scripts/build-webui-dist.mjs` (mirroring
      `--native-fs`): sets `VITE_SPROUT_NATIVE_TERMINAL=1`, excludes the
      webui terminal module set (`services/terminalWebSocket` via
      `nativeTerminalStubs/` aliases in `webui/vite.config.ts`; compile-time
      short-circuits in `useTerminalSession` and `useWasmTerminalInput`
      so the PTY WS and WASM terminal tier never initialize), emits the
      `terminal` portion in `capabilities.json` (`seam-only` default), and
      adds `--ratify-terminal` (requires `--native-terminal`; emits
      `status: "ratified"`). Add the runtime-gate leaf
      `services/nativeTerminal/` (both builds, inert by default) mirroring
      `services/nativeFs/`. Prohibit `--native-terminal` + `--components`.
      Update ADR-0008 (flag table + terminal seam subsection) and the
      decoupling audit §2.1 table. Tests: buildFlags (flag + manifest +
      ratify + additive-with-fs), native-terminal gate/stub tests, boot
      short-circuit tests both flag states. Default build byte-identical;
      `--native-terminal` dist builds with manifest; full webui suite
      green. Device verification stays batched with R-2 (out of scope
      here). **Evidence 2026-08-30 (main @ d245c5595):** seam shipped,
      reviewer-verified (all 8 invariants hold, no MUST_FIX). Build script:
      `--native-terminal` implemented (env var + terminal manifest entry,
      seam-only), `--ratify-terminal` (requires base flag, emits ratified),
      `--components` prohibition, additive with `--native-fs`; chat/git stay
      reserved. Vite: `nativeTerminalStubAliases` swap `services/
      terminalWebSocket` → `nativeTerminalStubs/` no-op stand-in (never opens
      a WS). Short-circuits (dead branch flag-off): `useTerminalSession` (+
      `terminalProvidedByShell`), `usePageVisibility`, TerminalPane renders
      the "provided by the native shell" placeholder once for either shell
      bit. Runtime-gate leaf `services/nativeTerminal/` mirrors `nativeFs`
      (inert until ratification). ADR-0008 flag table + "Terminal seam
      (R-3)" subsection; audit §2.1 updated. Verified: tsc clean; full
      webui suite 6042 passed / 0 failed (240 files, 24 batches ≤10,
      VITEST_MAX_WORKERS=2); real dist builds — `--native-terminal` cloud
      dist emits capabilities.json with terminal/seam-only and the real
      terminalWebSocket module genuinely absent (marker 3→0); default dist
      has NO capabilities.json and the module present; fail-fast exits
      verified. Device verification (joint with R-2 checklist) remains
      batched on the iPad recharge, as filed.

---


Context: 2026-08-30 end-to-end test — the studio iOS shell serving a
`--native-fs --ratify-fs` cloud dist shows the webui error screen
"Failed to load browser runtime … WebAssembly". The R-2w deferral gate
covers FS *operations*, but the boot path still unconditionally
instantiates the WASM command runtime, whose artifacts the ratified dist
excludes by design (`--native-fs` drops the 26 MB wasm chain). The dist
is thus correct per ADR-0008; the boot sequence is the gap.

- [x] **R-2f — conditional WASM boot**: when `NATIVE_FS_ENABLED` (the
      compile-time `--native-fs` flag), the webui must boot WITHOUT
      instantiating any excluded WASM module: no wasmShell fetch/instantiate
      (and no ONNX/embedding chain), chat/API flows over their normal HTTP
      channels, FS ops via the R-2w bridge deferral, and any UI surface
      that requires the local runtime (e.g. local terminal tab) renders a
      clear "provided by shell" placeholder instead of a boot failure.
      Default build unchanged (flag false → today's boot, byte-identical).
      Exit: a browser-harness test boots the ratified `--native-fs` cloud
      dist with a mocked bridge and renders the workspace error-free; the
      default dist still boots exactly as today; ADR-0008 "Deferral
      wiring" section gains a boot-sequence subsection.

---


Context: Studio side is DONE 2026-08-30 (sprout-studio `484436e`:
WebUIServeGate enforces the ADR-0008 servability gate; `484436e`/`0e2256d`
also shipped the `bridge.capabilities` handshake). The contract is
`docs/adr-0008-webui-native-seams.md` + `docs/WEBUI_DECOUPLING_AUDIT.md`
(both in this repo) — consume as written, do not invent new formats.

- [x] **R-2w — manifest-driven FS deferral**: when the dist's
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
