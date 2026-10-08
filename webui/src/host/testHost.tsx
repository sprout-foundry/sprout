import type { ReactNode } from 'react';
import { HostProvider, headlessHost } from './HostProvider';
import type { HostCapabilities, SproutHost } from './types';

/**
 * Build a host for isolated component/hook tests: start from the headless
 * defaults and override a subset of capabilities. Tests render a touched
 * component (which reads capabilities through the host) under a
 * HostProvider that carries exactly the capability values the test is
 * exercising, so the component's rendered output matches what it asserted
 * before capabilities moved to the host.
 */
export function makeTestHost(capabilities: Partial<HostCapabilities> = {}): SproutHost {
  return { ...headlessHost(), capabilities: { ...headlessHost().capabilities, ...capabilities } };
}

/**
 * Wrap an element in a HostProvider carrying a test host, for tests that use
 * a render helper (createRoot / renderHook wrapper) rather than the app's
 * root provider.
 */
export function hostWrapper(host: SproutHost) {
  return function HostTestWrapper({ children }: { children: ReactNode }) {
    return <HostProvider host={host}>{children}</HostProvider>;
  };
}
