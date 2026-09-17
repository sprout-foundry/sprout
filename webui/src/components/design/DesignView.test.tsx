/**
 * SP-140-3 item 3.3 — DesignView shell.
 *
 * Pins the shell contract the later tab items build on: three tabs render
 * (Flows, Screens, Tokens), the tab state swaps the panel body, and the
 * three-pane layout (rail / canvas / detail) is present.
 *
 * SP-140-4 item 4.8 adds one check: a rail selection reaches the detail pane's
 * resolution flow (§4d), i.e. the pane the shell threads the feedback read/write
 * seams to is the one the annotation resolution lives on.
 */

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { SproutAdapterProvider } from '../../contexts/SproutAdapterContext';
import DesignView, { DESIGN_TABS } from './DesignView';

function renderDesign(props: Partial<React.ComponentProps<typeof DesignView>> = {}) {
  return render(
    <SproutAdapterProvider>
      <DesignView {...props} />
    </SproutAdapterProvider>,
  );
}

describe('DesignView shell', () => {
  it('renders the view root', () => {
    renderDesign();
    expect(screen.getByTestId('design-view')).toBeInTheDocument();
  });

  it('exposes exactly three tabs in order: Flows, Screens, Tokens', () => {
    expect(DESIGN_TABS.map((t) => t.id)).toEqual(['flows', 'screens', 'tokens']);
    expect(DESIGN_TABS.map((t) => t.label)).toEqual(['Flows', 'Screens', 'Tokens']);

    renderDesign();
    expect(screen.getByTestId('design-tab-flows')).toHaveTextContent('Flows');
    expect(screen.getByTestId('design-tab-screens')).toHaveTextContent('Screens');
    expect(screen.getByTestId('design-tab-tokens')).toHaveTextContent('Tokens');
    expect(screen.getAllByRole('tab')).toHaveLength(3);
  });

  it('defaults to the Flows tab', () => {
    renderDesign();
    expect(screen.getByTestId('design-view')).toHaveAttribute('data-active-tab', 'flows');
    expect(screen.getByTestId('design-tab-flows')).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('design-tab-screens')).toHaveAttribute('aria-selected', 'false');
    expect(screen.getByTestId('design-flows-canvas')).toBeInTheDocument();
  });

  it('honours an initialTab override', () => {
    renderDesign({ initialTab: 'tokens' });
    expect(screen.getByTestId('design-view')).toHaveAttribute('data-active-tab', 'tokens');
    expect(screen.getByTestId('design-tokens-tree')).toBeInTheDocument();
    expect(screen.queryByTestId('design-flows-canvas')).not.toBeInTheDocument();
  });

  it('switches the rendered panel body when a tab is clicked', () => {
    renderDesign();

    fireEvent.click(screen.getByTestId('design-tab-screens'));
    expect(screen.getByTestId('design-screens-grid')).toBeInTheDocument();
    expect(screen.queryByTestId('design-flows-canvas')).not.toBeInTheDocument();
    expect(screen.getByTestId('design-tab-screens')).toHaveAttribute('aria-selected', 'true');

    fireEvent.click(screen.getByTestId('design-tab-tokens'));
    expect(screen.getByTestId('design-tokens-tree')).toBeInTheDocument();
    expect(screen.queryByTestId('design-screens-grid')).not.toBeInTheDocument();
  });

  it('keeps the tabpanel wired to the active tab', () => {
    renderDesign();
    const panel = screen.getByTestId('design-tabpanel');
    expect(panel).toHaveAttribute('aria-labelledby', 'design-tab-flows');

    fireEvent.click(screen.getByTestId('design-tab-tokens'));
    expect(screen.getByTestId('design-tabpanel')).toHaveAttribute('aria-labelledby', 'design-tab-tokens');
  });

  it('renders the left rail and right detail pane', () => {
    renderDesign();
    expect(screen.getByTestId('design-assets-rail')).toBeInTheDocument();
    expect(screen.getByTestId('design-detail-pane')).toBeInTheDocument();
    // No asset selected yet.
    expect(screen.getByTestId('design-detail-content')).toHaveAttribute('data-selected', '');
  });

  it('rail selection flows into the detail pane and can open the asset in the editor', () => {
    const onOpenFile = vi.fn();
    renderDesign({ onOpenFile });

    expect(screen.queryByText('Open in editor')).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId('design-rail-stub-row'));

    const detail = screen.getByTestId('design-detail-content');
    expect(detail).toHaveAttribute('data-selected', 'flows/');

    fireEvent.click(screen.getByText('Open in editor'));
    expect(onOpenFile).toHaveBeenCalledWith('flows/');
  });

  it('renders the back affordance only when onBack is provided', () => {
    const onBack = vi.fn();
    const { rerender } = renderDesign();
    expect(screen.queryByLabelText('Back to chat')).not.toBeInTheDocument();

    rerender(
      <SproutAdapterProvider>
        <DesignView onBack={onBack} />
      </SproutAdapterProvider>,
    );
    fireEvent.click(screen.getByLabelText('Back to chat'));
    expect(onBack).toHaveBeenCalledTimes(1);
  });
});

describe('DesignView resolution flow wiring (SP-140-4 §4d)', () => {
  it('mounts the detail pane resolution flow for the selected asset', async () => {
    const readFn = vi.fn().mockResolvedValue({
      ok: true,
      status: 404,
      text: async () => '',
    } as unknown as Response);
    renderDesign({ readFn });

    expect(screen.queryByTestId('design-feedback-resolution')).toBeNull();

    fireEvent.click(screen.getByTestId('design-rail-stub-row'));

    const section = await screen.findByTestId('design-feedback-resolution');
    expect(section.getAttribute('data-target')).toBe('design/flows/');
    // The shell's readFn seam reaches the pane's reader.
    await waitFor(() => expect(readFn).toHaveBeenCalled());
  });
});
