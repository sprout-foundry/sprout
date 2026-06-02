// @ts-nocheck

import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { useEditorManager } from '../contexts/EditorManagerContext';
import EditorTabs from './EditorTabs';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../contexts/EditorManagerContext', () => ({
  useEditorManager: vi.fn(),
}));

// Mock lucide-react icons — return null to avoid SVG rendering issues in jsdom.
// EditorTabs passes props like `size` and `fill` that these mocks safely ignore.
vi.mock('lucide-react', () => {
  const mockComponent = (props: any) => null;
  const icons = [
    'X', 'AlertTriangle', 'FolderOpen', 'FileCode', 'FileText', 'File',
    'Code2', 'Globe', 'Palette', 'Settings', 'Terminal', 'Braces',
    'MessageSquareText', 'GitCompareArrows', 'ShieldCheck', 'ArrowRightLeft',
    'PanelRightOpen', 'Eye', 'Sparkles', 'Pin', 'Plus', 'GitBranch',
    'Pencil', 'Trash2', 'ImageIcon', 'Video', 'Headphones', 'FileWarning',
  ];
  const result: Record<string, any> = {};
  icons.forEach((name) => {
    result[name] = mockComponent;
  });
  result.default = mockComponent;
  return result;
});

vi.mock('@sprout/ui', () => ({
  ContextMenu: ({ children, isOpen, onClose, className, zIndex }: any) => {
    if (!isOpen) return null;
    return (
      <div className={className || 'context-menu'} style={{ position: 'fixed', zIndex }} onClick={onClose}>
        {children}
      </div>
    );
  },
}));

vi.mock('./ThemedDialog', () => ({
  showThemedConfirm: vi.fn().mockResolvedValue(true),
}));

let rafId = 0;
beforeAll(() => {
  (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
  global.requestAnimationFrame = ((cb: FrameRequestCallback) => {
    rafId += 1;
    cb(Date.now());
    return rafId;
  }) as typeof requestAnimationFrame;
  global.cancelAnimationFrame = vi.fn();
  // scrollIntoView does not exist in jsdom
  Element.prototype.scrollIntoView = vi.fn();
});

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const makeMockBuffer = (id: string, paneId: string, overrides: Partial<any> = {}) => ({
  id,
  kind: 'file',
  file: {
    path: `src/${id}.tsx`,
    name: `${id}.tsx`,
    ext: '.tsx',
    isDir: false,
    size: 123,
    modified: 0,
  },
  content: 'line1',
  originalContent: 'line1',
  cursorPosition: { line: 0, column: 0 },
  scrollPosition: { top: 0, left: 0 },
  isModified: false,
  isActive: true,
  isClosable: true,
  metadata: {},
  paneId,
  ...overrides,
});

const mockCloseBuffer = vi.fn();
const mockSwitchToBuffer = vi.fn();
const mockSwitchPane = vi.fn();
const mockReorderBuffers = vi.fn();
const mockMoveBufferToPane = vi.fn();
const mockToggleBufferPin = vi.fn();

const defaultMockEditorManager = {
  buffers: new Map<string, any>(),
  panes: [{ id: 'pane-1', bufferId: null, isActive: true }],
  activeBufferId: null,
  activePaneId: 'pane-1',
  switchPane: mockSwitchPane,
  switchToBuffer: mockSwitchToBuffer,
  closeBuffer: mockCloseBuffer,
  reorderBuffers: mockReorderBuffers,
  moveBufferToPane: mockMoveBufferToPane,
  toggleBufferPin: mockToggleBufferPin,
};

const mockUseEditorManager = useEditorManager as vi.MockedFunction<typeof useEditorManager>;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;

beforeEach(() => {
  vi.clearAllMocks();
  mockCloseBuffer.mockClear();
  mockSwitchToBuffer.mockClear();
  mockSwitchPane.mockClear();
  mockReorderBuffers.mockClear();
  mockMoveBufferToPane.mockClear();
  mockToggleBufferPin.mockClear();

  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container!);
});

