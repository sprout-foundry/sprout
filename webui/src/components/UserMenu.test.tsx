/**
 * UserMenu (SP-016 P0.5) — the cloud-mode avatar menu: identity trigger,
 * account-surface exits (absolute when the platform base is known,
 * relative otherwise), and the reused platform sign-out path.
 */
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { UserMenu } from './UserMenu';

// Mutable per-test state for the mocked module surfaces.
const modeState = { isCloud: true };
const userState: { user: { id: string; email: string; tier: string } | undefined } = {
  user: { id: 'user-1', email: 'a@b.com', tier: 'pro' },
};
const platformURLState: { value: string | undefined } = { value: undefined };

vi.mock('../config/mode', () => ({
  get isCloud() {
    return modeState.isCloud;
  },
}));

vi.mock('../bootstrapAdapter', () => ({
  getBootstrapUser: () => userState.user,
  getPlatformURL: () => platformURLState.value,
}));

let container: HTMLDivElement;
let root: Root;

// jsdom does not implement navigation — a `window.location.href = ...`
// assignment is a no-op. Stub the location object with a working href
// accessor so sign-out navigation can be asserted (same pattern as
// EscalationListener.test.tsx).
const originalLocation = window.location;
let hrefValue = '';

function stubLocation() {
  hrefValue = '';
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

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  modeState.isCloud = true;
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
  it('renders the avatar trigger with the identity initial in cloud mode', () => {
    act(() => {
      root.render(createElement(UserMenu));
    });

    const trigger = container.querySelector('.user-menu-trigger');
    expect(trigger).not.toBeNull();
    expect(trigger!.querySelector('.user-menu-avatar')!.textContent).toBe('A');
    expect(trigger!.getAttribute('aria-haspopup')).toBe('menu');
    expect(trigger!.getAttribute('aria-expanded')).toBe('false');
  });

  it('renders nothing when the bootstrap carried no user identity', () => {
    userState.user = undefined;

    act(() => {
      root.render(createElement(UserMenu));
    });

    expect(container.querySelector('.user-menu')).toBeNull();
  });

  it('renders nothing in local mode (matches local mode today: no identity surface)', () => {
    modeState.isCloud = false;

    act(() => {
      root.render(createElement(UserMenu));
    });

    expect(container.querySelector('.user-menu')).toBeNull();
  });

  it('opens the menu with identity, the four exit items, and Sign out', () => {
    act(() => {
      root.render(createElement(UserMenu));
    });

    act(() => {
      container.querySelector('.user-menu-trigger')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const list = container.querySelector('.user-menu-list');
    expect(list).not.toBeNull();
    expect(list!.getAttribute('role')).toBe('menu');
    expect(list!.querySelector('.user-menu-identity-email')!.textContent).toBe('a@b.com');
    expect(list!.querySelector('.user-menu-tier')!.textContent).toBe('pro');

    const items = list!.querySelectorAll('a[role="menuitem"]');
    expect(Array.from(items).map((a) => a.textContent)).toEqual(['Dashboard', 'Tasks', 'Billing', 'Manage Team']);
    expect(list!.querySelector('.user-menu-signout')!.textContent).toBe('Sign out');
  });

  it('builds absolute exit URLs when the platform base is known (tagged ?from=editor)', () => {
    platformURLState.value = 'https://platform.sprout.dev';

    act(() => {
      root.render(createElement(UserMenu));
    });
    act(() => {
      container.querySelector('.user-menu-trigger')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const links = container.querySelector('.user-menu-list')!.querySelectorAll('a[role="menuitem"]');
    expect(links[0].getAttribute('href')).toBe('https://platform.sprout.dev/?from=editor');
    expect(links[1].getAttribute('href')).toBe('https://platform.sprout.dev/tasks?from=editor');
    expect(links[2].getAttribute('href')).toBe('https://platform.sprout.dev/account/billing?from=editor');
    expect(links[3].getAttribute('href')).toBe('https://platform.sprout.dev/#/team?from=editor');
  });

  it('falls back to relative exit URLs when the platform base is absent', () => {
    platformURLState.value = undefined;

    act(() => {
      root.render(createElement(UserMenu));
    });
    act(() => {
      container.querySelector('.user-menu-trigger')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    const links = container.querySelector('.user-menu-list')!.querySelectorAll('a[role="menuitem"]');
    expect(links[0].getAttribute('href')).toBe('/?from=editor');
    expect(links[1].getAttribute('href')).toBe('/tasks?from=editor');
  });

  it('closes the menu on Escape and returns focus to the trigger', () => {
    act(() => {
      root.render(createElement(UserMenu));
    });

    const trigger = container.querySelector('.user-menu-trigger')!;
    act(() => {
      trigger.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
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

    act(() => {
      root.render(createElement(UserMenu));
    });
    act(() => {
      container.querySelector('.user-menu-trigger')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    act(() => {
      container.querySelector('.user-menu-signout')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    // Flush the async handler (await fetch → navigation).
    await act(async () => {
      await Promise.resolve();
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/webui/auth/logout'); // relative — no platform base set
    expect(init.method).toBe('POST');
    expect(window.location.href).toBe('/login');
  });

  it('builds the sign-out URL absolutely when the platform base is known', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValue(new Response(null, { status: 200 }));
    platformURLState.value = 'https://platform.sprout.dev';

    act(() => {
      root.render(createElement(UserMenu));
    });
    act(() => {
      container.querySelector('.user-menu-trigger')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    act(() => {
      container.querySelector('.user-menu-signout')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await act(async () => {
      await Promise.resolve();
    });

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('https://platform.sprout.dev/webui/auth/logout');
  });

  it('surfaces a notification and stays signed in when the logout request fails', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockRejectedValue(new Error('network down'));

    act(() => {
      root.render(createElement(UserMenu));
    });
    act(() => {
      container.querySelector('.user-menu-trigger')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    act(() => {
      container.querySelector('.user-menu-signout')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    // The async handler must finish before we assert the failed state.
    await act(async () => {
      await Promise.resolve();
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    // The session cookie is intact — navigation to /login must NOT happen.
    expect(window.location.href).toBe('');
    // The menu re-opens so the user can retry.
    expect(container.querySelector('.user-menu-list')).not.toBeNull();
  });
});
