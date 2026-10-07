import {
  PLATFORM_ACCOUNT_ITEMS,
  PLATFORM_WORK_ITEMS,
  intentPath as platformIntentPath,
  platformEntitlements,
} from './platform';
import type { HostEntitlements, SproutHost } from './types';

/**
 * The live platform entitlements: Sprout renders whatever summary is present
 * and asks `resolve()` to refresh it; the host owns the fetch. A single
 * mutable object lets the resolved summary be replaced in place, so a chip
 * that re-reads after `resolve()` sees the new value without the host object
 * itself changing.
 */
const entitlements: HostEntitlements = {
  async resolve() {
    const next = await platformEntitlements();
    const summary = next?.usageSummary;
    if (summary) entitlements.usageSummary = summary;
    else delete entitlements.usageSummary;
  },
};

/** Resolve once now; the UI re-resolves on focus/visibility/Home-close. */
function resolveEntitlementsNow(): void {
  void entitlements.resolve?.();
}
resolveEntitlementsNow();

/**
 * The cloud host: the hosted build's contract.
 *
 * The capability set mirrors CloudAdapter — the source of truth for what a
 * hosted build actually exposes — not mode.ts's cloud defaults. Identity,
 * entitlements, chrome and theme are platform-provided at runtime: the
 * bootstrap adapter resolves the concrete values at startup and a later item
 * wires the live identity/chrome through. This constant declares the *shape* of
 * the cloud host and the platform's navigation surface, not its runtime
 * identity values.
 *
 * The transport records the cloud *policy* (auth + same-origin-or-Foundry
 * identity), not a hardcoded platform URL: the concrete URL is resolved at
 * startup by the bootstrap adapter. This constant is a value object describing
 * the cloud contract; it must not duplicate the bootstrap fetch logic.
 */
export const cloudHost: SproutHost = {
  // The platform account, host-provided at runtime; the constant declares the
  // shape, so the value is left absent here.
  user: undefined,
  // The platform usage summary, resolved live from the platform's billing
  // status via `platformEntitlements`; `resolve()` refreshes it in place.
  entitlements,
  // Platform-supplied header slots, host-provided at runtime.
  chrome: undefined,
  // The platform theme/tokens, host-provided at runtime.
  theme: undefined,
  transport: {
    // Same-origin-or-Foundry policy; '' is the "resolved at runtime" sentinel
    // (the bootstrap adapter resolves the concrete URL at startup).
    apiBaseURL: '',
    wsURL: '',
    // The hosted platform authenticates requests.
    authMode: 'bearer',
  },
  navigation: {
    // The platform resolves an intent to its own SPA page and opens it. The
    // destination path is built here (the platform strings stay on the host
    // side); callers that render a link read it via intentPath.
    open(intent) {
      const path = platformIntentPath(intent);
      if (path) window.location.href = path;
    },
    // The platform's account exits and Home "Work" places, as data.
    accountItems: PLATFORM_ACCOUNT_ITEMS,
    workItems: PLATFORM_WORK_ITEMS,
    // The host's own intent → platform page path resolution.
    intentPath: platformIntentPath,
  },
  notifications: {
    // No-op placeholder; a later item wires the platform's real sink.
    post() {},
  },
  capabilities: {
    // These mirror CloudAdapter's capability constants (the hosted build's
    // source of truth), not mode.ts's cloud defaults:
    ssh: false, // the hosted (WASM) shell has no host SSH access
    git: true, // browser-native git (isomorphic-git)
    chat: true, // agent chat via the BYOK proxy
    workspaceSwitching: false, // single virtual FS in cloud
    export: false, // no local filesystem to export to
    instances: true, // the platform serves the instances API
    localTerminal: false, // WASM terminal, not a local PTY
    settings: true, // BYOK settings available
    // Local-mode-only flags, off for a hosted build:
    folderPicker: false,
    automations: false,
    agentChanges: false,
    // Hosted builds are pre-configured and don't expose these:
    mcp: false,
    localModels: false,
    verification: false,
    serverGit: false,
  },
};
