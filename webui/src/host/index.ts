import { HostContext, HostProvider, headlessHost } from './HostProvider';

export type {
  HostCapabilities,
  HostChrome,
  HostEntitlements,
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
export { getActiveHost, setActiveHost } from './accessor';
export { default as HostNotificationCount } from './HostNotificationCount';
export { localHost } from './localHost';
export { cloudHost } from './cloudHost';
export { platformHref, repoHubPath, repoSlug, repoName, githubRepoSlug } from './platformUrl';
export {
  fetchPlatformGitHubConnected,
  listPlatformRepos,
  createPlatformRepo,
  usesPlatformGitHub,
  platformGitHubSettingsHref,
  CreateRepoError,
} from './platformGitHub';
export { default as PlatformGitHubAccountCard } from './PlatformGitHubAccountCard';
export { platformEntitlements } from './platform';

/**
 * A stable, pre-built headless host for hosts that want a single default
 * instance rather than calling the factory each time.
 */
export const defaultHost = headlessHost();
