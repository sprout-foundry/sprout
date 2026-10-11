/**
 * SproutWorkspace — what an own-mode composition assembles beyond the units.
 *
 * - `chatOverrides` merge over the assembled chat props (the arranged chat
 *   and the composed space shell's chat) without dropping the live send.
 * - An arranged `previewPanel` gets an open state the composition owns, and
 *   the host's `viewProps.previewPanel` is merged over it.
 * - Without `layout` or `shellProps`, the space shell is fed from the chat and
 *   editor units; host `shellProps` still win.
 * - The chat session list loads once the events transport connects, on both
 *   the layout path and the composed shell.
 *
 * The chat view, preview panel and shells are stubbed with nodes that expose
 * the props the composition handed them; the provider stack and the units are
 * the real ones.
 */

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

const eventsProviders = vi.hoisted(() => [] as Array<{ onEvent: { mock: { calls: unknown[][] } } }>);

vi.mock('../services/localEventsProvider', () => ({
  LocalEventsProvider: class {
    constructor() {
      eventsProviders.push(this);
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

interface StubChatProps {
  onSendMessage?: unknown;
  onModelClick?: (provider: string) => void;
  onRestoreSession?: unknown;
}

function describeChat(props: StubChatProps): Record<string, string> {
  return {
    'data-send': typeof props.onSendMessage,
    'data-model-click': typeof props.onModelClick,
    'data-restore': typeof props.onRestoreSession,
  };
}

vi.mock('../components/ChatView', () => ({
  default: (props: StubChatProps) => <div data-testid="composition-chat" {...describeChat(props)} />,
}));
vi.mock('../components/AgentChangesPanel', () => ({ default: () => <div /> }));
vi.mock('../components/PreviewPanel', () => ({
  PreviewPanel: (props: { open?: boolean; onClose?: () => void }) => (
    <button
      type="button"
      data-testid="composition-preview"
      data-open={String(props.open)}
      data-close={typeof props.onClose}
      onClick={() => props.onClose?.()}
    >
      close
    </button>
  ),
}));
vi.mock('./hostAgentBackend', () => ({ wireHostAgentBackend: vi.fn() }));
vi.mock('../services/apiFileCheck', () => ({ checkFilesModified: vi.fn().mockResolvedValue({ modified: [] }) }));
vi.mock('../services/api', () => {
  const instance = {
    getHotkeys: async () => ({ hotkeys: [], platform: 'test' }),
    applyHotkeyPreset: async () => ({ hotkeys: [] }),
    getWorkspace: async () => ({ workspace_root: '' }),
    getStats: async () => ({}),
    getSettings: async () => ({}),
  };
  return { ApiService: { getInstance: () => instance } };
});
vi.mock('../workspaces/CodeShell', () => ({
  default: (props: {
    currentView: string;
    currentBuffer: { file: { path: string } } | null;
    git: { workspaceRoot: string };
    chat: { chatProps: StubChatProps; onCreateChat?: unknown };
  }) => (
    <div
      data-testid="composition-code-shell"
      data-view={props.currentView}
      data-buffer={props.currentBuffer?.file.path ?? ''}
      data-root={props.git.workspaceRoot}
      data-create={typeof props.chat.onCreateChat}
      {...describeChat(props.chat.chatProps)}
    />
  ),
}));
vi.mock('../workspaces/DesignShell', () => ({
  default: (props: { chat: { chatProps: StubChatProps } }) => (
    <div data-testid="composition-design-shell" {...describeChat(props.chat.chatProps)} />
  ),
}));

import type { SproutHost } from '../host';
import { setActiveHost } from '../host/accessor';
import { headlessHost } from '../host/HostProvider';
import type { WorkspaceShellProps } from '../workspaces/shell';
import { SproutWorkspace } from './SproutWorkspace';
import type { SproutProject } from './SproutWorkspace';
import type { ViewsArrangement } from './ViewsLayout';

const PROJECT: SproutProject = { id: 'acme/widgets', root: '/work/widgets' };
const CHAT_ONLY: ViewsArrangement = { left: [], center: ['chat'], right: [], overlay: [] };
const PREVIEW_ONLY: ViewsArrangement = { left: [], center: [], right: [], overlay: ['previewPanel'] };

function makeHost(): SproutHost {
  return { ...headlessHost(), transport: { apiBaseURL: 'https://host.test', wsURL: '', authMode: 'none' } };
}

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  localStorage.clear();
  setActiveHost(headlessHost());
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => ({
      ok: true,
      status: 200,
      headers: new Headers(),
      json: async () => ({}),
      text: async () => '',
    })),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('SproutWorkspace chatOverrides', () => {
  it('merges host callbacks over the assembled chat on the layout path', async () => {
    const overrides = { onModelClick: vi.fn(), onRestoreSession: vi.fn() };
    render(
      <SproutWorkspace project={PROJECT} space="code" host={makeHost()} layout={CHAT_ONLY} chatOverrides={overrides} />,
    );
    const chat = await screen.findByTestId('composition-chat');
    expect(chat).toHaveAttribute('data-send', 'function');
    expect(chat).toHaveAttribute('data-model-click', 'function');
    expect(chat).toHaveAttribute('data-restore', 'function');
  });

  it('is ignored when the host replaces the chat through viewProps', async () => {
    render(
      <SproutWorkspace
        project={PROJECT}
        space="code"
        host={makeHost()}
        layout={CHAT_ONLY}
        viewProps={{ chat: {} }}
        chatOverrides={{ onModelClick: vi.fn() }}
      />,
    );
    const chat = await screen.findByTestId('composition-chat');
    expect(chat).toHaveAttribute('data-send', 'undefined');
    expect(chat).toHaveAttribute('data-model-click', 'undefined');
  });
});

describe('SproutWorkspace arranged preview panel', () => {
  it('starts open and closes from its own close affordance', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={makeHost()} layout={PREVIEW_ONLY} />);
    const preview = await screen.findByTestId('composition-preview');
    expect(preview).toHaveAttribute('data-open', 'true');
    expect(preview).toHaveAttribute('data-close', 'function');

    act(() => fireEvent.click(preview));
    expect(screen.getByTestId('composition-preview')).toHaveAttribute('data-open', 'false');
  });

  it('merges the host props over the assembled ones', async () => {
    render(
      <SproutWorkspace
        project={PROJECT}
        space="code"
        host={makeHost()}
        layout={PREVIEW_ONLY}
        viewProps={{ previewPanel: { open: false } }}
      />,
    );
    const preview = await screen.findByTestId('composition-preview');
    expect(preview).toHaveAttribute('data-open', 'false');
    expect(preview).toHaveAttribute('data-close', 'function');
  });
});

describe('SproutWorkspace space shell in own mode', () => {
  it('feeds the Code shell from the chat and editor units without shellProps', async () => {
    render(
      <SproutWorkspace project={PROJECT} space="code" host={makeHost()} chatOverrides={{ onModelClick: vi.fn() }} />,
    );
    const shell = await screen.findByTestId('composition-code-shell');
    expect(shell).toHaveAttribute('data-send', 'function');
    expect(shell).toHaveAttribute('data-model-click', 'function');
    expect(shell).toHaveAttribute('data-create', 'function');
    expect(shell).toHaveAttribute('data-root', '/work/widgets');
    expect(shell).toHaveAttribute('data-view', 'chat');

    act(() => {
      window.dispatchEvent(new CustomEvent('sprout:open-in-editor', { detail: { path: 'src/app.ts' } }));
    });
    await waitFor(() => expect(screen.getByTestId('composition-code-shell')).toHaveAttribute('data-view', 'editor'));
    expect(screen.getByTestId('composition-code-shell')).toHaveAttribute('data-buffer', 'src/app.ts');
  });

  it('feeds the Design shell chat too', async () => {
    render(<SproutWorkspace project={PROJECT} space="design" hasDesignTree host={makeHost()} />);
    const shell = await screen.findByTestId('composition-design-shell');
    expect(shell).toHaveAttribute('data-send', 'function');
  });

  it('lets host shellProps win', async () => {
    const shellProps = {
      currentView: 'git',
      currentBuffer: null,
      git: { workspaceRoot: '/host/root' },
      chat: { chatProps: {} },
    } as unknown as WorkspaceShellProps;
    render(
      <SproutWorkspace
        project={PROJECT}
        space="code"
        host={makeHost()}
        shellProps={shellProps}
        chatOverrides={{ onModelClick: vi.fn() }}
      />,
    );
    const shell = await screen.findByTestId('composition-code-shell');
    expect(shell).toHaveAttribute('data-view', 'git');
    expect(shell).toHaveAttribute('data-root', '/host/root');
    expect(shell).toHaveAttribute('data-send', 'undefined');
    expect(shell).toHaveAttribute('data-model-click', 'undefined');
  });
});

describe('SproutWorkspace chat session load', () => {
  /** Deliver a connection event to every chat reducer subscribed so far. */
  function connect(): void {
    for (const provider of eventsProviders) {
      for (const [handler] of provider.onEvent.mock.calls) {
        (handler as (event: unknown) => void)({ type: 'connection_status', data: { connected: true } });
      }
    }
  }

  function sessionListCalls(): unknown[][] {
    return vi.mocked(fetch).mock.calls.filter(([input]) => /\/api\/chat-sessions(\?|$)/.test(String(input)));
  }

  it.each([
    ['the layout path', { layout: CHAT_ONLY }, 'composition-chat'],
    ['the composed space shell', {}, 'composition-code-shell'],
  ])('loads the session list on connect on %s', async (_label, extra, testId) => {
    render(<SproutWorkspace project={PROJECT} space="code" host={makeHost()} {...extra} />);
    await screen.findByTestId(testId);
    expect(sessionListCalls()).toHaveLength(0);

    act(() => connect());

    await waitFor(() => expect(sessionListCalls().length).toBeGreaterThan(0));
  });
});
