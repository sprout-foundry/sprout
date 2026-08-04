/**
 * unsavedLineHighlight.facet.test.ts — Integration test for the
 * unsaved-line block-decoration delivery path.
 *
 * Regression test for "Block decorations may not be specified via
 * plugins": CodeMirror only allows `Decoration.line` (block) decorations
 * through the `EditorView.decorations` facet, not through a ViewPlugin's
 * `decorations` option. This test builds a real EditorView with the
 * extension and verifies the decorations land in the facet.
 */

import { EditorState } from '@codemirror/state';
import { type DecorationSet, EditorView } from '@codemirror/view';
import { afterEach, describe, it, expect } from 'vitest';
import { setOriginalContent, unsavedLineHighlight } from './unsavedLineHighlight';

/** Flush pending microtasks (plugin constructor/deferred pushes). */
function flushMicrotasks(): Promise<void> {
  return Promise.resolve();
}

const mountedParents: HTMLDivElement[] = [];

afterEach(() => {
  while (mountedParents.length) {
    mountedParents.pop()!.remove();
  }
});

/**
 * Create a real EditorView mounted into jsdom. The view must be
 * connected to the document (`isConnected === true`) — the plugin skips
 * deferred decoration pushes for detached views.
 */
function createView(content: string): EditorView {
  const parent = document.createElement('div');
  document.body.appendChild(parent);
  mountedParents.push(parent);
  const state = EditorState.create({
    doc: content,
    extensions: [unsavedLineHighlight()],
  });
  return new EditorView({ state, parent });
}

/** Collect 1-based line numbers carrying a `cm-unsavedLine` decoration. */
function unsavedLineNumbers(view: EditorView): number[] {
  const inputs = view.state.facet(EditorView.decorations);
  const lines: number[] = [];
  for (const input of inputs) {
    if (typeof input === 'function') continue;
    const set = input as DecorationSet;
    const cursor = set.iter();
    while (cursor.value) {
      const cls = (cursor.value.spec as { class?: string }).class ?? '';
      if (cls.includes('cm-unsavedLine')) {
        lines.push(view.state.doc.lineAt(cursor.from).number);
      }
      cursor.next();
    }
  }
  lines.sort((a, b) => a - b);
  return lines;
}

describe('unsavedLineHighlight block-decoration delivery', () => {
  it('constructs without throwing the block-decoration-via-plugin error', async () => {
    const view = createView('line one\nline two\nline three');
    await flushMicrotasks();
    expect(view.state.facet(EditorView.decorations).length).toBeGreaterThan(0);
    view.destroy();
  });

  it('shows no highlights when the document matches originalContent', async () => {
    const view = createView('line one\nline two\nline three');
    view.dispatch({ effects: setOriginalContent.of('line one\nline two\nline three') });
    await flushMicrotasks();
    expect(unsavedLineNumbers(view)).toEqual([]);
    view.destroy();
  });

  it('highlights modified lines after originalContent differs', async () => {
    const view = createView('line one\nline two\nline three');
    view.dispatch({ effects: setOriginalContent.of('line one\nline two\nline three') });
    await flushMicrotasks();
    expect(unsavedLineNumbers(view)).toEqual([]);

    // Original differs on line 2 → immediate rebuild (origChanged path).
    view.dispatch({ effects: setOriginalContent.of('line one\nORIGINAL\nline three') });
    await flushMicrotasks();
    expect(unsavedLineNumbers(view)).toEqual([2]);

    // Line 3 also differs now.
    view.dispatch({ effects: setOriginalContent.of('line one\nline two\nCHANGED') });
    await flushMicrotasks();
    expect(unsavedLineNumbers(view)).toEqual([3]);

    view.destroy();
  });

  it('highlights added lines in a shorter original', async () => {
    const view = createView('line one\nline two\nline three');
    view.dispatch({ effects: setOriginalContent.of('line one') });
    await flushMicrotasks();
    expect(unsavedLineNumbers(view)).toEqual([2, 3]);
    view.destroy();
  });
});
