import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../config/layout', () => ({ isLayeredLayout: true }));
vi.mock('./DocumentOutlinePanel', () => ({
  default: (props: { isCollapsed: boolean }) => (
    <div data-testid="outline" data-collapsed={String(props.isCollapsed)} />
  ),
}));

import EditorWithOutline from './EditorWithOutline';

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  (globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  localStorage.removeItem('sprout.outline-panel.collapsed');
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

function mount() {
  act(() =>
    root.render(
      <EditorWithOutline content="" fileExtension=".md" cursorLine={1} isFileOpen onNavigateToSymbol={() => {}}>
        <div />
      </EditorWithOutline>,
    ),
  );
  return container.querySelector('[data-testid="outline"]')?.getAttribute('data-collapsed');
}

describe('EditorWithOutline in the layered layout', () => {
  it('starts collapsed when the user never chose', () => {
    expect(mount()).toBe('true');
  });

  it('keeps the outline open once the user opened it', () => {
    localStorage.setItem('sprout.outline-panel.collapsed', '0');
    expect(mount()).toBe('false');
  });
});
