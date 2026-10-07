import { HostContext, HostProvider, headlessHost } from './HostProvider';

export type {
  HostCapabilities,
  HostChrome,
  HostEntitlements,
  HostHelpIntent,
  HostNavigation,
  HostNavigationIntent,
  HostNotification,
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
export { localHost } from './localHost';
export { cloudHost } from './cloudHost';

/**
 * A stable, pre-built headless host for hosts that want a single default
 * instance rather than calling the factory each time.
 */
export const defaultHost = headlessHost();
