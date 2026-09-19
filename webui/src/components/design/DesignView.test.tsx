/**
 * SP-140-3 item 3.3 — DesignView shell.
 *
 * Pins the shell contract the later tab items build on: the shell-controlled
 * `tab` prop drives the panel body (Flows, Screens, Tokens).
 *
 * SP-140-5: the in-view tab strip was removed — the mode's rail (the
 * sidebar) is the section control and drives `tab` from the shell — so
 * these tests drive the `tab` prop (and rerender with it) rather than
 * clicking tabs.
 *
 * SP-140-5 (unified layout): the assets rail lives in the sidebar's content
 * pane, backed by DesignWorkspaceContext. Selection tests go through the
 * provider — the same path the sidebar pane uses — instead of clicking
 * rows that used to sit in the surface. Standalone (no provider) the view
 * fetches and selects internally, which is what these tests render.
 *
 * SP-140-4 item 4.8 adds one check: a selection reaches the detail pane's
 * resolution flow (§4d), i.e. the pane the shell threads the feedback read/write
 * seams to is the one the annotation resolution lives on.
 */

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { SproutAdapterProvider } from '../../contexts/SproutAdapterContext';
import type * as designApiModule from '../../services/api/designApi';
import type { DesignInventory } from '../../services/api/types/design';
import DesignView, { DESIGN_TABS, type DesignTab } from './DesignView';
import { DesignWorkspaceProvider, useDesignWorkspace } from './DesignWorkspaceContext';

// The shell fetches the inventory itself; a fixture with one asset per class
// gives the tests real rows to select (the shell-stage stub row is gone).
vi.mock('../../services/api/designApi', async (importOriginal) => {
  const actual = (await importOriginal()) as typeof designApiModule;
  const entry = (path: string, kind: string) => ({
    path,
    name: path.split('/').pop(),
    kind,
    size: 128,
    modified: 0,
    status: '',
  });
  const inventory: DesignInventory = {
    exists: true,
    wireframes: [],
    layouts: [],
    screens: [entry('screens/inbox.html', 'screen')],
    flows: [entry('flows/sign-up.mmd', 'flow')],
    tokenFiles: [entry('tokens/colors.json', 'tokens')],
    flowSummaries: [],
    feedback: [],
  };
  return { ...actual, listAssets: vi.fn().mockResolvedValue(inventory) };
});

/**
 * Renders DesignView inside the workspace provider (the app's composition).
 * Returns a `rerenderTab` helper to switch the provider's section (the
 * mode-rail click path) plus the sidebar-selection simulator.
 */
function renderWorkspace(props: Partial<React.ComponentProps<typeof DesignView>> = {}) {
  let select: ((path: string | null) => void) | null = null;
  let currentTab: DesignTab = 'flows';
  let viewProps = props;

  function SelectionGrabber() {
    const workspace = useDesignWorkspace();
    if (workspace) select = workspace.select;
    return null;
  }

  function tree(tab: DesignTab) {
    return (
      <SproutAdapterProvider>
        <DesignWorkspaceProvider tab={tab} active>
          <SelectionGrabber />
          <DesignView {...viewProps} tab={tab} />
        </DesignWorkspaceProvider>
      </SproutAdapterProvider>
    );
  }

  const rendered = render(tree(currentTab));
  return {
    selectFromSidebar: (path: string | null) => {
      act(() => {
        select?.(path);
      });
    },
    rerenderTab: (tab: DesignTab) => {
      currentTab = tab;
      rendered.rerender(tree(tab));
    },
    setProps: (next: Partial<React.ComponentProps<typeof DesignView>>) => {
      viewProps = { ...viewProps, ...next };
      rendered.rerender(tree(currentTab));
    },
  };
}

function renderDesign(props: Partial<React.ComponentProps<typeof DesignView>> = {}) {
  return render(
    <SproutAdapterProvider>
      <DesignView {...props} />
    </SproutAdapterProvider>,
  );
}

/** Render with a controlled `tab`, returning a rerender helper for the same. */
function renderControlled(tab: DesignTab) {
  const rendered = render(
    <SproutAdapterProvider>
      <DesignView tab={tab} />
    </SproutAdapterProvider>,
  );
  const setTab = (next: DesignTab) =>
    rendered.rerender(
      <SproutAdapterProvider>
        <DesignView tab={next} />
      </SproutAdapterProvider>,
    );
  return { ...rendered, setTab };
}

