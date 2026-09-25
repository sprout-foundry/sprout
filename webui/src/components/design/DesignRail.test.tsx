// @ts-nocheck
/**
 * SP-140-8 §8a — the data-driven Design rail (item 8.1).
 *
 * Drives DesignRail from a mocked workspace context and pins the screen-centric
 * IA: the Screens group lists the wireframe stems with a status dot and an
 * open-annotation badge; selecting a screen drives the shared selection and
 * lands on the Screens section; the Library group switches kind sections;
 * and the rail degrades gracefully: a tree with no screens renders the
 * group with its §8d empty marker, while no workspace context or no
 * design/ tree renders the Library group only.
 *
 * Status source mirrors the data layer: the README status marker is
 * populated on the inventory's `screens` entries (designApi.listAssets keys
 * it by lowercased screen stem), not on wireframes. The fixture therefore
 * carries `status` on `screens` only; the rail derives a wireframe stem's dot
 * from the same-stem screen. A stem with no screen renders no dot.
 */

import { render, screen, fireEvent } from '@testing-library/react';
import React from 'react';
import DesignRail from './DesignRail';

const { wsFixture } = vi.hoisted(() => ({ wsFixture: { value: null } }));

vi.mock('./DesignWorkspaceContext', () => ({
  __esModule: true,
  useDesignWorkspace: () => wsFixture.value,
  DesignWorkspaceProvider: ({ children }) => children,
}));

const fixtureWorkspace = () => ({
  inventory: {
    exists: true,
    // Wireframes carry no status in production (the data layer keys the README
    // marker by screen stem and populates it on `screens`).
    wireframes: [
      { path: 'design/wireframes/login.svg', name: 'login.svg', kind: 'wireframe', size: 1, modified: 1 },
      { path: 'design/wireframes/dashboard.svg', name: 'dashboard.svg', kind: 'wireframe', size: 1, modified: 1 },
      { path: 'design/wireframes/checkout.svg', name: 'checkout.svg', kind: 'wireframe', size: 1, modified: 1 },
    ],
    // The status source: the same-stem screen carries the README marker.
    // `checkout` has no screen entry, so its stem renders no status dot.
    screens: [
      { path: 'design/screens/login.html', name: 'login.html', kind: 'screen', size: 1, modified: 1, status: 'review' },
      {
        path: 'design/screens/dashboard.html',
        name: 'dashboard.html',
        kind: 'screen',
        size: 1,
        modified: 1,
        status: 'ready',
      },
    ],
    feedback: [
      {
        name: 'login.json',
        path: 'design/feedback/login.json',
        status: 'changes-requested',
        annotationCount: 3,
        resolvedCount: 1,
      },
      {
        name: 'dashboard.json',
        path: 'design/feedback/dashboard.json',
        status: 'resolved',
        annotationCount: 2,
        resolvedCount: 2,
      },
    ],
  },
  selected: 'design/wireframes/login.svg',
  select: vi.fn(),
});

function renderRail(activeId = 'screens', opts: { select?: () => void; onSelect?: (id: string) => void } = {}) {
  const select = opts.select ?? vi.fn();
  const onSelect = opts.onSelect ?? vi.fn();
  wsFixture.value = { ...fixtureWorkspace(), select };
  const view = render(<DesignRail activeId={activeId} onSelect={onSelect} />);
  return { ...view, select, onSelect };
}

