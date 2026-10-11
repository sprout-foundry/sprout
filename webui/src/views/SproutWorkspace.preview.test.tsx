/**
 * SproutWorkspace — the arranged preview panel is safe by default.
 *
 * The real `PreviewPanel` (and its status hook) is mounted, so these tests
 * observe the requests it makes through the workspace transport:
 *
 * - The default arrangement (which places `previewPanel` in the overlay) on a
 *   host without `capabilities.preview` mounts no panel, registers no
 *   `toggle_preview` command, and never requests `/api/preview/*`.
 * - With the capability the panel starts closed and requests nothing until
 *   `toggle_preview` opens it; then it polls the status.
 */

import { act, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../services/localEventsProvider', () => ({
  LocalEventsProvider: class {
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

vi.mock('../components/ChatView', () => ({ default: () => <div data-testid="preview-test-chat" /> }));
vi.mock('../components/AgentChangesPanel', () => ({ default: () => <div /> }));
vi.mock('./hostAgentBackend', () => ({ wireHostAgentBackend: vi.fn(), resetHostAgentBackend: vi.fn() }));
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

import type { SproutHost } from '../host';
import { setActiveHost } from '../host/accessor';
import { headlessHost } from '../host/HostProvider';
import { __resetCommandHandlersForTests, hasCommandHandler } from '../services/commandHandlers';
import { SproutWorkspace } from './SproutWorkspace';
import type { SproutProject } from './SproutWorkspace';

const PROJECT: SproutProject = { id: 'acme/widgets', root: '/work/widgets' };
const BASE_HOST: SproutHost = {
  ...headlessHost(),
  transport: { apiBaseURL: 'https://host.test', wsURL: '', authMode: 'none' },
};
const PREVIEW_HOST: SproutHost = { ...BASE_HOST, capabilities: { ...BASE_HOST.capabilities, preview: true } };

let fetchMock: ReturnType<typeof vi.fn>;

function previewRequests(): string[] {
  return fetchMock.mock.calls
    .map(([input]) => (typeof input === 'string' ? input : String((input as Request).url ?? input)))
    .filter((url) => url.includes('/api/preview'));
}

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  localStorage.clear();
  __resetCommandHandlersForTests();
  setActiveHost(headlessHost());
  fetchMock = vi.fn(async () => ({
    ok: true,
    status: 200,
    headers: new Headers(),
    json: async () => ({ status: 'stopped' }),
    text: async () => '',
  }));
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('SproutWorkspace preview panel safety', () => {
  it('mounts no panel and never requests /api/preview on the default arrangement without the capability', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={BASE_HOST} layout={{}} />);
    await screen.findByTestId('preview-test-chat');
    expect(hasCommandHandler('toggle_preview')).toBe(false);

    // A toggle request has nothing to open.
    act(() => {
      window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'toggle_preview' } }));
    });
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });
    expect(screen.queryByTestId('preview-panel')).not.toBeInTheDocument();
    expect(previewRequests()).toEqual([]);
  });

  it('starts closed with the capability and polls only once toggle_preview opens it', async () => {
    render(<SproutWorkspace project={PROJECT} space="code" host={PREVIEW_HOST} layout={{}} />);
    await screen.findByTestId('preview-test-chat');
    await waitFor(() => expect(hasCommandHandler('toggle_preview')).toBe(true));
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });
    expect(screen.queryByTestId('preview-panel')).not.toBeInTheDocument();
    expect(previewRequests()).toEqual([]);

    act(() => {
      window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'toggle_preview' } }));
    });
    expect(screen.getByTestId('preview-panel')).toBeInTheDocument();
    await waitFor(() => expect(previewRequests().some((url) => url.endsWith('/api/preview/status'))).toBe(true));
  });
});