afterEach(() => {
  act(() => {
    if (root) {
      root.unmount();
      root = null;
    }
  });
  if (container) {
    document.body.removeChild(container);
    container = null;
  }
  // Clean up any portal menus left on body
  document.querySelectorAll('.tab-context-menu').forEach((el) => {
    if (el.parentNode) el.parentNode.removeChild(el);
  });
  document.querySelectorAll('.context-menu').forEach((el) => {
    if (el.parentNode) el.parentNode.removeChild(el);
  });
  document.querySelectorAll('.close-confirm-overlay').forEach((el) => {
    if (el.parentNode) el.parentNode.removeChild(el);
  });
});

function renderEditorTabs(props: { paneId?: string; actions?: ReactNode; compact?: boolean } = {}) {
  act(() => {
    root!.render(<EditorTabs paneId={props.paneId} actions={props.actions} compact={props.compact} />);
  });
}

/** Returns all visible `.tab-context-menu` elements on document.body (portals). */
function getContextMenuElements(): Element[] {
  return Array.from(document.body.querySelectorAll('.tab-context-menu'));
}

/** Returns menu items (`.tab-context-item`) from the first visible empty area menu. */
function getMenuItems(menu?: Element): Element[] {
  const m = menu || getContextMenuElements()[0];
  return m ? Array.from(m.querySelectorAll('.context-menu-item')) : [];
}

/** Get text content of each menu item. */
function getMenuTexts(menu?: Element): string[] {
  return getMenuItems(menu)
    .map((el) => el.textContent?.trim() ?? '')
    .filter(Boolean);
}

