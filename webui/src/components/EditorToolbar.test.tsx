import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import EditorToolbar from './EditorToolbar';

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  (globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const renderToolbar = (modified?: boolean) =>
  act(() => root.render(<EditorToolbar breadcrumbProps={{ filePath: '/README', showFileName: true, modified }} />));

describe('EditorToolbar', () => {
  it('shows the unsaved dot only when asked to', () => {
    renderToolbar(true);
    expect(container.querySelector('.toolbar-modified')?.getAttribute('aria-label')).toBe('Unsaved changes');
    renderToolbar(false);
    expect(container.querySelector('.toolbar-modified')).toBeNull();
  });
});
