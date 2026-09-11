/**
 * Shell identity (studio vs plain webui) — the runtime oracle for
 * mobile-only UI decisions.
 *
 * Device-class heuristics (coarse pointer, viewport width) cannot
 * distinguish an iPad running the Sprout Studio native shell from an
 * iPad running Safari against the plain webui. The shell injects the
 * `window.SproutStudio` bridge; the webui never does. That structural
 * difference is the only truthful signal, so shell-scoped UI (top
 * safe-area padding, the Files cwd selector row) keys off it.
 *
 * Two tiers:
 *   - `isStudioShellSync()` — bridge presence only. Synchronous, safe at
 *     module load. Used to set the `data-shell` attribute before first
 *     paint so CSS starts correct (no layout flash).
 *   - `resolveShellIdentity()` — bridge presence PLUS the ratified
 *     capabilities handshake (`nativeFsGate`). Async; resolves once per
 *     boot and notifies subscribers. UI that must not render against a
 *     half-capable bridge waits for this.
 *
 * Leaf: only imports from the nativeFs leaf and its flag stub. In the
 * default build NATIVE_FS_ENABLED is `false`, so resolveShellIdentity
 * short-circuits to plain-webui without touching `window`.
 */

import { detectSproutStudio, nativeFsGate, type NativeFsGateDecision } from '../services/nativeFs';
import { NATIVE_FS_ENABLED } from '../services/nativeFsStubs/nativeFsFlag';

export type ShellIdentity = 'studio' | 'webui';

let resolvedIdentity: ShellIdentity | null = null;
const listeners = new Set<(identity: ShellIdentity) => void>();

/**
 * Synchronous shell check: does the host page carry the studio bridge?
 * True for a studio shell even before its capabilities handshake
 * completes; false everywhere else (plain webui, cloud, tests, SSR).
 *
 * NOTE: unlike resolveShellIdentity, this reads `window` in every build
 * (a single property lookup — harmless, and needed to paint the right
 * chrome before the async handshake lands).
 */
export function isStudioShellSync(): boolean {
  return detectSproutStudio() !== null;
}

/**
 * Apply the shell identity to <html> as `data-shell="studio|webui"` —
 * the single CSS seam (mirrors data-theme / data-ui-scale). Kept in
 * sync on every resolution; called at boot before React renders.
 */
export function applyShellAttribute(identity: ShellIdentity): void {
  if (typeof document === 'undefined') return;
  document.documentElement.setAttribute('data-shell', identity);
}

/**
 * Async boot resolution. In a studio dist (--native-fs) this waits for
 * the ratified capabilities handshake before declaring "studio" — a
 * bridge that fails the gate is NOT granted shell-scoped UI. In the
 * default build it short-circuits to "webui" WITHOUT touching `window`
 * (NATIVE_FS_ENABLED is a compile-time constant, dead branch).
 *
 * Resolves once per page load; later calls return the same identity.
 * Subscribers registered via onShellIdentityChange fire when the
 * resolved identity differs from a previous value (webui → studio
 * after the handshake; the reverse cannot happen).
 */
export async function resolveShellIdentity(): Promise<ShellIdentity> {
  if (resolvedIdentity) return resolvedIdentity;

  let identity: ShellIdentity = 'webui';
  if (NATIVE_FS_ENABLED && isStudioShellSync()) {
    const gate: NativeFsGateDecision = await nativeFsGate();
    if (gate.active) identity = 'studio';
  }
  return publish(identity);
}

function publish(identity: ShellIdentity): ShellIdentity {
  const previous = resolvedIdentity;
  resolvedIdentity = identity;
  applyShellAttribute(identity);
  if (previous !== identity) {
    for (const notify of listeners) notify(identity);
  }
  return identity;
}

/**
 * Subscribe to identity changes. Returns an unsubscribe function.
 * Fires only on transitions (never for the initial value — callers
 * that need the current value read getShellIdentity()).
 */
export function onShellIdentityChange(notify: (identity: ShellIdentity) => void): () => void {
  listeners.add(notify);
  return () => {
    listeners.delete(notify);
  };
}

/** Current resolved identity, or null before resolveShellIdentity ran. */
export function getShellIdentity(): ShellIdentity | null {
  return resolvedIdentity;
}

/** Test-only: forget the resolved identity and clear subscribers. */
export function __resetShellIdentityForTests(): void {
  resolvedIdentity = null;
  listeners.clear();
}

/**
 * Test-only: force-publish an identity as if the boot resolver had
 * produced it (sets the attribute, fires subscribers). Lets component
 * tests exercise both sides of the seam without mocking the bridge.
 */
export function publishShellIdentityForTests(identity: ShellIdentity): void {
  publish(identity);
}
