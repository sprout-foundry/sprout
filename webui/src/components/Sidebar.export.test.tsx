// @ts-nocheck
/**
 * Focused Vitest tests for the export-all functionality in Sidebar.tsx.
 *
 * Mirrors Sidebar.sessionSearch.test.tsx exactly (heavy mocking, createRoot +
 * act pattern, vi.mock for all dependencies).
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { fireEvent, screen, waitFor } from '@testing-library/react';

// ---------------------------------------------------------------------------
// Mocks — MUST be set up BEFORE importing Sidebar or any of its deps
// ---------------------------------------------------------------------------

/* --- config/mode --- */
vi.mock('../config/mode', () => ({
  isCloud: false,
  supportsSettings: true,
  supportsLocalTerminal: false,
  supportsSSH: true,
  supportsInstances: false,
  mode: 'local',
}));

/* --- contexts --- */
vi.mock('../contexts/ThemeContext', () => ({
  useTheme: () => ({
    themePack: { id: 'default' },
    availableThemePacks: [],
    setThemePack: vi.fn(),
    importTheme: vi.fn(() => ({ success: true })),
    removeTheme: vi.fn(),
  }),
}));

vi.mock('../contexts/HotkeyContext', () => ({
  useHotkeys: () => ({ applyPreset: vi.fn() }),
}));

vi.mock('../contexts/EditorManagerContext', () => ({
  useEditorManager: () => ({
    paneSizes: {},
    updatePaneSize: vi.fn(),
    isAutoSaveEnabled: false,
    setAutoSaveEnabled: vi.fn(),
    whitespaceRenderingMode: 'boundary',
    setWhitespaceRenderingMode: vi.fn(),
    isFormatOnSaveEnabled: false,
    setFormatOnSaveEnabled: vi.fn(),
  }),
}));

vi.mock('../contexts/PlatformNavContext', () => ({
  usePlatformNav: () => ({ platformNavItems: [] }),
}));

vi.mock('../contexts/NotificationContext', () => ({
  NotificationProvider: ({ children }) => children,
  useNotifications: () => ({ addNotification: () => {} }),
  useLog: () => vi.fn(),
}));

vi.mock('../contexts/ProviderCatalogContext', () => ({
  useProviderCatalog: () => ({
    providers: [{ id: 'openai', name: 'OpenAI', models: ['gpt-4o-mini'] }],
    isLoading: false,
    currentProvider: 'openai',
    currentModel: 'gpt-4o-mini',
    refresh: vi.fn(),
    getProviderName: (id) => id || '',
  }),
}));

/* --- hooks --- */
vi.mock('../hooks/useSidebarModel', () => ({
  useSidebarModel: () => ({
    selectedProvider: 'openai',
    selectedModelState: 'gpt-4o-mini',
    selectedPersonaState: 'orchestrator',
    personas: [{ id: 'orchestrator', name: 'Orchestrator', enabled: true }],
    isLoadingPersonas: false,
    providers: [{ id: 'openai', name: 'OpenAI', models: ['gpt-4o-mini'] }],
    isLoadingProviders: false,
    settings: null,
    settingsFocusTarget: null,
    finalSelectedModel: 'gpt-4o-mini',
    availableModelsState: ['gpt-4o-mini'],
    finalAvailableModels: ['gpt-4o-mini'],
    setSelectedProvider: vi.fn(),
    setSelectedModelState: vi.fn(),
    setSelectedPersonaState: vi.fn(),
    setSettings: vi.fn(),
    setSettingsFocusTarget: vi.fn(),
  }),
}));

vi.mock('../hooks/useSidebarEventHandlers', () => ({
  useSidebarEventHandlers: vi.fn(),
}));

/* --- utils/log --- */
vi.mock('../utils/log', () => ({
  useLog: () => vi.fn(),
  debugLog: vi.fn(),
}));

/* --- services/api (ApiService singleton) --- */
vi.mock('../services/api', () => ({
  ApiService: { getInstance: vi.fn() },
}));

/* --- heavy sub-components (render as null / minimal stubs) --- */
vi.mock('./SettingsPanel', () => ({ default: () => null }));
vi.mock('./FileTree', () => ({ default: () => null }));
vi.mock('./SearchView', () => ({ default: () => null }));
vi.mock('./GitSidebarPanel', () => ({ default: () => null }));
vi.mock('./AgentChangesPanel', () => ({ default: () => null }));
vi.mock('./SproutLogo', () => ({ default: () => null }));
vi.mock('./LocationSwitcher', () => ({ default: () => null }));
vi.mock('./ResizeHandle', () => ({ default: () => null }));
vi.mock('./SidebarFilesSection', () => ({
  default: vi.fn(),
  FileTreeHandle: {},
}));
vi.mock('./SidebarGitSection', () => ({ default: vi.fn(() => null) }));
vi.mock('./SidebarLogsPane', () => ({ default: vi.fn(() => null) }));
vi.mock('./SidebarSettingsSection', () => ({ default: vi.fn(() => null) }));
vi.mock('./AutomationsPanel', () => ({ default: vi.fn(() => null) }));

