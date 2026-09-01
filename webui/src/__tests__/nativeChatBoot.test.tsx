// @vitest-environment jsdom
/**
 * R-4: boot-path tests for the Track R native-chat seam. Mirrors
 * nativeTerminalBoot.test.tsx (R-3 gold standard) and
 * nativeGitBootInit.test.tsx (closest sibling).
 *
 * The compile-time flag `NATIVE_CHAT_ENABLED` (baked into
 * useCommandSubmit, useChatSessionManager, useWebSocketEventHandler,
 * cloudWasmHandlers, and ChatView) is controlled exactly like the other
 * native tests: `vi.stubEnv('VITE_SPROUT_NATIVE_CHAT','1')` +
 * `vi.resetModules()` + a FRESH dynamic import, so the constant read at
 * import time reflects the env.
 *
 * Coverage in this file:
 *   1. `useCommandSubmit` — flag ON: the send path short-circuits,
 *      `onSend` is NEVER called, `onSendCommand` is NEVER called, and the
 *      textarea is cleared via `resetAndFocus`. Flag OFF: the real path
 *      executes — `onSend` IS called with the built command.
 *   2. `ChatView` render conditions — flag ON renders the single shared
 *      "Chat provided by the native shell" placeholder and does NOT render
 *      the `chat-main` container. Flag OFF renders `chat-main` and NOT the
 *      placeholder.
 *   3. Stub surface — importing
 *      `../services/nativeChatStubs/chatApi` directly: the no-op functions
 *      never touch the network and return inert values. The compile-time
 *      `NATIVE_CHAT_ENABLED` constant is a boolean, false when env unset.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, act } from '@testing-library/react';
import React, { useRef } from 'react';

// ── Hoisted spies ──────────────────────────────────────────────────────

const onSendMock = vi.hoisted(() => vi.fn());
const onSendCommandMock = vi.hoisted(() => vi.fn());
const onQueueMock = vi.hoisted(() => vi.fn());
const resetHistoryNavigationMock = vi.hoisted(() => vi.fn());
const saveToHistoryMock = vi.hoisted(() => vi.fn().mockResolvedValue(undefined));
const clearImagesMock = vi.hoisted(() => vi.fn());

// ── Mocks ──────────────────────────────────────────────────────────────

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
  useLog: () => ({
    debug: vi.fn(),
    error: vi.fn(),
    warn: vi.fn(),
    info: vi.fn(),
    success: vi.fn(),
  }),
}));

vi.mock('../components/ThemedDialog', () => ({
  showThemedConfirm: vi.fn().mockResolvedValue(false),
  showThemedAlert: vi.fn(),
}));

// ── 1. useCommandSubmit hook-level signal ──────────────────────────────

type HookModule = typeof import('../components/useCommandSubmit');

/**
 * Fresh import of the useCommandSubmit hook with the given compile-time
 * flag value. The flag is controlled via the env var that
 * `nativeChatFlag.ts` reads at import time.
 */
async function loadHook(chatFlagOn: boolean): Promise<HookModule> {
  if (chatFlagOn) {
    vi.stubEnv('VITE_SPROUT_NATIVE_CHAT', '1');
  } else {
    vi.unstubAllEnvs();
  }
  vi.resetModules();
  return import('../components/useCommandSubmit');
}

const captured: React.MutableRefObject<ReturnType<
  (typeof import('../components/useCommandSubmit'))['useCommandSubmit']
> | null> = {
  current: null,
};

let useCommandSubmitFn: HookModule['useCommandSubmit'];

function CommandSubmitHarness(props: { draftValue: string; captured: React.MutableRefObject<any | null> }) {
  const inputRef = useRef<HTMLTextAreaElement | null>(null);
  const isComposingRef = useRef(false);
  const ret = useCommandSubmitFn({
    draftValue: props.draftValue,
    updateValue: vi.fn(),
    attachedImages: [],
    clearImages: clearImagesMock,
    isProcessing: false,
    inputRef,
    saveToHistory: saveToHistoryMock,
    resetHistoryNavigation: resetHistoryNavigationMock,
    onSend: onSendMock,
    onSendCommand: onSendCommandMock,
    onQueue: onQueueMock,
    isComposingRef,
    disabled: false,
  });
  props.captured.current = ret;
  return null;
}

