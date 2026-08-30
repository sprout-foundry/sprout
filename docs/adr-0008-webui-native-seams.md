# ADR-0008: WebUI native seams (Track R selective replacement)

## Status
Accepted (2026-08-29).

## Context
The webui is the default product interface (Track P). Native implementations
are reaching implementation parity with portions of it (file system, terminal,
chat loop, git). We need a *mechanism* — not a side-by-side dual UI — for the
shell to take over a portion of the webui once it reaches **full
implementation parity**, so the product only ever improves and can roll back
instantly.

The seam inventory and parity-gated swap protocol are defined in
`sprout-studio/roadmap/Track-R-selective-replacement.md`. This ADR records the
concrete contract that contract depends on: the build-flag set, the
capability-manifest format, and the runtime handshake. The full code-grounded
rationale lives in [WEBUI_DECOUPLING_AUDIT.md](./WEBUI_DECOUPLING_AUDIT.md).

Two seam layers, used together:
- **Build-time (hard swap):** `scripts/build-webui-dist.mjs` feature flags
  physically exclude a portion's webui modules from the bundle and emit a
  capability manifest.
- **Runtime handshake (soft detect):** the bridge bootstrap carries a
  capabilities map; the webui feature-detects and defers to native when the
  shell declares a capability. Missing capability = the webui implementation,
  exactly as today.

## Decision

### Flag set

Implemented and reserved flags on `scripts/build-webui-dist.mjs`
(`parseArgs` / `validateArgs`):

| Flag | Status | Portion | Effect |
|---|---|---|---|
| `--native-fs` | **Implemented** | `fs` | Sets `VITE_SPROUT_NATIVE_FS=1` (enables the Vite `nativeFsStubAliases`) and emits `capabilities.json` with `fs` excluded. |
| `--native-terminal` | **Reserved** | `terminal` | Fails fast (exit 1) before any build step: "reserved for future Track R work (R-3)". |
| `--native-chat` | **Reserved** | `chat` | Fails fast (exit 1): "reserved for future Track R work (R-4)". |
| `--native-git` | **Reserved** | `git` | Fails fast (exit 1): "reserved for future Track R work (R-5)". |

Semantics:
- Any reserved `--native-*` flag, any unknown `--*` token, an invalid
  `--mode`, or `--native-fs` + `--components` together → **exit 1 before any
  build step** (no `npm ci`, no Vite run).
- A portion's flag only excludes that portion; flags are additive and each is
  gated on its own parity audit (roadmap: one portion at a time).

### Manifest format (`capabilities.json`)

Emitted to the dist output dir **only when at least one `--native-*` flag
excludes a portion**; a default build emits none (absence = nothing excluded).
Core schema (full field descriptions in the audit doc §2.2):

```json
{
  "schemaVersion": 1,
  "generatedAt": "<ISO-8601 UTC>",
  "buildMode": "cloud | local",
  "excluded": [
    {
      "portion": "fs",            // fs | terminal | chat | git | …
      "flag": "--native-fs",
      "replacedBy": "native",
      "hardExclusion": true,     // build-time: module physically stubbed from the bundle
      "status": "seam-only",     // servability: seam-only = do not serve yet; "ratified" = parity-proven swap
      "notes": "<what was excluded, where it's documented>"
    }
  ]
}
```

`excluded` is an empty array (and the file is not written) for a default
build. `schemaVersion` is bumped on any shape change.

**`hardExclusion` gates *module exclusion* (build-time); `status` gates
*servability*.** A `status: "seam-only"` entry means the dist is a build-time
artifact only: the shell MUST NOT serve a dist containing a `seam-only`
exclusion (a `--native-fs` dist served today would throw on mount, since no
shell provides the shell interface natively yet). Only once the R-2 parity
gate ratifies the swap does the entry carry `status: "ratified"`, making the
dist a parity-proven, shell-servable swap.

### Handshake contract (what sprout-studio consumes)

sprout-studio **reads `capabilities.json` from the served dist** and gates
which portions it serves natively:

1. For each entry in `excluded`, the shell provides the named `portion`
   natively and the webui's hard-excluded modules are no-ops/stubs.
2. For any portion **not** in `excluded`, the webui runs its own
   implementation — identical to today's behavior. The shell serves only the
   dist whose module set matches the capabilities it actually provides.
3. The bridge bootstrap (R-1) additionally carries a runtime capabilities map
   so the same defer-to-native logic works on shells that do not ship a
   per-build manifest. The webui is the source of truth for fallback; the
   bridge is the only bridge.

### Rollback story

Rollback = **rebuild the dist with the flag omitted.** The default build
(omitting every `--native-*` flag) is unchanged from before Track R — no
aliases active, no `capabilities.json`, byte-identical module set. Flipping
the flag off restores the portion in a single rebuild; no source changes, no
migrations.

### Invariants

1. **Default build unchanged.** Omitting all `--native-*` flags produces a
   dist byte-identical in module set to the pre-Track-R default (no aliases,
   no manifest). A swap never regresses the default.
2. **The manifest reflects the actual exclusion.** Every `excluded[]` entry
   with `hardExclusion: true` must correspond to a module that is genuinely
   removed/stubbed from the bundle — never to a portion the shell *expects*
   but that is still present. `hardExclusion` is a build-time honesty
   guarantee about the module set, not a wish — and not a servability
   claim: `status: "seam-only"` entries mark dists the shell must not serve
   until the parity gate ratifies them (`status: "ratified"`).

## Consequences
- Future swaps (R-3 terminal, R-4 chat, R-5 git) add a flag + a `portion`
  value + a parity audit; the manifest shape is stable.
- Reviewers reject a swap that (a) changes the default build, or (b) marks a
  portion `hardExclusion` without the module actually being excluded.
- Each swap ratifies only after: full test suites green on both platforms, a
  device check, and a tested rollback (rebuild with the flag off).
- The webui remains the fallback source of truth; the shell never ships a
  module set that omits a portion it does not provide.

## References
- `sprout-studio/roadmap/Track-R-selective-replacement.md` — swap protocol & queue.
- [docs/WEBUI_DECOUPLING_AUDIT.md](./WEBUI_DECOUPLING_AUDIT.md) — subsystem
  inventory, full manifest schema, extraction order, FS residual coupling.
- [docs/DIST_BUNDLE_LAYOUT.md](./DIST_BUNDLE_LAYOUT.md) — canonical dist layout
  (`capabilities.json` is an optional file in the verified layout).
- [ADR-0007](./adr-0007-locking-strategy.md) — house ADR format.