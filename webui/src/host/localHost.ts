import { notificationBus as rawNotificationBus } from '@sprout/ui';
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
    // The local build has no outward platform pages: open is a no-op. The
    // tasks intent is the one exception — the cloud task a local escalation
    // submits lives on the platform SPA served alongside the editor, at its
    // hash route. Returning null here dropped the toast's "View task" link
    // entirely (the CLOUD-2 regression the E2E pins). Other intents still
    // report "no page" honestly.
    open() {},
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
