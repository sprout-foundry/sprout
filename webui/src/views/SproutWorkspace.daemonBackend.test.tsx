/**
 * A composed workspace on a daemon backend: which store owns the conversation,
 * and which backend answers the workspace's own calls.
 *
 * The platform's transport names the platform; the daemon lives behind its
 * workspace proxy. A chat-session store the host serves is pinned to the host,
 * so a turn the daemon runs is written back there and restored from there on
 * the next load. Everything else — the change tracker included — belongs to the
 * daemon, so those calls resolve against the daemon's own base.
 */

import { act, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

const providers = vi.hoisted(() => [] as Array<{ onEvent: { mock: { calls: unknown[][] } } }>);

vi.mock('../services/localEventsProvider', () => ({
  LocalEventsProvider: class {
    constructor() {
      providers.push(this);
    }
    connect = vi.fn();
    disconnect = vi.fn();
    onEvent = vi.fn();
    removeEvent = vi.fn();
    sendEvent = vi.fn();
    isConnected = vi.fn(() => true);
    onReconnect = vi.fn();
    freeze = vi.fn();
    resume = vi.fn();
    resetAndReconnect = vi.fn();
    getQueuedMessageCount = vi.fn(() => 0);
  },
}));

vi.mock('../components/ChatView', () => ({ default: () => <div data-testid="daemon-chat" /> }));
vi.mock('../components/AgentChangesPanel', () => ({ default: () => <div data-testid="daemon-changes" /> }));
vi.mock('./hostAgentBackend', () => ({ wireHostAgentBackend: vi.fn(), resetHostAgentBackend: vi.fn() }));
vi.mock('../services/apiFileCheck', () => ({ checkFilesModified: vi.fn().mockResolvedValue({ modified: [] }) }));
vi.mock('../workspaces/CodeShell', () => ({ default: () => <div data-testid="daemon-code-shell" /> }));
vi.mock('../workspaces/DesignShell', () => ({ default: () => <div data-testid="daemon-design-shell" /> }));

import { setActiveHost } from '../host/accessor';
import { headlessHost } from '../host/HostProvider';
import type { SproutHost } from '../host/types';
import { SproutWorkspace } from './SproutWorkspace';

const PROJECT = { id: 'acme/widgets', root: '/work/widgets' };
const CHAT_AND_CHANGES = { left: [], center: ['chat'], right: ['changes'], overlay: [] };
const DAEMON_BASE = '/workspace/ws-1/proxy';

/** The platform once a project's workspace runs: bearer transport, daemon behind the proxy. */
function platformDaemonHost(): SproutHost {
  const base = headlessHost();
  return {
    ...base,
    transport: {
      apiBaseURL: 'https://platform.test',
      wsURL: 'wss://platform.test/ws',
      authMode: 'bearer',
      agent: { kind: 'daemon', apiBaseURL: DAEMON_BASE, wsURL: 'wss://platform.test/ws' },
    },
    capabilities: { ...base.capabilities, chatSessions: true, agentChanges: true },
  };
}

const TRANSCRIPT = {
  chat_sessions: [{ id: 'chat-1', name: 'Build it', active_query: false }],
  active_chat_id: 'chat-1',
  chat_session: {
    id: 'chat-1',
    messages: [
      { role: 'user', content: 'build the app' },
      { role: 'assistant', content: 'on it' },
    ],
    active_query: false,
  },
  message: 'ok',
  files: [],
  blocks: [],
  totals: {},
};

let fetchMock: ReturnType<typeof vi.fn>;

const calledURLs = () => fetchMock.mock.calls.map((call: unknown[]) => String(call[0]));
const turnPosts = () =>
  fetchMock.mock.calls
    .filter((call: unknown[]) => String(call[0]).includes('/api/chat-sessions/turn'))
    .map((call: unknown[]) => JSON.parse(String((call[1] as RequestInit | undefined)?.body)));

async function settle(): Promise<void> {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 60));
  });
}

function handler(): (event: { type: string; data: Record<string, unknown> }) => void {
  return providers[0].onEvent.mock.calls[0]?.[0] as never;
}

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  providers.length = 0;
  localStorage.clear();
  setActiveHost(headlessHost());
  fetchMock = vi.fn(async (input: unknown) => {
    const url = String(input);
    // The workspace-root probe expects its own envelope; every other call in
    // this file is a chat-session read, which the transcript shape answers.
    const body = url.includes('/api/workspace') ? { workspace_root: '', daemon_root: '' } : TRANSCRIPT;
    return {
      ok: true,
      status: 200,
      headers: new Headers(),
      json: async () => body,
      // `getWorkspace` parses the response text itself (it accepts both a JSON
      // envelope and a bare path), so the body has to be there too.
      text: async () => JSON.stringify(body),
    };
  });
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
  setActiveHost(headlessHost());
});

describe('SproutWorkspace on a daemon backend', () => {
  it('restores the conversation from the host store when it connects', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={platformDaemonHost()} layout={CHAT_AND_CHANGES} />);
    await screen.findByTestId('daemon-chat');

    await act(async () => {
      handler()({ type: 'connection_status', data: { connected: true } });
    });
    await settle();

    // The store is the host's, whatever backend runs the turns: the list and
    // the active chat's transcript come from the platform, not the daemon.
    expect(calledURLs()).toContain('https://platform.test/api/chat-sessions?project=acme%2Fwidgets');
    expect(calledURLs()).toContain('https://platform.test/api/chat-sessions/switch');
    expect(calledURLs().some((url) => url.startsWith(`${DAEMON_BASE}/api/chat-sessions`))).toBe(false);
  });

  it('writes each daemon turn back to the host store', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={platformDaemonHost()} layout={CHAT_AND_CHANGES} />);
    await screen.findByTestId('daemon-chat');
    const onEvent = handler();

    await act(async () => {
      onEvent({ type: 'query_started', data: { chat_id: 'chat-1', query: 'hello' } });
      onEvent({ type: 'query_completed', data: { chat_id: 'chat-1', query: 'hello', response: 'hi there' } });
    });
    await settle();

    // The question on the start, the question and the answer on the finish —
    // the host owns idempotency, so the repeat is the contract.
    expect(turnPosts()).toEqual([
      { chat_id: 'chat-1', query: 'hello', project: 'acme/widgets' },
      { chat_id: 'chat-1', query: 'hello', response: 'hi there', project: 'acme/widgets' },
    ]);
  });

  it("sends the change tracker's calls to the daemon, not the platform", async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={platformDaemonHost()} layout={CHAT_AND_CHANGES} />);
    await screen.findByTestId('daemon-changes');
    await settle();

    const changesApi = await import('../services/api/changesApi');
    const { clientFetch } = await import('../services/clientSession');
    await act(async () => {
      await changesApi.getSessionSummary(clientFetch);
      await changesApi.getSessionChanges(clientFetch, {});
    });

    // `/api/changes/*` lives on the daemon: the platform serves no such route,
    // so a call to its origin would 404 and the panel would stay empty.
    expect(calledURLs()).toContain(`${DAEMON_BASE}/api/changes/summary`);
    expect(calledURLs()).toContain(`${DAEMON_BASE}/api/changes/session`);
    expect(calledURLs()).not.toContain('https://platform.test/api/changes/summary');
  });
});
