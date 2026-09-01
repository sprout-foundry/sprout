import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ThemeProvider } from '../contexts/ThemeContext';
import { MergeViewWrapper } from './MergeViewWrapper';

// CodeMirror DOM in jsdom is heavy but works; we count MergeView instances
// by observing how many times the container gains a .cm-mergeView child and
// by patching console.error-free construction. Simpler: spy on the module's
// MergeView via a wrapper — but the import is namespace-bound. Instead we
// assert behaviorally: pane-B content survives a parent re-render with
// changed content props only via dispatch (view preserved), i.e. the same
// .cm-mergeView DOM node is still present (not replaced).

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
    root!.render(<ThemeProvider>{el}</ThemeProvider>);
  });
}

async function flushIdle() {
  // requestIdleCallback may not exist in jsdom; the component falls back to
  // setTimeout(0). Flush timers + microtasks.
  await act(async () => {
    await new Promise((r) => setTimeout(r, 10));
  });
}

describe('MergeViewWrapper deferred mount and edit preservation', () => {
  it('renders a pending state before the merge view mounts', async () => {
    render(<MergeViewWrapper originalContent="a\nb\n" modifiedContent="a\nc\n" mode="side-by-side" fileName="f.txt" />);
    expect(container!.querySelector('.merge-view-pending')).not.toBeNull();

    await flushIdle();
    expect(container!.querySelector('.merge-view-pending')).toBeNull();
    expect(container!.querySelector('.cm-mergeView')).not.toBeNull();
  });

  it('does NOT recreate the view when content props change (edits preserved)', async () => {
    render(<MergeViewWrapper originalContent="a\nb\n" modifiedContent="a\nc\n" mode="side-by-side" fileName="f.txt" />);
    await flushIdle();
    const viewEl = container!.querySelector('.cm-mergeView');
    expect(viewEl).not.toBeNull();

    // Parent re-renders with new content (git refresh). The view DOM node
    // must be preserved — a teardown/recreate would replace it and destroy
    // pane-B edits.
    render(<MergeViewWrapper originalContent="a\nb\n" modifiedContent="a\nZ\n" mode="side-by-side" fileName="f.txt" />);
    await flushIdle();
    const viewElAfter = container!.querySelector('.cm-mergeView');
    expect(viewElAfter).not.toBeNull();
    expect(viewElAfter).toBe(viewEl);
    // No pending state re-armed by the content change.
    expect(container!.querySelector('.merge-view-pending')).toBeNull();
  });

  it('recreates the view when mode changes', async () => {
    render(<MergeViewWrapper originalContent="a\nb\n" modifiedContent="a\nc\n" mode="side-by-side" fileName="f.txt" />);
    await flushIdle();
    const sbsEl = container!.querySelector('.cm-mergeView');

    render(<MergeViewWrapper originalContent="a\nb\n" modifiedContent="a\nc\n" mode="unified" fileName="f.txt" />);
    await flushIdle();
    // Unified mode creates a plain editor, not a merge view.
    expect(container!.querySelector('.cm-editor')).not.toBeNull();
    expect(container!.querySelector('.cm-mergeView')).not.toBe(sbsEl);
  });
});
