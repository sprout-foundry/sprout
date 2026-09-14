import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
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

describe('MergeViewWrapper side-by-side revert + undo', () => {
  it('undo restores content after a revert-arrow click', async () => {
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

    const mergeViewEl = container!.querySelector('.cm-mergeView');
    expect(mergeViewEl).not.toBeNull();

    // Pane B should initially hold the modified content.
    const paneB = mergeViewEl!.querySelector('.cm-merge-b .cm-content') as HTMLElement;
    expect(paneB.textContent).toContain('new');

    // Click the revert arrow (the ⇜ button inside .cm-merge-revert).
    const revertBtn = mergeViewEl!.querySelector('.cm-merge-revert button') as HTMLElement;
    expect(revertBtn).not.toBeNull();
    act(() => {
      revertBtn.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }));
    });
    await flushIdle();

    // The revert copied A's chunk into B: pane B now shows 'old'.
    expect(paneB.textContent).toContain('old');

    // Click the toolbar Undo button.
    const undoBtn = container!.querySelector('button.btn-undo') as HTMLElement;
    expect(undoBtn).not.toBeNull();
    act(() => {
      undoBtn.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    });
    await flushIdle();

    // Undo should restore the modified ('new') content in pane B.
    expect(paneB.textContent).toContain('new');
  });
});
