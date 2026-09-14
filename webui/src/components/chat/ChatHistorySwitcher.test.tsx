import { act, fireEvent, waitFor } from '@testing-library/react';
import { createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ChatHistorySwitcher } from './ChatHistorySwitcher';

vi.mock('../../services/clientSession', () => ({
  clientFetch: vi.fn(),
}));
vi.mock('../../services/api/sessionApi', () => ({
  getSessions: vi.fn(),
  searchSessions: vi.fn(),
}));
vi.mock('../ThemedDialog', () => ({
  showThemedConfirm: vi.fn().mockResolvedValue(true),
}));
vi.mock('../../utils/log', () => ({
  useLog: () => ({ info: vi.fn(), error: vi.fn(), success: vi.fn(), warn: vi.fn() }),
}));

const { getSessions, searchSessions } = await import('../../services/api/sessionApi');
const { showThemedConfirm } = await import('../ThemedDialog');

let container: HTMLDivElement;
let root: Root;
const onRestoreSession = vi.fn().mockResolvedValue(undefined);

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  vi.clearAllMocks();
  getSessions.mockResolvedValue({
    message: 'ok',
    current_session_id: 's1',
    sessions: [
      {
        session_id: 's1',
        name: 'resize fix',
        working_directory: '/w',
        last_updated: new Date().toISOString(),
        message_count: 4,
        total_tokens: 100,
      },
      {
        session_id: 's2',
        name: 'config migration',
        working_directory: '/w',
        last_updated: '2026-09-10T00:00:00Z',
        message_count: 12,
        total_tokens: 200,
      },
    ],
  });
});

afterEach(() => {
  act_unmount();
  document.body.querySelectorAll('.chs-popover').forEach((el) => el.remove());
  container?.remove();
});

function act_unmount() {
  try {
    root?.unmount();
  } catch {
    /* already unmounted */
  }
}

function renderSwitcher(props: { chatId?: string } = {}) {
  act(() => {
    root.render(createElement(ChatHistorySwitcher, { onRestoreSession, chatId: props.chatId ?? 'chat-7' }));
  });
}

async function openPopover() {
  renderSwitcher();
  const trigger = document.querySelector('[data-testid="chs-trigger"]') as HTMLButtonElement;
  expect(trigger).not.toBeNull();
  await act(async () => {
    fireEvent.click(trigger);
  });
  await waitFor(() => expect(document.querySelector('.chs-popover')).not.toBeNull());
  return document.querySelector('.chs-popover') as HTMLElement;
}

describe('ChatHistorySwitcher', () => {
  it('renders nothing without a restore callback', () => {
    act(() => {
      root.render(createElement(ChatHistorySwitcher, {}));
    });
    expect(container.querySelector('.chat-history-switcher')).toBeNull();
  });

  it('loads and lists recent sessions on open, newest first', async () => {
    const pop = await openPopover();
    await waitFor(() => expect(pop.querySelectorAll('.chs-row').length).toBe(2));
    const first = pop.querySelector('.chs-row .chs-row-name') as HTMLElement;
    expect(first.textContent).toBe('resize fix');
  });

  it('restore passes the owning chat id for scoping', async () => {
    const pop = await openPopover();
    await waitFor(() => expect(pop.querySelectorAll('.chs-row').length).toBe(2));
    await act(async () => {
      fireEvent.click(pop.querySelectorAll('.chs-row')[1]);
    });
    await waitFor(() => expect(onRestoreSession).toHaveBeenCalledWith('s2', 'chat-7'));
  });

  it('debounced search switches to search results', async () => {
    vi.useFakeTimers();
    try {
      const pop = await openPopover();
      searchSessions.mockResolvedValue({
        query: 'resize',
        total: 1,
        results: [
          {
            session_id: 's1',
            name: 'resize fix',
            working_directory: '/w',
            last_updated: new Date().toISOString(),
            total_cost: 0,
            excerpt: 'fix the resize handle',
            match_score: 2,
          },
        ],
      });
      const input = pop.querySelector('.chs-search-input') as HTMLInputElement;
      await act(async () => {
        fireEvent.change(input, { target: { value: 'resize' } });
        await vi.advanceTimersByTimeAsync(300);
      });
      await waitFor(() => {
        const rows = pop.querySelectorAll('.chs-row');
        return expect(
          rows.length === 1 &&
            (rows[0].querySelector('.chs-row-preview') as HTMLElement).textContent?.includes('resize handle'),
        ).toBe(true);
      });
      expect(searchSessions).toHaveBeenCalledWith(expect.anything(), 'resize', { limit: 30 });
    } finally {
      vi.useRealTimers();
    }
  });

  it('declines restore when the confirm dialog is dismissed', async () => {
    (showThemedConfirm as ReturnType<typeof vi.fn>).mockResolvedValueOnce(false);
    const pop = await openPopover();
    await waitFor(() => expect(pop.querySelectorAll('.chs-row').length).toBe(2));
    await act(async () => {
      fireEvent.click(pop.querySelectorAll('.chs-row')[0]);
    });
    await new Promise((r) => setTimeout(r, 10));
    expect(onRestoreSession).not.toHaveBeenCalled();
  });
});
