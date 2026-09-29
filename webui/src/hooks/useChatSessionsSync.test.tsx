import { renderHook } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { EditorBuffer } from '../types/editor';
import { useChatSessionsSync } from './useChatSessionsSync';

type Sessions = Array<{ id: string; name?: string; is_default?: boolean }>;

function makeBuffer(overrides: Partial<EditorBuffer> & { id: string }): EditorBuffer {
  return {
    kind: 'chat',
    file: { name: 'Chat', path: '__workspace/chat', isDir: false, size: 0, modified: 0, ext: '.chat' },
    content: '',
    originalContent: '',
    contentLoaded: true,
    cursorPosition: { line: 0, column: 0 },
    scrollPosition: { top: 0, left: 0 },
    isModified: false,
    isActive: false,
    paneId: 'pane-1',
    isPinned: false,
    isClosable: true,
    metadata: { chatId: null },
    ...overrides,
  };
}

function setup(opts: {
  sessions: Sessions;
  activeChatId: string | null;
  buffers: Map<string, EditorBuffer>;
  mode?: string;
  closedIds?: string[];
}) {
  const buffersRef: React.RefObject<Map<string, EditorBuffer>> = { current: opts.buffers };
  const calls: {
    open: Array<{ id: string; isPinned?: boolean; isClosable?: boolean; activate?: boolean }>;
    pinned: Array<[string, boolean]>;
    closable: Array<[string, boolean]>;
  } = { open: [], pinned: [], closable: [] };

  const openWorkspaceBuffer = (o: {
    kind: 'chat';
    path: string;
    title: string;
    isPinned?: boolean;
    isClosable?: boolean;
    activate?: boolean;
    metadata?: Record<string, unknown>;
  }) => {
    const id = o.path;
    calls.open.push({ id, isPinned: o.isPinned, isClosable: o.isClosable, activate: o.activate });
    // Mirror openWorkspaceBuffer: register a new buffer so subsequent runs dedupe.
    const chatId = o.metadata?.chatId as string;
    opts.buffers.set(
      id,
      makeBuffer({
        id,
        file: { ...makeBuffer({ id: id }).file, name: o.title, path: id },
        isPinned: o.isPinned ?? false,
        isClosable: o.isClosable ?? !o.isPinned,
        metadata: { chatId },
      }),
    );
    return id;
  };

  const utils = renderHook(() =>
    useChatSessionsSync({
      chatSessions: opts.sessions as never,
      activeChatId: opts.activeChatId,
      mode: opts.mode,
      buffersRef,
      updateBufferTitle: vi.fn(),
      setBufferPinned: (id, v) => {
        calls.pinned.push([id, v]);
        const b = opts.buffers.get(id);
        if (b) opts.buffers.set(id, { ...b, isPinned: v });
      },
      setBufferClosable: (id, v) => {
        calls.closable.push([id, v]);
        const b = opts.buffers.get(id);
        if (b) opts.buffers.set(id, { ...b, isClosable: v });
      },
      closeBuffer: (id) => {
        const b = opts.buffers.get(id);
        if (!b || b.isClosable === false) return;
        opts.closedIds?.push(id);
        opts.buffers.delete(id);
      },
      openWorkspaceBuffer: openWorkspaceBuffer as never,
    }),
  );

  return { ...utils, calls, buffers: opts.buffers };
}

