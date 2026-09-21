/**
 * DesignEmptyState tests — the no-design-tree onboarding surface.
 *
 * Pins: the three starter cards render; clicking one hands the ready-to-run
 * prompt to onAskAgent (the chat prefill contract — fill, never send); the
 * recheck control calls back.
 */

import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import DesignEmptyState from './DesignEmptyState';

describe('DesignEmptyState', () => {
  it('renders the intro and all three starter cards', () => {
    render(<DesignEmptyState onAskAgent={vi.fn()} onRecheck={vi.fn()} />);
    expect(screen.getByTestId('design-empty-state')).toBeInTheDocument();
    expect(screen.getByText('Import from Figma')).toBeInTheDocument();
    expect(screen.getByText('Bring designs from another tool')).toBeInTheDocument();
    expect(screen.getByText('Draft a new project')).toBeInTheDocument();
  });

  it('the Figma card preframes an MCP-mediated import in its prompt', () => {
    const onAskAgent = vi.fn();
    render(<DesignEmptyState onAskAgent={onAskAgent} onRecheck={vi.fn()} />);
    fireEvent.click(screen.getByTestId('design-empty-card-figma'));
    expect(onAskAgent).toHaveBeenCalledTimes(1);
    const prompt = onAskAgent.mock.calls[0][0] as string;
    expect(prompt).toMatch(/MCP/i);
    expect(prompt).toMatch(/Figma/i);
  });

  it('every card seeds a prompt — fill the chat, never auto-send', () => {
    const onAskAgent = vi.fn();
    render(<DesignEmptyState onAskAgent={onAskAgent} onRecheck={vi.fn()} />);
    for (const id of ['figma', 'tools', 'draft']) {
      fireEvent.click(screen.getByTestId(`design-empty-card-${id}`));
    }
    expect(onAskAgent).toHaveBeenCalledTimes(3);
    for (const call in onAskAgent.mock.calls) {
      expect(onAskAgent.mock.calls[call][0].length).toBeGreaterThan(40);
    }
  });

  it('the recheck control calls back', () => {
    const onRecheck = vi.fn();
    render(<DesignEmptyState onAskAgent={vi.fn()} onRecheck={onRecheck} />);
    fireEvent.click(screen.getByTestId('design-empty-recheck'));
    expect(onRecheck).toHaveBeenCalledTimes(1);
  });
});
