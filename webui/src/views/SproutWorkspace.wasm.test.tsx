/**
 * SproutWorkspace with an in-browser (wasm) agent backend.
 *
 * Such a host may serve no events socket, so "connected" follows the agent
 * wiring instead: not connected while the cloud adapter installs, connected
 * once it is in place. The work that waits for a connection (the provider
 * catalog and the chat session list) therefore runs after the adapter is
 * installed, and through it, never ahead of it.
 *
 * The real wiring, events transport and providers run; the cloud adapter is a
 * stub whose module import is held until the test releases it, and the
 * WebSocket never opens.
 */

import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

const gate = vi.hoisted(() => {
  let release: () => void = () => undefined;
  const opened = new Promise<void>((resolve) => {
    release = resolve;
  });
  return { opened, release: () => release(), adapterCalls: [] as string[] };
});

vi.mock('../services/cloudAdapter', async () => {
  // Hold the adapter module, as a slow dynamic import would.
  await gate.opened;
  return {
    CloudAdapter: class {
      name = 'cloud-test';
      requiresBackendHealthCheck = false;
      fileOpsViaAPI = false;
      showOnboarding = false;
      supportsSSH = false;
      supportsGit = false;
      supportsChat = true;
      supportsWorkspaceSwitching = false;
      supportsExport = false;
      supportsInstances = false;
      supportsLocalTerminal = false;
      supportsSettings = true;
      getWebSocketURL = () => '';
      getStartupRepo = () => undefined;
      fetch = async (input: RequestInfo | URL) => {
        gate.adapterCalls.push(String(input));
        return new Response(JSON.stringify({ providers: [], chat_sessions: [], active_chat_id: null }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      };
    },
  };
});
vi.mock('../services/cloudWasmHandlers', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../services/cloudWasmHandlers')>()),
  setAgentEventDispatcher: vi.fn(),
}));
vi.mock('../components/ChatView', () => ({ default: () => <div data-testid="wasm-chat" /> }));
vi.mock('../components/AgentChangesPanel', () => ({ default: () => <div /> }));

import type { SproutHost } from '../host';
import { setActiveHost } from '../host/accessor';
import { headlessHost } from '../host/HostProvider';
import { WebSocketService } from '../services/websocket';
import { SproutWorkspace } from './SproutWorkspace';
import type { ViewsArrangement } from './ViewsLayout';

const CHAT_ONLY: ViewsArrangement = { left: [], center: ['chat'], right: [], overlay: [] };

/** A socket that never opens: the host serves no events stream. */
class SilentWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;
  readyState = 0;
  onopen: unknown = null;
  onclose: unknown = null;
  onerror: unknown = null;
  onmessage: unknown = null;
  send = vi.fn();
  close = vi.fn();
}

function wasmHost(): SproutHost {
  const base = headlessHost();
  return {
    ...base,
    capabilities: { ...base.capabilities, settings: true },
    transport: { apiBaseURL: '', wsURL: '', authMode: 'none', agent: { kind: 'wasm', modelEndpoint: '' } },
  };
}

const isSessionList = (url: string) => /\/api\/chat-sessions(\?|$)/.test(url);
const isCatalog = (url: string) => /\/api\/providers(\?|$)/.test(url);

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  localStorage.clear();
  setActiveHost(headlessHost());
  vi.stubGlobal('WebSocket', SilentWebSocket);
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  WebSocketService.getInstance().setLocalAgentState('none');
});

describe('SproutWorkspace with a wasm agent backend', () => {
  it('loads the provider catalog and the session list once the adapter is installed, not before', async () => {
    render(
      <SproutWorkspace project={{ id: 'acme/widgets', root: '' }} space="code" host={wasmHost()} layout={CHAT_ONLY} />,
    );
    await screen.findByTestId('wasm-chat');

    // While the adapter installs: not connected, nothing loaded anywhere.
    await new Promise((resolve) => setTimeout(resolve, 50));
    const pageCalls = () => vi.mocked(fetch).mock.calls.map(([input]) => String(input));
    expect(WebSocketService.getInstance().isConnected()).toBe(false);
    expect(gate.adapterCalls).toEqual([]);
    expect(pageCalls().filter((url) => isSessionList(url) || isCatalog(url))).toEqual([]);

    gate.release();

    await waitFor(() => expect(gate.adapterCalls.some(isSessionList)).toBe(true));
    await waitFor(() => expect(gate.adapterCalls.some(isCatalog)).toBe(true));
    expect(WebSocketService.getInstance().isConnected()).toBe(true);
    // Both went through the installed adapter, none to the page origin.
    expect(pageCalls().filter((url) => isSessionList(url) || isCatalog(url))).toEqual([]);
  });
});
