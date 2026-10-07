import type { SproutHost } from './types';

/**
 * The cloud host: the hosted build's contract.
 *
 * The capability set mirrors CloudAdapter — the source of truth for what a
 * hosted build actually exposes — not mode.ts's cloud defaults. Identity,
 * entitlements, chrome and theme are platform-provided at runtime and filled
 * in by a later item; this constant declares the *shape* of the cloud host,
 * not its runtime values.
 *
 * The transport records the cloud *policy* (auth + same-origin-or-Foundry
 * identity), not a hardcoded platform URL: the concrete URL is resolved at
 * startup by the bootstrap adapter and a later item wires the live values
 * through. This constant is a value object describing the cloud contract; it
 * must not duplicate the bootstrap fetch logic.
 */
export const cloudHost: SproutHost = {
  // The platform account, host-provided at runtime; the constant declares the
  // shape, so the value is left absent here.
  user: undefined,
  // The platform usage summary, host-provided at runtime.
  entitlements: undefined,
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
    // No-op placeholder; the real platform navigation-intent resolution
    // arrives in a later item.
    open() {},
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
