/**
 * DesignEmptyState tests — the no-recognized-tree onboarding surface.
 *
 * Pins: all five starter cards render; position ordering (frontend-code
 * workspaces see code-discovery cards first, bare workspaces see draft/import
 * first); clicking a card hands the ready-to-run prompt to onAskAgent (the
 * chat prefill contract — fill, never send) and every prompt opens with an
 * ask-first instruction; the Figma card is honest about MCP; the foreign
 * folder renders the inventory-and-ask banner; the recheck control calls
 * back.
 */

import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import DesignEmptyState from './DesignEmptyState';

const noops = { onAskAgent: vi.fn(), onRecheck: vi.fn() };

describe('DesignEmptyState', () => {
  it('renders the intro and all five starter cards', () => {
    render(<DesignEmptyState {...noops} />);
    expect(screen.getByTestId('design-empty-state')).toBeInTheDocument();
    for (const id of ['discover-code', 'running-app', 'images', 'figma', 'draft']) {
      expect(screen.getByTestId(`design-empty-card-${id}`)).toBeInTheDocument();
    }
  });

  it('orders code-discovery cards first when the workspace has frontend code', () => {
    render(<DesignEmptyState {...noops} frontendCode />);
    const cards = screen.getAllByRole('listitem');
    expect(cards[0]).toHaveAttribute('data-testid', 'design-empty-card-discover-code');
    expect(cards[1]).toHaveAttribute('data-testid', 'design-empty-card-running-app');
    expect(cards[4]).toHaveAttribute('data-testid', 'design-empty-card-draft');
  });

  it('orders draft/import cards first on a bare workspace', () => {
    render(<DesignEmptyState {...noops} />);
    const cards = screen.getAllByRole('listitem');
    expect(cards[0]).toHaveAttribute('data-testid', 'design-empty-card-draft');
    expect(cards[1]).toHaveAttribute('data-testid', 'design-empty-card-images');
    expect(cards[4]).toHaveAttribute('data-testid', 'design-empty-card-running-app');
  });

  it('every card seeds a substantial prompt — fill the chat, never auto-send', () => {
    const onAskAgent = vi.fn();
    render(<DesignEmptyState onAskAgent={onAskAgent} onRecheck={vi.fn()} />);
    for (const id of ['discover-code', 'running-app', 'images', 'figma', 'draft']) {
      fireEvent.click(screen.getByTestId(`design-empty-card-${id}`));
    }
    expect(onAskAgent).toHaveBeenCalledTimes(5);
    for (const call of onAskAgent.mock.calls) {
      expect(String(call[0]).length).toBeGreaterThan(60);
    }
  });

  it('prompts open by asking the user, not by guessing', () => {
    const onAskAgent = vi.fn();
    render(<DesignEmptyState onAskAgent={onAskAgent} onRecheck={vi.fn()} />);
    for (const id of ['discover-code', 'running-app', 'images', 'figma', 'draft']) {
      onAskAgent.mockClear();
      fireEvent.click(screen.getByTestId(`design-empty-card-${id}`));
      expect(String(onAskAgent.mock.calls[0][0])).toMatch(/ask me/i);
    }
  });

  it('the Figma card names MCP and the token handoff honestly', () => {
    const onAskAgent = vi.fn();
    render(<DesignEmptyState onAskAgent={onAskAgent} onRecheck={vi.fn()} />);
    fireEvent.click(screen.getByTestId('design-empty-card-figma'));
    const prompt = String(onAskAgent.mock.calls[0][0]);
    expect(prompt).toMatch(/MCP/i);
    expect(prompt).toMatch(/mcp-setup/i);
    expect(prompt).toMatch(/token/i);
  });

  it('the running-app card asks for the URL before capturing', () => {
    const onAskAgent = vi.fn();
    render(<DesignEmptyState onAskAgent={onAskAgent} onRecheck={vi.fn()} />);
    fireEvent.click(screen.getByTestId('design-empty-card-running-app'));
    const prompt = String(onAskAgent.mock.calls[0][0]);
    expect(prompt).toMatch(/analyze_ui_screenshot/);
    expect(prompt).toMatch(/URL/i);
  });

  it('a foreign design/ folder shows the inventory-and-ask banner', () => {
    const onAskAgent = vi.fn();
    render(<DesignEmptyState onAskAgent={onAskAgent} onRecheck={vi.fn()} foreignTree />);
    expect(screen.getByTestId('design-empty-foreign')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('design-empty-foreign'));
    const prompt = String(onAskAgent.mock.calls[0][0]);
    expect(prompt).toMatch(/inventory/i);
    expect(prompt).toMatch(/never move|do not move/i);
  });

  it('no banner without a foreign tree', () => {
    render(<DesignEmptyState {...noops} />);
    expect(screen.queryByTestId('design-empty-foreign')).not.toBeInTheDocument();
  });

  it('the recheck control calls back', () => {
    const onRecheck = vi.fn();
    render(<DesignEmptyState onAskAgent={vi.fn()} onRecheck={onRecheck} />);
    fireEvent.click(screen.getByTestId('design-empty-recheck'));
    expect(onRecheck).toHaveBeenCalledTimes(1);
  });
});