/** Dispatch a contextmenu MouseEvent on `target` inside act(). */
function fireContextMenu(target: Element, x = 200, y = 200) {
  act(() => {
    const event = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: x,
      clientY: y,
    });
    target.dispatchEvent(event);
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('EditorTabs empty area context menu', () => {
  describe('basic visibility', () => {
    test('does not show menu on initial render', () => {
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        buffers: new Map(),
      });
      renderEditorTabs();

      expect(getContextMenuElements()).toHaveLength(0);
    });

    test('shows "Close All Tabs" menu when right-clicking empty area (no tabs)', () => {
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        buffers: new Map(),
      });
      renderEditorTabs();

      const tabsContainer = container!.querySelector('.tabs-container')!;
      fireContextMenu(tabsContainer);

      const menus = getContextMenuElements();
      expect(menus.length).toBeGreaterThanOrEqual(1);
      const texts = getMenuTexts(menus[menus.length - 1]);
      expect(texts).toEqual(expect.arrayContaining([expect.stringContaining('Close All Tabs')]));
    });

    test('shows "Close All Tabs" menu when right-clicking empty area (with tabs, clicking whitespace)', () => {
      const buf1 = makeMockBuffer('buf-1', 'pane-1');
      const buf2 = makeMockBuffer('buf-2', 'pane-1');
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        buffers: new Map([
          ['buf-1', buf1],
          ['buf-2', buf2],
        ]),
        activeBufferId: 'buf-1',
      });
      renderEditorTabs({ paneId: 'pane-1' });

      // The tabs-container is the correct target - it wraps everything.
      // Right-clicking the container itself (not on a tab) should open empty area menu.
      const tabsContainer = container!.querySelector('.tabs-container')!;
      fireContextMenu(tabsContainer);

      const menus = getContextMenuElements();
      // At least one menu should appear. The empty area menu is the one with "Close All Tabs"
      const hasCloseAll = menus.some((m) => getMenuTexts(m).some((t) => t.includes('Close All Tabs')));
      expect(hasCloseAll).toBe(true);
    });

    test('does NOT show empty area menu when right-clicking ON a tab', () => {
      const buf1 = makeMockBuffer('buf-1', 'pane-1');
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        buffers: new Map([['buf-1', buf1]]),
        activeBufferId: 'buf-1',
      });
      renderEditorTabs({ paneId: 'pane-1' });

      const tab = container!.querySelector('.tab')!;
      fireContextMenu(tab);

      // The per-tab context menu should appear, but it should NOT have "Close All Tabs"
      const menus = getContextMenuElements();
      const hasCloseAll = menus.some((m) => getMenuTexts(m).some((t) => t.includes('Close All Tabs')));
      expect(hasCloseAll).toBe(false);
    });
  });

  describe('close behavior', () => {
    test('clicking "Close All Tabs" closes all buffers when no paneId prop', () => {
      const buf1 = makeMockBuffer('buf-1', 'pane-1');
      const buf2 = makeMockBuffer('buf-2', 'pane-1');
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        buffers: new Map([
          ['buf-1', buf1],
          ['buf-2', buf2],
        ]),
      });
      renderEditorTabs(/* no paneId */);

      const tabsContainer = container!.querySelector('.tabs-container')!;
      fireContextMenu(tabsContainer);

      // Find the menu with "Close All Tabs"
      const menus = getContextMenuElements();
      const rightMenu = menus.find((m) => getMenuTexts(m).some((t) => t.includes('Close All Tabs')));
      expect(rightMenu).toBeDefined();

      const closeAllBtn = getMenuItems(rightMenu).find((el) => el.textContent?.trim().includes('Close All Tabs'));
      expect(closeAllBtn).toBeDefined();

      act(() => {
        (closeAllBtn as HTMLElement).click();
      });

      // Both buffers should be closed (no paneId means all buffers)
      expect(mockCloseBuffer).toHaveBeenCalledTimes(2);
      expect(mockCloseBuffer).toHaveBeenCalledWith('buf-1');
      expect(mockCloseBuffer).toHaveBeenCalledWith('buf-2');
    });

    test('clicking "Close All Tabs" closes only pane-scoped buffers when paneId is set', () => {
      const buf1 = makeMockBuffer('buf-1', 'pane-1');
      const buf2 = makeMockBuffer('buf-2', 'pane-1');
      const buf3 = makeMockBuffer('buf-3', 'pane-2');
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        buffers: new Map([
          ['buf-1', buf1],
          ['buf-2', buf2],
          ['buf-3', buf3],
        ]),
        panes: [
          { id: 'pane-1', bufferId: null, isActive: true },
          { id: 'pane-2', bufferId: null, isActive: false },
        ],
      });
      renderEditorTabs({ paneId: 'pane-1' });

      const tabsContainer = container!.querySelector('.tabs-container')!;
      fireContextMenu(tabsContainer);

      const menus = getContextMenuElements();
      const rightMenu = menus.find((m) => getMenuTexts(m).some((t) => t.includes('Close All Tabs')));
      expect(rightMenu).toBeDefined();

      const closeAllBtn = getMenuItems(rightMenu).find((el) => el.textContent?.trim().includes('Close All Tabs'));

      act(() => {
        (closeAllBtn as HTMLElement).click();
      });

      // Only pane-1 buffers should be closed
      expect(mockCloseBuffer).toHaveBeenCalledTimes(2);
      expect(mockCloseBuffer).toHaveBeenCalledWith('buf-1');
      expect(mockCloseBuffer).toHaveBeenCalledWith('buf-2');
      expect(mockCloseBuffer).not.toHaveBeenCalledWith('buf-3');
    });

    test('clicking "Close All Tabs" with no open buffers does nothing', () => {
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        buffers: new Map(),
      });
      renderEditorTabs();

      const tabsContainer = container!.querySelector('.tabs-container')!;
      fireContextMenu(tabsContainer);

      const menus = getContextMenuElements();
      const rightMenu = menus.find((m) => getMenuTexts(m).some((t) => t.includes('Close All Tabs')));
      const closeAllBtn = getMenuItems(rightMenu).find((el) => el.textContent?.trim().includes('Close All Tabs'));

      act(() => {
        (closeAllBtn as HTMLElement).click();
      });

      expect(mockCloseBuffer).not.toHaveBeenCalled();
    });

    test('non-closable buffers (isClosable: false) are skipped', () => {
      const buf1 = makeMockBuffer('buf-1', 'pane-1');
      const buf2 = makeMockBuffer('buf-2', 'pane-1', { isClosable: false });
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        buffers: new Map([
          ['buf-1', buf1],
          ['buf-2', buf2],
        ]),
      });
      renderEditorTabs();

      const tabsContainer = container!.querySelector('.tabs-container')!;
      fireContextMenu(tabsContainer);

      const menus = getContextMenuElements();
      const rightMenu = menus.find((m) => getMenuTexts(m).some((t) => t.includes('Close All Tabs')));
      const closeAllBtn = getMenuItems(rightMenu).find((el) => el.textContent?.trim().includes('Close All Tabs'));

      act(() => {
        (closeAllBtn as HTMLElement).click();
      });

      expect(mockCloseBuffer).toHaveBeenCalledTimes(1);
      expect(mockCloseBuffer).toHaveBeenCalledWith('buf-1');
      expect(mockCloseBuffer).not.toHaveBeenCalledWith('buf-2');
    });
  });

  describe('modified file handling', () => {
    test('triggers themed confirm when modified buffers exist', async () => {
      const { showThemedConfirm } = await import('./ThemedDialog');
      (showThemedConfirm as any).mockClear();

      const buf1 = makeMockBuffer('buf-1', 'pane-1', { isModified: true });
      const buf2 = makeMockBuffer('buf-2', 'pane-1');
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        buffers: new Map([
          ['buf-1', buf1],
          ['buf-2', buf2],
        ]),
      });
      renderEditorTabs();

      const tabsContainer = container!.querySelector('.tabs-container')!;
      fireContextMenu(tabsContainer);

      const menus = getContextMenuElements();
      const rightMenu = menus.find((m) => getMenuTexts(m).some((t) => t.includes('Close All Tabs')));
      const closeAllBtn = getMenuItems(rightMenu).find((el) => el.textContent?.trim().includes('Close All Tabs'));

      act(() => {
        (closeAllBtn as HTMLElement).click();
      });

      // Bespoke .close-confirm-overlay was replaced with showThemedConfirm
      // (the project-wide dialog used everywhere else).
      expect(showThemedConfirm).toHaveBeenCalledTimes(1);
    });

    test('confirming closes all buffers including modified ones', async () => {
      const { showThemedConfirm } = await import('./ThemedDialog');
      (showThemedConfirm as any).mockClear();
      (showThemedConfirm as any).mockResolvedValue(true);

      const buf1 = makeMockBuffer('buf-1', 'pane-1', { isModified: true });
      const buf2 = makeMockBuffer('buf-2', 'pane-1');
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        buffers: new Map([
          ['buf-1', buf1],
          ['buf-2', buf2],
        ]),
      });
      renderEditorTabs();

      const tabsContainer = container!.querySelector('.tabs-container')!;
      fireContextMenu(tabsContainer);

      const menus = getContextMenuElements();
      const rightMenu = menus.find((m) => getMenuTexts(m).some((t) => t.includes('Close All Tabs')));
      const closeAllBtn = getMenuItems(rightMenu).find((el) => el.textContent?.trim().includes('Close All Tabs'));

      await act(async () => {
        (closeAllBtn as HTMLElement).click();
        // Flush the showThemedConfirm Promise so the close logic runs.
        await Promise.resolve();
        await Promise.resolve();
      });

      expect(mockCloseBuffer).toHaveBeenCalledTimes(2);
      expect(mockCloseBuffer).toHaveBeenCalledWith('buf-1');
      expect(mockCloseBuffer).toHaveBeenCalledWith('buf-2');
    });

    test('cancelling does not close any buffers', async () => {
      const { showThemedConfirm } = await import('./ThemedDialog');
      (showThemedConfirm as any).mockClear();
      (showThemedConfirm as any).mockResolvedValue(false);

      const buf1 = makeMockBuffer('buf-1', 'pane-1', { isModified: true });
      const buf2 = makeMockBuffer('buf-2', 'pane-1');
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        buffers: new Map([
          ['buf-1', buf1],
          ['buf-2', buf2],
        ]),
      });
      renderEditorTabs();

      const tabsContainer = container!.querySelector('.tabs-container')!;
      fireContextMenu(tabsContainer);

      const menus = getContextMenuElements();
      const rightMenu = menus.find((m) => getMenuTexts(m).some((t) => t.includes('Close All Tabs')));
      const closeAllBtn = getMenuItems(rightMenu).find((el) => el.textContent?.trim().includes('Close All Tabs'));

      await act(async () => {
        (closeAllBtn as HTMLElement).click();
        await Promise.resolve();
        await Promise.resolve();
      });

      expect(mockCloseBuffer).not.toHaveBeenCalled();

      // Restore for subsequent tests
      (showThemedConfirm as any).mockResolvedValue(true);
    });
  });

  describe('menu dismissal', () => {
    // Note: Escape key dismissal is covered by the "clicking outside" and
    // "window blur" tests. The Escape handler follows the identical code path
    // (calling setEmptyAreaContextMenu(null)). Testing Escape directly with
    // createPortal in jsdom is unreliable due to async portal DOM cleanup.

    test('menu closes when clicking outside', () => {
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        buffers: new Map(),
      });
      renderEditorTabs();

      const tabsContainer = container!.querySelector('.tabs-container')!;
      fireContextMenu(tabsContainer);

      expect(getContextMenuElements().length).toBeGreaterThanOrEqual(1);

      act(() => {
        document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
      });

      // The useEffect cleanup removes the menus. Since React hasn't re-rendered yet
      // in the same act(), we need to check that the state is cleared on next render.
      // Flush a tick to let React re-render:
      const menusAfter = getContextMenuElements().length;
      expect(menusAfter).toBe(0);
    });

    test('menu closes on window blur', () => {
      mockUseEditorManager.mockReturnValue({
        ...defaultMockEditorManager,
        buffers: new Map(),
      });
      renderEditorTabs();

      const tabsContainer = container!.querySelector('.tabs-container')!;
      fireContextMenu(tabsContainer);

      expect(getContextMenuElements().length).toBeGreaterThanOrEqual(1);

      act(() => {
        window.dispatchEvent(new Event('blur'));
      });

      const menusAfter = getContextMenuElements().length;
      expect(menusAfter).toBe(0);
    });
  });
});

