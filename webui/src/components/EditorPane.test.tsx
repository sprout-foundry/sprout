// @ts-nocheck

import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react';
import EditorPane from './EditorPane';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { useHotkeys } from '../contexts/HotkeyContext';
import { useTheme } from '../contexts/ThemeContext';
import { ApiService } from '../services/api';
import { readFileWithConsent } from '../services/fileAccess';
import { copyToClipboard } from '../utils/clipboard';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

jest.mock('../contexts/EditorManagerContext', () => ({
  useEditorManager: jest.fn(),
}));

jest.mock('../contexts/HotkeyContext', () => ({
  useHotkeys: jest.fn(),
}));

jest.mock('../contexts/ThemeContext', () => ({
  useTheme: jest.fn(),
}));

jest.mock('../services/api', () => ({
  ApiService: {
    getInstance: jest.fn(),
  },
}));

jest.mock('../services/fileAccess', () => ({
  readFileWithConsent: jest.fn(),
}));

jest.mock('../utils/clipboard', () => ({
  copyToClipboard: jest.fn().mockResolvedValue(undefined),
}));

jest.mock('./EditorToolbar', () => () => <div data-testid="editor-toolbar" />);
jest.mock('./ImageViewer', () => () => <div data-testid="image-viewer" />);
jest.mock('./SvgPreview', () => () => <div data-testid="svg-preview" />);
jest.mock('./GoToSymbolOverlay', () => () => null);

// ---------------------------------------------------------------------------
// Constants & Helpers
// ---------------------------------------------------------------------------

const mockBuffer = {
  id: 'buffer-1',
  kind: 'file',
  file: {
    path: 'src/components/EditorPane.tsx',
    name: 'EditorPane.tsx',
    ext: '.tsx',
    isDir: false,
    size: 12345,
    modified: 0,
  },
  content: 'line1\nline2\nline3',
  originalContent: 'line1\nline2\nline3',
  cursorPosition: { line: 0, column: 0 },
  scrollPosition: { top: 0, left: 0 },
  isModified: false,
  isActive: true,
  isClosable: true,
  metadata: {},
};

const mockUseEditorManager = useEditorManager as jest.MockedFunction<typeof useEditorManager>;

const defaultMockEditorManager = {
  panes: [{ id: 'pane-1', bufferId: 'buffer-1', isActive: true }],
  buffers: new Map([['buffer-1', { ...mockBuffer }]]),
  updateBufferContent: jest.fn(),
  updateBufferCursor: jest.fn(),
  saveBuffer: jest.fn(),
  setBufferModified: jest.fn(),
  setBufferOriginalContent: jest.fn(),
  splitPane: jest.fn(),
  openWorkspaceBuffer: jest.fn(),
};

const flushPromises = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

/**
 * Flush any pending requestAnimationFrame callbacks.
 * The shared ContextMenu component registers its close listeners (mousedown,
 * keydown, scroll, blur) inside a requestAnimationFrame callback, so after
 * opening the menu we must flush the RAF before those listeners are active.
 */
const flushRAF = () =>
  act(async () => {
    await new Promise((resolve) => requestAnimationFrame(resolve));
    await Promise.resolve();
  });

function fireContextMenu(element: Element, x = 100, y = 100) {
  const event = new MouseEvent('contextmenu', {
    bubbles: true,
    cancelable: true,
    clientX: x,
    clientY: y,
  });
  element.dispatchEvent(event);
  return event;
}

/**
 * Helper to find the context menu. Since it's rendered via a portal to
 * document.body, we must query the body (not the container).
 */
function getMenu() {
  return document.body.querySelector('.context-menu');
}

function getMenuItems() {
  const menu = getMenu();
  return menu ? Array.from(menu.querySelectorAll('.context-menu-item')) : [];
}

// ---------------------------------------------------------------------------
// Test Suite
// ---------------------------------------------------------------------------

