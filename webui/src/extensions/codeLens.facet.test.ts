/**
 * codeLens.facet.test.ts — Integration test for the code lens block-widget
 * delivery path. Block widgets supplied by a ViewPlugin make CodeMirror throw
 * "Block decorations may not be specified via plugins", so the lenses must
 * arrive through a StateField. Uses a real EditorView.
 */

import { EditorState } from '@codemirror/state';
import { EditorView } from '@codemirror/view';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { codeLensPlugin } from './codeLens';

const GO_SOURCE = `package main

func helper() int {
\treturn 1
}

func main() {
\thelper()
\thelper()
}
`;

const views: EditorView[] = [];

afterEach(() => {
  while (views.length) {
    const view = views.pop()!;
    view.dom.parentElement?.remove();
    view.destroy();
  }
  vi.useRealTimers();
});

function mount(doc: string): EditorView {
  const parent = document.createElement('div');
  document.body.appendChild(parent);
  const view = new EditorView({
    state: EditorState.create({ doc, extensions: [codeLensPlugin(() => '.go')] }),
    parent,
  });
  views.push(view);
  return view;
}

describe('codeLensPlugin', () => {
  it('renders Go lenses as block widgets without throwing', () => {
    vi.useFakeTimers();
    const view = mount(GO_SOURCE);
    expect(() => vi.advanceTimersByTime(400)).not.toThrow();
    const lenses = Array.from(view.dom.querySelectorAll('.cm-codeLens')).map((el) => el.textContent);
    expect(lenses).toContain('2 refs');
  });

  it('keeps lens positions valid when the document shrinks before recompute', () => {
    vi.useFakeTimers();
    const view = mount(GO_SOURCE);
    vi.advanceTimersByTime(400);
    expect(() => view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: 'x' } })).not.toThrow();
    expect(() => vi.advanceTimersByTime(400)).not.toThrow();
    expect(view.dom.querySelectorAll('.cm-codeLens')).toHaveLength(0);
  });
});
