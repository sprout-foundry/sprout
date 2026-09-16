/**
 * SP-140-3 item 3.3 — DesignView shell.
 *
 * Pins the shell contract the later tab items build on: three tabs render
 * (Flows, Screens, Tokens), the tab state swaps the panel body, and the
 * three-pane layout (rail / canvas / detail) is present.
 */

import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import DesignView, { DESIGN_TABS } from './DesignView';

describe('DesignView shell', () => {
  it('renders the view root', () => {
    render(<DesignView />);
    expect(screen.getByTestId('design-view')).toBeInTheDocument();
  });

  it('exposes exactly three tabs in order: Flows, Screens, Tokens', () => {
    expect(DESIGN_TABS.map((t) => t.id)).toEqual(['flows', 'screens', 'tokens']);
    expect(DESIGN_TABS.map((t) => t.label)).toEqual(['Flows', 'Screens', 'Tokens']);

    render(<DesignView />);
    expect(screen.getByTestId('design-tab-flows')).toHaveTextContent('Flows');
    expect(screen.getByTestId('design-tab-screens')).toHaveTextContent('Screens');
    expect(screen.getByTestId('design-tab-tokens')).toHaveTextContent('Tokens');
    expect(screen.getAllByRole('tab')).toHaveLength(3);
  });

  it('defaults to the Flows tab', () => {
    render(<DesignView />);
    expect(screen.getByTestId('design-view')).toHaveAttribute('data-active-tab', 'flows');
    expect(screen.getByTestId('design-tab-flows')).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('design-tab-screens')).toHaveAttribute('aria-selected', 'false');
    expect(screen.getByTestId('design-flows-canvas')).toBeInTheDocument();
  });

  it('honours an initialTab override', () => {
    render(<DesignView initialTab="tokens" />);
    expect(screen.getByTestId('design-view')).toHaveAttribute('data-active-tab', 'tokens');
    expect(screen.getByTestId('design-tokens-tree')).toBeInTheDocument();
    expect(screen.queryByTestId('design-flows-canvas')).not.toBeInTheDocument();
  });

  it('switches the rendered panel body when a tab is clicked', () => {
    render(<DesignView />);

    fireEvent.click(screen.getByTestId('design-tab-screens'));
    expect(screen.getByTestId('design-screens-grid')).toBeInTheDocument();
    expect(screen.queryByTestId('design-flows-canvas')).not.toBeInTheDocument();
    expect(screen.getByTestId('design-tab-screens')).toHaveAttribute('aria-selected', 'true');

    fireEvent.click(screen.getByTestId('design-tab-tokens'));
    expect(screen.getByTestId('design-tokens-tree')).toBeInTheDocument();
    expect(screen.queryByTestId('design-screens-grid')).not.toBeInTheDocument();
  });

  it('keeps the tabpanel wired to the active tab', () => {
    render(<DesignView />);
    const panel = screen.getByTestId('design-tabpanel');
    expect(panel).toHaveAttribute('aria-labelledby', 'design-tab-flows');

    fireEvent.click(screen.getByTestId('design-tab-tokens'));
    expect(screen.getByTestId('design-tabpanel')).toHaveAttribute('aria-labelledby', 'design-tab-tokens');
  });

  it('renders the left rail and right detail pane', () => {
    render(<DesignView />);
    expect(screen.getByTestId('design-assets-rail')).toBeInTheDocument();
    expect(screen.getByTestId('design-detail-pane')).toBeInTheDocument();
    // No asset selected yet.
    expect(screen.getByTestId('design-detail-content')).toHaveAttribute('data-selected', '');
  });

  it('rail selection flows into the detail pane and can open the asset in the editor', () => {
    const onOpenFile = vi.fn();
    render(<DesignView onOpenFile={onOpenFile} />);

    expect(screen.queryByText('Open in editor')).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId('design-rail-stub-row'));

    const detail = screen.getByTestId('design-detail-content');
    expect(detail).toHaveAttribute('data-selected', 'flows/');

    fireEvent.click(screen.getByText('Open in editor'));
    expect(onOpenFile).toHaveBeenCalledWith('flows/');
  });

  it('renders the back affordance only when onBack is provided', () => {
    const onBack = vi.fn();
    const { rerender } = render(<DesignView />);
    expect(screen.queryByLabelText('Back to chat')).not.toBeInTheDocument();

    rerender(<DesignView onBack={onBack} />);
    fireEvent.click(screen.getByLabelText('Back to chat'));
    expect(onBack).toHaveBeenCalledTimes(1);
  });
});