/* --- apiAdapter --- */
vi.mock('../services/apiAdapter', () => ({
  getAdapter: vi.fn(() => ({
    name: 'local',
    supportsSSH: true,
    supportsInstances: false,
    supportsLocalTerminal: true,
    supportsSettings: true,
  })),
  installAdapter: vi.fn(),
  hasAdapter: vi.fn(() => true),
  requiresBackendHealthCheck: vi.fn(() => false),
}));

/* --- lucide-react --- */
vi.mock('lucide-react', async (importOriginal) => {
  const actual = await importOriginal();
  const Stub = (props: any) => createElement('svg', { 'data-testid': 'icon', ...props });
  return {
    ...actual,
    ScrollText: Stub,
    FolderCog: Stub,
    Settings: Stub,
    Search: Stub,
    GitBranch: Stub,
    CreditCard: Stub,
    ListChecks: Stub,
    Users: Stub,
    LayoutDashboard: Stub,
    ExternalLink: Stub,
    Zap: Stub,
    X: Stub,
    Loader2: Stub,
    Download: Stub,
  };
});

// ---------------------------------------------------------------------------
// Import AFTER mocks
// ---------------------------------------------------------------------------

import { ApiService } from '../services/api';
import Sidebar from './Sidebar';

// ---------------------------------------------------------------------------
// Test setup
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  container.style.width = '300px';
  container.style.height = '600px';
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
  vi.restoreAllMocks();
  vi.clearAllMocks();
});

