/**
 * registerActiveHost — the embedding counterpart to the entry's own host
 * registration.
 *
 * It records a host as the active one for the module-level services and
 * returns a restore function that puts the previous host back, so a mounted
 * workspace that registers its host does not leave it installed after unmount.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';
import { getActiveHost, registerActiveHost, setActiveHost } from './accessor';
import { headlessHost } from './HostProvider';
import type { SproutHost } from './types';

function hostWithApiBase(apiBaseURL: string): SproutHost {
  return { ...headlessHost(), transport: { apiBaseURL, wsURL: '', authMode: 'none' } };
}

describe('registerActiveHost', () => {
  afterEach(() => {
    // Reset the module singleton so tests do not leak into one another.
    setActiveHost(headlessHost());
  });

  it('records the host as the active one and returns a restore that puts the previous back', () => {
    const baseline = hostWithApiBase('https://baseline.test');
    setActiveHost(baseline);

    const host = hostWithApiBase('https://host.test');
    const restore = registerActiveHost(host);
    expect(getActiveHost()).toBe(host);

    restore();
    expect(getActiveHost()).toBe(baseline);
  });

  it('restores only once when the restore function is called more than once', () => {
    const baseline = hostWithApiBase('https://baseline.test');
    setActiveHost(baseline);

    const host = hostWithApiBase('https://host.test');
    const restore = registerActiveHost(host);
    restore();
    // A second call must not re-register the workspace host or clobber a host
    // another registration installed meanwhile.
    const later = hostWithApiBase('https://later.test');
    setActiveHost(later);
    restore();
    expect(getActiveHost()).toBe(later);
  });

  it('keeps the registered host when no host was active before (the embedding case)', () => {
    // A fresh module instance has no active host. The accessor has no null
    // setter, so the restore keeps the registered host rather than clearing
    // the singleton — a mounted-then-unmounted workspace leaves a resolvable
    // host, never none.
    vi.resetModules();
    return import('./accessor').then(({ getActiveHost: get, registerActiveHost: register }) => {
      const host = hostWithApiBase('https://host.test');
      const restore = register(host);
      expect(get()).toBe(host);
      restore();
      expect(get()).toBe(host);
    });
  });
});