describe('DesignRail (SP-140-8 §8a)', () => {
  it('lists the wireframe stems with a status dot and open-annotation badge', () => {
    renderRail('screens');

    // Every stem renders as a screen entry.
    expect(screen.getByTestId('design-rail-screen-login')).toBeTruthy();
    expect(screen.getByTestId('design-rail-screen-dashboard')).toBeTruthy();
    expect(screen.getByTestId('design-rail-screen-checkout')).toBeTruthy();

    // Status dots reflect the README marker, sourced from the same-stem
    // `screens` entry (the data layer populates status there, not on
    // wireframes). checkout has no screen, so it renders no dot.
    expect(screen.getByTestId('design-rail-screen-login').querySelector('[data-status="review"]')).toBeTruthy();
    expect(screen.getByTestId('design-rail-screen-dashboard').querySelector('[data-status="ready"]')).toBeTruthy();
    expect(screen.getByTestId('design-rail-screen-checkout').querySelector('[data-status]')).toBeNull();

    // Open-annotation badge: login has 3-1=2 open, dashboard has 0 (none shown).
    expect(screen.getByTestId('design-rail-screen-login').querySelector('[data-open-count="2"]')).toBeTruthy();
    expect(screen.getByTestId('design-rail-screen-dashboard').querySelector('[data-open-count]')).toBeNull();
  });

  it('selecting a screen drives the shared selection and lands on Screens', () => {
    const { select, onSelect } = renderRail('flows');

    fireEvent.click(screen.getByTestId('design-rail-screen-dashboard'));

    expect(select).toHaveBeenCalledTimes(1);
    expect(select).toHaveBeenCalledWith('design/wireframes/dashboard.svg');
    // The seam: the screen pick also drives the section to 'screens'.
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith('screens');
  });

  it('a Library entry switches its section without touching the selection', () => {
    const { select, onSelect } = renderRail('tokens');

    fireEvent.click(screen.getByTestId('design-rail-tokens'));

    expect(select).not.toHaveBeenCalled();
    expect(onSelect).toHaveBeenCalledWith('tokens');
  });

  it('the Screens section entry switches the section without selecting a screen', () => {
    // Post-8.2 a selected screen renders the workbench, so the grid is only
    // reachable through a selection-free section switch.
    const { select, onSelect } = renderRail('flows');

    fireEvent.click(screen.getByTestId('design-rail-screens-section'));

    expect(select).not.toHaveBeenCalled();
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith('screens');
  });

  it('marks the active screen when the selection matches', () => {
    renderRail('screens');

    expect(screen.getByTestId('design-rail-screen-login').getAttribute('aria-selected')).toBe('true');
    expect(screen.getByTestId('design-rail-screen-dashboard').getAttribute('aria-selected')).toBe('false');
  });

  it('renders the Screens group with its empty marker when a tree exists but has no screens', () => {
    // §8d: tokens-only tree — the group stays with a "no screens yet"
    // marker; the Library views remain the default content.
    wsFixture.value = {
      inventory: { exists: true, wireframes: [], feedback: [] },
      selected: null,
      select: vi.fn(),
    };
    render(<DesignRail activeId="flows" onSelect={vi.fn()} />);

    expect(screen.getByTestId('design-rail-screens')).toBeTruthy();
    const empty = screen.getByTestId('design-rail-screens-empty');
    expect(empty.getAttribute('aria-label')).toBe('No screens yet');
    expect(empty.getAttribute('title')).toBe('No screens yet — the token palette is ready');
    // No interactive screen entries, and the Library group is still reachable.
    expect(screen.queryByTestId(/^design-rail-screen-/)).toBeNull();
    expect(screen.getByTestId('design-rail-tokens')).toBeTruthy();
    expect(screen.getByTestId('design-rail-flows')).toBeTruthy();
  });

  it('omits the Screens group when there is no workspace context', () => {
    wsFixture.value = null;
    render(<DesignRail activeId="flows" onSelect={vi.fn()} />);

    expect(screen.queryByTestId('design-rail-screens')).toBeNull();
    expect(screen.getByTestId('design-rail-tokens')).toBeTruthy();
    expect(screen.getByTestId('design-rail-flows')).toBeTruthy();
  });

  it('omits the Screens group when no design/ tree exists', () => {
    wsFixture.value = {
      inventory: { exists: false, wireframes: [], feedback: [] },
      selected: null,
      select: vi.fn(),
    };
    render(<DesignRail activeId="flows" onSelect={vi.fn()} />);

    expect(screen.queryByTestId('design-rail-screens')).toBeNull();
    expect(screen.queryByTestId('design-rail-screens-empty')).toBeNull();
    expect(screen.getByTestId('design-rail-tokens')).toBeTruthy();
    expect(screen.getByTestId('design-rail-flows')).toBeTruthy();
  });
});
