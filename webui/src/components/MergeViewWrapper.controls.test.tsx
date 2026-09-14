import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ThemeProvider } from '../contexts/ThemeContext';
import { MergeViewWrapper } from './MergeViewWrapper';

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
});

function render(el: React.ReactElement) {
  act(() => {
    root!.render(<ThemeProvider>{el}</ThemeProvider>);
  });
}

async function flushIdle() {
  await act(async () => {
    for (let i = 0; i < 8; i++) {
      await new Promise((r) => setTimeout(r, 10));
    }
  });
}

describe('MergeViewWrapper controls redesign', () => {
  it('shows no Accept/Reject toolbar buttons and no chunk hover buttons by default', async () => {
    render(
      <MergeViewWrapper
        originalContent={'line1\nold\nline3\n'}
        modifiedContent={'line1\nnew\nline3\n'}
        mode="unified"
        fileName="f.txt"
      />,
    );
    await flushIdle();

    // Toolbar: Prev/Next + icon undo/redo only.
    const labels = Array.from(container!.querySelectorAll('.merge-view-controls button')).map(
      (b) => b.textContent?.trim() || b.getAttribute('aria-label') || '',
    );
    expect(labels.join(' ')).not.toContain('Reject');
    expect(labels.join(' ')).not.toContain('Accept');

    // Per-chunk hover buttons from the merge library are opt-in now.
    expect(container!.querySelector('.cm-chunkButtons')).toBeNull();
  });

  it('keeps chunk buttons when mergeControls is explicitly enabled', async () => {
    render(
      <MergeViewWrapper
        originalContent={'line1\nold\nline3\n'}
        modifiedContent={'line1\nnew\nline3\n'}
        mode="unified"
        fileName="f.txt"
        mergeControls={true}
      />,
    );
    await flushIdle();
    expect(container!.querySelector('.cm-chunkButtons')).not.toBeNull();
  });

  it('undo icon button is disabled until an edit happens, then enabled', async () => {
    const onModifiedChange = vi.fn();
    render(
      <MergeViewWrapper
        originalContent={'line1\nold\nline3\n'}
        modifiedContent={'line1\nnew\nline3\n'}
        mode="side-by-side"
        fileName="f.txt"
        onModifiedChange={onModifiedChange}
      />,
    );
    await flushIdle();

    const undoBtn = container!.querySelector('button.btn-undo') as HTMLButtonElement;
    expect(undoBtn).not.toBeNull();
    expect(undoBtn.disabled).toBe(true);

    // Revert a chunk via the arrow; undo should become enabled.
    const revertBtn = container!.querySelector('.cm-merge-revert button') as HTMLElement;
    act(() => {
      revertBtn.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }));
    });
    await flushIdle();

    const undoBtnAfter = container!.querySelector('button.btn-undo') as HTMLButtonElement;
    expect(undoBtnAfter.disabled).toBe(false);

    // And clicking it undoes the revert.
    act(() => {
      undoBtnAfter.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    });
    await flushIdle();
    const paneB = container!.querySelector('.cm-merge-b .cm-content') as HTMLElement;
    expect(paneB.textContent).toContain('new');
    expect(paneB.textContent).not.toContain('old');
  });
});