describe('DesignView shell', () => {
  it('renders the view root', () => {
    renderDesign();
    expect(screen.getByTestId('design-view')).toBeInTheDocument();
  });

  it('exposes exactly three sections in order: Flows, Screens, Tokens', () => {
    expect(DESIGN_TABS.map((t) => t.id)).toEqual(['flows', 'screens', 'tokens']);
    expect(DESIGN_TABS.map((t) => t.label)).toEqual(['Flows', 'Screens', 'Tokens']);

    const { setTab } = renderControlled('flows');
    for (const spec of DESIGN_TABS) {
      setTab(spec.id);
      expect(screen.getByTestId('design-view')).toHaveAttribute('data-active-tab', spec.id);
    }
  });

  it('defaults to the Flows tab', () => {
    renderDesign();
    expect(screen.getByTestId('design-view')).toHaveAttribute('data-active-tab', 'flows');
    expect(screen.getByTestId('design-flows-canvas')).toBeInTheDocument();
    expect(screen.queryByTestId('design-screens-grid')).not.toBeInTheDocument();
    expect(screen.queryByTestId('design-tokens-tree')).not.toBeInTheDocument();
  });

  it('honours a controlled tab', () => {
    renderDesign({ tab: 'tokens' });
    expect(screen.getByTestId('design-view')).toHaveAttribute('data-active-tab', 'tokens');
    expect(screen.getByTestId('design-tokens-tree')).toBeInTheDocument();
    expect(screen.queryByTestId('design-flows-canvas')).not.toBeInTheDocument();
  });

  it('switches the rendered panel body when the controlled tab changes', () => {
    const { setTab } = renderControlled('flows');
    expect(screen.getByTestId('design-flows-canvas')).toBeInTheDocument();

    setTab('screens');
    expect(screen.getByTestId('design-screens-grid')).toBeInTheDocument();
    expect(screen.queryByTestId('design-flows-canvas')).not.toBeInTheDocument();
    expect(screen.getByTestId('design-view')).toHaveAttribute('data-active-tab', 'screens');

    setTab('tokens');
    expect(screen.getByTestId('design-tokens-tree')).toBeInTheDocument();
    expect(screen.queryByTestId('design-screens-grid')).not.toBeInTheDocument();
  });

  it('keeps the tabpanel labelled by the active section', () => {
    const { setTab } = renderControlled('flows');
    const panel = screen.getByTestId('design-tabpanel');
    expect(panel).toHaveAttribute('aria-label', 'flows panel');

    setTab('tokens');
    expect(screen.getByTestId('design-tabpanel')).toHaveAttribute('aria-label', 'tokens panel');
  });

  it('renders no in-surface assets rail: the sidebar owns the asset browser', () => {
    renderDesign();
    expect(screen.queryByTestId('design-assets-rail')).not.toBeInTheDocument();
    expect(screen.getByTestId('design-detail-pane')).toBeInTheDocument();
    // No asset selected yet.
    expect(screen.getByTestId('design-detail-content')).toHaveAttribute('data-selected', '');
  });

  it('a selection from the shared workspace reaches the detail pane and opens in the editor', async () => {
    const onOpenFile = vi.fn();
    const { selectFromSidebar } = renderWorkspace({ onOpenFile });

    expect(screen.queryByText('Open in editor')).not.toBeInTheDocument();

    selectFromSidebar('flows/sign-up.mmd');

    const detail = screen.getByTestId('design-detail-content');
    expect(detail).toHaveAttribute('data-selected', 'flows/sign-up.mmd');

    fireEvent.click(screen.getByText('Open in editor'));
    expect(onOpenFile).toHaveBeenCalledWith('flows/sign-up.mmd');
  });

  it('selection is section-scoped: another section does not keep filling the pane', async () => {
    const { selectFromSidebar, rerenderTab } = renderWorkspace();
    selectFromSidebar('flows/sign-up.mmd');
    expect(screen.getByTestId('design-detail-content')).toHaveAttribute('data-selected', 'flows/sign-up.mmd');

    // Switching to Screens leaves the flow selection behind: the pane goes
    // idle rather than showing a flows asset on the Screens tab.
    rerenderTab('screens');
    expect(screen.getByTestId('design-detail-content')).toHaveAttribute('data-selected', '');
    expect(screen.getByTestId('design-detail-pane')).toHaveAttribute('data-idle', 'true');
  });
});

describe('DesignView resolution flow wiring (SP-140-4 §4d)', () => {
  it('mounts the detail pane resolution flow for the selected asset', async () => {
    const readFn = vi.fn().mockResolvedValue({
      ok: true,
      status: 404,
      text: async () => '',
    } as unknown as Response);
    const { selectFromSidebar } = renderWorkspace({ readFn });

    expect(screen.queryByTestId('design-feedback-resolution')).toBeNull();

    selectFromSidebar('flows/sign-up.mmd');

    const section = await screen.findByTestId('design-feedback-resolution');
    expect(section.getAttribute('data-target')).toBe('design/flows/sign-up.mmd');
    // The shell's readFn seam reaches the pane's reader.
    await waitFor(() => expect(readFn).toHaveBeenCalled());
  });
});