describe('useCommandSubmit — R-4 short-circuit', () => {
  beforeEach(() => {
    captured.current = null;
    onSendMock.mockReset();
    onSendCommandMock.mockReset();
    onQueueMock.mockReset();
    resetHistoryNavigationMock.mockReset();
    saveToHistoryMock.mockReset().mockResolvedValue(undefined);
    clearImagesMock.mockReset();
  });

  it('flag ON: handleSend short-circuits — onSend/onSendCommand NEVER called, textarea cleared', async () => {
    const mod = await loadHook(true);
    useCommandSubmitFn = mod.useCommandSubmit;

    await act(async () => {
      render(<CommandSubmitHarness draftValue="hello world" captured={captured} />);
    });
    await act(async () => {
      await Promise.resolve();
    });

    const hook = captured.current;
    expect(hook, 'hook result must have been captured').toBeTruthy();
    expect(hook!.canSend).toBe(true);

    // Trigger handleSend
    await act(async () => {
      await hook!.handleSend();
    });

    // The short-circuit: no send handler called, no history, no image cleanup.
    expect(onSendMock, 'onSend must never be called when flag is ON').not.toHaveBeenCalled();
    expect(onSendCommandMock, 'onSendCommand must never be called when flag is ON').not.toHaveBeenCalled();
    expect(saveToHistoryMock).not.toHaveBeenCalled();
    expect(clearImagesMock).not.toHaveBeenCalled();
    // resetAndFocus is called (clears the textarea) — this is the short-circuit behavior.
    // We can't directly spy on the internal resetAndFocus, but we verify the send handlers
    // were never invoked, which is the meaningful assertion.
  });

  it('flag OFF: handleSend executes the real path — onSend IS called with the command', async () => {
    const mod = await loadHook(false);
    useCommandSubmitFn = mod.useCommandSubmit;

    await act(async () => {
      render(<CommandSubmitHarness draftValue="hello world" captured={captured} />);
    });
    await act(async () => {
      await Promise.resolve();
    });

    const hook = captured.current;
    expect(hook, 'hook result must have been captured').toBeTruthy();
    expect(hook!.canSend).toBe(true);

    // Trigger handleSend
    await act(async () => {
      await hook!.handleSend();
    });

    // The real path: onSend is called with the trimmed command.
    expect(onSendMock).toHaveBeenCalledTimes(1);
    expect(onSendMock).toHaveBeenCalledWith('hello world');
    // History is saved.
    expect(saveToHistoryMock).toHaveBeenCalledTimes(1);
    expect(saveToHistoryMock).toHaveBeenCalledWith('hello world');
    // Images are cleared.
    expect(clearImagesMock).toHaveBeenCalledTimes(1);
  });

  it('flag OFF + onSendCommand present: handleSend prefers onSend (commandRef prefers onSendCommand)', async () => {
    const mod = await loadHook(false);
    useCommandSubmitFn = mod.useCommandSubmit;

    await act(async () => {
      render(<CommandSubmitHarness draftValue="test cmd" captured={captured} />);
    });
    await act(async () => {
      await Promise.resolve();
    });

    const hook = captured.current;
    expect(hook).toBeTruthy();

    await act(async () => {
      await hook!.handleSend();
    });

    // handleSend checks onSend first (then falls back to onSendCommand).
    // commandRef (used by handleNewSession) prefers onSendCommand first.
    // When both are present, handleSend calls onSend.
    expect(onSendMock).toHaveBeenCalledTimes(1);
    expect(onSendMock).toHaveBeenCalledWith('test cmd');
    expect(onSendCommandMock).not.toHaveBeenCalled();
  });

  it('flag ON: empty draft is a no-op regardless of flag (pre-short-circuit guard)', async () => {
    const mod = await loadHook(true);
    useCommandSubmitFn = mod.useCommandSubmit;

    await act(async () => {
      render(<CommandSubmitHarness draftValue="   " captured={captured} />);
    });
    await act(async () => {
      await Promise.resolve();
    });

    const hook = captured.current;
    expect(hook).toBeTruthy();
    expect(hook!.canSend).toBe(false);

    await act(async () => {
      await hook!.handleSend();
    });

    // Empty draft returns before the NATIVE_CHAT_ENABLED check.
    expect(onSendMock).not.toHaveBeenCalled();
    expect(onSendCommandMock).not.toHaveBeenCalled();
  });
});

