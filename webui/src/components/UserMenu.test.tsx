/**
 * UserMenu (SP-016 P0.5) — the cloud-mode avatar menu: identity trigger,
 * host-provided account exits (absolute when the platform base is known,
 * relative otherwise), and the reused platform sign-out path.
 */
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { setActiveHost } from '../host/accessor';
import { HostProvider } from '../host/HostProvider';
import { localHost } from '../host/localHost';
import { PLATFORM_ACCOUNT_ITEMS, intentPath } from '../host/platform';
import { makeTestHost } from '../host/testHost';
import type { SproutHost } from '../host/types';
import { __resetHomeViewForTests, getHomeView } from '../services/homeView';
import { UserMenu } from './UserMenu';

// Mutable per-test state for the mocked module surfaces.
const userState: { user: { id: string; email: string; tier: string; admin?: boolean } | undefined } = {
  user: { id: 'user-1', email: 'a@b.com', tier: 'pro' },
};
const platformURLState: { value: string | undefined } = { value: undefined };

vi.mock('../bootstrapAdapter', () => ({
  getBootstrapUser: () => userState.user,
}));

/** A host with the cloud account surface, no platform strings in the test. */
function cloudNavHost(): SproutHost {
  return {
    ...makeTestHost(),
    // The platform base is host-provided transport data: the menu
    // reads it from the host's transport, not the bootstrap adapter.
    transport: { ...makeTestHost().transport, authMode: 'bearer', platformURL: platformURLState.value },
    navigation: {
      open: () => undefined,
      accountItems: PLATFORM_ACCOUNT_ITEMS,
      intentPath,
    },
  };
}

let container: HTMLDivElement;
let root: Root;

// jsdom does not implement navigation — a `window.location.href = ...`
// assignment is a no-op. Stub the location object with a working href
// accessor so sign-out navigation can be asserted (same pattern as
// EscalationListener.test.tsx).
const originalLocation = window.location;
let hrefValue = originalLocation.href;

function stubLocation() {
  hrefValue = originalLocation.href;
  Object.defineProperty(window, 'location', {
    configurable: true,
    writable: true,
    value: {
      ...originalLocation,
      get href() {
        return hrefValue;
      },
      set href(value: string) {
        hrefValue = value;
      },
    },
  });
}

function render(host: SproutHost) {
  // The component reads nav items from the provider but resolves exit URLs
  // through the module-level active host (platformHref reads the accessor), so
  // record the same host in both channels.
  setActiveHost(host);
  act(() => {
    root.render(createElement(HostProvider, { host }, createElement(UserMenu)));
  });
}

function openMenu() {
  act(() => {
    container.querySelector('.user-menu-trigger')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
}

beforeEach(() => {
  __resetHomeViewForTests();
  // Clear the module-level active host so a previous test's host (or the
  // platform base it carried) cannot leak into this one.
  setActiveHost(localHost);
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  userState.user = { id: 'user-1', email: 'a@b.com', tier: 'pro' };
  platformURLState.value = undefined;
  stubLocation();
  vi.stubGlobal('fetch', vi.fn());
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
  Object.defineProperty(window, 'location', {
    configurable: true,
    writable: true,
    value: originalLocation,
  });
  vi.unstubAllGlobals();
});

describe('UserMenu (SP-016 P0.5)', () => {
  it('renders the avatar trigger with the identity initial', () => {
    render(cloudNavHost());
    const trigger = container.querySelector('.user-menu-trigger');
    expect(trigger).not.toBeNull();
    expect(trigger!.querySelector('.user-menu-avatar')!.textContent).toBe('A');
    expect(trigger!.getAttribute('aria-expanded')).toBe('false');
  });

  it('renders nothing when the bootstrap carried no user identity', () => {
    userState.user = undefined;
    render(cloudNavHost());
    expect(container.querySelector('.user-menu')).toBeNull();
  });

  it('renders nothing when the host offers no account surface (local host)', () => {
    render(makeTestHost());
    expect(container.querySelector('.user-menu')).toBeNull();
  });

  it('opens the menu with identity, the host exit items, and Sign out', () => {
    render(cloudNavHost());
    openMenu();
    const list = container.querySelector('.user-menu-list');
    expect(list!.querySelector('.user-menu-identity-email')!.textContent).toBe('a@b.com');
    expect(list!.querySelector('.user-menu-tier')!.textContent).toBe('pro');
    const items = list!.querySelectorAll('a[role="menuitem"]');
    expect(Array.from(items).map((a) => a.textContent)).toEqual([
      'Dashboard',
      'Tasks',
      'Usage & billing',
      'Team',
      'Runners',
      'Settings',
    ]);
    expect(list!.querySelector('.user-menu-signout')!.textContent).toBe('Sign out');
  });

  it('offers Admin to platform administrators', () => {
    userState.user = { id: 'user-1', email: 'a@b.com', tier: 'pro', admin: true };
    render(cloudNavHost());
    openMenu();
    const items = container.querySelectorAll('.user-menu-list a[role="menuitem"]');
    const admin = Array.from(items).find((a) => a.textContent === 'Admin');
    expect(admin?.getAttribute('href')).toBe('/?from=editor#/admin');
  });

  it('builds absolute exit URLs when the platform base is known', () => {
    platformURLState.value = 'https://platform.sprout.dev';
    render(cloudNavHost());
    openMenu();
    const links = container.querySelector('.user-menu-list')!.querySelectorAll('a[role="menuitem"]');
    expect(links[0].getAttribute('href')).toBe('https://platform.sprout.dev/?from=editor');
    expect(links[1].getAttribute('href')).toBe('https://platform.sprout.dev/?from=editor#/tasks');
    expect(links[3].getAttribute('href')).toBe('https://platform.sprout.dev/?from=editor#/team');
  });

  it('falls back to relative exit URLs when the platform base is absent', () => {
    render(cloudNavHost());
    openMenu();
    const links = container.querySelector('.user-menu-list')!.querySelectorAll('a[role="menuitem"]');
    expect(links[0].getAttribute('href')).toBe('/?from=editor');
    expect(links[1].getAttribute('href')).toBe('/?from=editor#/tasks');
  });

  it('opens a platform page inside the shell (layered) instead of leaving', () => {
    render(cloudNavHost());
    openMenu();
    const billing = Array.from(
      container.querySelectorAll<HTMLAnchorElement>('.user-menu-list a[role="menuitem"]'),
    ).find((a) => a.textContent === 'Usage & billing')!;
    act(() => billing.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true })));
    expect(getHomeView()).toEqual({ open: true, path: '/account/billing' });
  });

  it('closes the menu on Escape and returns focus to the trigger', () => {
    render(cloudNavHost());
    const trigger = container.querySelector('.user-menu-trigger')!;
    openMenu();
    expect(container.querySelector('.user-menu-list')).not.toBeNull();
    act(() => {
      trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(container.querySelector('.user-menu-list')).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it('signs out via the platform logout endpoint and navigates to /login', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValue(new Response(null, { status: 200 }));
    render(cloudNavHost());
    openMenu();
    act(() => {
      container.querySelector('.user-menu-signout')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await act(async () => {
      await Promise.resolve();
    });
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/webui/auth/logout');
    expect(init.method).toBe('POST');
    expect(window.location.href).toBe('/login');
  });

  it('surfaces a notification and stays signed in when the logout request fails', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockRejectedValue(new Error('network down'));
    render(cloudNavHost());
    openMenu();
    act(() => {
      container.querySelector('.user-menu-signout')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(window.location.href).toBe(originalLocation.href);
    expect(container.querySelector('.user-menu-list')).not.toBeNull();
  });
});