describe('useChatSessionsSync', () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => vi.restoreAllMocks());

  it('gives the active chat its own focused tab and retires the initial stand-in', () => {
    const closedIds: string[] = [];
    const buffers = new Map<string, EditorBuffer>([
      ['buffer-chat', makeBuffer({ id: 'buffer-chat', isClosable: false, isActive: true })],
    ]);
    const { calls } = setup({ sessions: [{ id: 'A' }], activeChatId: 'A', buffers, closedIds });
    expect(calls.open.find((c) => c.id === '__workspace/chat/A')).toMatchObject({ activate: true, isPinned: false });
    expect(closedIds).toEqual(['buffer-chat']);
  });

  it('opens tabs in creation order, not most-recently-active order', () => {
    const buffers = new Map<string, EditorBuffer>();
    const { calls } = setup({
      sessions: [
        { id: 'newer', created_at: '2026-01-02T00:00:00Z' },
        { id: 'older', created_at: '2026-01-01T00:00:00Z' },
      ] as never,
      activeChatId: 'newer',
      buffers,
    });
    expect(calls.open.map((c) => c.id)).toEqual(['__workspace/chat/older', '__workspace/chat/newer']);
  });

  it('closes the tab of a chat that was deleted', () => {
    const closedIds: string[] = [];
    const tab = (chatId: string) =>
      makeBuffer({
        id: `__workspace/chat/${chatId}`,
        file: { ...makeBuffer({ id: 'x' }).file, path: `__workspace/chat/${chatId}` },
        metadata: { chatId },
      });
    const buffers = new Map<string, EditorBuffer>([
      ['__workspace/chat/A', tab('A')],
      ['__workspace/chat/gone', { ...tab('gone'), isClosable: false }],
    ]);
    const props = { sessions: [{ id: 'A' }, { id: 'gone' }, { id: 'fresh' }] as { id: string }[] };
    const { rerender } = renderHook(() =>
      useChatSessionsSync({
        chatSessions: props.sessions as never,
        activeChatId: 'A',
        buffersRef: { current: buffers },
        updateBufferTitle: vi.fn(),
        setBufferPinned: vi.fn(),
        setBufferClosable: vi.fn(),
        closeBuffer: (id) => {
          closedIds.push(id);
          buffers.delete(id);
        },
        openWorkspaceBuffer: ((o: { path: string; metadata?: Record<string, unknown> }) => {
          buffers.set(o.path, tab(String(o.metadata?.chatId)));
          return o.path;
        }) as never,
      }),
    );
    // "gone" is deleted; "late" has a tab but hasn't reached the list yet.
    buffers.set('__workspace/chat/late', tab('late'));
    props.sessions = [{ id: 'A' }, { id: 'fresh' }];
    rerender();
    expect(closedIds).toEqual(['__workspace/chat/gone']);
  });

  it('does not pull focus from a restored file when the active chat first becomes known', () => {
    const buffers = new Map<string, EditorBuffer>([
      [
        'file-1',
        makeBuffer({
          id: 'file-1',
          kind: 'file',
          isActive: true,
          file: { ...makeBuffer({ id: 'x' }).file, path: 'README.md' },
        }),
      ],
    ]);
    const props = { active: 'A' as string | null };
    const opened: Array<{ path: string; activate?: boolean }> = [];
    const { rerender } = renderHook(() =>
      useChatSessionsSync({
        chatSessions: [{ id: 'A' }, { id: 'B' }] as never,
        activeChatId: props.active,
        buffersRef: { current: buffers },
        updateBufferTitle: vi.fn(),
        setBufferPinned: vi.fn(),
        setBufferClosable: vi.fn(),
        openWorkspaceBuffer: ((o: { path: string; activate?: boolean; metadata?: Record<string, unknown> }) => {
          opened.push({ path: o.path, activate: o.activate });
          buffers.set(o.path, makeBuffer({ id: o.path, metadata: { chatId: o.metadata?.chatId } }));
          return o.path;
        }) as never,
      }),
    );
    expect(opened.find((o) => o.path === '__workspace/chat/A')?.activate).toBe(false);

    // Switching chats does focus the chat you switched to.
    buffers.delete('__workspace/chat/B');
    props.active = 'B';
    rerender();
    expect(opened.filter((o) => o.path === '__workspace/chat/B').pop()?.activate).toBe(true);
  });

  it('keeps the stand-in while the active chat is unknown', () => {
    const closedIds: string[] = [];
    const buffers = new Map<string, EditorBuffer>([
      ['buffer-chat', makeBuffer({ id: 'buffer-chat', isClosable: false, isActive: true })],
    ]);
    const { calls } = setup({ sessions: [{ id: 'A' }, { id: 'B' }], activeChatId: null, buffers, closedIds });
    expect(closedIds).toEqual([]);
    expect(calls.open.every((c) => c.activate === false)).toBe(true);
  });

  it('opens a non-active session as an unpinned, closable, NON-activating tab', () => {
    const buffers = new Map<string, EditorBuffer>([
      [
        'buffer-chat',
        makeBuffer({ id: 'buffer-chat', isPinned: true, isClosable: false, metadata: { chatId: 'A' }, isActive: true }),
      ],
    ]);
    const { calls } = setup({ sessions: [{ id: 'A' }, { id: 'B' }], activeChatId: 'A', buffers });
    const b = calls.open.find((o) => o.id.endsWith('/B'));
    expect(b).toBeTruthy();
    expect(b!.isPinned).toBe(false);
    expect(b!.isClosable).toBe(true);
    // The background tab must not hijack the active chat.
    expect(b!.activate).toBe(false);
  });

  it('repairs a permanently-pinned non-active tab so it becomes closable/unpinned', () => {
    // Simulate the old-bug state: session B's tab was permanently pinned and unclosable.
    const buffers = new Map<string, EditorBuffer>([
      ['buffer-chat', makeBuffer({ id: 'buffer-chat', isPinned: true, isClosable: false, metadata: { chatId: 'A' } })],
      [
        '__workspace/chat/B',
        makeBuffer({
          id: '__workspace/chat/B',
          isPinned: true,
          isClosable: false,
          metadata: { chatId: 'B', isActive: false },
        }),
      ],
    ]);
    const activeChatId = 'A';
    const { calls, buffers: mutated } = setup({
      sessions: [{ id: 'A' }, { id: 'B' }],
      activeChatId,
      buffers,
    });
    // B's tab was repaired: it must now be closable and unpinned.
    expect(calls.closable.some(([id, v]) => id === '__workspace/chat/B' && v === true)).toBe(true);
    expect(calls.pinned.some(([id, v]) => id === '__workspace/chat/B' && v === false)).toBe(true);
    expect(mutated.get('__workspace/chat/B')?.isClosable).toBe(true);
    expect(mutated.get('__workspace/chat/B')?.isPinned).toBe(false);
  });

  it('keeps the active chat tab unclosable but never pinned', () => {
    const buffers = new Map<string, EditorBuffer>([
      ['buffer-chat', makeBuffer({ id: 'buffer-chat', isPinned: true, isClosable: false, metadata: { chatId: 'A' } })],
    ]);
    const { calls } = setup({
      sessions: [{ id: 'A' }, { id: 'B' }],
      activeChatId: 'A',
      buffers,
    });
    expect(calls.closable.some(([id, v]) => id === 'buffer-chat' && v === true)).toBe(false);
    // A pinned chat tab collapses to an icon with the selected accent; the
    // active chat's tab is unpinned so only the focused tab looks focused.
    expect(buffers.get('buffer-chat')?.isPinned).toBe(false);
    expect(Array.from(buffers.values()).some((b) => b.isPinned)).toBe(false);
  });

  it('opens the active chat tab unpinned', () => {
    const buffers = new Map<string, EditorBuffer>();
    const { calls } = setup({ sessions: [{ id: 'A' }, { id: 'B' }], activeChatId: 'B', buffers });
    expect(calls.open.find((c) => c.id === '__workspace/chat/B')).toMatchObject({ isPinned: false, activate: true });
  });

  it('drops the unclaimed initial tab when the active chat already has its own tab', () => {
    const closedIds: string[] = [];
    const buffers = new Map<string, EditorBuffer>([
      ['buffer-chat', makeBuffer({ id: 'buffer-chat', isClosable: false, metadata: { chatId: null } })],
      [
        '__workspace/chat/B',
        makeBuffer({
          id: '__workspace/chat/B',
          file: { ...makeBuffer({ id: 'x' }).file, path: '__workspace/chat/B' },
          metadata: { chatId: 'B' },
        }),
      ],
    ]);
    setup({ sessions: [{ id: 'A' }, { id: 'B' }], activeChatId: 'B', buffers, closedIds });
    expect(closedIds).toEqual(['buffer-chat']);
    expect(Array.from(buffers.values()).filter((b) => b.metadata?.chatId === 'B')).toHaveLength(1);
  });

  it('mode lanes: the design lane mirrors only design chats, the code lane the rest', () => {
    const sessions = [
      { id: 'code-1', name: 'Code one' },
      { id: 'design-1', name: 'Design one', mode: 'design' },
      { id: 'legacy-1', name: 'Legacy' },
    ];
    // Code lane: design-1 gets no tab.
    const codeBuffers = new Map<string, EditorBuffer>();
    const code = setup({ sessions, activeChatId: 'code-1', buffers: codeBuffers });
    expect(code.calls.open.map((c) => c.id)).toEqual(
      expect.arrayContaining(['__workspace/chat/code-1', '__workspace/chat/legacy-1']),
    );
    expect(code.calls.open.some((c) => c.id === '__workspace/chat/design-1')).toBe(false);
  });

  it('mode lanes: design mode mirrors only the design chats', () => {
    const sessions = [
      { id: 'code-1', name: 'Code one' },
      { id: 'design-1', name: 'Design one', mode: 'design' },
    ];
    const designBuffers = new Map<string, EditorBuffer>();
    const design = setup({ sessions, activeChatId: 'design-1', buffers: designBuffers, mode: 'design' });
    expect(design.calls.open.map((c) => c.id)).toEqual(['__workspace/chat/design-1']);
  });

  it('mode lanes: a lane switch closes the previous lane chat tabs', () => {
    const sessions = [
      { id: 'code-1', name: 'Code one' },
      { id: 'design-1', name: 'Design one', mode: 'design' },
    ];
    const closedIds: string[] = [];
    const buffers = new Map<string, EditorBuffer>();
    const closeBuffer = (id: string) => {
      closedIds.push(id);
      buffers.delete(id);
    };
    const openWorkspaceBuffer = (o: { path: string; metadata?: Record<string, unknown> }) => {
      buffers.set(o.path, makeBuffer({ id: o.path, metadata: { chatId: o.metadata?.chatId } }));
      return o.path;
    };

    const props = { mode: 'code' as string };
    const utils = renderHook(() =>
      useChatSessionsSync({
        chatSessions: sessions as never,
        activeChatId: 'code-1',
        mode: props.mode,
        buffersRef: { current: buffers },
        updateBufferTitle: vi.fn(),
        setBufferPinned: vi.fn(),
        setBufferClosable: vi.fn(),
        closeBuffer,
        openWorkspaceBuffer: openWorkspaceBuffer as never,
      }),
    );
    // Code lane mounted: code-1 has a tab, design-1 does not.
    expect(buffers.has('__workspace/chat/code-1')).toBe(true);
    expect(buffers.has('__workspace/chat/design-1')).toBe(false);

    // Switch to the design lane: the code tab closes, the design tab opens.
    props.mode = 'design';
    utils.rerender();
    expect(closedIds).toContain('__workspace/chat/code-1');
    expect(buffers.has('__workspace/chat/design-1')).toBe(true);
    expect(buffers.has('__workspace/chat/code-1')).toBe(false);
    utils.unmount();
  });

  it("mode lanes: a lane switch also closes the previous lane's active (unclosable) chat tab", () => {
    const sessions = [
      { id: 'code-1', name: 'Code one' },
      { id: 'design-1', name: 'Design one', mode: 'design' },
    ];
    const buffers = new Map<string, EditorBuffer>();
    // Like the buffer manager: an unclosable buffer is left alone.
    const closeBuffer = (id: string) => {
      if (buffers.get(id)?.isClosable === false) return;
      buffers.delete(id);
    };
    const setBufferClosable = (id: string, isClosable: boolean) => {
      const b = buffers.get(id);
      if (b) buffers.set(id, { ...b, isClosable });
    };
    const openWorkspaceBuffer = (o: { path: string; isClosable?: boolean; metadata?: Record<string, unknown> }) => {
      buffers.set(
        o.path,
        makeBuffer({ id: o.path, isClosable: o.isClosable, metadata: { chatId: o.metadata?.chatId } }),
      );
      return o.path;
    };

    const props = { mode: 'design' as string, active: 'design-1' };
    const utils = renderHook(() =>
      useChatSessionsSync({
        chatSessions: sessions as never,
        activeChatId: props.active,
        mode: props.mode,
        buffersRef: { current: buffers },
        updateBufferTitle: vi.fn(),
        setBufferPinned: vi.fn(),
        setBufferClosable,
        closeBuffer,
        openWorkspaceBuffer: openWorkspaceBuffer as never,
      }),
    );
    expect(buffers.get('__workspace/chat/design-1')?.isClosable).toBe(false);

    // Back to Code while the design chat is still the active one.
    props.mode = 'code';
    utils.rerender();
    expect(buffers.has('__workspace/chat/design-1')).toBe(false);
    expect(buffers.has('__workspace/chat/code-1')).toBe(true);
    utils.unmount();
  });
});
