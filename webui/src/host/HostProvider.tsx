import { createContext, useMemo, type ReactNode } from 'react';
import type { SproutHost } from './types';

/**
 * The headless, local-transport default host: no account, no entitlements,
 * same-origin transport, no-op navigation and notifications, and every
 * capability off. Used when no host is supplied, so an app that has not been
 * wired to a real host still renders with unchanged behavior.
 */
export function headlessHost(): SproutHost {
  return {
    transport: {
      apiBaseURL: '',
      wsURL: '',
      authMode: 'none',
    },
    navigation: {
      open() {},
    },
    notifications: {
      post() {},
    },
    capabilities: {
      ssh: false,
      git: false,
      chat: false,
      workspaceSwitching: false,
      folderPicker: false,
      export: false,
      instances: false,
      localTerminal: false,
      settings: false,
      automations: false,
      agentChanges: false,
      mcp: false,
      localModels: false,
      verification: false,
      serverGit: false,
    },
  };
}

/**
 * The context that carries the active SproutHost. Null until a HostProvider
 * provides one; useHost() throws when read while it is still null.
 */
export const HostContext = createContext<SproutHost | null>(null);

export interface HostProviderProps {
  /**
   * The host to provide to the subtree. When omitted, the headless default is
   * used so the tree renders with unchanged behavior.
   */
  host?: SproutHost;
  children: ReactNode;
}

/**
 * Provides the SproutHost to its subtree. The value is memoized on the
 * `host` reference (falling back to the headless default) so consumers only
 * re-render when the host instance actually changes.
 */
export function HostProvider({ host, children }: HostProviderProps) {
  const value = useMemo(() => host ?? headlessHost(), [host]);
  return <HostContext.Provider value={value}>{children}</HostContext.Provider>;
}

HostProvider.displayName = 'HostProvider';
