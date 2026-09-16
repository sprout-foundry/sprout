import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { describe, it, expect } from 'vitest';
import DiffView from './DiffView';

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  // @ts-expect-error test env flag
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

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
});

function render(diff: string) {
  // Raw createRoot (not RTL render) — act is required here.
  // eslint-disable-next-line testing-library/no-unnecessary-act
  act(() => {
    root.render(createElement(DiffView, { diff }));
  });
}

const lineKinds = () =>
  Array.from(container.querySelectorAll('.diff-line')).map((el) => {
    const cls = el.className;
    return cls.includes('diff-line--add')
      ? 'add'
      : cls.includes('diff-line--del')
        ? 'del'
        : cls.includes('diff-line--hunk')
          ? 'hunk'
          : cls.includes('diff-line--meta')
            ? 'meta'
            : 'context';
  });

describe('DiffView', () => {
  it('classifies add, del, hunk, and header lines', () => {
    // difflib output (what /api/changes/diff emits) has ---/+++ headers
    // but no "diff --git" line.
    render(['--- a/x.go', '+++ b/x.go', '@@ -1,2 +1,2 @@', '-old', '+new', ' ctx'].join('\n'));
    expect(lineKinds()).toEqual(['meta', 'meta', 'hunk', 'del', 'add', 'context']);
  });

  it('renders a plain placeholder line as context, not as a deletion', () => {
    // "(no tracked change for x)" starts with "(" — must NOT classify as add/del.
    render('(no tracked change for main.go)');
    expect(lineKinds()).toEqual(['context']);
  });

  it('drops the trailing-newline split artifact', () => {
    render('+a\n+b\n');
    expect(container.querySelectorAll('.diff-line').length).toBe(2);
  });

  it('prefixes gutter marks for add and del rows', () => {
    render('-old\n+new');
    const gutters = Array.from(container.querySelectorAll('.diff-line-gutter')).map((el) => el.textContent);
    expect(gutters).toEqual(['−', '+']);
  });

  it('re-renders when the diff changes', () => {
    render('-old\n');
    // eslint-disable-next-line testing-library/no-unnecessary-act
    act(() => {
      root.render(createElement(DiffView, { diff: '+new\n' }));
    });
    expect(lineKinds()).toEqual(['add']);
  });

  it('renders an empty diff as an empty view without crashing', () => {
    render('');
    expect(container.querySelectorAll('.diff-line').length).toBe(0);
  });

  it('windows large diffs behind a show-all expander', () => {
    const many = Array.from({ length: 2000 }, (_, i) => `+added ${i}`).join('\n');
    render(many);

    const rows = () => container.querySelectorAll('.diff-line').length;
    expect(rows()).toBe(1500);
    const expand = container.querySelector('.diff-view-expand')!;
    expect(expand.textContent).toContain('500 more lines');

    act(() => {
      (expand as HTMLButtonElement).click();
    });
    expect(rows()).toBe(2000);
    expect(container.querySelector('.diff-view-expand')).toBeNull();
  });

  it('does not render an expander under the window size', () => {
    render('+a\n+b\n');
    expect(container.querySelector('.diff-view-expand')).toBeNull();
  });
});
