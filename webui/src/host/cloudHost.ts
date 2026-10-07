import { notificationBus as rawNotificationBus } from '@sprout/ui';
// Concrete modules, not the './platform' index: the index re-exports
// cloudHost, so importing through it is a cycle that leaves these bindings
// undefined while this module evaluates under some module loaders.
import {
  PLATFORM_ACCOUNT_ITEMS,
  PLATFORM_WORK_ITEMS,
  intentPath as platformIntentPath,
  platformEntitlements,
  platformPagePath,
  platformEmbedPagePath,
} from './platform/pages';
import { CLOUD_NAV_ITEMS } from './platformNav';
import { platformHref } from './platformUrl';
import { createPlatformRepo, fetchPlatformGitHubConnected, listPlatformRepos } from './platformGitHub';
import { platformChrome } from './platform/platformChrome';
import type { HostEntitlements, SproutHost } from './types';

/**
 * The live platform entitlements: Sprout renders whatever summary is present
 * and asks `resolve()` to refresh it; the host owns the fetch. A single
 * mutable object lets the resolved summary be replaced in place, so a chip
 * that re-reads after `resolve()` sees the new value without the host object
 * itself changing.
 *
 * Resolution is lazy by design: `resolve()` is only called from the consumer
 * (the credits chip calls it on mount, on focus, while the tab is visible and
 * when Home closes). Importing the host therefore performs no fetch — the
 * package entry stays free of platform calls on import.
 */
const entitlements: HostEntitlements = {
  async resolve() {
    const next = await platformEntitlements();
    const summary = next?.usageSummary;
    if (summary) entitlements.usageSummary = summary;
    else delete entitlements.usageSummary;
  },
};

/**
 * The platform sign-out: the server-side logout path (POST the platform's
 * logout endpoint — the Kratos two-step browser logout the platform SPA's
 * signOut uses), then a hard navigation to the login screen (which drops any
 * cached client-side session state). Kept on the host side: the paths are the
 * platform's, and the host owns the resolution of a `signOut` intent.
 *
 * A network failure propagates to the caller (the intent's dispatcher), which
 * surfaces it: the session cookie is intact, so staying is the honest outcome.
 */
async function signOut(): Promise<void> {
  const res = await fetch(platformHref('/webui/auth/logout'), {
    method: 'POST',
    credentials: 'include',
  });
  if (!res.ok) throw new Error(`Sign out failed (HTTP ${res.status}).`);
}

/**
 * Perform the sign-out: server-side logout, then a hard navigation to the
 * login screen. The redirect is deliberately OUTSIDE the failure surface: the
 * logout POST decides success, so a navigation that the browser throttles or a
 * test stubs never masquerades as a failed sign-out (the cookie is already
 * cleared at that point; the caller must not reopen the menu and claim the
 * user is still signed in).
 */
function signOutAndRedirect(): Promise<void> {
  return signOut().then(() => {
    // The redirect runs after the sign-out promise settles, outside the
    // failure surface: the logout POST decides success, so a navigation the
    // browser throttles (or a stub throws on) never rejects this promise and
    // masquerades as a failed sign-out.
    if (typeof window !== 'undefined') {
      try {
        window.location.href = platformHref('/login');
      } catch {
        // A blocked navigation leaves the page on the signed-out screen; the
        // session is already cleared, so there is nothing the caller must do.
      }
    }
  });
}

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
 * startup by the bootstrap adapter and records it on this host object's
 * transport (`platformURL`), so the constant declares the shape and the host
 * object carries the resolved value as data. This constant is a value object
 * describing the cloud contract; it must not duplicate the bootstrap fetch
 * logic.
 */
export const cloudHost: SproutHost = {
  // The platform account, host-provided at runtime; the constant declares the
  // shape, so the value is left absent here.
  user: undefined,
  // The platform usage summary, resolved live from the platform's billing
  // status via `platformEntitlements`; `resolve()` refreshes it in place.
  entitlements,
  // Platform-supplied header slots, host-provided at runtime; the GitHub
  // account card is the one chrome surface the platform supplies today (the
  // platform manages the account's GitHub connection), so Sprout renders it
  // wherever it shows a GitHub account panel.
  chrome: platformChrome,
  // GitHub is managed on the platform account, not a per-user token: the
  // platform supplies the connection check and the account's repository list
  // as data, so the picker holds no platform call of its own.
  github: {
    isConnected: fetchPlatformGitHubConnected,
    listRepos: listPlatformRepos,
    createRepo: createPlatformRepo,
  },
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
    // side); callers that render a link read it via intentPath. A sign-out
    // intent runs the platform's two-step browser logout (POST the logout
    // endpoint, then land on the login page) — the host owns it, so no Sprout
    // component holds the sign-out paths. The sign-out promise is returned so
    // the caller can surface a failure.
    open(intent) {
      if (intent.type === 'signOut') return signOutAndRedirect();
      const path = platformIntentPath(intent);
      if (path) window.location.href = platformHref(path);
      return undefined;
    },
    // The platform's account exits and Home "Work" places, as data.
    accountItems: PLATFORM_ACCOUNT_ITEMS,
    workItems: PLATFORM_WORK_ITEMS,
    // The platform's fallback nav items (used by the hosted adapter when the
    // runtime bootstrap did not provide its own list).
    navItems: CLOUD_NAV_ITEMS,
    // The host's own intent → platform page path resolution.
    intentPath: platformIntentPath,
    // The host's own route → full outward page URL (platform base applied, no
    // embed decoration): what a link's href uses.
    platformPagePath,
    // The host's own route → full embeddable page URL (the `?embed=1`
    // decoration lives on the platform side): what the Home frame loads.
    embedPagePath: platformEmbedPagePath,
  },
  notifications: {
    // The platform's own sink is a later item. Until it is wired, the honest
    // behavior is to keep raising notifications in the editor's own in-app
    // channel (the toast stack + NotificationCenter), so a hosted build does
    // not silently lose notifications it would have shown. This forwards to
    // the same in-app bus the local host uses; when the platform sink lands it
    // replaces this with a real platform post.
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