describe('EditorTabs pin toggle button', () => {
  test('renders pin button on non-pinned tab with "Pin tab" title and not disabled', () => {
    const buf1 = makeMockBuffer('buf-1', 'pane-1');
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([['buf-1', buf1]]),
      activeBufferId: 'buf-1',
    });
    renderEditorTabs({ paneId: 'pane-1' });

    const pinBtn = container!.querySelector('.pin-indicator') as HTMLElement;
    expect(pinBtn).not.toBeNull();
    expect(pinBtn.title).toBe('Pin tab');
    expect(pinBtn.disabled).toBe(false);
  });

  test('renders pin button on pinned tab with "Unpin tab" title and tab has .pinned class', () => {
    const buf1 = makeMockBuffer('buf-1', 'pane-1', { isPinned: true });
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([['buf-1', buf1]]),
      activeBufferId: 'buf-1',
    });
    renderEditorTabs({ paneId: 'pane-1' });

    const tab = container!.querySelector('.tab')!;
    expect(tab.classList.contains('pinned')).toBe(true);

    const pinBtn = container!.querySelector('.pin-indicator') as HTMLElement;
    expect(pinBtn).not.toBeNull();
    expect(pinBtn.title).toBe('Unpin tab');
  });

  test('clicking pin button on non-pinned tab calls toggleBufferPin', () => {
    const buf1 = makeMockBuffer('buf-1', 'pane-1');
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([['buf-1', buf1]]),
      activeBufferId: 'buf-1',
    });
    renderEditorTabs({ paneId: 'pane-1' });

    const pinBtn = container!.querySelector('.pin-indicator') as HTMLElement;
    act(() => {
      pinBtn.click();
    });

    expect(mockToggleBufferPin).toHaveBeenCalledTimes(1);
    expect(mockToggleBufferPin).toHaveBeenCalledWith('buf-1');
  });

  test('clicking pin button does NOT switch to buffer (stopPropagation)', () => {
    const buf1 = makeMockBuffer('buf-1', 'pane-1');
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([['buf-1', buf1]]),
      activeBufferId: null, // buffer is NOT active
    });
    renderEditorTabs({ paneId: 'pane-1' });

    const pinBtn = container!.querySelector('.pin-indicator') as HTMLElement;
    act(() => {
      pinBtn.click();
    });

    expect(mockToggleBufferPin).toHaveBeenCalledWith('buf-1');
    expect(mockSwitchToBuffer).not.toHaveBeenCalled();
  });

  test('pin button is disabled for non-closable buffers that are not pinned', () => {
    const buf1 = makeMockBuffer('buf-1', 'pane-1', { isClosable: false });
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([['buf-1', buf1]]),
      activeBufferId: 'buf-1',
    });
    renderEditorTabs({ paneId: 'pane-1' });

    const pinBtn = container!.querySelector('.pin-indicator') as HTMLElement;
    expect(pinBtn).not.toBeNull();
    expect(pinBtn.disabled).toBe(true);
  });

  test('clicking unpin on pinned tab calls toggleBufferPin', () => {
    const buf1 = makeMockBuffer('buf-1', 'pane-1', { isPinned: true });
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([['buf-1', buf1]]),
      activeBufferId: 'buf-1',
    });
    renderEditorTabs({ paneId: 'pane-1' });

    const pinBtn = container!.querySelector('.pin-indicator') as HTMLElement;
    expect(pinBtn.title).toBe('Unpin tab');
    act(() => {
      pinBtn.click();
    });

    expect(mockToggleBufferPin).toHaveBeenCalledTimes(1);
    expect(mockToggleBufferPin).toHaveBeenCalledWith('buf-1');
  });
});

