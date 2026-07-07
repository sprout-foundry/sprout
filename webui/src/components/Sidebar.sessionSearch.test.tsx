// @ts-nocheck
/**
 * Focused Vitest tests for the session-search functionality in Sidebar.tsx.
 *
 * Lives in a separate file to avoid the OOM in Sidebar.test.tsx (heavy module
 * resolution cascade under Node 22 + Jest 27). Uses aggressive mocking to pull
 * in only what is needed for the search-input → API-call → result-dropdown flow.
 */

import { fireEvent, screen, waitFor } from '@testing-library/react';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';

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

/* --- apiAdapter (used by config/mode & clientSession) --- */
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

describe('Sidebar session-search', () => {
  let mockApiService: any;
  let searchSessionsMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    searchSessionsMock = vi.fn();
    mockApiService = {
      searchSessions: searchSessionsMock,
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

  /* ---- 1. Input triggers debounced API call ---- */
  it('calls searchSessions after debouncing input text (fake timers)', async () => {
    vi.useFakeTimers();

    searchSessionsMock.mockResolvedValue({ query: 'hello', total: 1, results: [] });

    renderSidebar();
    const input = screen.getByTestId('sidebar-session-search-input');
    expect(input).toBeDefined();

    // Type a query
    act(() => {
      fireEvent.change(input, { target: { value: 'hello' } });
    });

    // Advance debounce window, then flush microtasks so .then() callbacks settle
    await act(async () => {
      vi.advanceTimersByTime(350);
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(searchSessionsMock).toHaveBeenCalledWith('hello', { limit: 20 });

    vi.useRealTimers();
  });

  /* ---- 2. Empty query short-circuits ---- */
  it('does NOT call searchSessions when the query is empty/whitespace', async () => {
    vi.useFakeTimers();

    renderSidebar();
    const input = screen.getByTestId('sidebar-session-search-input');

    // Type whitespace then clear
    act(() => {
      fireEvent.change(input, { target: { value: '   ' } });
    });

    act(() => {
      vi.advanceTimersByTime(350);
    });

    expect(searchSessionsMock).not.toHaveBeenCalled();

    vi.useRealTimers();
  });

  /* ---- 3. Clearing input resets without calling API ---- */
  it('clearing the input cancels the pending timer and resets state', async () => {
    vi.useFakeTimers();

    renderSidebar();
    const input = screen.getByTestId('sidebar-session-search-input');

    // Start typing
    act(() => {
      fireEvent.change(input, { target: { value: 'foo' } });
    });

    // Immediately clear — should NOT call API
    act(() => {
      fireEvent.change(input, { target: { value: '' } });
    });

    act(() => {
      vi.advanceTimersByTime(350);
    });

    expect(searchSessionsMock).not.toHaveBeenCalled();

    vi.useRealTimers();
  });

  /* ---- 4. Results render with data-session-id ---- */
  it('renders search results with data-session-id on each item', async () => {
    searchSessionsMock.mockResolvedValue({
      query: 'test',
      total: 2,
      results: [
        {
          session_id: 'sess-1',
          name: 'Session One',
          working_directory: '/a',
          last_updated: '2025-01-01T00:00:00Z',
          total_cost: 0.1,
          excerpt: 'first [result]',
          match_score: 2,
        },
        {
          session_id: 'sess-2',
          name: 'Session Two',
          working_directory: '/b',
          last_updated: '2025-01-02T00:00:00Z',
          total_cost: 0.2,
          excerpt: 'second [result]',
          match_score: 1,
        },
      ],
    });

    renderSidebar();
    const input = screen.getByTestId('sidebar-session-search-input');

    // Focus to show dropdown, then type
    act(() => {
      fireEvent.focus(input);
      fireEvent.change(input, { target: { value: 'test' } });
    });

    // Wait for the debounce + async call to resolve
    await waitFor(
      () => {
        const results = container.querySelectorAll('[data-testid="chat-item"]');
        expect(results.length).toBe(2);
      },
      { timeout: 2000 },
    );

    const results = container.querySelectorAll('[data-testid="chat-item"]');
    expect(results[0].getAttribute('data-session-id')).toBe('sess-1');
    expect(results[1].getAttribute('data-session-id')).toBe('sess-2');
  });

  /* ---- 5. Clicking a result calls onSessionSearchRestore ---- */
  it('clicking a result calls onSessionSearchRestore with the session_id', async () => {
    const onSessionSearchRestore = vi.fn();

    searchSessionsMock.mockResolvedValue({
      query: 'click',
      total: 1,
      results: [
        {
          session_id: 'sess-click',
          name: 'Click Me',
          working_directory: '/c',
          last_updated: '2025-01-01T00:00:00Z',
          total_cost: 0.05,
          excerpt: 'click [test]',
          match_score: 1,
        },
      ],
    });

    renderSidebar({ onSessionSearchRestore });
    const input = screen.getByTestId('sidebar-session-search-input');

    act(() => {
      fireEvent.focus(input);
      fireEvent.change(input, { target: { value: 'click' } });
    });

    await waitFor(
      () => {
        const result = container.querySelector('[data-testid="chat-item"]');
        expect(result).not.toBeNull();
      },
      { timeout: 2000 },
    );

    const resultBtn = container.querySelector('[data-session-id="sess-click"]');
    act(() => {
      resultBtn!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(onSessionSearchRestore).toHaveBeenCalledWith('sess-click');
  });

  /* ---- 6. API error renders error state ---- */
  it('renders sidebar-session-search-error when the API throws', async () => {
    searchSessionsMock.mockRejectedValue(new Error('network failure'));

    renderSidebar();
    const input = screen.getByTestId('sidebar-session-search-input');

    act(() => {
      fireEvent.focus(input);
      fireEvent.change(input, { target: { value: 'err' } });
    });

    await waitFor(
      () => {
        const error = container.querySelector('[data-testid="sidebar-session-search-error"]');
        expect(error).not.toBeNull();
      },
      { timeout: 2000 },
    );

    const errorEl = container.querySelector('[data-testid="sidebar-session-search-error"]');
    expect(errorEl!.textContent).toContain('network failure');
  });

  /* ---- 7. Loading state renders spinner ---- */
  it('shows loading state while searchSessions is pending', async () => {
    vi.useFakeTimers();
    // Delay the resolution so loading state is visible
    searchSessionsMock.mockImplementation(
      () => new Promise((resolve) => setTimeout(() => resolve({ query: 'loading', total: 0, results: [] }), 500)),
    );

    renderSidebar();
    const input = screen.getByTestId('sidebar-session-search-input');

    act(() => {
      fireEvent.focus(input);
      fireEvent.change(input, { target: { value: 'loading' } });
    });

    // Advance debounce (300ms) so the API call fires, then flush microtasks
    await act(async () => {
      vi.advanceTimersByTime(350);
      await Promise.resolve();
      await Promise.resolve();
    });

    const loading = container.querySelector('[data-testid="sidebar-session-search-loading"]');
    expect(loading).not.toBeNull();

    vi.useRealTimers();
  });

  /* ---- 8. No results renders the no-results message ---- */
  it('shows no-results message when API returns empty results', async () => {
    searchSessionsMock.mockResolvedValue({ query: 'nothing', total: 0, results: [] });

    renderSidebar();
    const input = screen.getByTestId('sidebar-session-search-input');

    act(() => {
      fireEvent.focus(input);
      fireEvent.change(input, { target: { value: 'nothing' } });
    });

    await waitFor(
      () => {
        const noResults = container.querySelector('[data-testid="chat-sessions-empty"]');
        expect(noResults).not.toBeNull();
      },
      { timeout: 2000 },
    );
  });
});
