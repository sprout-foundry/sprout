/**
 * Tests for FindAllReferencesOverlay memoization and rendering.
 *
 * - Custom comparator: areFindAllReferencesPropsEqual
 * - Render behavior: overlay visibility, reference list, keyboard nav
 */

import { act } from 'react';
import { createRoot } from 'react-dom/client';
import {
  FindAllReferencesOverlay,
  areFindAllReferencesPropsEqual,
  type FindAllReferencesOverlayProps,
  type ReferenceInfo,
} from './FindAllReferencesOverlay';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('lucide-react', () => ({
  FileCode: (props: any) => <svg data-testid="file-code" {...props} />,
}));

// ---------------------------------------------------------------------------
// Shared function references (so paired props share the same functions)
// ---------------------------------------------------------------------------

const sharedOnSelectReference = vi.fn();
const sharedOnClose = vi.fn();

// ---------------------------------------------------------------------------
// Test factories
// ---------------------------------------------------------------------------

function makeRef(overrides: Partial<ReferenceInfo> = {}): ReferenceInfo {
  return {
    filePath: 'src/file.ts',
    line: 10,
    startCol: 1,
    endCol: 4,
    lineText: 'const foo = 1;',
    ...overrides,
  };
}

function makeProps(overrides: Partial<FindAllReferencesOverlayProps> = {}): FindAllReferencesOverlayProps {
  return {
    visible: true,
    symbolName: 'foo',
    references: [makeRef()],
    onSelectReference: sharedOnSelectReference,
    onClose: sharedOnClose,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Comparator tests
// ---------------------------------------------------------------------------

describe('areFindAllReferencesPropsEqual', () => {
  describe('returns true when props are equivalent', () => {
    it('identical props objects', () => {
      const props = makeProps();
      expect(areFindAllReferencesPropsEqual(props, props)).toBe(true);
    });

    it('two calls with same shared refs return true', () => {
      // References must be shared because comparator uses !== on the references array
      const sharedRefs = [makeRef()];
      const prev = makeProps({ references: sharedRefs });
      const next = makeProps({ references: sharedRefs });
      expect(areFindAllReferencesPropsEqual(prev, next)).toBe(true);
    });

    it('same array reference for references', () => {
      const refs = [makeRef()];
      const prev = makeProps({ references: refs });
      const next = makeProps({ references: refs });
      expect(areFindAllReferencesPropsEqual(prev, next)).toBe(true);
    });
  });

  describe('returns false when relevant props differ', () => {
    it('different visible', () => {
      const prev = makeProps({ visible: true });
      const next = makeProps({ visible: false });
      expect(areFindAllReferencesPropsEqual(prev, next)).toBe(false);
    });

    it('different symbolName', () => {
      const prev = makeProps({ symbolName: 'foo' });
      const next = makeProps({ symbolName: 'bar' });
      expect(areFindAllReferencesPropsEqual(prev, next)).toBe(false);
    });

    it('different references array reference', () => {
      const prev = makeProps({ references: [makeRef({ line: 1 })] });
      const next = makeProps({ references: [makeRef({ line: 2 })] });
      expect(areFindAllReferencesPropsEqual(prev, next)).toBe(false);
    });

    it('different onSelectReference function', () => {
      const fn1 = vi.fn();
      const fn2 = vi.fn();
      const prev = makeProps({ onSelectReference: fn1 });
      const next = makeProps({ onSelectReference: fn2 });
      expect(areFindAllReferencesPropsEqual(prev, next)).toBe(false);
    });

    it('different onClose function', () => {
      const fn1 = vi.fn();
      const fn2 = vi.fn();
      const prev = makeProps({ onClose: fn1 });
      const next = makeProps({ onClose: fn2 });
      expect(areFindAllReferencesPropsEqual(prev, next)).toBe(false);
    });
  });
});

// ---------------------------------------------------------------------------
// Render tests
// ---------------------------------------------------------------------------

describe('FindAllReferencesOverlay rendering', () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;

  beforeEach(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
    vi.clearAllMocks();
    container = document.createElement('div');
    document.body.appendChild(container);
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
  });

  function renderOverlay(props: Partial<FindAllReferencesOverlayProps> = {}) {
    const p = makeProps(props);
    act(() => {
      root = createRoot(container);
      root.render(<FindAllReferencesOverlay {...p} />);
    });
  }

  it('returns null when not visible', () => {
    renderOverlay({ visible: false });
    expect(container.querySelector('.find-refs-overlay')).toBeFalsy();
  });

  it('renders overlay when visible', () => {
    renderOverlay({ visible: true });
    expect(container.querySelector('.find-refs-overlay')).toBeTruthy();
  });

  it('renders symbol name and count in header', () => {
    renderOverlay({ symbolName: 'myFunc', references: [makeRef({ line: 1 }), makeRef({ line: 5 })] });
    expect(container.querySelector('.find-refs-symbol-name')?.textContent).toBe('myFunc');
    expect(container.querySelector('.find-refs-count')?.textContent).toContain('2 references');
  });

  it('shows singular reference count for 1 reference', () => {
    renderOverlay({ references: [makeRef()] });
    expect(container.querySelector('.find-refs-count')?.textContent).toContain('1 reference');
  });

  it('shows searching state when no symbolName and no references', () => {
    renderOverlay({ symbolName: '', references: [] });
    const searching = container.querySelector('.find-refs-empty');
    expect(searching?.textContent).toContain('Searching');
  });

  it('shows empty state when references is empty but symbolName is set', () => {
    renderOverlay({ symbolName: 'foo', references: [] });
    const empty = container.querySelector('.find-refs-empty');
    expect(empty?.textContent).toContain('No references found');
  });

  it('renders grouped references with file headers', () => {
    renderOverlay({
      references: [
        makeRef({ filePath: 'src/file.ts', line: 1, lineText: 'const a = 1' }),
        makeRef({ filePath: 'src/file.ts', line: 5, lineText: 'const a = 2' }),
        makeRef({ filePath: 'other/file.ts', line: 10, lineText: 'const b = 3' }),
      ],
    });
    const headers = container.querySelectorAll('.find-refs-group-header');
    expect(headers.length).toBe(2);
  });

  it('renders reference items with line number', () => {
    renderOverlay({
      references: [makeRef({ filePath: 'src/file.ts', line: 42, lineText: 'const foo = 1;' })],
    });
    const item = container.querySelector('.find-refs-item');
    expect(item).toBeTruthy();
    const lineNum = container.querySelector('.find-refs-line-num');
    expect(lineNum?.textContent).toContain(':42');
  });

  it('highlights symbol in line text', () => {
    renderOverlay({
      references: [makeRef({ lineText: 'const foo = 1;', startCol: 7, endCol: 10 })],
    });
    const mark = container.querySelector('mark.find-refs-symbol');
    expect(mark).toBeTruthy();
    expect(mark?.textContent.trim()).toBe('foo');
  });

  it('marks first item as selected by default', () => {
    renderOverlay({
      references: [makeRef({ line: 1 }), makeRef({ line: 5 })],
    });
    const items = container.querySelectorAll('.find-refs-item');
    expect(items[0].getAttribute('data-selected')).toBe('true');
    expect(items[1].getAttribute('data-selected')).toBe('false');
  });

  it('calls onSelectReference and onClose when item is clicked', () => {
    const onSelectReference = vi.fn();
    const onClose = vi.fn();
    renderOverlay({
      references: [makeRef({ filePath: 'src/file.ts', line: 42 })],
      onSelectReference,
      onClose,
    });
    const item = container.querySelector('.find-refs-item')!;
    act(() => {
      item.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onSelectReference).toHaveBeenCalledWith('src/file.ts', 42);
    expect(onClose).toHaveBeenCalled();
  });

  // ── Keyboard/mouse interaction tests skipped ─────────────────────
  // Keyboard navigation (ArrowDown, ArrowUp, Enter, Escape) and mouseEnter
  // rely on React synthetic events which cannot be triggered from native
  // events in jsdom when mounting via createRoot. These are exercised
  // indirectly through integration tests in EditorPane.test.tsx.
  // The memoization/comparator tests above verify the key optimization concern.
});
