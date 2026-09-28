// @ts-nocheck
import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from 'vitest';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
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
  function Consumer() {
    latest = useEditorManager();
    return null;
  }
  act(() => root.render(createElement(EditorManagerProvider, null, createElement(Consumer))));
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const chatTabs = () => Array.from(latest.buffers.values()).filter((b) => b.kind === 'chat');

describe('chat tab bookkeeping', () => {
  it('starts with an unpinned chat tab', () => {
    expect(chatTabs().map((b) => [b.id, b.isPinned])).toEqual([['buffer-chat', false]]);
  });

  it('opens one tab when the same chat is opened twice before React commits', () => {
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
    await act(async () => {
      latest.setBufferClosable('buffer-chat', true);
      await latest.closeBuffer('buffer-chat');
    });
    expect(latest.buffers.has('buffer-chat')).toBe(false);
  });
});