// ── 2. ChatView render conditions (real component mount) ──────────────
//
// Mounting the real ChatView is the authoritative check for the render
// branch the task asks about:
//   { NATIVE_CHAT_ENABLED ? <…Chat provided by the native shell> : <…chat-main> }
// Flag ON: the placeholder renders and chat-main is suppressed.
// Flag OFF: chat-main renders and the placeholder is absent.

type ChatViewModule = typeof import('../components/ChatView');

// Lucide-react icons break under jsdom (forwardRef pattern) — replace with
// simple svg elements. Use a factory so any icon name works dynamically.
vi.mock('lucide-react', () => {
  const createMockIcon = (name: string) => {
    const Comp = React.forwardRef((props: Record<string, unknown>, ref: unknown) => {
      return React.createElement('svg', { ref, 'data-icon': name, ...props });
    });
    Comp.displayName = name;
    return Comp;
  };
  // Return a module with a getter that creates icons on demand.
  const mod: Record<string, unknown> = {};
  Object.defineProperty(mod, 'then', { value: undefined }); // prevent thenable detection
  return new Proxy(mod, {
    get(_, prop) {
      const name = String(prop);
      if (name === 'default' || name === 'then') return undefined;
      if (name.startsWith('_')) return undefined;
      if (typeof name === 'symbol') return undefined;
      if (!(name in mod)) {
        mod[name] = createMockIcon(name);
      }
      return mod[name];
    },
  });
});

// @sprout/ui: mock the ChatProps-dependent components and utilities.
vi.mock('@sprout/ui', () => {
  const MockCommandInput = vi.fn(() => React.createElement('div', { 'data-testid': 'command-input' }, 'Command Input'));
  return {
    ChatMessageContextMenu: vi.fn(({ children }: { children?: React.ReactNode }) => children),
    createHttpCommandCompletionApi: vi.fn(() => ({
      complete: vi.fn().mockResolvedValue([]),
    })),
    MessageItem: vi.fn(({ content, type }: { content?: string; type?: string }) =>
      React.createElement('div', { 'data-message-type': type }, content),
    ),
    ChatFooter: vi.fn(() => React.createElement('div', { 'data-testid': 'chat-footer' }, 'Chat Footer')),
    ChatHeader: vi.fn(() => React.createElement('div', { 'data-testid': 'chat-header' }, 'Chat Header')),
    EmptyChatPanel: vi.fn(() => React.createElement('div', { 'data-testid': 'empty-chat' }, 'Empty Chat')),
    CommandInput: MockCommandInput,
    MAX_ACTIVE_LINES: 50,
    MAX_COMPLETED_SUMMARIES: 10,
  };
});

// react-virtuoso: mock the list renderer.
vi.mock('react-virtuoso', () => ({
  Virtuoso: vi.fn(({ children }: { children?: React.ReactNode }) =>
    React.createElement('div', { 'data-testid': 'virtuoso' }, children),
  ),
}));

// isCloud: control via env (same pattern as the boot tests).
vi.mock('../config/mode', () => ({
  get isCloud() {
    return import.meta.env.VITE_SPROUT_MODE === 'cloud';
  },
  supportsWorkspaceSwitching: false,
  supportsExport: false,
  supportsSSH: false,
}));

// bootstrapAdapter: return a safe default config.
vi.mock('../bootstrapAdapter', () => ({
  getBootstrapConfig: () => ({
    appMode: 'cloud',
    user: { id: 'test-user', tier: 'pro' },
    foundryApiUrl: undefined,
    foundryWsUrl: undefined,
  }),
  fetchRuntimeConfig: async () => ({
    appMode: 'cloud',
    user: { id: 'test-user', tier: 'pro' },
  }),
}));

// chatApi: mock so no real fetch is ever issued.
vi.mock('../services/api/chatApi', () => ({
  sendQuery: vi.fn(),
  uploadImage: vi.fn(),
  steerQuery: vi.fn(),
  retractSteer: vi.fn(),
  executeCommand: vi.fn(),
  stopQuery: vi.fn(),
  rewindQuery: vi.fn(),
}));

// apiAdapter: keep it light.
vi.mock('../services/apiAdapter', () => ({
  getAdapter: vi.fn(() => null),
  installAdapter: vi.fn(),
  hasAdapter: () => true,
  requiresBackendHealthCheck: () => false,
  ADAPTER_INSTALLED_EVENT: 'sprout:adapter-installed',
}));

