import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { HostProvider } from '../../host/HostProvider';
import { makeTestHost } from '../../host/testHost';
import { PLATFORM_WORK_ITEMS, intentPath } from '../../host/platform';
import { __resetHomeViewForTests, openHome } from '../../services/homeView';
import PlatformHome from './PlatformHome';

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
  const host = {
    ...makeTestHost(),
    navigation: { open: () => undefined, workItems: PLATFORM_WORK_ITEMS, intentPath },
  };
  act(() =>
    root.render(
      <HostProvider host={host}>
        <PlatformHome />
      </HostProvider>,
    ),
  );
}

describe('PlatformHome', () => {
  it('loads the page Home first opens to', () => {
    render();
    expect(container.querySelector('iframe')).toBeNull();

    act(() => openHome('/account/billing'));
    expect(container.querySelector('iframe')?.getAttribute('src')).toContain('#/account/billing');
  });

  it('labels the mobile bar from the host nav items', () => {
    render();
    act(() => openHome('/tasks'));
    // Re-render with the mobile bar; the label comes from the host's items.
    const host = {
      ...makeTestHost(),
      navigation: { open: () => undefined, workItems: PLATFORM_WORK_ITEMS, intentPath },
    };
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