describe('EditorPane context menu', () => {
  let container: HTMLDivElement;
  let root: any;
  let apiServiceMock: any;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);

    apiServiceMock = {
      getWorkspace: jest.fn().mockResolvedValue({
        workspace_root: '/home/user/project',
        daemon_root: '/home/user/project/.ledit',
      }),
      getGitDiff: jest.fn().mockResolvedValue({ diff: '' }),
    };
    (ApiService.getInstance as jest.Mock).mockReturnValue(apiServiceMock);

    mockUseEditorManager.mockReturnValue({ ...defaultMockEditorManager });

    (useHotkeys as jest.MockedFunction<typeof useHotkeys>).mockReturnValue({ hotkeys: [] });

    (useTheme as jest.MockedFunction<typeof useTheme>).mockReturnValue({
      theme: 'dark',
      themePack: { id: 'dark', mode: 'dark', editorSyntaxStyle: 'one-dark' },
      customHighlightStyle: undefined,
    });

    (readFileWithConsent as jest.Mock).mockResolvedValue({
      ok: true,
      statusText: 'OK',
      text: () => Promise.resolve('line1\nline2\nline3'),
    });
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
    jest.clearAllMocks();
  });

  it('renders without crashing', async () => {
    await act(async () => {
      root.render(<EditorPane paneId="pane-1" />);
    });
    await flushPromises();
    expect(container.querySelector('.editor-pane')).toBeTruthy();
  });

  it('context menu appears on right-click in the editor area', async () => {
    await act(async () => {
      root.render(<EditorPane paneId="pane-1" />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.pane-content');
    expect(paneContent).toBeTruthy();

    fireContextMenu(paneContent);
    await act(async () => { await Promise.resolve(); });

    const menu = getMenu();
    expect(menu).toBeTruthy();
  });

  it('context menu shows the three expected items when workspace root is available', async () => {
    await act(async () => {
      root.render(<EditorPane paneId="pane-1" />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.pane-content');
    fireContextMenu(paneContent);
    await act(async () => { await Promise.resolve(); });

    const items = getMenuItems();
    const texts = items.map((el) => el.textContent?.trim());

    expect(texts).toContain('Reveal in File Explorer');
    expect(texts).toContain('Copy relative path');
    expect(texts).toContain('Copy absolute path');
  });

  it('"Reveal in File Explorer" dispatches ledit:reveal-in-explorer event', async () => {
    await act(async () => {
      root.render(<EditorPane paneId="pane-1" />);
    });
    await flushPromises();

    const listener = jest.fn();
    window.addEventListener('ledit:reveal-in-explorer', listener);

    const paneContent = container.querySelector('.pane-content');
    fireContextMenu(paneContent);
    await act(async () => { await Promise.resolve(); });

    const items = getMenuItems();
    expect(items[0]).toBeTruthy();

    await act(async () => {
      items[0].click();
    });
    await flushPromises();

    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener).toHaveBeenCalledWith(
      expect.objectContaining({
        detail: { path: 'src/components/EditorPane.tsx' },
      })
    );

    window.removeEventListener('ledit:reveal-in-explorer', listener);
  });

  it('"Copy relative path" calls copyToClipboard with the file path', async () => {
    await act(async () => {
      root.render(<EditorPane paneId="pane-1" />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.pane-content');
    fireContextMenu(paneContent);
    await act(async () => { await Promise.resolve(); });

    const items = getMenuItems();
    // Second item = "Copy relative path"
    expect(items[1]).toBeTruthy();

    await act(async () => {
      items[1].click();
    });
    await flushPromises();

    expect(copyToClipboard).toHaveBeenCalledWith('src/components/EditorPane.tsx');
  });

  it('"Copy absolute path" calls copyToClipboard with workspaceRoot + file path', async () => {
    await act(async () => {
      root.render(<EditorPane paneId="pane-1" />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.pane-content');
    fireContextMenu(paneContent);
    await act(async () => { await Promise.resolve(); });

    const items = getMenuItems();
    // Third item = "Copy absolute path"
    expect(items[2]).toBeTruthy();

    await act(async () => {
      items[2].click();
    });
    await flushPromises();

    expect(copyToClipboard).toHaveBeenCalledWith(
      '/home/user/project/src/components/EditorPane.tsx'
    );
  });

  it('context menu closes after clicking an item', async () => {
    await act(async () => {
      root.render(<EditorPane paneId="pane-1" />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.pane-content');
    fireContextMenu(paneContent);
    await act(async () => { await Promise.resolve(); });

    expect(getMenu()).toBeTruthy();

    const items = getMenuItems();
    await act(async () => {
      items[0].click();
    });
    await flushPromises();

    expect(getMenu()).toBeFalsy();
  });

  it('"Copy absolute path" item is NOT present when workspace root is empty', async () => {
    apiServiceMock.getWorkspace.mockResolvedValueOnce({
      workspace_root: '',
      daemon_root: '/home/user/project/.ledit',
    });

    await act(async () => {
      root.render(<EditorPane paneId="pane-1" />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.pane-content');
    fireContextMenu(paneContent);
    await act(async () => { await Promise.resolve(); });

    const texts = getMenuItems().map((el) => el.textContent?.trim());

    expect(texts).toContain('Reveal in File Explorer');
    expect(texts).toContain('Copy relative path');
    expect(texts).not.toContain('Copy absolute path');
  });

  it('context menu closes when clicking outside it', async () => {
    await act(async () => {
      root.render(<EditorPane paneId="pane-1" />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.pane-content');
    fireContextMenu(paneContent);
    await act(async () => { await Promise.resolve(); });

    // Flush RAF so the menu registers its mousedown listener
    await flushRAF();

    expect(getMenu()).toBeTruthy();

    // Click outside the menu (on the body, not the menu itself)
    await act(async () => {
      document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });
    await flushPromises();

    expect(getMenu()).toBeFalsy();
  });

  it('context menu closes when pressing Escape', async () => {
    await act(async () => {
      root.render(<EditorPane paneId="pane-1" />);
    });
    await flushPromises();

    const paneContent = container.querySelector('.pane-content');
    fireContextMenu(paneContent);
    await act(async () => { await Promise.resolve(); });

    // Flush RAF so the menu registers its keydown listener
    await flushRAF();

    expect(getMenu()).toBeTruthy();

    await act(async () => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    await flushPromises();

    expect(getMenu()).toBeFalsy();
  });

  it('context menu does NOT appear when there is no buffer (empty state)', async () => {
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      panes: [{ id: 'pane-1', bufferId: null, isActive: true }],
      buffers: new Map(),
    });

    await act(async () => {
      root.render(<EditorPane paneId="pane-1" />);
    });
    await flushPromises();

    // Should show empty state
    const noFileEl = container.querySelector('.no-file-selected');
    expect(noFileEl).toBeTruthy();
    // No pane-content should exist for empty state
    const paneContent = container.querySelector('.pane-content');
    expect(paneContent).toBeFalsy();
  });

  it('context menu does NOT appear for image files', async () => {
    mockUseEditorManager.mockReturnValue({
      ...defaultMockEditorManager,
      buffers: new Map([
        [
          'buffer-1',
          {
            ...mockBuffer,
            file: { ...mockBuffer.file, ext: '.png', path: 'images/test.png', name: 'test.png' },
          },
        ],
      ]),
    });

    await act(async () => {
      root.render(<EditorPane paneId="pane-1" />);
    });
    await flushPromises();

    // Should show ImageViewer
    const imageView = container.querySelector('[data-testid="image-viewer"]');
    expect(imageView).toBeTruthy();
    // No pane-content div for images
    const paneContent = container.querySelector('.pane-content');
    expect(paneContent).toBeFalsy();
  });
});
