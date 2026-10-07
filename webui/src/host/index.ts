/**
 * The host contract — the public entry point.
 *
 * This module is the contract a host provides and the surface a Sprout
 * component imports: the `SproutHost` types, the provider and hooks (`useHost`,
 * `useHostCapabilities`), the active-host accessor, the notification count, the
 * built-in `localHost`, and the host-agnostic repository-naming helpers.
 *
 * The platform implementation (`cloudHost`, `platformHref`, the platform page
 * names, `PlatformGitHubAccountCard`, `platformEntitlements`, …) is NOT part of
 * this entry: it lives in the internal `host/platform/` module, which the app
 * entry and tests import directly but which is deliberately unreachable from
 * here, so it never leaks into the published package's public API.
 */

import { HostContext, HostProvider, headlessHost } from './HostProvider';

export type {
  HostCapabilities,
  HostChrome,
  HostEntitlements,
  HostGitHub,
  HostGitHubRepo,
  HostHelpIntent,
  HostNavIntent,
  HostNavItem,
  HostNavigation,
  HostNavigationIntent,
  HostNotification,
  HostNotificationAction,
  HostNotifications,
  HostProjectIntent,
  HostSignOutIntent,
  HostSpaceIntent,
  HostTheme,
  HostTransport,
  HostUsageIntent,
  HostAccountIntent,
  HostUser,
  SproutHost,
} from './types';
export { HostContext, HostProvider, headlessHost };
export type { HostProviderProps } from './HostProvider';
export { useHost } from './useHost';
export { useHostCapabilities } from './useHostCapabilities';
export { getActiveHost, setActiveHost, upsertActiveHostCapabilities } from './accessor';
export { default as HostNotificationCount } from './HostNotificationCount';
export { localHost } from './localHost';
export { outwardURL } from './outwardURL';
export { githubRepoSlug, repoName, repoSlug } from './repoName';

/**
 * A stable, pre-built headless host for hosts that want a single default
 * instance rather than calling the factory each time.
 */
export const defaultHost = headlessHost();