describe('EditorTabs chat tab close behavior', () => {
  test('pinned chat tab shows close button and confirmation dialog on close click', async () => {
    const buf1 = makeMockBuffer('buf-chat', 'pane-1', {
      isPinned: true,
      kind: 'chat',
      file: { path: '__workspace/chat', name: 'Chat', ext: '.chat', isDir: false, size: 0, modified: 0 },
    });
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([['buf-chat', buf1]]),
      activeBufferId: null,
    });
    renderEditorTabs({ paneId: 'pane-1' });

    // Close button should be visible for pinned chat tabs
    const closeBtn = container!.querySelector('.tab-close') as HTMLElement;
    expect(closeBtn).not.toBeNull();

    // Clicking close should trigger themed confirm (not bypass pin protection)
    const { showThemedConfirm } = await import('./ThemedDialog');
    (showThemedConfirm as any).mockClear();
    (showThemedConfirm as any).mockResolvedValue(false);
    await act(async () => {
      closeBtn.click();
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(showThemedConfirm).toHaveBeenCalledTimes(1);
    expect(mockCloseBuffer).not.toHaveBeenCalled();
    (showThemedConfirm as any).mockResolvedValue(true);
  });

  test('pinned non-chat tab does NOT show close button and cannot be closed', () => {
    const buf1 = makeMockBuffer('buf-1', 'pane-1', { isPinned: true, kind: 'file' });
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([['buf-1', buf1]]),
      activeBufferId: 'buf-1',
    });
    renderEditorTabs({ paneId: 'pane-1' });

    // Close button should NOT be visible for pinned non-chat tabs
    const closeBtn = container!.querySelector('.tab-close') as HTMLElement;
    expect(closeBtn).toBeNull();
  });

  test('non-pinned chat tab closes directly without confirmation', () => {
    const buf1 = makeMockBuffer('buf-chat', 'pane-1', {
      kind: 'chat',
      isPinned: false,
      file: { path: '__workspace/chat/123', name: 'Chat 2', ext: '.chat', isDir: false, size: 0, modified: 0 },
    });
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([['buf-chat', buf1]]),
      activeBufferId: 'buf-chat',
    });
    renderEditorTabs({ paneId: 'pane-1' });

    const closeBtn = container!.querySelector('.tab-close') as HTMLElement;
    expect(closeBtn).not.toBeNull();

    act(() => {
      closeBtn.click();
    });

    expect(mockCloseBuffer).toHaveBeenCalledWith('buf-chat');
    expect(container!.querySelector('.close-confirm-overlay')).toBeNull();
  });

  test('pinned chat tab close confirmation dialog allows closing', async () => {
    const { showThemedConfirm } = await import('./ThemedDialog');
    (showThemedConfirm as any).mockClear();
    (showThemedConfirm as any).mockResolvedValue(true);

    const buf1 = makeMockBuffer('buf-chat', 'pane-1', {
      isPinned: true,
      kind: 'chat',
      file: { path: '__workspace/chat', name: 'Chat', ext: '.chat', isDir: false, size: 0, modified: 0 },
    });
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([['buf-chat', buf1]]),
      activeBufferId: null,
    });
    renderEditorTabs({ paneId: 'pane-1' });

    const closeBtn = container!.querySelector('.tab-close') as HTMLElement;
    await act(async () => {
      closeBtn.click();
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(mockCloseBuffer).toHaveBeenCalledWith('buf-chat');
  });

  test('pinned chat tab close confirmation dialog cancel does not close', async () => {
    const { showThemedConfirm } = await import('./ThemedDialog');
    (showThemedConfirm as any).mockClear();
    (showThemedConfirm as any).mockResolvedValue(false);

    const buf1 = makeMockBuffer('buf-chat', 'pane-1', {
      isPinned: true,
      kind: 'chat',
      file: { path: '__workspace/chat', name: 'Chat', ext: '.chat', isDir: false, size: 0, modified: 0 },
    });
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([['buf-chat', buf1]]),
      activeBufferId: null,
    });
    renderEditorTabs({ paneId: 'pane-1' });

    const closeBtn = container!.querySelector('.tab-close') as HTMLElement;
    await act(async () => {
      closeBtn.click();
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(mockCloseBuffer).not.toHaveBeenCalled();
    (showThemedConfirm as any).mockResolvedValue(true);
  });

  test('middle-click on pinned chat tab triggers themed confirm', async () => {
    const { showThemedConfirm } = await import('./ThemedDialog');
    (showThemedConfirm as any).mockClear();
    (showThemedConfirm as any).mockResolvedValue(false);

    const buf1 = makeMockBuffer('buf-chat', 'pane-1', {
      isPinned: true,
      kind: 'chat',
      file: { path: '__workspace/chat', name: 'Chat', ext: '.chat', isDir: false, size: 0, modified: 0 },
    });
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([['buf-chat', buf1]]),
      activeBufferId: null,
    });
    renderEditorTabs({ paneId: 'pane-1' });

    const tab = container!.querySelector('.tab') as HTMLElement;
    await act(async () => {
      tab.dispatchEvent(new MouseEvent('auxclick', { button: 1, bubbles: true }));
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(showThemedConfirm).toHaveBeenCalledTimes(1);
    (showThemedConfirm as any).mockResolvedValue(true);
  });

  test('context menu Close item is visible for pinned chat tab', async () => {
    const buf1 = makeMockBuffer('buf-chat', 'pane-1', {
      isPinned: true,
      kind: 'chat',
      file: { path: '__workspace/chat', name: 'Chat', ext: '.chat', isDir: false, size: 0, modified: 0 },
    });
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([['buf-chat', buf1]]),
      panes: [{ id: 'pane-1', bufferId: null, isActive: true }],
      activeBufferId: 'buf-chat',
      activePaneId: 'pane-1',
    });
    renderEditorTabs({ paneId: 'pane-1' });

    const tab = container!.querySelector('.tab') as HTMLElement;
    fireContextMenu(tab, 100, 200);

    const menus = getContextMenuElements();
    expect(menus.length).toBeGreaterThan(0);
    const hasClose = menus.some((m) =>
      Array.from(m.querySelectorAll('.context-menu-item')).some((item) => item.textContent?.includes('Close')),
    );
    expect(hasClose).toBe(true);
  });
});

