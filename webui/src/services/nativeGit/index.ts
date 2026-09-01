/**
 * Track R (R-4): manifest-driven native-git deferral gate.
 *
 * This leaf module is the runtime half of the Track R git seam. It ships in
 * BOTH the default build and the `--native-git` build. It is deliberately NOT
 * under `nativeGitStubs/` and is NOT aliased by `nativeGitStubAliases` (see
 * webui/vite.config.ts — those regexes only rewrite imports of exactly
 * `services/api/gitApi`), so it is importable from both the `--native-git`
 * stub and the default-build app, and lands in both bundles.
 *
 * In the default build the compile-time constant `NATIVE_GIT_ENABLED` is
 * `false`, so every gate here short-circuits into a dead branch and the module
 * is inert — default behavior stays byte-identical.
 *
 * This leaf is SMALL on purpose: the git channel (the client API surface +
 * boot wiring) has no path normalization and no bridge-result → Response
 * mapping (unlike the FS leaf), so it carries only the narrow bridge type +
 * detector, the PURE gate decision, and the cached async resolver. It is the
 * future R-4 ratification runtime surface (the shell-side deferral decision
 * for git operations) — resolved-but-unused until ratification, imported by
 * nothing yet except tests.
 *
 * Note on the git seam: unlike the chat seam (a single transport module), the
 * git seam is the client-API surface (`services/api/gitApi`) + the
 * compile-time short-circuits of the boot wiring (`useAppInitialization`).
 * The deeper `gitClient` / `browserGit` modules are NOT aliased (see
 * `nativeGitStubs/gitApi.ts` header + the ADR git-seam subsection for the
 * VFS / IndexedDB-namespace rationale), so this gate's reason names are
 * `git-not-declared` / `git-not-ratified` (mirroring the chat gate's
 * `chat-not-declared` / `chat-not-ratified` shape).
 *
 * Leaf: no new dependencies. It imports the compile-time flag constant from
 * the `nativeGitStubs/` stand-in (a real module present in every build;
 * `nativeGitFlag` is itself NOT one of the aliased stub names) and the
 * shared `SproutStudioCapabilities` shape from the FS leaf (type-only —
 * erased at compile time, so it pulls no real code into the bundle).
 */

import type { SproutStudioCapabilities } from '../nativeFs';
import { NATIVE_GIT_ENABLED } from '../nativeGitStubs/nativeGitFlag';

// Re-export the shared capabilities shape so a git consumer can import it
// from this leaf without reaching into the FS module directly (type-only).
export type { SproutStudioCapabilities } from '../nativeFs';

// ── Narrow structural type for the SproutStudio git bridge ──────────────────
//
// The git channel's capabilities map already carries a `git` entry, so a
// `getCapabilities`-only narrow bridge type is sufficient (the FS leaf's
// four-method surface is not needed here).

/**
 * The narrow `window.SproutStudio` subset the webui uses for git deferral:
 * just `getCapabilities()`, whose response carries the `git` capability
 * flag and the `excluded[]` manifest entries.
 */
export type SproutStudioGitBridge = {
  getCapabilities(): Promise<SproutStudioCapabilities>;
};

// ── Structural detector ───────────────────────────────────────────────────────

/**
 * Type guard: is `obj` a usable SproutStudio git bridge? Checks that
 * `getCapabilities` is a function. Pure and synchronous; safe to call with
 * `null`, `undefined`, or a plain object.
 */
export function hasSproutStudioGitBridge(obj: unknown): obj is SproutStudioGitBridge {
  if (!obj || typeof obj !== 'object') return false;
  const c = obj as Record<string, unknown>;
  return typeof c.getCapabilities === 'function';
}

/**
 * Detect the shell-injected bridge on `window.SproutStudio`. Returns `null`
 * when there is no usable git bridge (no `window`, no `SproutStudio`, or a
 * missing `getCapabilities`). Never throws.
 */
export function detectSproutStudioGit(): SproutStudioGitBridge | null {
  if (typeof window === 'undefined') return null;
  const candidate = (window as unknown as { SproutStudio?: unknown }).SproutStudio;
  return hasSproutStudioGitBridge(candidate) ? (candidate as SproutStudioGitBridge) : null;
}

// ── Gate decision (pure) + cached resolver ────────────────────────────────────

export interface NativeGitGateDecision {
  /** True only when git operations route to the shell. */
  active: boolean;
  /** Human-readable reason for the decision (for logging/tests). */
  reason: string;
}

/**
 * PURE gate decision. Git deferral is active IFF, in precedence order:
 *   1. `nativeGitEnabled` is true (the compile-time `NATIVE_GIT_ENABLED`), AND
 *   2. `bridge` is a usable SproutStudio git bridge, AND
 *   3. `capabilitiesResponse.capabilities.git === true`, AND
 *   4. `capabilitiesResponse.excluded[]` contains `{ portion: 'git',
 *      status: 'ratified' }`.
 *
 * Any failing step returns `{ active: false, reason: <step> }`. Malformed
 * capabilities (non-object / missing) are a gate-fail, never a throw.
 */
export function resolveNativeGitGate(
  nativeGitEnabled: boolean,
  bridge: unknown,
  capabilitiesResponse: unknown,
): NativeGitGateDecision {
  if (!nativeGitEnabled) {
    return { active: false, reason: 'native-git-disabled' };
  }
  if (!hasSproutStudioGitBridge(bridge)) {
    return { active: false, reason: 'no-bridge' };
  }
  const caps = capabilitiesResponse as SproutStudioCapabilities | null | undefined;
  if (!caps || typeof caps !== 'object') {
    return { active: false, reason: 'malformed-capabilities' };
  }
  if (caps.capabilities?.git !== true) {
    return { active: false, reason: 'git-not-declared' };
  }
  const ratified =
    Array.isArray(caps.excluded) && caps.excluded.some((e) => e && e.portion === 'git' && e.status === 'ratified');
  if (!ratified) {
    return { active: false, reason: 'git-not-ratified' };
  }
  return { active: true, reason: 'active' };
}

let gatePromise: Promise<NativeGitGateDecision> | null = null;

/**
 * Cached async resolver: calls `bridge.getCapabilities()` ONCE for the app's
 * lifetime and returns the decision. Never re-fetches and never throws (all
 * failures resolve to a `{ active: false, reason }` decision).
 *
 * In the default build (`NATIVE_GIT_ENABLED === false`) this
 * short-circuits BEFORE ever touching `window.SproutStudio` — a dead branch.
 */
export function nativeGitGate(): Promise<NativeGitGateDecision> {
  if (gatePromise) return gatePromise;
  gatePromise = (async () => {
    // Compile-time short-circuit: the default build never reaches here.
    if (!NATIVE_GIT_ENABLED) {
      return { active: false, reason: 'native-git-disabled' } as NativeGitGateDecision;
    }
    try {
      const bridge = detectSproutStudioGit();
      if (!bridge) {
        return { active: false, reason: 'no-bridge' };
      }
      let caps: unknown;
      try {
        caps = await bridge.getCapabilities();
      } catch {
        // getCapabilities rejected → gate-fail (throw as today), never crash.
        return { active: false, reason: 'getCapabilities-rejected' };
      }
      return resolveNativeGitGate(NATIVE_GIT_ENABLED, bridge, caps);
    } catch {
      return { active: false, reason: 'unexpected-error' };
    }
  })();
  return gatePromise;
}

// For tests / diagnostics: reset the cached promise (not part of the app path).
export function __resetNativeGitGateForTests(): void {
  gatePromise = null;
}
