import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../host/accessor', async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  getActiveHost: vi.fn(() => null),
}));
vi.mock('./clientSession', () => ({
  clientFetch: vi.fn(),
  appendClientIdToUrl: vi.fn((u: string) => u),
  getProxyBase: vi.fn(() => ''),
}));

import { getActiveHost } from '../host/accessor';
import { setChatRunning } from './cloudChatSessions';
import { agentRunsInPage, fetchChatRunning } from './runStatus';
import { WebSocketService } from './websocket';

const activeHost = getActiveHost as ReturnType<typeof vi.fn>;

function statusResponse(active: boolean, ok = true): Response {
  return { ok, status: ok ? 200 : 503, json: () => Promise.resolve({ active }) } as Response;
}

beforeEach(() => {
  activeHost.mockReturnValue(null);
  WebSocketService.getInstance().setLocalAgentState('none');
});

afterEach(() => {
  setChatRunning('chat-1', false);
  WebSocketService.getInstance().setLocalAgentState('none');
});

describe('fetchChatRunning with a daemon', () => {
  it('asks the per-chat query status', async () => {
    const fetchFn = vi.fn().mockResolvedValue(statusResponse(true));
    await expect(fetchChatRunning('chat 1', fetchFn)).resolves.toBe(true);
    expect(fetchFn).toHaveBeenCalledWith('/api/query/status?chat_id=chat%201');
  });

  it('throws on a server error so callers can retry', async () => {
    const fetchFn = vi.fn().mockResolvedValue(statusResponse(false, false));
    await expect(fetchChatRunning('chat-1', fetchFn)).rejects.toThrow('503');
  });
});

// In the hosted (WASM) build the status endpoints are answered by the
// platform (/api/query/status → /proxy/chat/status, /api/stats →
// /api/proxy/stats), which knows nothing about a run in the page. The
// page's run registry must decide instead.
describe('fetchChatRunning with the agent in the page', () => {
  it('reads the in-page run registry and never asks the server', async () => {
    WebSocketService.getInstance().setLocalAgentState('ready');
    const fetchFn = vi.fn().mockResolvedValue(statusResponse(false));
    expect(agentRunsInPage()).toBe(true);

    setChatRunning('chat-1', true);
    await expect(fetchChatRunning('chat-1', fetchFn)).resolves.toBe(true);
    setChatRunning('chat-1', false);
    await expect(fetchChatRunning('chat-1', fetchFn)).resolves.toBe(false);
    expect(fetchFn).not.toHaveBeenCalled();
  });

  it('treats the hosted platform transport as in-page', async () => {
    activeHost.mockReturnValue({ transport: { authMode: 'bearer' } });
    expect(agentRunsInPage()).toBe(true);
    activeHost.mockReturnValue({ transport: { agent: { kind: 'wasm' } } });
    expect(agentRunsInPage()).toBe(true);
    activeHost.mockReturnValue({ transport: { agent: { kind: 'daemon' } } });
    expect(agentRunsInPage()).toBe(false);
  });
});

describe('a daemon backend is authoritative over stale in-page wiring', () => {
  it('reads the daemon status even while the in-browser wiring still reports ready', () => {
    // An in-place wasm→daemon swap retires the adapter; the connection state
    // the in-browser wiring set can outlive it for a tick. The daemon runs the
    // agent, so its status endpoint is the truth.
    WebSocketService.getInstance().setLocalAgentState('ready');
    activeHost.mockReturnValue({
      transport: {
        apiBaseURL: 'https://platform.test',
        wsURL: 'wss://platform.test/ws',
        authMode: 'bearer',
        agent: {
          kind: 'daemon',
          apiBaseURL: '/workspace/ws-1/proxy',
          wsURL: 'wss://platform.test/workspace/ws-1/proxy/ws',
        },
      },
    });
    expect(agentRunsInPage()).toBe(false);
  });
});