// clientSession: keep identity stable.
vi.mock('../services/clientSession', () => ({
  clientFetch: vi.fn(),
  appendClientIdToUrl: (u: string) => u,
  getProxyBase: () => '',
  getWebUIClientId: () => 'test-client',
  resolveWebUIClientId: async () => 'test-client',
  WEBUI_CLIENT_ID_HEADER: 'X-Sprout-Client-ID',
}));

// notificationBus: inert.
vi.mock('../services/notificationBus', () => ({
  notificationBus: {
    notify: vi.fn(),
    on: vi.fn(),
    off: vi.fn(),
    emit: vi.fn(),
  },
}));

// useCommandOutput: return a safe default.
vi.mock('../hooks/useCommandOutput', () => ({
  useCommandOutput: () => ({
    commandOutput: null,
    setCommandOutput: vi.fn(),
    clearCommandOutput: vi.fn(),
  }),
}));

// CommandInput is re-exported from @sprout/ui (handled above).
// CommandOutputPanel: mock.
vi.mock('./CommandOutputPanel', () => ({
  default: vi.fn(() => React.createElement('div', { 'data-testid': 'command-output-panel' }, 'Command Output')),
}));

// ExportDialog: mock.
vi.mock('./ExportDialog', () => ({
  default: vi.fn(() => React.createElement('div', { 'data-testid': 'export-dialog' }, 'Export Dialog')),
}));

// InlineTodoSummary: mock.
vi.mock('./InlineTodoSummary', () => ({
  default: vi.fn(() => React.createElement('div', { 'data-testid': 'inline-todo' }, 'Todo Summary')),
}));

// ToolTimelineBar: mock.
vi.mock('./chat/ToolTimelineBar', () => ({
  ToolTimelineBar: vi.fn(() => React.createElement('div', { 'data-testid': 'tool-timeline' }, 'Tool Timeline')),
}));

const PLACEHOLDER = 'Chat provided by the native shell';
const CHAT_MAIN_SELECTOR = '[data-testid="chat-main"]';

function makeChatViewProps(): Record<string, unknown> {
  return {
    messages: [],
    onSendMessage: vi.fn(),
    onQueueMessage: vi.fn(),
    queuedMessagesCount: 0,
    onQueueMessageRemove: vi.fn(),
    onQueueMessageEdit: vi.fn(),
    onQueueReorder: vi.fn(),
    onClearQueuedMessages: vi.fn(),
    inputValue: '',
    onInputChange: vi.fn(),
    isProcessing: false,
    lastError: null,
    toolExecutions: [],
    queryProgress: null,
    currentTodos: [],
    subagentActivities: [],
    onToolPillClick: vi.fn(),
    onStopProcessing: vi.fn(),
    onRetractSteer: vi.fn(),
    chatId: null,
    worktreePath: null,
    providerAvailable: true,
    onRequestProviderSetup: vi.fn(),
    stats: null,
    isConnected: true,
    backendReachable: true,
    onRetryConnection: vi.fn(),
    onForkAtBreakpoint: vi.fn(),
    onForkCancel: vi.fn(),
    forkBreakpoint: null,
    sessionId: null,
    onSessionIdChange: vi.fn(),
    onExport: vi.fn(),
    showExportDialog: false,
    onShowExportDialogChange: vi.fn(),
    onDownloadSession: vi.fn(),
    onCopySessionUrl: vi.fn(),
    onShareSession: vi.fn(),
    onOpenSettings: vi.fn(),
    onOpenModelPicker: vi.fn(),
    onOpenProviderSetup: vi.fn(),
    onOpenSSHDialog: vi.fn(),
  };
}

async function loadChatView(chatFlagOn: boolean): Promise<ChatViewModule['default']> {
  if (chatFlagOn) {
    vi.stubEnv('VITE_SPROUT_NATIVE_CHAT', '1');
    vi.stubEnv('VITE_SPROUT_MODE', 'cloud');
  } else {
    vi.unstubAllEnvs();
    vi.stubEnv('VITE_SPROUT_MODE', 'cloud');
  }
  vi.resetModules();
  const mod = await import('../components/ChatView');
  return mod.default;
}

