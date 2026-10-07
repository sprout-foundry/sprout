import type { SproutHost } from './types';

/**
 * The local host: no account, no entitlements, the local backend at the
 * current origin, and every local capability on.
 *
 * The page and the local daemon share an origin (the daemon serves /api/* and
 * /ws on the page's own origin), so the transport records the same-origin
 * sentinel '' (the headless default's expression of same-origin) rather than a
 * fixed host — the http://localhost:56000 dev port is only Vite's dev-server
 * default, never the real backend, so it is not hardcoded.
 */
export const localHost: SproutHost = {
  // No account concept in the local build.
  user: null,
  // No entitlement to surface; the local build is free.
  entitlements: undefined,
  transport: {
    // '' = same origin (derived at runtime by the local transport).
    apiBaseURL: '',
    wsURL: '',
    authMode: 'none',
  },
  navigation: {
    // No outward platform links exist yet; a no-op is honest.
    open() {},
  },
  notifications: {
    // The local UI keeps its own in-app toast/center; the host sink is a
    // no-op placeholder until that sink is wired.
    post() {},
  },
  capabilities: {
    // Mirrors today's local-mode defaults (config/mode.ts local values):
    ssh: true,
    git: true,
    chat: true,
    workspaceSwitching: true,
    folderPicker: false,
    export: true,
    instances: false,
    localTerminal: true,
    settings: true,
    automations: true,
    agentChanges: true,
    // Spec-only flags (not yet in mode.ts). A full local desktop build offers
    // all of these, so they are on:
    mcp: true,
    localModels: true,
    verification: true,
    serverGit: true,
  },
};
