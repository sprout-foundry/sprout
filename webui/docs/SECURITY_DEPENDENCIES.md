# Web UI Dependency Security Notes

Last reviewed: 2026-06-18

## Summary

The webui test/build toolchain carries advisories on devDependencies that are
**not exploitable** in our actual usage. They are listed below with the
reasoning for accepting them. None of these packages ship to end users; they
run only in local dev and CI.

## Accepted advisories

### vitest `2.1.9` — GHSA-5xrq-8626-4rwp (CVSS 9.8, critical)

**Advisory**: When the Vitest UI server is listening, arbitrary files can be
read and arbitrary code executed via CSWSH (cross-site WebSocket hijacking).

**Why accepted**:
- The vulnerable code path is `vitest --ui`, which starts an HTTP/WS API
  server. This project never runs `--ui` — the test scripts use
  `vitest` / `vitest run` / `vitest run --coverage`, which do not start the
  API server.
- vitest is a devDependency (not shipped to users).
- The identical advisory was patched in the `2.1.x` line at **2.1.9**
  (our version) for CVE-2025-24964 / CVE-2025-24963. The Dependabot alert
  remains open because GHSA-5xrq-8626-4rwp's `vulnerable_version_range`
  (`<=3.2.5`) technically includes the 2.x line, but the relevant fix commit
  landed before 2.1.9.

**Why not upgrade**:
- Upgrading to `vitest@3.x` was attempted and reverted. vitest 3.2.6 causes a
  JavaScript heap OOM when running the 154-file test suite (5315 tests). This
  is a confirmed vitest v3 regression: the main process leaks ~700MB/5s of
  heap, independent of pool (`threads`/`forks`), isolation mode, or worker
  count. See research notes below.
- vitest 4.1.9 (audit's recommended fix) sits on the same rewritten pool
  architecture and is expected to carry the same regression.

### @vitest/coverage-v8 `2.1.9` — inherits vitest

Same reasoning as above; the advisory flows transitively from `vitest`.

### vite `5.4.21` (transitive via `vite-node`, part of vitest 2.1.9)

**Advisories**: Path traversal in optimized deps `.map` handling (GHSA-4w7w-66w2-5vf9),
`launch-editor` NTLMv2 hash disclosure on Windows (GHSA-v6wh-96g9-6wx3),
`server.fs.deny` bypass on Windows alternate paths (GHSA-fx2h-pf6j-xcff).

**Why accepted**:
- The vulnerable `vite@5.4.21` is pulled in by `vite-node` (a vitest internal)
  and is used **only during test execution**, never for `vite dev` / production builds.
- Our top-level `vite@6.4.3` (used for `vite dev` and `vite build`) is patched.
- The advisories are specific to the dev server / Windows environments, neither
  of which apply to the vitest test runner.

### esbuild `0.21.5` (transitive via vite-node → vite@5)

**Advisory**: GHSA-67mh-4wv8-2f99 — dev server request forwarding.

**Why accepted**: Same reasoning as vite@5.4.21 — used only by the test
runner's internal vite-node, not the dev server or build pipeline. Our
production esbuild (`0.25.12` via `vite@6.4.3`) is patched.

### jest / babel-jest / @jest/* (21 moderate, transitive via Storybook)

**Why accepted**: Storybook bundles jest for its test-runner integration.
The only fix is `npm audit fix --force`, which downgrades Storybook to 7.0.6
(a breaking major downgrade). These run only during Storybook preview, never
in production or CI test runs.

## Research notes: vitest v3 OOM

Reproduction (vitest 3.2.6, webui, `src/components/` suite, 154 files / 5315 tests):

| Config | Result |
|--------|--------|
| default (threads, isolate=true) | OOM at ~4GB after ~40s |
| `--pool=forks` | OOM at ~4GB after ~45s |
| `--pool=forks --poolOptions.forks.singleFork=true` | OOM |
| `--fileParallelism=false` | OOM (slower growth, still unbounded) |
| `--isolate=false` | OOM (faster — ~8GB) |
| `--shard=1/2` (65 files) | OOM at ~4GB |
| `NODE_OPTIONS=--max-old-space-size=8192` | OOM at ~8GB |
| `NODE_OPTIONS=--max-old-space-size=16384` | OOM at ~16GB |

Memory profiling showed the **main vitest process** growing ~700MB/5s while
worker processes stayed small (~70MB). The leak is unbounded and unrelated to
parallelism or isolation. vitest v2.1.9 runs the same suite cleanly (5267
passed, OOM only during worker teardown after completion).

Reference: [vitest-dev/vitest#9560](https://github.com/vitest-dev/vitest/issues/9560)

## When to revisit

- vitest publishes a release that addresses the main-process leak.
- The project adopts vitest browser mode or the `vitest --ui` workflow
  (at which point the critical advisory becomes real).
- A new advisory affects vitest's test-execution path rather than the API server.
