import { useHost } from './useHost';
import type { HostCapabilities } from './types';

/**
 * Convenience hook for the host's capability flags. Components that only need
 * the flat capability set (terminal, git, SSH, …) use this instead of
 * useHost().capabilities, so the read site stays narrow and the intent —
 * "I'm reading a capability" — is explicit at the call site.
 *
 * Like useHost(), it throws when called outside a HostProvider: a component
 * that renders a capability-gated surface must always be mounted under a host.
 */
export function useHostCapabilities(): HostCapabilities {
  return useHost().capabilities;
}