/** Minimal Sidebar props — enough to render without side-effects */
const minimalProps = {
  isConnected: true,
  isOpen: true,
  isMobile: false,
  provider: 'openai',
  model: 'gpt-4o-mini',
  selectedModel: 'gpt-4o-mini',
  currentView: 'chat',
  onViewChange: vi.fn(),
  onSectionChange: vi.fn(),
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('Sidebar export-all', () => {
  let mockApiService: any;
  let getSessionsMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    getSessionsMock = vi.fn();
    mockApiService = {
      searchSessions: vi.fn(),
      getSessions: getSessionsMock,
      getProviders: vi.fn().mockResolvedValue({
        providers: [{ id: 'openai', name: 'OpenAI', models: ['gpt-4o-mini'] }],
        current_provider: 'openai',
        current_model: 'gpt-4o-mini',
      }),
      getSettings: vi.fn().mockResolvedValue({}),
    };
    (ApiService.getInstance as vi.Mock).mockReturnValue(mockApiService);
  });

  /** Render the Sidebar inside act() for consistent state updates */
  const renderSidebar = (extraProps?: Record<string, unknown>) => {
    act(() => {
      root.render(createElement(Sidebar, { ...minimalProps, ...extraProps }));
    });
  };

  /* ---- 1. Export-all button exists ---- */
  it('renders the export-all button', () => {
    renderSidebar();
    const btn = screen.getByTestId('sidebar-export-all');
    expect(btn).toBeInTheDocument();
    expect(btn.tagName).toBe('BUTTON');
  });

  /* ---- 2. Clicking Export-all calls getSessions with scope 'current' ---- */
  it('clicking export-all calls getSessions with scope "current"', async () => {
    getSessionsMock.mockResolvedValue({
      message: 'ok',
      sessions: [],
      current_session_id: '',
    });

    renderSidebar();
    const btn = screen.getByTestId('sidebar-export-all');

    act(() => {
      fireEvent.click(btn);
    });

    await waitFor(() => {
      expect(getSessionsMock).toHaveBeenCalledWith('current');
    });
  });

  /* ---- 3. Filters to sessions with message_count > 0 ---- */
  it('only creates downloads for sessions with message_count > 0', async () => {
    vi.useFakeTimers();

    // Mock HEAD requests to succeed for the pre-check
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200 }));

    const capturedAnchors: HTMLAnchorElement[] = [];
    const originalAppendChild = document.body.appendChild.bind(document.body);
    document.body.appendChild = function (node: Node) {
      if (node instanceof HTMLAnchorElement) {
        capturedAnchors.push(node);
      }
      return originalAppendChild(node);
    };

    try {
      getSessionsMock.mockResolvedValue({
        message: 'ok',
        sessions: [
          { session_id: 's1', name: 'Active 1', working_directory: '/a', last_updated: '2025-01-01T00:00:00Z', message_count: 5, total_tokens: 100 },
          { session_id: 's2', name: 'Empty', working_directory: '/b', last_updated: '2025-01-01T00:00:00Z', message_count: 0, total_tokens: 0 },
          { session_id: 's3', name: 'Active 2', working_directory: '/c', last_updated: '2025-01-01T00:00:00Z', message_count: 10, total_tokens: 200 },
        ],
        current_session_id: 's1',
      });

      renderSidebar();
      const btn = screen.getByTestId('sidebar-export-all');

      act(() => {
        fireEvent.click(btn);
      });

      // Flush the initial microtask (getSessions resolve)
      await act(async () => {
        await Promise.resolve();
        await Promise.resolve();
      });

      // First anchor (s1) should exist immediately
      expect(capturedAnchors.length).toBe(1);

      // Advance past the 300ms delay to trigger the second download (s3)
      await act(async () => {
        vi.advanceTimersByTime(350);
        await Promise.resolve();
        await Promise.resolve();
      });

      // Should now have 2 — s1 and s3, not s2 (message_count === 0)
      expect(capturedAnchors.length).toBe(2);
    } finally {
      document.body.appendChild = originalAppendChild;
      vi.unstubAllGlobals();
      vi.useRealTimers();
    }
  });

  /* ---- 4. Downloads use the correct URL pattern ---- */
  it('download URLs follow the correct pattern', async () => {
    vi.useFakeTimers();

    // Mock HEAD requests to succeed for the pre-check
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200 }));

    const capturedAnchors: HTMLAnchorElement[] = [];
    const originalAppendChild = document.body.appendChild.bind(document.body);
    document.body.appendChild = function (node: Node) {
      if (node instanceof HTMLAnchorElement) {
        capturedAnchors.push(node);
      }
      return originalAppendChild(node);
    };

    try {
      getSessionsMock.mockResolvedValue({
        message: 'ok',
        sessions: [
          { session_id: 'sess-abc', name: 'Test', working_directory: '/d', last_updated: '2025-01-01T00:00:00Z', message_count: 3, total_tokens: 50 },
        ],
        current_session_id: 'sess-abc',
      });

      renderSidebar();
      const btn = screen.getByTestId('sidebar-export-all');

      act(() => {
        fireEvent.click(btn);
      });

      // Flush the initial microtask (getSessions resolve)
      await act(async () => {
        await Promise.resolve();
        await Promise.resolve();
      });

      expect(capturedAnchors.length).toBe(1);
      expect(capturedAnchors[0].href).toContain(
        '/api/sessions/sess-abc/export?format=markdown&include_tool_calls=false&include_cost=true',
      );
    } finally {
      document.body.appendChild = originalAppendChild;
      vi.unstubAllGlobals();
      vi.useRealTimers();
    }
  });

  /* ---- 5. Loading state shows spinner ---- */
  it('shows loading state while getSessions is pending', async () => {
    vi.useFakeTimers();

    // Never-resolving promise
    getSessionsMock.mockImplementation(() => new Promise(() => {}));

    renderSidebar();
    const btn = screen.getByTestId('sidebar-export-all');

    act(() => {
      fireEvent.click(btn);
    });

    expect(btn.textContent).toContain('Exporting...');
    expect(btn).toBeDisabled();

    vi.useRealTimers();
  });

  /* ---- 6. Error state shows error ---- */
  it('shows error when getSessions rejects', async () => {
    getSessionsMock.mockRejectedValue(new Error('connection refused'));

    renderSidebar();
    const btn = screen.getByTestId('sidebar-export-all');

    act(() => {
      fireEvent.click(btn);
    });

    await waitFor(() => {
      const errorEl = container.querySelector('.sidebar-export-all-error');
      expect(errorEl).not.toBeNull();
    }, { timeout: 2000 });

    const errorEl = container.querySelector('.sidebar-export-all-error');
    expect(errorEl!.textContent).toContain('connection refused');
  });

  /* ---- 7. Re-entry guard prevents double-click ---- */
  it('prevents re-entry when export-all is already in progress', async () => {
    vi.useFakeTimers();

    // Delayed promise that doesn't resolve immediately
    let resolvePromise: () => void;
    getSessionsMock.mockImplementation(
      () => new Promise((resolve) => { resolvePromise = () => resolve({
        message: 'ok',
        sessions: [
          { session_id: 's1', name: 'Test', working_directory: '/a', last_updated: '2025-01-01T00:00:00Z', message_count: 1, total_tokens: 10 },
        ],
        current_session_id: 's1',
      }); }),
    );

    renderSidebar();
    const btn = screen.getByTestId('sidebar-export-all');

    // First click
    act(() => {
      fireEvent.click(btn);
    });

    // Second click immediately (before first resolves)
    act(() => {
      fireEvent.click(btn);
    });

    // getSessions should only have been called once
    expect(getSessionsMock).toHaveBeenCalledTimes(1);

    // Resolve the promise so cleanup can proceed
    act(() => {
      resolvePromise!();
    });
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    vi.useRealTimers();
  });
});