describe('EditorTabs tab title tooltip', () => {
  test('outer tab element has title attribute with full file path', () => {
    const buf1 = makeMockBuffer('buf-1', 'pane-1', {
      file: { path: 'src/components/App.tsx', name: 'App.tsx', ext: '.tsx', isDir: false, size: 42, modified: 0 },
    });
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([['buf-1', buf1]]),
      activeBufferId: 'buf-1',
    });
    renderEditorTabs({ paneId: 'pane-1' });

    const tab = container!.querySelector('.tab') as HTMLElement;
    expect(tab).not.toBeNull();
    expect(tab.title).toBe('src/components/App.tsx');
  });

  test('inner tab-name span has title attribute with full file path', () => {
    const buf1 = makeMockBuffer('buf-1', 'pane-1', {
      file: { path: 'src/components/App.tsx', name: 'App.tsx', ext: '.tsx', isDir: false, size: 42, modified: 0 },
    });
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([['buf-1', buf1]]),
      activeBufferId: 'buf-1',
    });
    renderEditorTabs({ paneId: 'pane-1' });

    const tabName = container!.querySelector('.tab-name') as HTMLElement;
    expect(tabName).not.toBeNull();
    expect(tabName.title).toBe('src/components/App.tsx');
  });

  test('multiple tabs each show their own file path in title', () => {
    const buf1 = makeMockBuffer('buf-1', 'pane-1', {
      file: { path: 'src/components/App.tsx', name: 'App.tsx', ext: '.tsx', isDir: false, size: 42, modified: 0 },
    });
    const buf2 = makeMockBuffer('buf-2', 'pane-1', {
      file: { path: 'src/utils/helpers.ts', name: 'helpers.ts', ext: '.ts', isDir: false, size: 99, modified: 0 },
    });
    const buf3 = makeMockBuffer('buf-3', 'pane-1', {
      file: { path: 'src/index.ts', name: 'index.ts', ext: '.ts', isDir: false, size: 55, modified: 0 },
    });
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([
        ['buf-1', buf1],
        ['buf-2', buf2],
        ['buf-3', buf3],
      ]),
      activeBufferId: 'buf-1',
    });
    renderEditorTabs({ paneId: 'pane-1' });

    const tabs = Array.from(container!.querySelectorAll('.tab[data-buffer-id]'));
    expect(tabs).toHaveLength(3);

    // Each outer tab div has the correct title
    expect(tabs.find((t) => t.dataset.bufferId === 'buf-1')!.title).toBe('src/components/App.tsx');
    expect(tabs.find((t) => t.dataset.bufferId === 'buf-2')!.title).toBe('src/utils/helpers.ts');
    expect(tabs.find((t) => t.dataset.bufferId === 'buf-3')!.title).toBe('src/index.ts');

    // Each inner tab-name span also has the correct title
    const tabNames = Array.from(container!.querySelectorAll('.tab-name'));
    expect(tabNames).toHaveLength(3);
    expect(tabNames[0].title).toBe('src/components/App.tsx');
    expect(tabNames[1].title).toBe('src/utils/helpers.ts');
    expect(tabNames[2].title).toBe('src/index.ts');
  });

  test('title tooltip works for chat buffers', () => {
    const chatBuf = makeMockBuffer('buf-chat', 'pane-1', {
      kind: 'chat',
      file: { path: '__workspace/chat/abc-123', name: 'Chat', ext: '.chat', isDir: false, size: 0, modified: 0 },
    });
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([['buf-chat', chatBuf]]),
      activeBufferId: 'buf-chat',
    });
    renderEditorTabs({ paneId: 'pane-1' });

    const tab = container!.querySelector('.tab') as HTMLElement;
    expect(tab).not.toBeNull();
    expect(tab.title).toBe('__workspace/chat/abc-123');

    const tabName = container!.querySelector('.tab-name') as HTMLElement;
    expect(tabName.title).toBe('__workspace/chat/abc-123');
  });
});
