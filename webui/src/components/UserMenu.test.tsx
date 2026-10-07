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
import { PLATFORM_ACCOUNT_ITEMS, intentPath, platformPagePath } from '../host/platform';
import { makeTestHost } from '../host/testHost';
import type { HostNavigationIntent, SproutHost } from '../host/types';
import { __resetHomeViewForTests, getHomeView, openHome } from '../services/homeView';
import { UserMenu } from './UserMenu';

// Mutable per-test state for the mocked module surfaces.
const userState: { user: { id: string; email: string; tier: string; admin?: boolean } | undefined } = {
  user: { id: 'user-1', email: 'a@b.com', tier: 'pro' },
};
const platformURLState: { value: string | undefined } = { value: undefined };

vi.mock('../bootstrapAdapter', () => ({
  getBootstrapUser: () => userState.user,
}));

/**
 * A host with the cloud account surface, no platform strings in the test body:
 * the paths come from the host's own `intentPath`, and the host (not the
 * component) performs sign-out and opens page intents.
 */
function cloudNavHost(): { host: SproutHost; opened: HostNavigationIntent[] } {
  const opened: HostNavigationIntent[] = [];
  const host: SproutHost = {
    ...makeTestHost(),
    // The platform base is host-provided transport data: the menu reads it
    // from the host's transport, not the bootstrap adapter.
    transport: { ...makeTestHost().transport, authMode: 'bearer', platformURL: platformURLState.value },
    navigation: {
      // The host resolves an intent exactly as cloudHost does: sign-out runs
      // the platform logout and redirects; any other intent hard-navigates to
      // the platform page. Sprout decides to open a page in the shell (layered
      // layout) *before* dispatching, so the host is never asked to leave.
      open(intent) {
        opened.push(intent);
        if (intent.type === 'signOut') {
          // Mirror cloudHost: return the promise so the caller can surface a
          // failure (the real host does this too).
          return fetch(platformPagePath('/webui/auth/logout'), { method: 'POST', credentials: 'include' }).then(
            (res) => {
              if (!res.ok) throw new Error(`Sign out failed (HTTP ${res.status}).`);
              window.location.href = platformPagePath('/login');
            },
          );
        }
        const path = intentPath(intent);
        if (path) window.location.href = platformPagePath(path);
        return undefined;
      },
      accountItems: PLATFORM_ACCOUNT_ITEMS,
      intentPath,
      platformPagePath,
    },
  };
  return { host, opened };
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
  // through the module-level active host, so record the same host in both
  // channels.
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
    render(cloudNavHost().host);
    const trigger = container.querySelector('.user-menu-trigger');
    expect(trigger).not.toBeNull();
    expect(trigger!.querySelector('.user-menu-avatar')!.textContent).toBe('A');
    expect(trigger!.getAttribute('aria-expanded')).toBe('false');
  });

  it('renders nothing when the bootstrap carried no user identity', () => {
    userState.user = undefined;
    render(cloudNavHost().host);
    expect(container.querySelector('.user-menu')).toBeNull();
  });

  it('renders nothing when the host offers no account surface (local host)', () => {
    render(makeTestHost());
    expect(container.querySelector('.user-menu')).toBeNull();
  });

  it('opens the menu with identity, the host exit items, and Sign out', () => {
    render(cloudNavHost().host);
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
    render(cloudNavHost().host);
    openMenu();
    const items = container.querySelectorAll('.user-menu-list a[role="menuitem"]');
    const admin = Array.from(items).find((a) => a.textContent === 'Admin');
    expect(admin?.getAttribute('href')).toBe('/?from=editor#/admin');
  });

  it('builds absolute exit URLs when the platform base is known', () => {
    platformURLState.value = 'https://platform.sprout.dev';
    render(cloudNavHost().host);
    openMenu();
    const links = container.querySelector('.user-menu-list')!.querySelectorAll('a[role="menuitem"]');
    expect(links[0].getAttribute('href')).toBe('https://platform.sprout.dev/?from=editor');
    expect(links[1].getAttribute('href')).toBe('https://platform.sprout.dev/?from=editor#/tasks');
    expect(links[3].getAttribute('href')).toBe('https://platform.sprout.dev/?from=editor#/team');
  });

  it('falls back to relative exit URLs when the platform base is absent', () => {
    render(cloudNavHost().host);
    openMenu();
    const links = container.querySelector('.user-menu-list')!.querySelectorAll('a[role="menuitem"]');
    expect(links[0].getAttribute('href')).toBe('/?from=editor');
    expect(links[1].getAttribute('href')).toBe('/?from=editor#/tasks');
  });

  it('opens a platform page inside the shell (layered) instead of leaving', () => {
    render(cloudNavHost().host);
    openMenu();
    const before = window.location.href;
    const billing = Array.from(
      container.querySelectorAll<HTMLAnchorElement>('.user-menu-list a[role="menuitem"]'),
    ).find((a) => a.textContent === 'Usage & billing')!;
    act(() => billing.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true })));
    // The page shows in the embedded shell; the editor does not navigate away
    // (the host's `open` would hard-navigate to the platform, so it must not be
    // reached — the only URL change is the shell's own `?home=` entry).
    expect(getHomeView()).toEqual({ open: true, path: '/account/billing' });
    expect(window.location.href).not.toContain('platform.sprout.dev');
    expect(window.location.href.startsWith(before.split('?')[0])).toBe(true);
  });

  it('closes the menu on Escape and returns focus to the trigger', () => {
    render(cloudNavHost().host);
    const trigger = container.querySelector('.user-menu-trigger')!;
    openMenu();
    expect(container.querySelector('.user-menu-list')).not.toBeNull();
    act(() => {
      trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(container.querySelector('.user-menu-list')).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it('signs out through the host (platform logout endpoint, then /login)', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValue(new Response(null, { status: 200 }));
    const { host, opened } = cloudNavHost();
    render(host);
    openMenu();
    act(() => {
      container.querySelector('.user-menu-signout')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await act(async () => {
      await Promise.resolve();
    });
    await act(async () => {
      await Promise.resolve();
    });
    // The component requested the sign-out intent; the host performed the
    // logout POST and the redirect.
    expect(opened).toEqual([{ type: 'signOut' }]);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/webui/auth/logout');
    expect(init.method).toBe('POST');
    expect(window.location.href).toBe('/login');
  });

  it('does not double-post on a second sign-out click', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValue(new Response(null, { status: 200 }));
    render(cloudNavHost().host);
    const click = () => {
      openMenu();
      act(() => {
        container.querySelector('.user-menu-signout')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });
    };
    click();
    // The menu closes on the first click (and the button is disabled while
    // signing out); reopening it and clicking again must not post a second
    // logout — the host performs one sign-out.
    click();
    await act(async () => {
      await Promise.resolve();
    });
    expect(fetchMock.mock.calls.length).toBe(1);
  });

  it('surfaces a notification and stays signed in when the logout request fails', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockRejectedValue(new Error('network down'));
    render(cloudNavHost().host);
    openMenu();
    act(() => {
      container.querySelector('.user-menu-signout')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await act(async () => {
      await Promise.resolve();
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(window.location.href).toBe(originalLocation.href);
    expect(container.querySelector('.user-menu-list')).not.toBeNull();
  });

  it('a local host (no platform account) never performs a logout fetch', async () => {
    const fetchMock = vi.mocked(fetch);
    // The local host has no account surface, so the menu renders nothing —
    // there is no sign-out affordance to click.
    render(localHost);
    expect(container.querySelector('.user-menu')).toBeNull();
    expect(container.querySelector('.user-menu-trigger')).toBeNull();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
