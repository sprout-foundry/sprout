/**
 * Tests for EditorContextMenu memoization and rendering.
 *
 * - Custom comparator: areContextMenuEqual
 * - Render behavior: context menu items based on props
 */

import { Fragment, act } from 'react';
import { createRoot } from 'react-dom/client';
import {
  EditorContextMenu,
  areContextMenuEqual,
  type EditorContextMenuProps,
} from './EditorContextMenu';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Mock requestAnimationFrame so close-listener effect fires synchronously.
let rafId = 0;
beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  global.requestAnimationFrame = ((cb: (...args: any[]) => void) => {
    rafId += 1;
    cb(Date.now());
    return rafId;
  }) as typeof requestAnimationFrame;
  global.cancelAnimationFrame = vi.fn();
});

vi.mock('lucide-react', () => ({
  Copy: (props: any) => <svg data-testid="copy-icon" {...props} />,
  Navigation: (props: any) => <svg data-testid="navigation-icon" {...props} />,
  Eye: (props: any) => <svg data-testid="eye-icon" {...props} />,
  FolderOpen: (props: any) => <svg data-testid="folderopen-icon" {...props} />,
  ClipboardCopy: (props: any) => <svg data-testid="clipboardcopy-icon" {...props} />,
}));

// ---------------------------------------------------------------------------
// Shared function references (so paired props share the same functions)
// ---------------------------------------------------------------------------

const sharedHideContextMenu = () => {};
const sharedHandleCopySelection = () => {};
const sharedHandleGoToDefinitionFromMenu = () => {};
const sharedHandleFindAllReferencesFromMenu = () => {};
const sharedHandleRevealInExplorer = () => {};
const sharedHandleCopyRelativePath = () => {};
const sharedHandleCopyAbsolutePath = () => {};
const sharedIsSemanticLanguage = () => false;

// ---------------------------------------------------------------------------
// Test factories
// ---------------------------------------------------------------------------

function makeContextMenuBag(overrides: Partial<EditorContextMenuProps['contextMenu']> = {}) {
  return {
    contextMenu: { x: 100, y: 200, hasSelection: false, languageId: undefined },
    workspaceRoot: '/workspace',
    hideContextMenu: sharedHideContextMenu,
    handleCopySelection: sharedHandleCopySelection,
    handleGoToDefinitionFromMenu: sharedHandleGoToDefinitionFromMenu,
    handleFindAllReferencesFromMenu: sharedHandleFindAllReferencesFromMenu,
    handleRevealInExplorer: sharedHandleRevealInExplorer,
    handleCopyRelativePath: sharedHandleCopyRelativePath,
    handleCopyAbsolutePath: sharedHandleCopyAbsolutePath,
    ...overrides,
  };
}

