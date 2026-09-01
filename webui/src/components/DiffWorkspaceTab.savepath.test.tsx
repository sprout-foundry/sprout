import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { NotificationProvider } from '@sprout/ui';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import DiffWorkspaceTab from './DiffWorkspaceTab';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const writeFileMock = vi.fn().mockResolvedValue(new Response('ok', { status: 200 }));
vi.mock('../services/fileAccess', () => ({
  writeFileWithConsent: (...args: unknown[]) => writeFileMock(...(args as [string, string])),
}));

// MergeViewWrapper mock that captures onSave and fires it immediately with
// the pane-B content, so the save path executes end-to-end.
const onSaveCapture = vi.fn<(content: string) => void>();
vi.mock('./MergeViewWrapper', () => ({
  MergeViewWrapper: ({ onSave }: { onSave?: (content: string) => void }) => {
    onSaveCapture.mockImplementation((content: string) => onSave?.(content));
    return <div data-testid="merge-view" />;
  },
}));

vi.mock('./DiffSurface', () => ({
  default: () => <div data-testid="diff-surface" />,
}));

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

let container: HTMLElement | null = null;
let root: Root | null = null;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  container = null;
  root = null;
  vi.clearAllMocks();
});

function render(el: React.ReactElement) {
  act(() => {
    root!.render(<NotificationProvider>{el}</NotificationProvider>);
  });
}

const SAMPLE_DIFF = ['--- a/f.txt', '+++ b/f.txt', '@@ -1,2 +1,2 @@', ' ctx', '-old', '+new'].join('\n');

const baseProps = {
  path: 'f.txt',
  diff: {
    message: 'success',
    path: 'f.txt',
    has_staged: false,
    has_unstaged: true,
    staged_diff: '',
    unstaged_diff: SAMPLE_DIFF,
    diff: SAMPLE_DIFF,
  } as const,
  diffMode: 'unstaged' as const,
  isLoading: false,
  error: null,
  onDiffModeChange: () => {},
  fullOriginal: 'a\n',
  fullModified: 'b\n',
  canSave: true,
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('DiffWorkspaceTab save path (Cmd+S through merge view)', () => {
  it('dispatches file:editor-saved before the HTTP write, with path and mtime', async () => {
    const sequence: string[] = [];

    const dispatchSpy = vi.spyOn(document, 'dispatchEvent').mockImplementation((event: Event) => {
      if (event.type === 'file:editor-saved') {
        sequence.push('event');
      }
      return true;
    });
    writeFileMock.mockImplementation(async () => {
      sequence.push('write');
      return new Response('ok', { status: 200 });
    });

    render(<DiffWorkspaceTab {...baseProps} />);

    const beforeS = Math.floor(Date.now() / 1000);
    await act(async () => {
      onSaveCapture('pane-b content');
    });
    const afterS = Math.floor(Date.now() / 1000);

    // The fsnotify-echo suppression event fired exactly once, before the write.
    expect(sequence).toEqual(['event', 'write']);

    const events = dispatchSpy.mock.calls
      .map(([ev]) => ev)
      .filter((ev) => ev.type === 'file:editor-saved') as CustomEvent[];
    expect(events).toHaveLength(1);
    expect((events[0].detail as { path: string }).path).toBe('f.txt');
    const mtime = (events[0].detail as { mtime: number }).mtime;
    expect(mtime).toBeGreaterThanOrEqual(beforeS);
    expect(mtime).toBeLessThanOrEqual(afterS);

    // The write carries the pane-B content to the real path.
    expect(writeFileMock).toHaveBeenCalledWith('f.txt', 'pane-b content');
  });

  it('logs an error and does not throw when the write fails', async () => {
    vi.spyOn(document, 'dispatchEvent').mockReturnValue(true);
    writeFileMock.mockResolvedValue(new Response('denied', { status: 403 }));

    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
    try {
      render(<DiffWorkspaceTab {...baseProps} />);
      await act(async () => {
        onSaveCapture('content');
      });
      // Save failure surfaces via the notification log, not an exception.
      expect(writeFileMock).toHaveBeenCalledTimes(1);
    } finally {
      consoleError.mockRestore();
    }
  });

  it('never wires a save when canSave is false', async () => {
    vi.spyOn(document, 'dispatchEvent').mockReturnValue(true);
    render(<DiffWorkspaceTab {...baseProps} canSave={false} />);
    await act(async () => {
      onSaveCapture('content');
    });
    expect(writeFileMock).not.toHaveBeenCalled();
  });
});
