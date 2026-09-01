import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { NotificationProvider } from '@sprout/ui';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import DiffWorkspaceTab from './DiffWorkspaceTab';

vi.mock('../services/fileAccess', () => ({
  writeFileWithConsent: vi.fn().mockResolvedValue(new Response('ok', { status: 200 })),
}));

// Fake MergeViewWrapper: records the mode it was last rendered in (or not).
const mergeRenderMock = vi.fn(({ readOnly }: { readOnly?: boolean }) => (
  <div data-testid="merge-view" data-readonly={String(readOnly ?? false)} />
));
vi.mock('./MergeViewWrapper', () => ({
  MergeViewWrapper: (props: Record<string, unknown>) => mergeRenderMock(props),
}));

const surfaceMock = vi.fn(() => <div data-testid="diff-surface" />);
vi.mock('./DiffSurface', () => ({
  default: () => surfaceMock(),
}));

let container: HTMLElement | null = null;
let root: Root | null = null;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  mergeRenderMock.mockClear();
  surfaceMock.mockClear();
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
};

describe('DiffWorkspaceTab default view seeding', () => {
  it('defaults to merge view for working-tree diffs', () => {
    render(<DiffWorkspaceTab {...baseProps} fullOriginal="a\n" fullModified="b\n" canSave />);
    expect(container!.querySelector('[data-testid="merge-view"]')).not.toBeNull();
    expect(container!.querySelector('[data-testid="diff-surface"]')).toBeNull();
  });

  it('defaults to text view when defaultView="text" (commit/history diffs)', () => {
    render(<DiffWorkspaceTab {...baseProps} defaultView="text" />);
    expect(container!.querySelector('[data-testid="diff-surface"]')).not.toBeNull();
    expect(container!.querySelector('[data-testid="merge-view"]')).toBeNull();
    // The deferred/expensive CodeMirror mount never happens for text-first.
    expect(mergeRenderMock).not.toHaveBeenCalled();
  });
});
