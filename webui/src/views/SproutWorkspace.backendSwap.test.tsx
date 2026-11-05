/**
 * SproutWorkspace — switching the agent backend in place.
 *
 * A host that re-reads its transport (a new `transport.agent`) passes the
 * updated `host` to the mounted workspace; the composition re-wires the
 * backend without a re-mount. This pins what survives that swap:
 *
 * - the open editor tabs (the buffer manager under `SproutProviders` is not
 *   re-mounted),
 * - the active chat session and its transcript (the chat store is created once
 *   from the seed and not re-created when the `host` prop changes),
 * - the in-browser adapter is retired (the daemon's transport drives the calls
 *   again, so `getAdapter()` is null),
 * - the event stream reconnects to the new backend's socket (a new WebSocket is
 *   constructed against the daemon's `wsURL`).
 *
 * The real wiring, events transport, editor manager and provider stack run;
 * the cloud adapter, the chat view and the editor pane are stubbed, and the
 * WebSocket is a recorder that never opens.
 */

import type { Message } from '@sprout/ui';
import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

const wsUrls = vi.hoisted(() => [] as string[]);

/** A socket that records the URL it is constructed with and never opens. */
class RecordingWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;
  readyState = 0;
  onopen: unknown = null;
  onclose: unknown = null;
  onerror: unknown = null;
  onmessage: unknown = null;
  constructor(url: string) {
    wsUrls.push(url);
  }
  send = vi.fn();
  close = vi.fn();
}

vi.mock('../services/cloudAdapter', () => ({
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
    fetch = async () =>
      new Response(JSON.stringify({ providers: [], chat_sessions: [], active_chat_id: null }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
  },
}));
vi.mock('../services/cloudWasmHandlers', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../services/cloudWasmHandlers')>()),
  setAgentEventDispatcher: vi.fn(),
}));
// The chat view is a composite that needs the whole chat context stack; stub it
// with a node that reveals the active session and the transcript it was handed.
vi.mock('../components/ChatView', () => ({
  default: (props: { chatId?: string; messages?: unknown[] }) => (
    <div data-testid="swap-chat" data-active={props.chatId ?? ''} data-count={String(props.messages?.length ?? 0)} />
  ),
}));
vi.mock('../components/AgentChangesPanel', () => ({ default: () => <div /> }));
// The editor pane (CodeMirror) is stubbed with a node that shows the buffer it
// was given; the buffer manager itself is the real one.
vi.mock('../components/EditorPane', async () => {
  const { useEditorManager } = await import('../contexts/EditorManagerContext');
  return {
    default: ({ paneId }: { paneId: string }) => {
      const { panes, buffers } = useEditorManager();
      const buffer = buffers.get(panes.find((p) => p.id === paneId)?.bufferId ?? '');
      return <div data-testid="editor-pane" data-path={buffer?.file.path} />;
    },
  };
});

import type { SproutHost } from '../host';
import { setActiveHost } from '../host/accessor';
import { headlessHost } from '../host/HostProvider';
import { getAdapter } from '../services/apiAdapter';
import { WebSocketService } from '../services/websocket';
import { SproutWorkspace } from './SproutWorkspace';
import type { SproutProject } from './SproutWorkspace';
import type { ViewsArrangement } from './ViewsLayout';
import { createEmptyChatState } from './WorkspaceChatContext';

const PROJECT: SproutProject = { id: 'acme/widgets', root: '/work/widgets' };
/** Chat + editor in the center: the arrangement whose state must survive a swap. */
const LAYOUT: ViewsArrangement = { left: [], center: ['chat', 'editor'], right: [], overlay: [] };

function wasmHost(): SproutHost {
  const base = headlessHost();
  return {
    ...base,
    capabilities: { ...base.capabilities, settings: true },
    transport: { apiBaseURL: '', wsURL: '', authMode: 'none', agent: { kind: 'wasm', modelEndpoint: '' } },
  };
}

function daemonHost(): SproutHost {
  const base = headlessHost();
  return {
    ...base,
    transport: {
      apiBaseURL: 'https://daemon.test',
      wsURL: 'wss://daemon.test/ws',
      authMode: 'none',
      agent: { kind: 'daemon', apiBaseURL: 'https://daemon.test', wsURL: 'wss://daemon.test/ws' },
    },
  };
}

const USER_MSG: Message = { id: 'm1', type: 'user', content: 'hello', timestamp: new Date() };
const ASSISTANT_MSG: Message = { id: 'm2', type: 'assistant', content: 'hi there', timestamp: new Date() };

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  localStorage.clear();
  wsUrls.length = 0;
  setActiveHost(headlessHost());
  vi.stubGlobal('WebSocket', RecordingWebSocket);
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  WebSocketService.getInstance().setLocalAgentState('none');
});

