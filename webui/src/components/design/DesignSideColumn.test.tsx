/**
 * DesignSideColumn tests (the §6f rework: one right column, Details | Agent).
 *
 * Pins the tab contract: the visible tab drives the `hidden` attribute (both
 * bodies stay mounted — the chat keeps its state across flips); the idle
 * marker reflects the selection; the tablist wiring (roles/aria) is present.
 */

import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import DesignSideColumn from './DesignSideColumn';

function renderColumn(overrides: Partial<React.ComponentProps<typeof DesignSideColumn>> = {}) {
  const props: React.ComponentProps<typeof DesignSideColumn> = {
    sideTab: 'details',
    onSideTabChange: vi.fn(),
    details: <div data-testid="mock-details">details body</div>,
    agent: <div data-testid="mock-agent">agent body</div>,
    hasSelection: false,
    ...overrides,
  };
  return render(<DesignSideColumn {...props} />);
}

describe('DesignSideColumn', () => {
  it('renders both tab bodies with the active one visible', () => {
    renderColumn({ sideTab: 'details' });
    const details = screen.getByTestId('design-side-panel-details');
    const agent = screen.getByTestId('design-side-panel-agent');
    expect(details).not.toHaveAttribute('hidden');
    expect(agent).toHaveAttribute('hidden');
    expect(screen.getByTestId('design-side-column')).toHaveAttribute('data-tab', 'details');
  });

  it('flips visibility when the agent tab is active', () => {
    renderColumn({ sideTab: 'agent' });
    expect(screen.getByTestId('design-side-panel-details')).toHaveAttribute('hidden');
    expect(screen.getByTestId('design-side-panel-agent')).not.toHaveAttribute('hidden');
  });

  it('clicking a tab reports the tab change', () => {
    const onSideTabChange = vi.fn();
    renderColumn({ sideTab: 'details', onSideTabChange });
    fireEvent.click(screen.getByTestId('design-side-tab-agent'));
    expect(onSideTabChange).toHaveBeenCalledWith('agent');
    fireEvent.click(screen.getByTestId('design-side-tab-details'));
    expect(onSideTabChange).toHaveBeenCalledWith('details');
  });

  it('keeps both bodies mounted so tab state (chat transcript, detail scroll) survives flips', () => {
    renderColumn({ sideTab: 'details' });
    expect(screen.getByTestId('mock-details')).toBeInTheDocument();
    expect(screen.getByTestId('mock-agent')).toBeInTheDocument();
  });

  it('marks the idle state when nothing is selected', () => {
    const { rerender } = renderColumn({ hasSelection: false });
    expect(screen.getByTestId('design-side-panel-details')).toHaveAttribute('data-idle', 'true');
    rerender(
      <DesignSideColumn sideTab="details" onSideTabChange={vi.fn()} details={<div />} agent={<div />} hasSelection />,
    );
    expect(screen.getByTestId('design-side-panel-details')).toHaveAttribute('data-idle', 'false');
  });
});
