/**
 * Track R (R-4): manifest-driven native-chat deferral gate.
 *
 * This leaf module is the runtime half of the Track R chat seam. It ships in
 * BOTH the default build and the `--native-chat` build. It is deliberately NOT
 * under `nativeChatStubs/` and is NOT aliased by `nativeChatStubAliases` (see
 * webui/vite.config.ts — those regexes only rewrite imports of exactly
 * `services/api/chatApi`), so it is importable from both the `--native-chat`
 * stub and the default-build app, and lands in both bundles.
 *
 * In the default build the compile-time constant `NATIVE_CHAT_ENABLED` is
 * `false`, so every gate here short-circuits into a dead branch and the module
 * is inert — default behavior stays byte-identical.
 *
 * This leaf is SMALL on purpose: the chat channel has no path
 * normalization and no bridge-result → Response mapping (unlike the FS
 * leaf), so it carries only the narrow bridge type + detector, the PURE gate
 * decision, and the cached async resolver. It is the future R-4 ratification
 * runtime surface (the shell-side deferral decision for chat sessions) —
 * resolved-but-unused until ratification, imported by nothing yet except
 * tests.
 *
 * Leaf: no new dependencies. It imports the compile-time flag constant from
 * the `nativeChatStubs/` stand-in (a real module present in every build;
 * `nativeChatFlag` is itself NOT one of the aliased stub names) and the
 * shared `SproutStudioCapabilities` shape from the FS leaf (type-only —
 * erased at compile time, so it pulls no real code into the bundle).
 */

import { NATIVE_CHAT_ENABLED } from '../nativeChatStubs/nativeChatFlag';
import type { SproutStudioCapabilities } from '../nativeFs';

// Re-export the shared capabilities shape so a chat consumer can import it
// from this leaf without reaching into the FS module directly (type-only).
export type { SproutStudioCapabilities } from '../nativeFs';

// ── Narrow structural type for the SproutStudio chat bridge ─────────────────
//
// The chat channel's capabilities map already carries a `chat` entry, so a
// `getCapabilities`-only narrow bridge type is sufficient (the FS leaf's
// four-method surface is not needed here).

/**
 * The narrow `window.SproutStudio` subset the webui uses for chat deferral:
 * just `getCapabilities()`, whose response carries the `chat` capability
 * flag and the `excluded[]` manifest entries.
 */
export type SproutStudioChatBridge = {
  getCapabilities(): Promise<SproutStudioCapabilities>;
};

// ── Structural detector ───────────────────────────────────────────────────────

/**
 * Type guard: is `obj` a usable SproutStudio chat bridge? Checks that
 * `getCapabilities` is a function. Pure and synchronous; safe to call with
 * `null`, `undefined`, or a plain object.
 */
export function hasSproutStudioChatBridge(obj: unknown): obj is SproutStudioChatBridge {
  if (!obj || typeof obj !== 'object') return false;
  const c = obj as Record<string, unknown>;
  return typeof c.getCapabilities === 'function';
}

/**
 * Detect the shell-injected bridge on `window.SproutStudio`. Returns `null`
 * when there is no usable chat bridge (no `window`, no `SproutStudio`, or a
 * missing `getCapabilities`). Never throws.
 */
export function detectSproutStudioChat(): SproutStudioChatBridge | null {
  if (typeof window === 'undefined') return null;
  const candidate = (window as unknown as { SproutStudio?: unknown }).SproutStudio;
  return hasSproutStudioChatBridge(candidate) ? (candidate as SproutStudioChatBridge) : null;
}

// ── Gate decision (pure) + cached resolver ────────────────────────────────────

export interface NativeChatGateDecision {
  /** True only when chat sessions route to the shell. */
  active: boolean;
  /** Human-readable reason for the decision (for logging/tests). */
  reason: string;
}

/**
 * PURE gate decision. Chat deferral is active IFF, in precedence order:
 *   1. `nativeChatEnabled` is true (the compile-time
 *      `NATIVE_CHAT_ENABLED`), AND
 *   2. `bridge` is a usable SproutStudio chat bridge, AND
 *   3. `capabilitiesResponse.capabilities.chat === true`, AND
 *   4. `capabilitiesResponse.excluded[]` contains `{ portion: 'chat',
 *      status: 'ratified' }`.
 *
 * Any failing step returns `{ active: false, reason: <step> }`. Malformed
 * capabilities (non-object / missing) are a gate-fail, never a throw.
 */
export function resolveNativeChatGate(
  nativeChatEnabled: boolean,
  bridge: unknown,
  capabilitiesResponse: unknown,
): NativeChatGateDecision {
  if (!nativeChatEnabled) {
    return { active: false, reason: 'native-chat-disabled' };
  }
  if (!hasSproutStudioChatBridge(bridge)) {
    return { active: false, reason: 'no-bridge' };
  }
  const caps = capabilitiesResponse as SproutStudioCapabilities | null | undefined;
  if (!caps || typeof caps !== 'object') {
    return { active: false, reason: 'malformed-capabilities' };
  }
  if (caps.capabilities?.chat !== true) {
    return { active: false, reason: 'chat-not-declared' };
  }
  const ratified =
    Array.isArray(caps.excluded) && caps.excluded.some((e) => e && e.portion === 'chat' && e.status === 'ratified');
  if (!ratified) {
    return { active: false, reason: 'chat-not-ratified' };
  }
  return { active: true, reason: 'active' };
}

let gatePromise: Promise<NativeChatGateDecision> | null = null;

/**
 * Cached async resolver: calls `bridge.getCapabilities()` ONCE for the app's
 * lifetime and returns the decision. Never re-fetches and never throws (all
 * failures resolve to a `{ active: false, reason }` decision).
 *
 * In the default build (`NATIVE_CHAT_ENABLED === false`) this
 * short-circuits BEFORE ever touching `window.SproutStudio` — a dead branch.
 */
export function nativeChatGate(): Promise<NativeChatGateDecision> {
  if (gatePromise) return gatePromise;
  gatePromise = (async () => {
    // Compile-time short-circuit: the default build never reaches here.
    if (!NATIVE_CHAT_ENABLED) {
      return { active: false, reason: 'native-chat-disabled' } as NativeChatGateDecision;
    }
    try {
      const bridge = detectSproutStudioChat();
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
      return resolveNativeChatGate(NATIVE_CHAT_ENABLED, bridge, caps);
    } catch {
      return { active: false, reason: 'unexpected-error' };
    }
  })();
  return gatePromise;
}

// For tests / diagnostics: reset the cached promise (not part of the app path).
export function __resetNativeChatGateForTests(): void {
  gatePromise = null;
}
