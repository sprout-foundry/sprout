import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { HostProvider } from '../../host/HostProvider';
import { makeTestHost } from '../../host/testHost';
import type { HostNavigationIntent, SproutHost } from '../../host/types';
import { PLATFORM_ACCOUNT_ITEMS, PLATFORM_ADMIN_ITEM, PLATFORM_WORK_ITEMS, intentPath } from '../../host/platform';
import { __resetHomeViewForTests } from '../../services/homeView';
import HomeNav, { homePageLabel } from './HomeNav';

const { admin } = vi.hoisted(() => ({ admin: { value: false } }));
vi.mock('../../bootstrapAdapter', () => ({
  getBootstrapUser: () => ({ id: 'u', email: 'a@b.c', tier: 'pro', admin: admin.value }),
}));

/**
 * A host with the platform nav surface. The paths come from the host's own
 * `intentPath`, so the test resolves exactly as production does.
 */
function platformNavHost(accountItems = PLATFORM_ACCOUNT_ITEMS): { host: SproutHost; opened: HostNavigationIntent[] } {
  const opened: HostNavigationIntent[] = [];
  const host: SproutHost = {
    ...makeTestHost(),
    navigation: {
      open: (intent) => opened.push(intent),
      workItems: PLATFORM_WORK_ITEMS,
      accountItems,
      intentPath,
    },
  };
  return { host, opened };
}

let container: HTMLDivElement;
let root: Root;
beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});
beforeEach(() => {
  __resetHomeViewForTests();
  admin.value = false;
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});
afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const labels = () => Array.from(container.querySelectorAll('.project-nav-item')).map((b) => b.textContent?.trim());

function render(host: SproutHost) {
  act(() =>
    root.render(
      <HostProvider host={host}>
        <HomeNav path="/" projectLabel="acme/app" onBackToProject={() => undefined} />
      </HostProvider>,
    ),
  );
}

describe('HomeNav', () => {
  it('renders the Work and Account sections from the host items', () => {
    render(platformNavHost().host);
    expect(labels()).toEqual(
      expect.arrayContaining(['Dashboard', 'Tasks', 'Workspaces', 'Usage & billing', 'Team', 'Runners', 'Settings']),
    );
  });

  it('dispatches the clicked item intent through the host', () => {
    const { host, opened } = platformNavHost();
    render(host);
    const billing = Array.from(container.querySelectorAll<HTMLButtonElement>('.project-nav-item')).find((b) =>
      b.textContent?.includes('Usage & billing'),
    )!;
    act(() => billing.click());
    expect(opened).toEqual([{ type: 'usage' }]);
  });

  it('shows Admin to admins, sourced from the host account items', () => {
    admin.value = true;
    render(platformNavHost([...PLATFORM_ACCOUNT_ITEMS, PLATFORM_ADMIN_ITEM]).host);
    expect(labels()).toContain('Admin');
  });

  it('does not show Admin to non-admins', () => {
    render(platformNavHost().host);
    expect(labels()).not.toContain('Admin');
  });
});

describe('homePageLabel', () => {
  const items = [...PLATFORM_WORK_ITEMS, ...PLATFORM_ACCOUNT_ITEMS];
  it.each([
    ['/', 'Dashboard'],
    ['/tasks/abc', 'Tasks'],
    ['/scheduled', 'Home'],
    ['/account/billing', 'Usage & billing'],
    ['/admin', 'Admin'],
    ['/nowhere', 'Home'],
    ['/team?invite=abc', 'Team'],
  ])('%s → %s', (path, label) => {
    expect(homePageLabel(path, items, intentPath)).toBe(label);
  });
});
