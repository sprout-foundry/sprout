import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../../services/activeRepo', () => ({ useActiveRepoURL: () => 'https://github.com/acme/app' }));
vi.mock('../UserMenu', () => ({ UserMenu: ({ label }: { label?: string }) => <button type="button">{label}</button> }));

import { OPEN_COMMAND_PALETTE_EVENT, OPEN_NOTIFICATIONS_EVENT } from '../../config/layout';
import { __resetHomeViewForTests, getHomeView, openHome } from '../../services/homeView';
import PhoneTabBar from './PhoneTabBar';

let container: HTMLDivElement;
let root: Root;
const drawer = { open: false, toggle: vi.fn(), close: vi.fn() };

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});
beforeEach(() => {
  __resetHomeViewForTests();
  drawer.open = false;
  drawer.toggle.mockReset();
  drawer.close.mockReset();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});
afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
});

function render() {
  act(() =>
    root.render(<PhoneTabBar drawerOpen={drawer.open} onToggleDrawer={drawer.toggle} onCloseDrawer={drawer.close} />),
  );
}

const tab = (label: string) =>
  Array.from(container.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.textContent === label)!;
const activeTab = () => container.querySelector('.phone-tab.active')?.textContent;

describe('PhoneTabBar', () => {
  it('goes Home and back to the project', () => {
    render();
    expect(activeTab()).toBe('app');

    act(() => tab('Home').click());
    expect(getHomeView().open).toBe(true);
    expect(activeTab()).toBe('Home');

    act(() => tab('app').click());
    expect(getHomeView().open).toBe(false);
    expect(drawer.toggle).not.toHaveBeenCalled();
  });

  it("opens the project's sections from the project tab once you're in it", () => {
    render();
    act(() => tab('app').click());
    expect(drawer.toggle).toHaveBeenCalledTimes(1);
  });

  it('closes an open drawer when going elsewhere', () => {
    drawer.open = true;
    render();
    act(() => tab('Home').click());
    expect(drawer.close).toHaveBeenCalledTimes(1);
  });

  it('opens notifications and search, and search shows the project', () => {
    const heard: string[] = [];
    const listen = (name: string) => {
      const fn = () => heard.push(name);
      window.addEventListener(name, fn);
      return () => window.removeEventListener(name, fn);
    };
    const stop = [listen(OPEN_NOTIFICATIONS_EVENT), listen(OPEN_COMMAND_PALETTE_EVENT)];
    openHome('/tasks');
    render();

    act(() => tab('Activity').click());
    act(() => tab('Search').click());

    expect(heard).toEqual([OPEN_NOTIFICATIONS_EVENT, OPEN_COMMAND_PALETTE_EVENT]);
    expect(getHomeView().open).toBe(false);
    stop.forEach((s) => s());
  });

  it('reserves bottom space while shown and steps aside for the keyboard', () => {
    const viewport = Object.assign(new EventTarget(), { height: window.innerHeight });
    vi.stubGlobal('visualViewport', viewport);
    render();
    expect(container.querySelector('[data-testid="phone-tab-bar"]')).not.toBeNull();
    expect(document.documentElement.classList.contains('phone-tab-bar-shown')).toBe(true);

    act(() => {
      viewport.height = window.innerHeight - 300;
      viewport.dispatchEvent(new Event('resize'));
    });
    expect(container.querySelector('[data-testid="phone-tab-bar"]')).toBeNull();
    expect(document.documentElement.classList.contains('phone-tab-bar-shown')).toBe(false);
  });
});
