import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { NotificationProvider } from '@sprout/ui';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import DiffWorkspaceTab from './DiffWorkspaceTab';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../services/fileAccess', () => ({
  writeFileWithConsent: vi.fn().mockResolvedValue(new Response('ok', { status: 200 })),
}));

vi.mock('./MergeViewWrapper', () => ({
  MergeViewWrapper: vi.fn(({ onSave, readOnly, originalContent, modifiedContent }) => (
    <div
      data-testid="merge-view"
      data-readonly={String(readOnly ?? false)}
      data-has-onsave={String(!!onSave)}
      data-original={originalContent}
      data-modified={modifiedContent}
    />
  )),
}));

vi.mock('./DiffSurface', () => ({
  default: () => <div data-testid="diff-surface" />,
}));

const writeFileWithConsent = vi.mocked(await import('../services/fileAccess')).writeFileWithConsent;

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
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('DiffWorkspaceTab merge-mode content and save gating', () => {
  it('uses full contents when available (not the fragment reconstruction)', () => {
    render(
      <DiffWorkspaceTab
        {...baseProps}
        fullOriginal={'full original\nline2\nline3\n'}
        fullModified={'full modified\nline2\nline3\n'}
        canSave
      />,
    );
    const view = container!.querySelector('[data-testid="merge-view"]')!;
    expect(view.getAttribute('data-original')).toBe('full original\nline2\nline3\n');
    expect(view.getAttribute('data-modified')).toBe('full modified\nline2\nline3\n');
  });

  it('falls back to fragment documents when full contents are absent, read-only', () => {
    render(<DiffWorkspaceTab {...baseProps} />);
    const view = container!.querySelector('[data-testid="merge-view"]')!;
    // Fragment reconstruction: context + added lines only (no removed).
    expect(view.getAttribute('data-original')).toBe('ctx\nold');
    expect(view.getAttribute('data-modified')).toBe('ctx\nnew');
    expect(view.getAttribute('data-readonly')).toBe('true');
    expect(view.getAttribute('data-has-onsave')).toBe('false');
  });

  it('is read-only when canSave is false even with full contents (commit diffs)', () => {
    render(<DiffWorkspaceTab {...baseProps} fullOriginal="a" fullModified="b" canSave={false} />);
    const view = container!.querySelector('[data-testid="merge-view"]')!;
    expect(view.getAttribute('data-readonly')).toBe('true');
    expect(view.getAttribute('data-has-onsave')).toBe('false');
  });

  it('wires save only when editable (canSave + full contents)', () => {
    render(<DiffWorkspaceTab {...baseProps} fullOriginal="a" fullModified="b" canSave />);
    const view = container!.querySelector('[data-testid="merge-view"]')!;
    expect(view.getAttribute('data-readonly')).toBe('false');
    expect(view.getAttribute('data-has-onsave')).toBe('true');
  });

  it('treats empty-string full contents as available (new/deleted files)', () => {
    render(<DiffWorkspaceTab {...baseProps} fullOriginal="" fullModified="brand new" />);
    const view = container!.querySelector('[data-testid="merge-view"]')!;
    expect(view.getAttribute('data-original')).toBe('');
    expect(view.getAttribute('data-modified')).toBe('brand new');
  });

  it('never calls writeFileWithConsent directly (save goes through MergeView onSave)', () => {
    render(<DiffWorkspaceTab {...baseProps} fullOriginal="a" fullModified="b" canSave />);
    expect(writeFileWithConsent).not.toHaveBeenCalled();
  });
});
