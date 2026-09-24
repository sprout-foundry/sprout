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
      updateBufferMetadata: (id, updates) => {
        const b = opts.buffers.get(id);
        if (b) opts.buffers.set(id, { ...b, metadata: { ...b.metadata, ...updates } });
      },
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
      openWorkspaceBuffer: openWorkspaceBuffer as never,
    }),
  );

  return { ...utils, calls, buffers: opts.buffers };
}

describe('useChatSessionsSync', () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => vi.restoreAllMocks());

  it('claims the initial chat buffer for the active session (stays pinned)', () => {
    const buffers = new Map<string, EditorBuffer>([
      ['buffer-chat', makeBuffer({ id: 'buffer-chat', isPinned: true, isClosable: false, isActive: true })],
    ]);
    setup({ sessions: [{ id: 'A' }], activeChatId: 'A', buffers });
    // No new tab should be opened for the active session when buffer-chat is unclaimed.
    expect(buffers.get('buffer-chat')?.metadata?.chatId).toBe('A');
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

  it('keeps the active chat tab pinned and unclosable (single pinned tab)', () => {
    const buffers = new Map<string, EditorBuffer>([
      ['buffer-chat', makeBuffer({ id: 'buffer-chat', isPinned: true, isClosable: false, metadata: { chatId: 'A' } })],
    ]);
    const { calls } = setup({
      sessions: [{ id: 'A' }, { id: 'B' }],
      activeChatId: 'A',
      buffers,
    });
    // Active chat's own tab must NOT be unpinned or made closable.
    expect(calls.closable.some(([id, v]) => id === 'buffer-chat' && v === true)).toBe(false);
    expect(calls.pinned.some(([id, v]) => id === 'buffer-chat' && v === false)).toBe(false);
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
        updateBufferMetadata: vi.fn(),
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
});