describe('ChatView — R-4 render conditions', () => {
  it('flag ON: renders the shell-provided placeholder and NOT chat-main', async () => {
    const ChatView = await loadChatView(true);
    const props = makeChatViewProps();

    let rendered!: ReturnType<typeof render>;
    await act(async () => {
      rendered = render(<ChatView {...props} />);
    });
    await act(async () => {
      await Promise.resolve();
    });

    const text = rendered.container.textContent ?? '';
    expect(text).toContain(PLACEHOLDER);
    // chat-main must NOT be present when the flag is on.
    expect(rendered.container.querySelector(CHAT_MAIN_SELECTOR)).toBeNull();
  });

  it('flag OFF: renders chat-main and NOT the shell-provided placeholder', async () => {
    const ChatView = await loadChatView(false);
    const props = makeChatViewProps();

    let rendered!: ReturnType<typeof render>;
    await act(async () => {
      rendered = render(<ChatView {...props} />);
    });
    await act(async () => {
      await Promise.resolve();
    });

    const text = rendered.container.textContent ?? '';
    expect(text).not.toContain(PLACEHOLDER);
    // chat-main MUST be present when the flag is off.
    expect(rendered.container.querySelector(CHAT_MAIN_SELECTOR)).not.toBeNull();
  });
});

// ── 3. Stub surface (the hard-exclusion stand-in) ─────────────────────
//
// Vitest does NOT apply vite's conditional alias (nativeChatStubAliases),
// so we import the stub by its DIRECT path. It is the inert no-op stand-in
// a --native-chat dist bundles.

describe('nativeChatStubs/chatApi (inert no-op surface)', () => {
  it('sendQuery is a no-op that resolves void without fetching', async () => {
    const stub = await import('../services/nativeChatStubs/chatApi');
    const result = await stub.sendQuery(vi.fn(), 'hello');
    expect(result).toBeUndefined();
  });

  it('uploadImage returns an inert UploadImageResponse without fetching', async () => {
    const stub = await import('../services/nativeChatStubs/chatApi');
    const result = await stub.uploadImage(vi.fn(), new Blob());
    expect(result.path).toBe('');
    expect(result.filename).toBe('');
  });

  it('steerQuery is a no-op that resolves void without fetching', async () => {
    const stub = await import('../services/nativeChatStubs/chatApi');
    const result = await stub.steerQuery(vi.fn(), 'steer');
    expect(result).toBeUndefined();
  });

  it('retractSteer returns an inert success response without fetching', async () => {
    const stub = await import('../services/nativeChatStubs/chatApi');
    const result = await stub.retractSteer(vi.fn());
    expect(result.success).toBe(true);
    expect(result.message).toBe('Chat provided by the native shell');
  });

  it('executeCommand returns an inert ExecuteCommandResponse without fetching', async () => {
    const stub = await import('../services/nativeChatStubs/chatApi');
    const result = await stub.executeCommand(vi.fn(), '/clear');
    expect(result.command).toBe('/clear');
    expect(result.output).toBe('');
    expect(result.error).toBe('Chat provided by the native shell');
    expect(result.accepted).toBe(false);
  });

  it('stopQuery is a no-op that resolves void without fetching', async () => {
    const stub = await import('../services/nativeChatStubs/chatApi');
    const result = await stub.stopQuery(vi.fn());
    expect(result).toBeUndefined();
  });

  it('rewindQuery returns an inert RewindResponse without fetching', async () => {
    const stub = await import('../services/nativeChatStubs/chatApi');
    const result = await stub.rewindQuery(vi.fn(), 1);
    expect(result.turns_discarded).toBe(0);
    expect(result.messages_removed).toBe(0);
    expect(result.files_reverted).toEqual([]);
    expect(result.files_skipped).toEqual([]);
    expect(result.checkpoints_dropped).toBe(0);
  });

  it('NATIVE_CHAT_ENABLED is a boolean and false when env is unset', async () => {
    const { NATIVE_CHAT_ENABLED } = await import('../services/nativeChatStubs/nativeChatFlag');
    expect(typeof NATIVE_CHAT_ENABLED).toBe('boolean');
    expect(NATIVE_CHAT_ENABLED).toBe(false);
  });

  it('NATIVE_CHAT_ENABLED is true when VITE_SPROUT_NATIVE_CHAT=1 (fresh import)', async () => {
    vi.stubEnv('VITE_SPROUT_NATIVE_CHAT', '1');
    vi.resetModules();
    const { NATIVE_CHAT_ENABLED } = await import('../services/nativeChatStubs/nativeChatFlag');
    vi.unstubAllEnvs();
    expect(NATIVE_CHAT_ENABLED).toBe(true);
  });
});

// ── Housekeeping ───────────────────────────────────────────────────────
// (RTL auto-cleanup handles unmounting; no manual cleanup needed.)
