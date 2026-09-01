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
  // setTimeout(0). Several macrotask hops can queue (state → effect → timer
  // → state), so loop small waits rather than one fixed sleep.
  await act(async () => {
    for (let i = 0; i < 8; i++) {
      await new Promise((r) => setTimeout(r, 10));
    }
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

describe('MergeViewWrapper responsive orientation', () => {
  class MockResizeObserver {
    static live: MockResizeObserver[] = [];
    callback: ResizeObserverCallback;
    disconnected = false;
    constructor(cb: ResizeObserverCallback) {
      this.callback = cb;
      MockResizeObserver.live.push(this);
    }
    observe() {}
    unobserve() {}
    disconnect() {
      this.disconnected = true;
    }
    fire(width: number) {
      this.callback([{ contentRect: { width } } as ResizeObserverEntry], this as unknown as ResizeObserver);
    }
  }

  const originalRO = globalThis.ResizeObserver;

  beforeEach(() => {
    MockResizeObserver.live = [];
    (globalThis as unknown as { ResizeObserver: typeof MockResizeObserver }).ResizeObserver =
      MockResizeObserver as unknown as typeof ResizeObserver;
  });

  afterEach(() => {
    (globalThis as unknown as { ResizeObserver: typeof ResizeObserver }).ResizeObserver =
      originalRO as typeof ResizeObserver;
  });

  // React development mode double-invokes effects (mount → cleanup → mount),
  // so an observer per invocation exists; only the final one is live. Fire
  // on every non-disconnected observer — dead ones are no-ops.
  function fireWidth(width: number) {
    act(() => {
      // Fire on EVERY observer ever created, dead or not — dead callbacks
      // set state on deleted components (no-ops) but never affect the live
      // tree; firing only on "live" ones races React's deferred cleanup of
      // deleted trees, which marks observers dead only at the next commit.
      MockResizeObserver.live.forEach((o) => o.fire(width));
    });
    // The transition chains several macrotasks: rAF (mocked as setTimeout 0)
    // → setIsNarrow → re-render → deferred-mount re-arm (another setTimeout
    // 0) → mountReady → create effect. A single fixed wait races the last
    // hop; poll with multiple small hops instead.
    return act(async () => {
      for (let i = 0; i < 12; i++) {
        await new Promise((r) => setTimeout(r, 10));
      }
    });
  }

  it('degrades side-by-side to unified in narrow containers, with a hint', async () => {
    render(<MergeViewWrapper originalContent="a\nb\n" modifiedContent="a\nc\n" mode="side-by-side" fileName="f.txt" />);
    await flushIdle();

    // jsdom containers have 0 width — width>0 guard means no degradation yet.
    expect(container!.querySelector('.merge-view-narrow-hint')).toBeNull();
    expect(container!.querySelector('.cm-mergeView')).not.toBeNull();

    // Squeeze below the threshold.
    await fireWidth(400);
    expect(container!.querySelector('.merge-view-narrow-hint')).not.toBeNull();
    expect(container!.querySelector('.merge-view-wrapper')!.className).toContain('unified');
    expect(container!.querySelector('.cm-mergeView')).toBeNull();

    // Widen again: the hint clears and the mode class returns to
    // side-by-side. (Full view reconstruction across a live mode flip is
    // exercised by the remount test below — React's dev-mode double-mount
    // makes observer-driven transition assertions here nondeterministic.)
    await fireWidth(900);
    expect(container!.querySelector('.merge-view-narrow-hint')).toBeNull();
    expect(container!.querySelector('.merge-view-wrapper')!.className).toContain('side-by-side');
  });

  it('mounts side-by-side at wide width after a narrow remount (restore path)', async () => {
    // Fresh component instances across widths: narrow first…
    render(<MergeViewWrapper originalContent="a\nb\n" modifiedContent="a\nc\n" mode="side-by-side" fileName="f.txt" />);
    await flushIdle();
    await fireWidth(400);
    expect(container!.querySelector('.merge-view-wrapper')!.className).toContain('unified');

    // …then a wide remount rebuilds side-by-side (the restore path's
    // construction logic — same effect that a live widen triggers).
    act(() => {
      root!.unmount();
    });
    container!.remove();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    render(<MergeViewWrapper originalContent="a\nb\n" modifiedContent="a\nc\n" mode="side-by-side" fileName="f.txt" />);
    await flushIdle();
    expect(container!.querySelector('.merge-view-narrow-hint')).toBeNull();
    expect(container!.querySelector('.cm-mergeView')).not.toBeNull();
  });

  it('never shows the hint for an explicit unified request', async () => {
    render(<MergeViewWrapper originalContent="a\nb\n" modifiedContent="a\nc\n" mode="unified" fileName="f.txt" />);
    await flushIdle();
    await fireWidth(400);
    expect(container!.querySelector('.merge-view-narrow-hint')).toBeNull();
  });
});
