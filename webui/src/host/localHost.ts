import { notificationBus as rawNotificationBus } from '@sprout/ui';
import { buildBugReportURLFromEnv, type BugReportEnvironment } from './reportBugURL';
import type { SproutHost } from './types';

/**
 * A minimal environment for a bare `reportBug` intent (one the caller did not
 * prefill). The host tree must not import the bootstrap adapter, so the
 * version falls back to 'dev' here; the buttons and the CLI collect the real
 * build version outside the host tree and pass it on the intent.
 */
function fallbackBugEnvironment(): BugReportEnvironment {
  const nav = typeof navigator !== 'undefined' ? navigator : undefined;
  return {
    version: 'dev',
    mode: 'local daemon',
    os: nav?.platform || 'unknown',
    browser: nav?.userAgent || 'unknown',
  };
}

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
    // The local build has no outward platform pages. `open` handles the one
    // intent it can resolve itself (`reportBug` — the local host has no support
    // flow, so it opens the public repository's prefilled new-issue page) and
    // is otherwise a no-op. The tasks intent is the one exception in
    // `intentPath` — the cloud task a local escalation submits lives on the
    // platform SPA served alongside the editor, at its hash route. Returning
    // null there dropped the toast's "View task" link entirely (the CLOUD-2
    // regression the E2E pins). Other intents still report "no page" honestly.
    //
    // `reportBug`'s environment comes from the intent (collected outside the
    // host tree, since the host tree must not import the bootstrap adapter); a
    // fallback covers a bare intent.
    open(intent) {
      if (intent.type === 'reportBug') {
        if (typeof window !== 'undefined') {
          window.open(
            buildBugReportURLFromEnv(intent.environment ?? fallbackBugEnvironment()),
            '_blank',
            'noopener,noreferrer',
          );
        }
        return;
      }
    },
    intentPath(intent) {
      if (intent.type === 'nav' && intent.id === 'tasks') {
        return intent.detail ? `/#/tasks/${intent.detail}` : '/#/tasks';
      }
      return null;
    },
  },
  notifications: {
    // The local host's sink IS the in-app notification bus: it forwards posted
    // notifications to the same bus that drives the toast stack and the
    // NotificationCenter, so local behavior is unchanged. The web UI's host-aware
    // notificationBus routes here when localHost is active — exactly once, so
    // nothing double-posts. No unread count: the local UI owns its own history.
    post(notification) {
      rawNotificationBus.notify(
        notification.level,
        notification.title,
        notification.message,
        notification.duration,
        notification.action,
      );
    },
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
