// @ts-nocheck
/**
 * Vitest tests for PastSessionsHint component (SP-092-3).
 *
 * Tests cover: input rendering, debounce behavior, fetch mocking,
 * empty results, result cards, and CustomEvent dispatch on click.
 */

import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { describe, it, test, expect, vi, beforeEach, afterEach } from 'vitest';
import { PastSessionsHint } from './PastSessionsHint';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function mockFetchResponse(items: Array<{ session_id: string; content_preview: string; similarity: number }>) {
  return {
    ok: true,
    json: async () => ({ query: 'test', items, count: items.length }),
  };
}

function setupFetchMock(items: Array<{ session_id: string; content_preview: string; similarity: number }>) {
  return vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockFetchResponse(items));
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('PastSessionsHint', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  test('renders input field with correct testid', () => {
    render(<PastSessionsHint />);
    expect(screen.getByTestId('past-sessions-hint-input')).toBeInTheDocument();
    expect(screen.getByLabelText('Search past sessions')).toBeInTheDocument();
  });

  test('empty query does not trigger fetch', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockFetchResponse([]));
    render(<PastSessionsHint />);

    // Type and clear — no trimmed query should remain
    const input = screen.getByTestId('past-sessions-hint-input');
    fireEvent.change(input, { target: { value: '   ' } });

    // Advance timers — should not call fetch for whitespace-only input
    await vi.advanceTimersByTimeAsync(350);

    expect(fetchSpy).not.toHaveBeenCalled();
  });

  test('debounced query triggers fetch after DEBOUNCE_MS', async () => {
    setupFetchMock([]);
    render(<PastSessionsHint />);

    const input = screen.getByTestId('past-sessions-hint-input');
    fireEvent.change(input, { target: { value: 'test query' } });

    // Before debounce, fetch should not have been called
    expect(globalThis.fetch).not.toHaveBeenCalled();

    // Advance past debounce
    await vi.advanceTimersByTimeAsync(350);

    expect(globalThis.fetch).toHaveBeenCalledWith(
      '/api/recall?query=test%20query&limit=5',
    );
  });

  test('mock fetch with empty items shows "No matching sessions."', async () => {
    setupFetchMock([]);
    render(<PastSessionsHint />);

    const input = screen.getByTestId('past-sessions-hint-input');
    fireEvent.change(input, { target: { value: 'nothing' } });

    await vi.advanceTimersByTimeAsync(350);
    await waitFor(() => {
      expect(screen.getByTestId('past-sessions-hint-empty')).toBeInTheDocument();
    });
    expect(screen.getByText('No matching sessions.')).toBeInTheDocument();
  });

  test('mock fetch with 2 items renders 2 cards with correct session_ids', async () => {
    const items = [
      { session_id: 'session-abc', content_preview: 'Fixed login bug', similarity: 0.85 },
      { session_id: 'session-def', content_preview: 'Added API endpoint', similarity: 0.72 },
    ];
    setupFetchMock(items);
    render(<PastSessionsHint />);

    const input = screen.getByTestId('past-sessions-hint-input');
    fireEvent.change(input, { target: { value: 'fix' } });

    await vi.advanceTimersByTimeAsync(350);

    await waitFor(() => {
      const cardAbc = screen.getByTestId('past-sessions-hint-card-session-abc');
      const cardDef = screen.getByTestId('past-sessions-hint-card-session-def');
      expect(cardAbc).toBeInTheDocument();
      expect(cardDef).toBeInTheDocument();
    });

    // Verify session IDs are visible
    expect(screen.getByText('session-abc')).toBeInTheDocument();
    expect(screen.getByText('session-def')).toBeInTheDocument();
  });

  test('click on a card dispatches sprout:session-restored CustomEvent', async () => {
    const items = [
      { session_id: 'clicked-session', content_preview: 'Clicked this one', similarity: 0.9 },
    ];
    setupFetchMock(items);
    render(<PastSessionsHint />);

    const input = screen.getByTestId('past-sessions-hint-input');
    fireEvent.change(input, { target: { value: 'click' } });

    await vi.advanceTimersByTimeAsync(350);

    await waitFor(() => {
      expect(screen.getByTestId('past-sessions-hint-card-clicked-session')).toBeInTheDocument();
    });

    // Listen for the custom event
    const eventPromise = new Promise<CustomEvent>((resolve) => {
      window.addEventListener('sprout:session-restored', (e) => resolve(e as CustomEvent), { once: true });
    });

    // Click the card
    const card = screen.getByTestId('past-sessions-hint-card-clicked-session');
    fireEvent.click(card);

    const event = await eventPromise;
    expect(event.detail).toEqual({ session_id: 'clicked-session' });
  });
});
