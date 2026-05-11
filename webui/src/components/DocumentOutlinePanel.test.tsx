// @ts-nocheck

import ReactDOM from 'react-dom';
import { act } from 'react-dom/test-utils';
import DocumentOutlinePanel from './DocumentOutlinePanel';

// ── JSDOM polyfills and compat ───────────────────────────────────────────

const originalError = console.error;
beforeAll(() => {
  Element.prototype.scrollIntoView = vi.fn();
  console.error = (...args: any[]) => {
    if (typeof args[0] === 'string' && args[0].includes('ReactDOM.render is no longer supported')) return;
    originalError.call(console, ...args);
  };
});
afterAll(() => {
  console.error = originalError;
});

// ── Helpers ──────────────────────────────────────────────────────────────

const tsContent = [
  'function topLevel() {',
  '  return true;',
  '}',
  '',
  'class MyClass {',
  '  constructor() {}',
  '  myMethod() { return 42; }',
  '  private helper() { return true; }',
  '}',
  '',
  'interface MyInterface { name: string; age: number; }',
  '',
  'const myVar = 42;',
  "const MY_CONSTANT = 'hello';",
].join('\n');

/** Helper to grab names rendered in the tree. */
function renderedNames(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll('.outline-node-name')).map((el) => el.textContent);
}

/** Shared props builder. */
function defaultProps(overrides: Record<string, any> = {}) {
  return {
    content: '',
    fileExtension: '.ts',
    cursorLine: 1,
    onNavigateToSymbol: vi.fn(),
    isFileOpen: true,
    isCollapsed: false,
    onToggleCollapse: vi.fn(),
    ...overrides,
  };
}

/** Render once, like GoToSymbolOverlay test helper. */
function renderPanel(overrides: Record<string, any> = {}) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const props = defaultProps(overrides);
  act(() => {
    ReactDOM.render(<DocumentOutlinePanel {...props} />, container);
  });
  return {
    container,
    props,
    unmount() {
      act(() => {
        ReactDOM.unmountComponentAtNode(container);
        document.body.removeChild(container);
      });
    },
  };
}

/**
 * Render panel with an initial cursorLine of 0, then re-render with
 * `targetLine` to trigger the auto-expand effect (which only fires
 * when cursorLine changes from its previous value).
 */
function renderWithAutoExpand(content: string, targetLine: number) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const props = defaultProps({ content, cursorLine: 0 });
  act(() => {
    ReactDOM.render(<DocumentOutlinePanel {...props} />, container);
  });
  act(() => {
    ReactDOM.render(<DocumentOutlinePanel {...{ ...props, cursorLine: targetLine }} />, container);
  });
  return {
    container,
    props,
    unmount() {
      act(() => {
        ReactDOM.unmountComponentAtNode(container);
        document.body.removeChild(container);
      });
    },
  };
}

/** Simulate typing into the search input (jsdom compat). */
function typeSearch(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set!;
  setter.call(input, value);
  input.dispatchEvent(new Event('input', { bubbles: true }));
  input.dispatchEvent(new Event('change', { bubbles: true }));
}

// ── Tests ────────────────────────────────────────────────────────────────

