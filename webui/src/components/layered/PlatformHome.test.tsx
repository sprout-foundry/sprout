import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { HostProvider } from '../../host/HostProvider';
import { makeTestHost } from '../../host/testHost';
import { PLATFORM_WORK_ITEMS, intentPath, platformEmbedPagePath } from '../../host/platform';
import { __resetHomeViewForTests, openHome } from '../../services/homeView';
import PlatformHome from './PlatformHome';

// The host resolves a page route to its own embeddable page URL (the embed
// decoration `?embed=1#…` lives on the platform side, not in this component).
const host = {
  ...makeTestHost(),
  navigation: {
    open: () => undefined,
    workItems: PLATFORM_WORK_ITEMS,
    intentPath,
    embedPagePath: platformEmbedPagePath,
  },
};

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  __resetHomeViewForTests();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

function render() {
  act(() =>
    root.render(
      <HostProvider host={host}>
        <PlatformHome />
      </HostProvider>,
    ),
  );
}

describe('PlatformHome', () => {
  it('loads the host-resolved embed URL for the page Home first opens to', () => {
    render();
    expect(container.querySelector('iframe')).toBeNull();

    act(() => openHome('/account/billing'));
    // The host decorates the route; Sprout loads exactly what it returns.
    expect(container.querySelector('iframe')?.getAttribute('src')).toBe('/?embed=1#/account/billing');
  });

  it('labels the mobile bar from the host nav items', () => {
    render();
    act(() => openHome('/tasks'));
    // Re-render with the mobile bar; the label comes from the host's items.
    act(() =>
      root.render(
        <HostProvider host={host}>
          <PlatformHome isMobile />
        </HostProvider>,
      ),
    );
    expect(container.querySelector('.platform-home-mobile-bar')?.textContent).toContain('Tasks');
  });

  it("offsets Home below the editor bar in the app's real nesting", () => {
    // SP-160 §160a: the app renders its space through `SproutWorkspace`, so the
    // space's <main> is a DOM grandchild of `.app`
    // (`.app > .sprout-workspace > main > .header-bar`). The offset query must
    // be a descendant query for that nesting, or `top` silently stays 0 and
    // Home hides under the editor bar.
    const app = document.createElement('div');
    app.className = 'app';
    const workspaceRoot = document.createElement('div');
    workspaceRoot.className = 'sprout-workspace';
    const main = document.createElement('main');
    main.className = 'main-content';
    const header = document.createElement('div');
    header.className = 'header-bar';
    // jsdom does no layout, so offsetHeight is 0; a stub makes the height real
    // and lets the assertion distinguish "matched" from "not matched".
    Object.defineProperty(header, 'offsetHeight', { value: 35, configurable: true });
    main.appendChild(header);
    workspaceRoot.appendChild(main);
    app.appendChild(workspaceRoot);
    document.body.appendChild(app);

    try {
      render();
      act(() => openHome('/account/billing'));
      const home = container.querySelector<HTMLElement>('[data-testid="platform-home"]');
      expect(home?.style.top).toBe('35px');
    } finally {
      app.remove();
    }
  });
});
