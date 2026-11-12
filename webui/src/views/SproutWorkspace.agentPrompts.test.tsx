/**
 * The agent's blocking prompts in a composed workspace (own mode).
 *
 * A daemon agent that needs the user — a tool approval, an `ask_user`
 * question, a per-part shell approval — sends a request over the events
 * stream and waits. The composed workspace must show the dialog and send the
 * answer back, in the full space shell and in an arranged layout alike;
 * otherwise the turn hangs with nothing on screen.
 *
 * The events transport is a fake that records what the workspace sends; the
 * provider stack, the chat unit and the dialogs are the real ones. The Code
 * shell and the chat view are stubbed, as they play no part in the prompts.
 */

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

type EventCallback = (event: { type: string; data: Record<string, unknown> }) => void;
type FakeProvider = {
  onEvent: { mock: { calls: unknown[][] } };
  sendEvent: { mock: { calls: unknown[][] } };
};

const providers = vi.hoisted(() => [] as FakeProvider[]);

vi.mock('../services/localEventsProvider', () => ({
  LocalEventsProvider: class {
    constructor() {
      providers.push(this as unknown as FakeProvider);
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
    setActiveChatId = vi.fn();
    getQueuedMessageCount = vi.fn(() => 0);
  },
}));

vi.mock('../components/ChatView', () => ({ default: () => <div data-testid="prompts-chat" /> }));
vi.mock('./hostAgentBackend', () => ({ wireHostAgentBackend: vi.fn(), resetHostAgentBackend: vi.fn() }));
vi.mock('../services/apiFileCheck', () => ({ checkFilesModified: vi.fn().mockResolvedValue({ modified: [] }) }));
vi.mock('../workspaces/CodeShell', () => ({ default: () => <div data-testid="prompts-code-shell" /> }));

import { setActiveHost } from '../host/accessor';
import { headlessHost } from '../host/HostProvider';
import type { SproutHost } from '../host/types';
import { SproutWorkspace } from './SproutWorkspace';

const PROJECT = { id: 'acme/widgets', repoUrl: 'https://github.com/acme/widgets' };
const CHAT_ONLY = { left: [], center: ['chat'], right: [], overlay: [] };

/** The platform once a project's workspace runs: bearer transport, daemon behind the proxy. */
function platformDaemonHost(): SproutHost {
  const base = headlessHost();
  return {
    ...base,
    transport: {
      apiBaseURL: 'https://platform.test',
      wsURL: 'wss://platform.test/ws',
      authMode: 'bearer',
      agent: { kind: 'daemon', apiBaseURL: '/workspace/ws-1/proxy', wsURL: 'wss://platform.test/ws' },
    },
    capabilities: { ...base.capabilities, chat: true, localTerminal: false },
  };
}

let fetchMock: ReturnType<typeof vi.fn>;

/** Deliver an event to everything subscribed to the workspace's transport. */
function emit(event: { type: string; data: Record<string, unknown> }): void {
  act(() => {
    for (const provider of providers) {
      for (const call of provider.onEvent.mock.calls) (call[0] as EventCallback)(event);
    }
  });
}

function sent(): unknown[] {
  return providers.flatMap((provider) => provider.sendEvent.mock.calls.map((call) => call[0]));
}

const APPROVAL_REQUEST = {
  type: 'security_approval_request',
  data: {
    // The daemon sends tool approvals without the turn's chat id.
    chat_id: '',
    request_id: 'appr-1',
    tool_name: 'shell_command',
    risk_level: 'CAUTION',
    reasoning: 'Runs a command in the workspace.',
    command: 'uname -a && df -h /',
  },
};

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
    const body = url.includes('/api/shell-approvals/')
      ? { delivered: true }
      : url.includes('/api/workspace')
        ? { workspace_root: '', daemon_root: '' }
        : { chat_sessions: [{ id: 'default', name: 'Chat' }], active_chat_id: 'default' };
    return {
      ok: true,
      status: 200,
      headers: new Headers(),
      json: async () => body,
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

describe('SproutWorkspace agent prompts', () => {
  it('shows a tool approval in the full space shell and sends the answer back', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={platformDaemonHost()} />);
    await screen.findByTestId('prompts-code-shell');
    await waitFor(() => expect(providers[0]?.onEvent.mock.calls.length).toBeGreaterThan(0));

    emit(APPROVAL_REQUEST);
    const dialog = await screen.findByRole('dialog', { name: 'Security approval required' });
    expect(dialog).toHaveTextContent('uname -a && df -h /');

    fireEvent.click(screen.getByRole('button', { name: /allow/i }));
    expect(sent()).toContainEqual({
      type: 'security_approval_response',
      data: { request_id: 'appr-1', approved: true },
    });
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Security approval required' })).not.toBeInTheDocument(),
    );
  });

  it('shows a tool approval in an arranged layout too', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={platformDaemonHost()} layout={CHAT_ONLY} />);
    await screen.findByTestId('prompts-chat');
    await waitFor(() => expect(providers[0]?.onEvent.mock.calls.length).toBeGreaterThan(0));

    emit(APPROVAL_REQUEST);
    expect(await screen.findByRole('dialog', { name: 'Security approval required' })).toBeInTheDocument();
  });

  it('answers an ask_user question from a daemon over the socket', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={platformDaemonHost()} />);
    await screen.findByTestId('prompts-code-shell');
    await waitFor(() => expect(providers[0]?.onEvent.mock.calls.length).toBeGreaterThan(0));

    emit({
      type: 'ask_user_request',
      data: {
        request_id: 'ask-1',
        question: 'Which branch?',
        options: [{ label: 'main' }, { label: 'dev' }],
      },
    });
    expect(await screen.findByRole('dialog', { name: 'User input required' })).toHaveTextContent('Which branch?');

    fireEvent.click(screen.getByText('dev'));
    await waitFor(() =>
      expect(sent()).toContainEqual({ type: 'ask_user_response', data: { request_id: 'ask-1', response: 'dev' } }),
    );
    // A daemon has no in-browser ask-user endpoint.
    expect(fetchMock.mock.calls.map((call: unknown[]) => String(call[0]))).not.toContainEqual(
      expect.stringContaining('/api/ask-user/response'),
    );
  });

  it('shows a shell approval and posts the decision to the backend', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={platformDaemonHost()} />);
    await screen.findByTestId('prompts-code-shell');
    await waitFor(() => expect(providers[0]?.onEvent.mock.calls.length).toBeGreaterThan(0));

    emit({
      type: 'shell_approval_request',
      data: {
        request_id: 'shell-1',
        command: 'rm -rf build',
        parts: [{ id: 'p1', command: 'rm -rf build', risk_level: 'CAUTION' }],
      },
    });
    await waitFor(() => expect(document.body).toHaveTextContent('rm -rf build'));

    fireEvent.click(screen.getByTestId('shell-approval-submit'));
    await waitFor(() =>
      expect(fetchMock.mock.calls.map((call: unknown[]) => String(call[0]))).toContainEqual(
        expect.stringContaining('/api/shell-approvals/shell-1/decision'),
      ),
    );
    await waitFor(() => expect(screen.queryByTestId('shell-approval-submit')).not.toBeInTheDocument());
  });
});