describe('SproutWorkspace in-place backend swap', () => {
  it('keeps the open editor tabs and the active chat across a wasm→daemon swap', async () => {
    // The seed is created once and passed to both renders (a stable reference):
    // the chat store is created from it and must not be re-created on the swap.
    const seed = createEmptyChatState();
    seed.activeChatId = 'chat-1';
    seed.messages = [USER_MSG, ASSISTANT_MSG];

    const { rerender } = render(
      <SproutWorkspace project={PROJECT} space="code" host={wasmHost()} layout={LAYOUT} chatInitialState={seed} />,
    );
    // Wait for the in-browser adapter to install (the wasm wiring is async).
    await vi.waitFor(() => expect(getAdapter()).not.toBeNull());

    // Open two files in the editor through the composition's open-in-editor
    // listener (the same path the file tree and chat file links use).
    act(() => {
      window.dispatchEvent(new CustomEvent('sprout:open-in-editor', { detail: { path: 'src/app.ts' } }));
    });
    act(() => {
      window.dispatchEvent(new CustomEvent('sprout:open-in-editor', { detail: { path: 'src/util.ts' } }));
    });
    await screen.findByRole('tab', { name: /util\.ts/ });
    expect(screen.getByRole('tab', { name: /app\.ts/ })).toBeInTheDocument();
    expect(screen.getByTestId('swap-chat')).toHaveAttribute('data-active', 'chat-1');
    expect(screen.getByTestId('swap-chat')).toHaveAttribute('data-count', '2');

    // Swap to the daemon backend in place: a new `host` with a new `agent` and
    // a new socket URL. The project, the layout and the chat seed are unchanged.
    rerender(
      <SproutWorkspace project={PROJECT} space="code" host={daemonHost()} layout={LAYOUT} chatInitialState={seed} />,
    );

    // The editor tabs survived (the buffer manager was not re-mounted).
    expect(screen.getByRole('tab', { name: /app\.ts/ })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /util\.ts/ })).toBeInTheDocument();
    // The active chat session and transcript survived (the chat store was not
    // re-created).
    expect(screen.getByTestId('swap-chat')).toHaveAttribute('data-active', 'chat-1');
    expect(screen.getByTestId('swap-chat')).toHaveAttribute('data-count', '2');
    // The in-browser adapter was retired, so the daemon's transport drives the
    // calls again.
    await vi.waitFor(() => expect(getAdapter()).toBeNull());
    // The event stream reconnected to the daemon's socket (a new WebSocket
    // against its `wsURL`, not the in-browser host's).
    const daemonSocket = wsUrls.find((url) => url.startsWith('wss://daemon.test/ws'));
    expect(daemonSocket).toBeDefined();
  });

  it('reconnects the stream on a re-mount that swaps the backend (the socket outlives the mount)', async () => {
    // The socket is a process-lifetime singleton and the connection is left
    // open across unmounts, so a re-mount with a new transport must reconnect
    // exactly as an in-place swap does — the first wire of a fresh instance
    // cannot rely on a per-instance "no socket yet".
    const seed = createEmptyChatState();
    const first = render(
      <SproutWorkspace project={PROJECT} space="code" host={wasmHost()} layout={LAYOUT} chatInitialState={seed} />,
    );
    await vi.waitFor(() => expect(getAdapter()).not.toBeNull());
    first.unmount();
    wsUrls.length = 0;

    render(
      <SproutWorkspace project={PROJECT} space="code" host={daemonHost()} layout={LAYOUT} chatInitialState={seed} />,
    );
    const daemonSocket = wsUrls.find((url) => url.startsWith('wss://daemon.test/ws'));
    expect(daemonSocket).toBeDefined();
  });
});

describe('SproutWorkspace in-place swap to a proxied daemon', () => {
  it("reconnects the event stream to the daemon backend's socket, not the host's", async () => {
    // The platform's transport.wsURL is the platform's own socket; the running
    // workspace's daemon events live behind its workspace proxy. The swap must
    // reconnect to the daemon's socket, or the chat never receives the run.
    const base = headlessHost();
    const platformDaemonHost: SproutHost = {
      ...base,
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
    };
    const seed = createEmptyChatState();

    const { rerender } = render(
      <SproutWorkspace project={PROJECT} space="code" host={wasmHost()} layout={LAYOUT} chatInitialState={seed} />,
    );
    await vi.waitFor(() => expect(getAdapter()).not.toBeNull());

    rerender(
      <SproutWorkspace
        project={PROJECT}
        space="code"
        host={platformDaemonHost}
        layout={LAYOUT}
        chatInitialState={seed}
      />,
    );

    await vi.waitFor(() => expect(getAdapter()).toBeNull());
    expect(wsUrls.some((url) => url.startsWith('wss://platform.test/workspace/ws-1/proxy/ws'))).toBe(true);
    expect(wsUrls.some((url) => url.startsWith('wss://platform.test/ws'))).toBe(false);
  });
});
