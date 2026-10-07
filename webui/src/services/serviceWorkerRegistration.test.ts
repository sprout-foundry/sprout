/**
 * Service Worker registration is gated on the host's `localTerminal` capability
 * through the non-React host accessor (host-or-fallback, read at use time). A
 * hosted shell (no local terminal) ships no service worker and must return null
 * before touching navigator.serviceWorker; a local-terminal shell registers the
 * root sw.js. With no active host the capability falls back to the mode-aware
 * default, so a local build still registers.
 *
 * The production/dev branch keys off import.meta.env.PROD (read at use time), so
 * the dev/unregister and production/register paths are exercised by re-writing
 * import.meta.env before each call — no fresh module registry needed.
 */

import { makeTestHost } from '../host/testHost';
import type { SproutHost } from '../host/types';

vi.mock('../utils/log', () => ({ debugLog: vi.fn() }));

vi.mock('./apiAdapter', () => ({
  getAdapter: vi.fn(() => null),
  ADAPTER_INSTALLED_EVENT: 'adapter-installed',
}));

const activeHostRef = vi.hoisted(() => ({ value: null }));
vi.mock('../host/accessor', () => ({
  getActiveHost: () => activeHostRef.value,
  setActiveHost: (host: SproutHost) => {
    activeHostRef.value = host;
  },
  HOST_UPDATED_EVENT: 'sprout:host-updated',
}));

const env = import.meta.env as unknown as Record<string, unknown>;

function setServiceWorkerSupport() {
  const register = vi.fn().mockResolvedValue({
    scope: '/webui',
    update: vi.fn(),
    addEventListener: vi.fn(),
    waiting: null,
  });
  Object.defineProperty(navigator, 'serviceWorker', {
    value: {
      register,
      getRegistrations: vi.fn().mockResolvedValue([]),
      addEventListener: vi.fn(),
      controller: null,
    },
    configurable: true,
  });
  (globalThis as { __swRegister?: (url: string) => Promise<unknown> }).__swRegister = register;
}

function clearServiceWorkerSupport() {
  delete (navigator as { serviceWorker?: unknown }).serviceWorker;
  delete (globalThis as { __swRegister?: unknown }).__swRegister;
}

function getSwRegister() {
  return (globalThis as { __swRegister?: (url: string) => Promise<unknown> }).__swRegister;
}

async function importModule() {
  const mod = await import('./serviceWorkerRegistration');
  return mod.registerServiceWorker;
}

beforeEach(() => {
  activeHostRef.value = null;
  clearServiceWorkerSupport();
  env.PROD = false;
});

describe('registerServiceWorker capability gate', () => {
  it('returns null (no SW) for a hosted shell (no local terminal) without touching the SW API', async () => {
    setServiceWorkerSupport();
    activeHostRef.value = makeTestHost({ localTerminal: false });
    const registerServiceWorker = await importModule();

    expect(await registerServiceWorker()).toBeNull();
    // The gate short-circuits before the dev/unregister path, so the SW
    // register call is never issued.
    const register = getSwRegister() as unknown as ReturnType<typeof vi.fn>;
    expect(register).not.toHaveBeenCalled();
  });

  it('returns null when the browser has no service worker support (local-terminal host)', async () => {
    activeHostRef.value = makeTestHost({ localTerminal: true });
    env.PROD = false;
    const registerServiceWorker = await importModule();

    expect(await registerServiceWorker()).toBeNull();
  });
});

describe('registerServiceWorker dev path (unregisters existing SW)', () => {
  it('a local-terminal host in a dev build unregisters existing registrations and returns null', async () => {
    const getRegistrations = vi.fn().mockResolvedValue([{ unregister: vi.fn().mockResolvedValue(undefined) }]);
    Object.defineProperty(navigator, 'serviceWorker', {
      value: { getRegistrations, addEventListener: vi.fn(), controller: null },
      configurable: true,
    });
    activeHostRef.value = makeTestHost({ localTerminal: true });
    env.PROD = false;
    const registerServiceWorker = await importModule();

    expect(await registerServiceWorker()).toBeNull();
    expect(getRegistrations).toHaveBeenCalledTimes(1);
  });
});

describe('registerServiceWorker production registration', () => {
  it('a local-terminal host in a production build registers the root sw.js', async () => {
    setServiceWorkerSupport();
    activeHostRef.value = makeTestHost({ localTerminal: true });
    env.PROD = true;
    const registerServiceWorker = await importModule();

    expect(await registerServiceWorker()).not.toBeNull();
    const register = getSwRegister() as (url: string) => Promise<unknown>;
    expect(register).toHaveBeenCalledWith('/sw.js');
  });
});

describe('registerServiceWorker no-host fallback (mode-aware capability default)', () => {
  it('local build registers the root sw.js with no active host', async () => {
    setServiceWorkerSupport();
    activeHostRef.value = null;
    env.PROD = true;
    const registerServiceWorker = await importModule();

    expect(await registerServiceWorker()).not.toBeNull();
    const register = getSwRegister() as (url: string) => Promise<unknown>;
    expect(register).toHaveBeenCalledWith('/sw.js');
  });
});
