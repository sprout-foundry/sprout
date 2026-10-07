import { useContext } from 'react';
import { HostContext } from './HostProvider';
import type { SproutHost } from './types';

/**
 * Returns the active SproutHost provided by HostProvider.
 *
 * Throws a helpful error when used outside a HostProvider so a mis-mounted
 * component fails loudly rather than silently reading the headless default.
 */
export function useHost(): SproutHost {
  const host = useContext(HostContext);
  if (!host) {
    throw new Error('useHost must be used within a HostProvider');
  }
  return host;
}
