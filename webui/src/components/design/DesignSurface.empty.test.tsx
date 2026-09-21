/**
 * DesignSurface empty-state tests — Design mode on a workspace without a
 * recognized `design/` tree.
 *
 * Pins the amended SP-140-3 §3a contract: an absent tree renders the
 * onboarding surface (starter cards + agent column) instead of bouncing the
 * user back to Code; a foreign folder flips the surface's tree-state props
 * through; the prefill handoff from card click to chat input is asserted
 * end to end through the mocked Chat.
 */

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { SproutAdapterProvider } from '../../contexts/SproutAdapterContext';
import DesignSurface from './DesignSurface';

vi.mock('../ChatView', () => ({
  default: function MockChat(props: { inputValue?: string; onInputChange?: (v: string) => void }) {
    return (
      <div data-testid="mock-empty-chat">
        <input
          data-testid="mock-empty-chat-input"
          value={props.inputValue ?? ''}
          onChange={(event) => props.onInputChange?.(event.target.value)}
          readOnly
        />
      </div>
    );
  },
}));

function renderEmpty(props: Partial<Parameters<typeof DesignSurface>[0]> = {}) {
  const onRecheck = vi.fn();
  return {
    onRecheck,
    ...render(
      <DesignSurface
        loading={false}
        present={false}
        tab="flows"
        onTabChange={vi.fn()}
        chatProps={{ inputValue: '', onSendMessage: vi.fn(), onInputChange: vi.fn() }}
        onRecheck={onRecheck}
        {...props}
      />,
    ),
  };
}

describe('DesignSurface empty state (no recognized design/ tree)', () => {
  it('renders the onboarding surface instead of the dead fallback', () => {
    renderEmpty();
    expect(screen.getByTestId('design-surface-empty')).toBeInTheDocument();
    expect(screen.getByTestId('design-empty-state')).toBeInTheDocument();
    expect(screen.queryByTestId('design-surface-fallback')).not.toBeInTheDocument();
  });

  it('mounts the agent chat beside the cards', () => {
    renderEmpty();
    expect(screen.getByTestId('design-empty-agent')).toBeInTheDocument();
    expect(screen.getByTestId('design-empty-agent')).toContainElement(screen.getByTestId('mock-empty-chat'));
  });

  it('frontendLike reorders the cards — code discovery first', () => {
    renderEmpty({ frontendLike: true });
    const cards = screen.getAllByRole('listitem');
    expect(cards[0]).toHaveAttribute('data-testid', 'design-empty-card-discover-code');
  });

  it('a foreign tree shows the inventory-and-ask banner and its prefill guards the files', async () => {
    const onInputChange = vi.fn();
    render(
      <DesignSurface
        loading={false}
        present={false}
        treeState="foreign"
        tab="flows"
        onTabChange={vi.fn()}
        chatProps={{ inputValue: '', onSendMessage: vi.fn(), onInputChange }}
        onRecheck={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId('design-empty-foreign'));
    await waitFor(() => {
      expect(onInputChange).toHaveBeenCalledWith(expect.stringMatching(/never move|do not move/i));
    });
  });

  it("the empty surface's recheck control re-runs the presence probe", () => {
    const { onRecheck } = renderEmpty();
    fireEvent.click(screen.getByTestId('design-empty-recheck'));
    expect(onRecheck).toHaveBeenCalledTimes(1);
  });

  it('a present tree renders the live surface, not the empty state', async () => {
    render(
      <SproutAdapterProvider>
        <DesignSurface
          loading={false}
          present
          tab="flows"
          onTabChange={vi.fn()}
          chatProps={{ inputValue: '', onSendMessage: vi.fn(), onInputChange: vi.fn() }}
          onRecheck={vi.fn()}
        />
      </SproutAdapterProvider>,
    );
    // HealthStrip fetches its status asynchronously; settle before asserting
    // so the update lands inside act's scope.
    await screen.findByTestId('design-surface');
    expect(screen.queryByTestId('design-surface-empty')).not.toBeInTheDocument();
  });
});