function makeProps(overrides: Partial<EditorContextMenuProps> = {}): EditorContextMenuProps {
  return {
    contextMenu: makeContextMenuBag(),
    isSemanticLanguage: sharedIsSemanticLanguage,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Comparator tests
// ---------------------------------------------------------------------------

describe('areContextMenuEqual', () => {
  describe('returns true when props are equivalent', () => {
    it('identical props objects', () => {
      const props = makeProps();
      expect(areContextMenuEqual(props, props)).toBe(true);
    });

    it('new wrapper objects with same inner function references', () => {
      // Both prev/next share the SAME outer contextMenu bag-of-functions object
      // because the comparator uses !== on the contextMenu prop.
      // But the inner .contextMenu object is also checked with !==, so it must also be shared.
      const sharedInnerContextMenu = { x: 100, y: 200, hasSelection: false };
      const sharedContextMenuBag = makeContextMenuBag({ contextMenu: sharedInnerContextMenu });
      const prev = makeProps({ contextMenu: sharedContextMenuBag });
      const next = makeProps({ contextMenu: sharedContextMenuBag });
      expect(areContextMenuEqual(prev, next)).toBe(true);
    });

    it('same contextMenu inner object reference with same values', () => {
      const cm = { x: 50, y: 60, hasSelection: true };
      const ctx = makeContextMenuBag({ contextMenu: cm });
      const prev = makeProps({ contextMenu: ctx });
      const next = makeProps({ contextMenu: ctx });
      expect(areContextMenuEqual(prev, next)).toBe(true);
    });
  });

  describe('returns false when relevant props differ', () => {
    it('different contextMenu inner object reference', () => {
      const prev = makeProps({ contextMenu: makeContextMenuBag({ contextMenu: { x: 100, y: 200, hasSelection: false } }) });
      const next = makeProps({ contextMenu: makeContextMenuBag({ contextMenu: { x: 100, y: 200, hasSelection: false } }) });
      expect(areContextMenuEqual(prev, next)).toBe(false);
    });

    it('different workspaceRoot', () => {
      const prev = makeProps({ contextMenu: makeContextMenuBag({ workspaceRoot: '/ws1' }) });
      const next = makeProps({ contextMenu: makeContextMenuBag({ workspaceRoot: '/ws2' }) });
      expect(areContextMenuEqual(prev, next)).toBe(false);
    });

    it('different hideContextMenu function', () => {
      const fn1 = vi.fn();
      const fn2 = vi.fn();
      const prev = makeProps({ contextMenu: makeContextMenuBag({ hideContextMenu: fn1 }) });
      const next = makeProps({ contextMenu: makeContextMenuBag({ hideContextMenu: fn2 }) });
      expect(areContextMenuEqual(prev, next)).toBe(false);
    });

    it('different handleCopySelection function', () => {
      const fn1 = vi.fn();
      const fn2 = vi.fn();
      const prev = makeProps({ contextMenu: makeContextMenuBag({ handleCopySelection: fn1 }) });
      const next = makeProps({ contextMenu: makeContextMenuBag({ handleCopySelection: fn2 }) });
      expect(areContextMenuEqual(prev, next)).toBe(false);
    });

    it('different handleGoToDefinitionFromMenu function', () => {
      const fn1 = vi.fn();
      const fn2 = vi.fn();
      const prev = makeProps({ contextMenu: makeContextMenuBag({ handleGoToDefinitionFromMenu: fn1 }) });
      const next = makeProps({ contextMenu: makeContextMenuBag({ handleGoToDefinitionFromMenu: fn2 }) });
      expect(areContextMenuEqual(prev, next)).toBe(false);
    });

    it('different handleFindAllReferencesFromMenu function', () => {
      const fn1 = vi.fn();
      const fn2 = vi.fn();
      const prev = makeProps({ contextMenu: makeContextMenuBag({ handleFindAllReferencesFromMenu: fn1 }) });
      const next = makeProps({ contextMenu: makeContextMenuBag({ handleFindAllReferencesFromMenu: fn2 }) });
      expect(areContextMenuEqual(prev, next)).toBe(false);
    });

    it('different handleRevealInExplorer function', () => {
      const fn1 = vi.fn();
      const fn2 = vi.fn();
      const prev = makeProps({ contextMenu: makeContextMenuBag({ handleRevealInExplorer: fn1 }) });
      const next = makeProps({ contextMenu: makeContextMenuBag({ handleRevealInExplorer: fn2 }) });
      expect(areContextMenuEqual(prev, next)).toBe(false);
    });

    it('different handleCopyRelativePath function', () => {
      const fn1 = vi.fn();
      const fn2 = vi.fn();
      const prev = makeProps({ contextMenu: makeContextMenuBag({ handleCopyRelativePath: fn1 }) });
      const next = makeProps({ contextMenu: makeContextMenuBag({ handleCopyRelativePath: fn2 }) });
      expect(areContextMenuEqual(prev, next)).toBe(false);
    });

    it('different handleCopyAbsolutePath function', () => {
      const fn1 = vi.fn();
      const fn2 = vi.fn();
      const prev = makeProps({ contextMenu: makeContextMenuBag({ handleCopyAbsolutePath: fn1 }) });
      const next = makeProps({ contextMenu: makeContextMenuBag({ handleCopyAbsolutePath: fn2 }) });
      expect(areContextMenuEqual(prev, next)).toBe(false);
    });

    it('different isSemanticLanguage function', () => {
      const fn1 = vi.fn(() => true);
      const fn2 = vi.fn(() => false);
      const prev = makeProps({ isSemanticLanguage: fn1 });
      const next = makeProps({ isSemanticLanguage: fn2 });
      expect(areContextMenuEqual(prev, next)).toBe(false);
    });
  });
});

// ---------------------------------------------------------------------------
// Render tests
// ---------------------------------------------------------------------------

describe('EditorContextMenu rendering', () => {
  let mountPoint: HTMLDivElement | null = null;
  let root: ReturnType<typeof createRoot> | null = null;

  beforeEach(() => {
    vi.clearAllMocks();
    mountPoint = document.createElement('div');
    document.body.appendChild(mountPoint);
  });

  afterEach(() => {
    act(() => {
      if (root) {
        root.unmount();
      }
    });
    if (mountPoint) {
      mountPoint.remove();
    }
    // Clean up any portal-attached menus
    document.querySelectorAll('.context-menu').forEach((el) => el.remove());
  });

  function renderContextMenu(props: EditorContextMenuProps) {
    act(() => {
      root = createRoot(mountPoint!);
      root.render(<EditorContextMenu {...props} />);
    });
  }

  it('renders Reveal in Explorer button (always shown)', () => {
    renderContextMenu({
      contextMenu: makeContextMenuBag({
        contextMenu: { x: 100, y: 200, hasSelection: false },
      }),
      isSemanticLanguage: () => false,
    });
    const menu = document.querySelector('.context-menu');
    expect(menu).not.toBeNull();
    const items = menu!.querySelectorAll('.context-menu-item');
    expect(items.length).toBeGreaterThanOrEqual(1);
    expect(items[0].querySelector('.menu-item-label')?.textContent).toBe('Reveal in Explorer');
  });

  it('renders Copy relative path button (always shown)', () => {
    renderContextMenu({
      contextMenu: makeContextMenuBag({
        contextMenu: { x: 100, y: 200, hasSelection: false },
      }),
      isSemanticLanguage: () => false,
    });
    const menu = document.querySelector('.context-menu');
    const items = menu!.querySelectorAll('.context-menu-item');
    expect(items.length).toBeGreaterThanOrEqual(2);
  });

  it('renders Copy absolute path when workspaceRoot is set', () => {
    renderContextMenu({
      contextMenu: makeContextMenuBag({
        contextMenu: { x: 100, y: 200, hasSelection: false },
        workspaceRoot: '/workspace',
      }),
      isSemanticLanguage: () => false,
    });
    const menu = document.querySelector('.context-menu');
    const items = menu!.querySelectorAll('.context-menu-item');
    expect(items.length).toBe(3);
  });

  it('hides Copy absolute path when workspaceRoot is null', () => {
    renderContextMenu({
      contextMenu: makeContextMenuBag({
        contextMenu: { x: 100, y: 200, hasSelection: false },
        workspaceRoot: null,
      }),
      isSemanticLanguage: () => false,
    });
    const menu = document.querySelector('.context-menu');
    const items = menu!.querySelectorAll('.context-menu-item');
    expect(items.length).toBe(2);
  });

  it('renders Copy button when hasSelection is true', () => {
    const handleCopySelection = vi.fn();
    renderContextMenu({
      contextMenu: makeContextMenuBag({
        contextMenu: { x: 100, y: 200, hasSelection: true },
        handleCopySelection,
      }),
      isSemanticLanguage: () => false,
    });
    const menu = document.querySelector('.context-menu');
    const items = menu!.querySelectorAll('.context-menu-item');
    expect(items[0].querySelector('.menu-item-label')?.textContent).toBe('Copy');
  });

  it('renders Go to Definition and Find All References for semantic languages', () => {
    renderContextMenu({
      contextMenu: makeContextMenuBag({
        contextMenu: { x: 100, y: 200, hasSelection: false, languageId: 'typescript' },
      }),
      isSemanticLanguage: () => true,
    });
    const menu = document.querySelector('.context-menu');
    const labels = Array.from(menu!.querySelectorAll('.menu-item-label')).map(
      (el) => el.textContent,
    );
    expect(labels).toContain('Go to Definition');
    expect(labels).toContain('Find All References');
  });

  it('hides Go to Definition and Find All References for non-semantic languages', () => {
    renderContextMenu({
      contextMenu: makeContextMenuBag({
        contextMenu: { x: 100, y: 200, hasSelection: false, languageId: 'plain-text' },
      }),
      isSemanticLanguage: () => false,
    });
    const menu = document.querySelector('.context-menu');
    const labels = Array.from(menu!.querySelectorAll('.menu-item-label')).map(
      (el) => el.textContent,
    );
    expect(labels).not.toContain('Go to Definition');
    expect(labels).not.toContain('Find All References');
  });

  it('renders divider when selection or semantic features are present', () => {
    renderContextMenu({
      contextMenu: makeContextMenuBag({
        contextMenu: { x: 100, y: 200, hasSelection: true },
      }),
      isSemanticLanguage: () => false,
    });
    const menu = document.querySelector('.context-menu');
    expect(menu!.querySelector('.context-menu-divider')).toBeTruthy();
  });

  it('calls handleCopySelection when Copy is clicked', () => {
    const handleCopySelection = vi.fn();
    renderContextMenu({
      contextMenu: makeContextMenuBag({
        contextMenu: { x: 100, y: 200, hasSelection: true },
        handleCopySelection,
      }),
      isSemanticLanguage: () => false,
    });
    const menu = document.querySelector('.context-menu');
    act(() => {
      menu!.querySelector('.context-menu-item')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(handleCopySelection).toHaveBeenCalled();
  });

  it('does not render menu when contextMenu is null', () => {
    renderContextMenu({
      contextMenu: makeContextMenuBag({ contextMenu: null }),
      isSemanticLanguage: () => false,
    });
    expect(document.querySelector('.context-menu')).toBeFalsy();
  });
});
