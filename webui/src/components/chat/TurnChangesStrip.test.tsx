import { act, fireEvent, waitFor } from '@testing-library/react';
import { createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { TurnChangesStrip } from './TurnChangesStrip';
import type { FileEdit } from '@sprout/ui';

vi.mock('../../services/clientSession', () => ({
  clientFetch: vi.fn(),
}));
vi.mock('../../services/api/changesApi', () => ({
  getChangeDiff: vi.fn(),
  revertChanges: vi.fn(),
}));
vi.mock('../ThemedDialog', () => ({
  showThemedConfirm: vi.fn().mockResolvedValue(true),
}));
vi.mock('../../utils/log', () => ({
  useLog: () => ({
    info: vi.fn(),
    error: vi.fn(),
    success: vi.fn(),
    warn: vi.fn(),
  }),
}));

const { getChangeDiff, revertChanges } = await import('../../services/api/changesApi');

let container: HTMLDivElement;
let root: Root;
const onReviewChange = vi.fn();

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  vi.clearAllMocks();
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

function mkEdit(overrides: Partial<FileEdit> & { path: string }): FileEdit {
  return {
    action: 'modified',
    timestamp: new Date(),
    queryId: 3,
    serverTs: '2026-09-14T10:00:00Z',
    ...overrides,
  };
}

function renderStrip(props: { fileEdits: FileEdit[]; queryId?: number; isLatestTurn?: boolean }) {
  act(() => {
    root.render(
      createElement(TurnChangesStrip, {
        fileEdits: props.fileEdits,
        queryId: props.queryId ?? 3,
        isLatestTurn: props.isLatestTurn ?? true,
        onReviewChange,
      }),
    );
  });
}

describe('TurnChangesStrip', () => {
  it('renders nothing when the turn has no edits', () => {
    renderStrip({ fileEdits: [mkEdit({ path: 'a.go', queryId: 2 })] });
    expect(container.querySelector('.turn-changes-strip')).toBeNull();
  });

  it('summarizes this turn\u2019s edits, dedupes paths, and expands', () => {
    renderStrip({
      fileEdits: [
        mkEdit({ path: 'a.go' }),
        mkEdit({ path: 'a.go' }), // duplicate path → one row
        mkEdit({ path: 'b.ts', action: 'created' }),
        mkEdit({ path: 'c.py', action: 'deleted' }),
        mkEdit({ path: 'other.go', queryId: 1 }), // other turn → excluded
      ],
    });

    const summary = container.querySelector('.tcs-summary')!;
    expect(summary.textContent).toContain('3 files changed this turn');
    expect(summary.querySelector('.tcs-add')?.textContent).toBe('+1');
    expect(summary.querySelector('.tcs-del')?.textContent).toBe('−1');

    act(() => {
      fireEvent.click(summary);
    });
    const rows = container.querySelectorAll('.tcs-row');
    expect(rows.length).toBe(3);
  });

  it('Review fetches the change diff and hands it to the caller', async () => {
    getChangeDiff.mockResolvedValue({ found: true, stats: '+10 -2', diff: 'diff text' });
    renderStrip({ fileEdits: [mkEdit({ path: 'a.go' })] });
    act(() => {
      fireEvent.click(container.querySelector('.tcs-summary')!);
    });
    await act(async () => {
      fireEvent.click(container.querySelector('.tcs-path')!);
    });
    await waitFor(() => expect(onReviewChange).toHaveBeenCalledWith('a.go', { stats: '+10 -2', diff: 'diff text' }));
  });

  it('Revert posts since = first serverTs minus margin', async () => {
    revertChanges.mockResolvedValue({ restored: 1, failed: 0, summary: '1 restored, 0 failed (scope=all)' });
    renderStrip({
      fileEdits: [
        mkEdit({ path: 'a.go', serverTs: '2026-09-14T10:00:02Z' }),
        mkEdit({ path: 'b.go', serverTs: '2026-09-14T10:00:01Z' }),
      ],
    });
    act(() => {
      fireEvent.click(container.querySelector('.tcs-summary')!);
    });
    await act(async () => {
      fireEvent.click(container.querySelector('.tcs-revert')!);
    });
    await waitFor(() => expect(revertChanges).toHaveBeenCalledTimes(1));
    const since = revertChanges.mock.calls[0][1].since as string;
    expect(since).toBe('2026-09-14T10:00:00.000Z'); // 10:00:01 − 1s
    // Successful revert dismisses the strip.
    await waitFor(() => expect(container.querySelector('.turn-changes-strip')).toBeNull());
  });

  it('older-turn label says "Revert from here"', () => {
    renderStrip({ fileEdits: [mkEdit({ path: 'a.go' })], isLatestTurn: false });
    act(() => {
      fireEvent.click(container.querySelector('.tcs-summary')!);
    });
    expect(container.querySelector('.tcs-revert')!.textContent).toContain('Revert from here');
  });

  it('dismiss hides the strip for this turn but not the next', () => {
    renderStrip({ fileEdits: [mkEdit({ path: 'a.go' })] });
    act(() => {
      fireEvent.click(container.querySelector('.tcs-summary')!);
    });
    act(() => {
      fireEvent.click(container.querySelector('.tcs-dismiss')!);
    });
    expect(container.querySelector('.turn-changes-strip')).toBeNull();

    // Next turn's edits render their own strip (dismissal is per-turn).
    renderStrip({ fileEdits: [mkEdit({ path: 'b.go', queryId: 4 })], queryId: 4 });
    expect(container.querySelector('.turn-changes-strip')).not.toBeNull();
    expect(container.querySelector('.tcs-summary')!.textContent).toContain('1 file changed');
  });
});
