/**
 * The "Report a bug" button dispatches the host's `reportBug` intent — it
 * never hardcodes a repository URL, so a host can resolve it to its own
 * support flow.
 */

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { setActiveHost } from '../host/accessor';
import { HostProvider } from '../host/HostProvider';
import { makeTestHost } from '../host/testHost';
import type { HostNavigationIntent, SproutHost } from '../host/types';
import { __resetHomeViewForTests, getHomeView } from '../services/homeView';
import { ReportBugButton } from './ReportBugButton';

let container: HTMLDivElement;
let root: Root;
let opened: HostNavigationIntent[];

function host(intentPath?: (intent: HostNavigationIntent) => string | null): SproutHost {
  return {
    ...makeTestHost(),
    navigation: {
      open(intent) {
        opened.push(intent);
      },
      intentPath,
    },
  };
}

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  __resetHomeViewForTests();
  opened = [];
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

function render(h: SproutHost = host()) {
  // The button reads the host from context but the embeddable-page seam
  // (openPlatformPage) reads the module-level active host, so record the same
  // host in both channels.
  setActiveHost(h);
  act(() => {
    root.render(
      <HostProvider host={h}>
        <ReportBugButton />
      </HostProvider>,
    );
  });
}

describe('ReportBugButton', () => {
  it('renders a visible "Report a bug" affordance', () => {
    render();
    const button = container.querySelector('[data-testid="report-bug-button"]') as HTMLButtonElement;
    expect(button).not.toBeNull();
    expect(button.textContent).toContain('Report a bug');
    expect(button.getAttribute('aria-label')).toBe('Report a bug');
  });

  it('dispatches the reportBug intent on click (no hardcoded URL)', () => {
    render();
    const button = container.querySelector('[data-testid="report-bug-button"]') as HTMLButtonElement;
    act(() => {
      button.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(opened.map((i) => i.type)).toEqual(['reportBug']);
  });

  it('opens the host-resolved page in the shell (layered) instead of dispatching', () => {
    // The layered layout embeds a host's outward page in Home; the button asks
    // the host to resolve the intent to a route and opens it there, so the
    // editor never navigates away and the host is not asked to leave. The
    // embeddable-page seam only applies to a hosted transport (authMode
    // 'bearer'), matching every other platform link.
    const hosted: SproutHost = {
      ...host(() => '/?from=editor#/support'),
      transport: { ...makeTestHost().transport, authMode: 'bearer' },
    };
    render(hosted);
    const button = container.querySelector('[data-testid="report-bug-button"]') as HTMLButtonElement;
    act(() => {
      button.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(getHomeView()).toEqual({ open: true, path: '/support' });
    expect(opened).toEqual([]);
  });

  it('supports an icon-only variant for compact surfaces', () => {
    act(() => {
      root.render(
        <HostProvider host={host()}>
          <ReportBugButton iconOnly testId="compact-report-bug" />
        </HostProvider>,
      );
    });
    const button = container.querySelector('[data-testid="compact-report-bug"]') as HTMLButtonElement;
    expect(button).not.toBeNull();
    expect(button.querySelector('.report-bug-label')).toBeNull();
  });
});
