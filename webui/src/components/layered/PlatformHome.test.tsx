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
});
