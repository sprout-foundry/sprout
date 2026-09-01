import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import DiffSurface from './DiffSurface';

// ---------------------------------------------------------------------------
// Setup
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
    root!.render(el);
  });
}

const SAMPLE_DIFF = [
  '--- a/src/foo.ts',
  '+++ b/src/foo.ts',
  '@@ -10,4 +10,5 @@ function foo()',
  ' context line',
  '-const total = bar;',
  '+const total = baz;',
  ' more context',
].join('\n');

// jsdom lacks ResizeObserver.
class MockResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
(globalThis as unknown as { ResizeObserver: typeof ResizeObserver }).ResizeObserver = MockResizeObserver;

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('DiffSurface', () => {
  it('renders nothing for empty diff text', () => {
    render(<DiffSurface diffText="" />);
    expect(container?.innerHTML).toBe('');
  });

  it('renders real old/new line numbers', () => {
    render(<DiffSurface diffText={SAMPLE_DIFF} />);
    const oldNums = Array.from(container!.querySelectorAll('.diff-ln-old')).map((e) => e.textContent);
    const newNums = Array.from(container!.querySelectorAll('.diff-ln-new')).map((e) => e.textContent);
    // rows: hunk-header (blank), context 10/10, del 11/—, add —/11, context 12/12
    expect(oldNums).toEqual(['', '10', '11', '', '12']);
    expect(newNums).toEqual(['', '10', '', '11', '12']);
  });

  it('renders the file path and hunk header', () => {
    render(<DiffSurface diffText={SAMPLE_DIFF} />);
    expect(container!.textContent).toContain('src/foo.ts');
    expect(container!.textContent).toContain('@@ -10,4 +10,5 @@');
    expect(container!.textContent).toContain('function foo()');
  });

  it('shows the stats bar with +N/−M', () => {
    render(<DiffSurface diffText={SAMPLE_DIFF} />);
    const stats = container!.querySelector('.diff-stats');
    expect(stats?.getAttribute('aria-label')).toBe('1 additions, 1 deletions');
    expect(stats?.textContent).toContain('+1');
    expect(stats?.textContent).toContain('−1');
  });

  it('renders word-level highlights on changed spans', () => {
    render(<DiffSurface diffText={SAMPLE_DIFF} />);
    const marks = container!.querySelectorAll('mark.diff-word-changed');
    expect(marks.length).toBeGreaterThan(0);
    const marked = Array.from(marks)
      .map((m) => m.textContent)
      .join(' ');
    expect(marked).toContain('bar');
    expect(marked).toContain('baz');
  });

  it('renders multi-file diffs with per-file sections and stats', () => {
    const multi = [
      'diff --git a/one.ts b/one.ts',
      '--- a/one.ts',
      '+++ b/one.ts',
      '@@ -1,2 +1,2 @@',
      ' ctx',
      '-del1',
      '+add1',
      'diff --git a/two.ts b/two.ts',
      '--- a/two.ts',
      '+++ b/two.ts',
      '@@ -1,1 +1,2 @@',
      ' ctx',
      '+add2',
    ].join('\n');
    render(<DiffSurface diffText={multi} />);
    const sections = container!.querySelectorAll('.diff-file');
    expect(sections).toHaveLength(2);
    expect(container!.textContent).toContain('one.ts');
    expect(container!.textContent).toContain('two.ts');
  });

  it('shows a note for binary files', () => {
    const binary = ['diff --git a/img.png b/img.png', 'Binary files a/img.png and b/img.png differ'].join('\n');
    render(<DiffSurface diffText={binary} />);
    expect(container!.querySelector('.diff-binary-note')?.textContent).toContain('Binary file');
  });

  it('prefers the explicit path prop over the parsed path', () => {
    render(<DiffSurface diffText={SAMPLE_DIFF} path="custom/path.ts" title="Commit" />);
    expect(container!.querySelector('.diff-surface-path')?.textContent).toBe('custom/path.ts');
    expect(container!.querySelector('.diff-surface-eyebrow')?.textContent).toBe('Commit');
  });

  it('shows raw text verbatim when there are no parseable hunks', () => {
    render(<DiffSurface diffText="(no staged changes)" />);
    const raw = container!.querySelector('.diff-surface-raw');
    expect(raw?.textContent).toBe('(no staged changes)');
    expect(container!.querySelector('.diff-surface-empty')).toBeNull();
  });

  it('shows the empty state only for whitespace-only input', () => {
    render(<DiffSurface diffText="   " />);
    expect(container!.querySelector('.diff-surface-empty')?.textContent).toContain('No changes');
  });
});
