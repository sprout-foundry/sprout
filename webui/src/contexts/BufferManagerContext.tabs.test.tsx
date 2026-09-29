// @ts-nocheck
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from 'vitest';
import { EditorManagerProvider, useEditorManager } from './EditorManagerContext';

vi.mock('../services/apiFileCheck', () => ({ checkFilesModified: vi.fn().mockResolvedValue({ modified: [] }) }));

vi.mock('./SproutAdapterContext', () => ({
  SproutAdapterProvider: ({ children }) => children,
  useSproutAdapter: () => ({ clientFetch: vi.fn() }),
  useSproutFetch: () => vi.fn(),
}));

let container: HTMLDivElement;
let root: Root;
let latest: any;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  latest = undefined;
  localStorage.setItem('sprout-welcome-dismissed', 'true');
  localStorage.removeItem('sprout.editor.layoutState');
});

function Consumer() {
  latest = useEditorManager();
  return null;
}

function mount() {
  act(() => root.render(createElement(EditorManagerProvider, null, createElement(Consumer))));
}

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const chatTabs = () => Array.from(latest.buffers.values()).filter((b) => b.kind === 'chat');

describe('chat tab bookkeeping', () => {
  it('starts with an unpinned chat tab', () => {
    mount();
    expect(chatTabs().map((b) => [b.id, b.isPinned])).toEqual([['buffer-chat', false]]);
  });

  it('opens one tab when the same chat is opened twice before React commits', () => {
    mount();
    let now = 1_000;
    vi.spyOn(Date, 'now').mockImplementation(() => ++now);
    act(() => {
      const open = { kind: 'chat', path: '__workspace/chat/c2', title: 'Chat 2', metadata: { chatId: 'c2' } };
      latest.openWorkspaceBuffer({ ...open, activate: false });
      latest.openWorkspaceBuffer({ ...open, title: 'New Chat' });
    });
    expect(chatTabs().filter((b) => b.metadata?.chatId === 'c2')).toHaveLength(1);
    vi.restoreAllMocks();
  });

  it('keeps focus on a just-opened tab when a background tab closes in the same tick', async () => {
    mount();
    let now = 5_000;
    vi.spyOn(Date, 'now').mockImplementation(() => ++now);
    let placeholder;
    act(() => {
      placeholder = latest.openWorkspaceBuffer({
        kind: 'chat',
        path: '__workspace/chat/creating-1',
        title: 'Creating…',
      });
    });
    let real;
    await act(async () => {
      real = latest.openWorkspaceBuffer({
        kind: 'chat',
        path: '__workspace/chat/c9',
        title: 'New Chat',
        metadata: { chatId: 'c9' },
      });
      await latest.closeBuffer(placeholder);
    });
    expect(latest.activeBufferId).toBe(real);
    expect(latest.panes.find((pane) => pane.id === latest.buffers.get(real).paneId).bufferId).toBe(real);
    vi.restoreAllMocks();
  });

  it('closes a tab made closable in the same tick', async () => {
    mount();
    await act(async () => {
      latest.setBufferClosable('buffer-chat', true);
      await latest.closeBuffer('buffer-chat');
    });
    expect(latest.buffers.has('buffer-chat')).toBe(false);
  });

  it('restores a saved split and opens a chat tab in the pane it was in', async () => {
    localStorage.setItem(
      'sprout.editor.panes:_default',
      JSON.stringify([
        { id: 'pane-1', position: 'primary' },
        { id: 'pane-9', position: 'secondary' },
      ]),
    );
    localStorage.setItem('sprout.editor.paneLayout:_default', 'split-vertical');
    mount();
    // Restored once the workspace lookup settles (it fails in tests; the
    // restore still runs, against the default workspace keys).
    for (let i = 0; i < 50 && latest.panes.length < 2; i++) {
      await act(async () => {
        await new Promise((r) => setTimeout(r, 20));
      });
    }
    expect(latest.panes.map((p) => p.id)).toEqual(['pane-1', 'pane-9']);
    expect(latest.paneLayout).toBe('split-vertical');

    act(() => {
      latest.openWorkspaceBuffer({
        kind: 'chat',
        path: '__workspace/chat/c7',
        title: 'Chat 7',
        paneId: 'pane-1',
        metadata: { chatId: 'c7' },
      });
    });
    expect(chatTabs().find((b) => b.metadata?.chatId === 'c7')?.paneId).toBe('pane-1');
    localStorage.removeItem('sprout.editor.panes:_default');
    localStorage.removeItem('sprout.editor.paneLayout:_default');
  });

  it('reopens tabs in the saved order and keeps a later drag', async () => {
    localStorage.setItem('sprout.editor.tabOrder:_default', JSON.stringify(['__workspace/chat/c2', 'notes.md']));
    mount();
    for (let i = 0; i < 50 && !latest; i++) {
      await act(async () => {
        await new Promise((r) => setTimeout(r, 20));
      });
    }
    // Let the restore settle before tabs reopen (file first, then the chat).
    await act(async () => {
      await new Promise((r) => setTimeout(r, 100));
    });
    let now = 9_000;
    vi.spyOn(Date, 'now').mockImplementation(() => ++now);
    act(() => {
      latest.openWorkspaceBuffer({ kind: 'file', path: 'notes.md', title: 'notes.md' });
    });
    act(() => {
      latest.openWorkspaceBuffer({
        kind: 'chat',
        path: '__workspace/chat/c2',
        title: 'Chat 2',
        metadata: { chatId: 'c2' },
      });
    });
    vi.restoreAllMocks();
    const order = () =>
      Array.from(latest.buffers.values())
        .map((b) => b.file.path)
        .filter((p) => p !== '__workspace/chat');
    expect(order()).toEqual(['__workspace/chat/c2', 'notes.md']);

    const ids = Object.fromEntries(Array.from(latest.buffers.values()).map((b) => [b.file.path, b.id]));
    act(() => latest.reorderBuffers(ids['notes.md'], ids['__workspace/chat/c2']));
    expect(order()).toEqual(['notes.md', '__workspace/chat/c2']);
    localStorage.removeItem('sprout.editor.tabOrder:_default');
  });
});