describe('DocumentOutlinePanel', () => {
  // ── Basic Rendering ────────────────────────────────────────────────

  describe('Basic rendering', () => {
    it('shows "No file open" empty state when isFileOpen=false', () => {
      const { container, unmount } = renderPanel({ isFileOpen: false });
      try {
        expect(container.querySelector('.outline-empty-text')?.textContent).toContain('No file open');
      } finally {
        unmount();
      }
    });

    it('shows "No symbols found" for comment-only content', () => {
      const { container, unmount } = renderPanel({ content: '// only comments\n/* block */\n' });
      try {
        expect(container.querySelector('.outline-empty-text')?.textContent).toContain('No symbols found');
      } finally {
        unmount();
      }
    });

    it('renders symbol tree when content has extractable symbols', () => {
      const { container, unmount } = renderPanel({ content: tsContent });
      try {
        expect(container.querySelectorAll('.outline-tree-node').length).toBeGreaterThan(0);
        expect(renderedNames(container)).toContain('topLevel');
      } finally {
        unmount();
      }
    });

    it('always shows "Outline" in the panel header', () => {
      const { container, unmount } = renderPanel({ isFileOpen: false });
      try {
        expect(container.querySelector('.outline-panel-title')?.textContent).toBe('Outline');
      } finally {
        unmount();
      }
    });
  });

  // ── TypeScript Symbols ─────────────────────────────────────────────

  describe('TypeScript symbols', () => {
    it('displays a top-level function symbol', () => {
      const { container, unmount } = renderPanel({ content: 'function foo() {}\n' });
      try {
        expect(renderedNames(container)).toContain('foo');
        expect(container.querySelector('.outline-kind-icon.function')).not.toBeNull();
      } finally {
        unmount();
      }
    });

    it('displays a class symbol', () => {
      const { container, unmount } = renderPanel({ content: 'class Foo {}\n' });
      try {
        expect(renderedNames(container)).toContain('Foo');
        expect(container.querySelector('.outline-kind-icon.class')).not.toBeNull();
      } finally {
        unmount();
      }
    });

    it('displays an interface symbol', () => {
      const { container, unmount } = renderPanel({ content: 'interface MyInterface { name: string; }\n' });
      try {
        expect(renderedNames(container).some((n) => n?.includes('MyInterface'))).toBe(true);
      } finally {
        unmount();
      }
    });

    it('shows nested children for class with methods after cursor moves inside', () => {
      const { container, unmount } = renderWithAutoExpand('class MyClass {\n  myMethod() {}\n}\n', 2);
      try {
        expect(renderedNames(container)).toContain('MyClass');
        expect(container.querySelector('.outline-children')).not.toBeNull();
        expect(renderedNames(container)).toContain('myMethod');
      } finally {
        unmount();
      }
    });
  });

  // ── Search / Filter ────────────────────────────────────────────────

  describe('Search / filter', () => {
    it('filters symbols when typing in search input', () => {
      const { container, unmount } = renderPanel({ content: tsContent });
      try {
        const input = container.querySelector('.outline-search-input') as HTMLInputElement;
        act(() => typeSearch(input, 'topLevel'));
        expect(renderedNames(container)).toContain('topLevel');
      } finally {
        unmount();
      }
    });

    it('clears search and shows all symbols again', () => {
      const content = 'function alpha() {}\nfunction beta() {}\nfunction gamma() {}\n';
      const { container, unmount } = renderPanel({ content });
      try {
        const input = container.querySelector('.outline-search-input') as HTMLInputElement;
        act(() => typeSearch(input, 'alpha'));
        act(() => typeSearch(input, ''));
        const names = renderedNames(container);
        expect(names).toContain('alpha');
        expect(names).toContain('beta');
        expect(names).toContain('gamma');
      } finally {
        unmount();
      }
    });
  });

  // ── Cursor Sync ────────────────────────────────────────────────────

  describe('Cursor sync', () => {
    it('highlights the enclosing symbol at cursor position', () => {
      const { container, unmount } = renderPanel({
        content: tsContent,
        cursorLine: 2,
      });
      try {
        const active = container.querySelector('.outline-tree-node.active');
        expect(active).not.toBeNull();
        expect(active?.querySelector('.outline-node-name')?.textContent).toBe('topLevel');
      } finally {
        unmount();
      }
    });
  });

  // ── Click to Navigate ──────────────────────────────────────────────

  describe('Click to navigate', () => {
    it('calls onNavigateToSymbol with correct line when a symbol is clicked', () => {
      const { container, props, unmount } = renderPanel({
        content: 'function hello() {}\nfunction world() {}\n',
      });
      try {
        const helloNode = container.querySelector('[data-line="1"]');
        expect(helloNode).not.toBeNull();
        act(() => {
          helloNode.dispatchEvent(new MouseEvent('click', { bubbles: true }));
        });
        expect(props.onNavigateToSymbol).toHaveBeenCalledWith(1);
      } finally {
        unmount();
      }
    });
  });

  // ── Collapse / Expand ──────────────────────────────────────────────

  describe('Collapse / expand', () => {
    const classContent = 'class MyClass {\n  methodA() {}\n  methodB() {}\n}\n';

    it('collapse all button hides children of container symbols', () => {
      const { container, unmount } = renderWithAutoExpand(classContent, 2);
      try {
        expect(container.querySelector('.outline-children')).not.toBeNull();
        const btns = container.querySelectorAll('.outline-panel-toggle');
        act(() => {
          btns[btns.length - 1].dispatchEvent(new MouseEvent('click', { bubbles: true }));
        });
        expect(container.querySelector('.outline-children')).toBeNull();
      } finally {
        unmount();
      }
    });

    it('expand all button restores collapsed children', () => {
      const { container, unmount } = renderWithAutoExpand(classContent, 2);
      try {
        const btns = container.querySelectorAll('.outline-panel-toggle');
        const collapseBtn = btns[btns.length - 1];
        const expandBtn = btns[btns.length - 2];
        act(() => collapseBtn.dispatchEvent(new MouseEvent('click', { bubbles: true })));
        expect(container.querySelector('.outline-children')).toBeNull();
        act(() => expandBtn.dispatchEvent(new MouseEvent('click', { bubbles: true })));
        expect(container.querySelector('.outline-children')).not.toBeNull();
      } finally {
        unmount();
      }
    });

    it('chevron on container shows correct aria-expanded when expanded', () => {
      const { container, unmount } = renderWithAutoExpand('class MyClass {\n  methodA() {}\n}\n', 2);
      try {
        const chevron = container.querySelector('.outline-node-chevron');
        expect(chevron).not.toBeNull();
        expect(chevron.getAttribute('aria-expanded')).toBe('true');
        expect(chevron.classList.contains('expanded')).toBe(true);
      } finally {
        unmount();
      }
    });
  });

  // ── Panel Collapse ─────────────────────────────────────────────────

  describe('Panel collapse', () => {
    it('hides tree and search when isCollapsed=true', () => {
      const { container, unmount } = renderPanel({
        content: tsContent,
        isCollapsed: true,
      });
      try {
        expect(container.querySelector('.outline-panel')?.classList.contains('collapsed')).toBe(true);
        expect(container.querySelector('.outline-search-input')).toBeNull();
        expect(container.querySelector('.outline-panel-tree')).toBeNull();
        expect(container.querySelector('.outline-panel-toggle')).not.toBeNull();
      } finally {
        unmount();
      }
    });
  });
});
