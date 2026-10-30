/**
 * websocketChatAttach.test.ts — the events socket attaches to the chat on
 * screen when the agent runs on a daemon but the chat sessions live in the
 * host's store.
 *
 * The page's session calls go to the host there, so the daemon never learns
 * which chat the page shows and routes a run started elsewhere (by the host,
 * or another tab) to nobody. The socket therefore connects with
 * `reattach=<chat>` (from `after_seq=0` when nothing was seen yet), and moves
 * when the chat on screen changes. A daemon that owns its sessions, and an
 * in-page agent, connect as before.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { setActiveHost } from '../host/accessor';
import type { SproutHost } from '../host/types';
import { WebSocketService, attachesToChatOnScreen } from './websocket';

// vi.mock calls are hoisted above the imports.
vi.mock('../utils/log', () => ({ debugLog: vi.fn() }));

vi.mock('./clientSession', () => ({
  appendClientIdToUrl: vi.fn((url: string) => url),
  clientFetch: vi.fn(),
  getProxyBase: vi.fn(() => ''),
}));

vi.mock('./apiAdapter', () => ({
  getAdapter: vi.fn(() => null),
  ADAPTER_INSTALLED_EVENT: 'sprout:adapter-installed',
}));

vi.mock('./notificationBus', () => ({ notificationBus: { notify: vi.fn() } }));

const WS_CONNECTING = 0;
const WS_OPEN = 1;
const WS_CLOSED = 3;

interface FakeSocket {
  url: string;
  readyState: number;
  onopen: ((event: Event) => void) | null;
  onclose: ((event: CloseEvent) => void) | null;
  onerror: ((event: Event) => void) | null;
  onmessage: ((event: MessageEvent) => void) | null;
  send: ReturnType<typeof vi.fn>;
  close: ReturnType<typeof vi.fn>;
}

function stubWebSocket(): FakeSocket[] {
  const sockets: FakeSocket[] = [];
  const Fake = vi.fn(function (url: string) {
    const s: FakeSocket = {
      url,
      readyState: WS_CONNECTING,
      onopen: null,
      onclose: null,
      onerror: null,
      onmessage: null,
      send: vi.fn(),
      close: vi.fn(),
    };
    sockets.push(s);
    return s;
  }) as unknown as typeof WebSocket;
  (Fake as unknown as Record<string, number>).CONNECTING = WS_CONNECTING;
  (Fake as unknown as Record<string, number>).OPEN = WS_OPEN;
  (Fake as unknown as Record<string, number>).CLOSED = WS_CLOSED;
  vi.stubGlobal('WebSocket', Fake);
  return sockets;
}

function host(opts: { chatSessions: boolean; agent?: 'daemon' | 'wasm' }): SproutHost {
  const agent =
    opts.agent === 'daemon'
      ? { kind: 'daemon' as const, apiBaseURL: '/workspace/w1/proxy', wsURL: 'wss://app.example/workspace/w1/proxy/ws' }
      : opts.agent === 'wasm'
        ? { kind: 'wasm' as const }
        : undefined;
  return {
    transport: { apiBaseURL: '', wsURL: 'wss://app.example/ws', authMode: 'bearer', ...(agent ? { agent } : {}) },
    capabilities: { chatSessions: opts.chatSessions },
    navigation: {},
  } as unknown as SproutHost;
}

function freshService(): WebSocketService {
  // @ts-expect-error — the singleton is private; tests start from a fresh one.
  WebSocketService.instance = null;
  return WebSocketService.getInstance();
}

async function open(sockets: FakeSocket[]): Promise<FakeSocket> {
  await vi.waitFor(() => expect(sockets.length).toBeGreaterThan(0));
  const s = sockets[sockets.length - 1];
  s.readyState = WS_OPEN;
  s.onopen?.(new Event('open'));
  return s;
}

describe('the events socket attaches to the chat on screen', () => {
  let sockets: FakeSocket[];

  beforeEach(() => {
    sockets = stubWebSocket();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('applies only to a daemon backend whose sessions live in the host store', () => {
    setActiveHost(host({ chatSessions: true, agent: 'daemon' }));
    expect(attachesToChatOnScreen()).toBe(true);
    setActiveHost(host({ chatSessions: false, agent: 'daemon' }));
    expect(attachesToChatOnScreen()).toBe(false);
    setActiveHost(host({ chatSessions: true, agent: 'wasm' }));
    expect(attachesToChatOnScreen()).toBe(false);
  });

  it('connects attached to the chat on screen, from the start of its retained run', async () => {
    setActiveHost(host({ chatSessions: true, agent: 'daemon' }));
    const service = freshService();
    service.setActiveChatId('chat-host-1');
    void service.connect();
    const s = await open(sockets);
    const url = new URL(s.url);
    expect(url.pathname).toBe('/workspace/w1/proxy/ws');
    expect(url.searchParams.get('reattach')).toBe('chat-host-1');
    expect(url.searchParams.get('after_seq')).toBe('0');
  });

  it('moves the socket to a newly shown chat', async () => {
    setActiveHost(host({ chatSessions: true, agent: 'daemon' }));
    const service = freshService();
    service.setActiveChatId('chat-a');
    void service.connect();
    await open(sockets);

    service.setActiveChatId('chat-b');
    await vi.waitFor(() => expect(sockets.length).toBe(2));
    expect(new URL(sockets[1].url).searchParams.get('reattach')).toBe('chat-b');

    // The same chat again does not reconnect.
    service.setActiveChatId('chat-b');
    expect(sockets.length).toBe(2);
  });

  it('leaves a daemon that owns its sessions as it was', async () => {
    setActiveHost(host({ chatSessions: false, agent: 'daemon' }));
    const service = freshService();
    service.setActiveChatId('chat-a');
    void service.connect();
    const s = await open(sockets);
    expect(new URL(s.url).searchParams.get('reattach')).toBeNull();

    service.setActiveChatId('chat-b');
    expect(sockets.length).toBe(1);
  });
});
