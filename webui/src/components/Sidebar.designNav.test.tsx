// @ts-nocheck
/**
 * SP-140-3 item 3.3 — Sidebar Design nav affordance.
 *
 * The design nav item is visible ONLY when the workspace has a `design/`
 * directory, and clicking it routes to the design view. Follows the
 * Sidebar.costsNav.test.tsx lightweight-mock pattern.
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';

// ---------------------------------------------------------------------------
// Mocks — MUST be set up BEFORE importing Sidebar
// ---------------------------------------------------------------------------

let mockDesignPresent = false;

vi.mock('./design/useDesignPresence', () => ({
  __esModule: true,
  useDesignPresence: () => ({ present: mockDesignPresent, loading: false }),
}));

vi.mock('../contexts/PlatformNavContext', () => ({
  __esModule: true,
  PlatformNavProvider: ({ children }) => children,
  usePlatformNav: () => ({ platformNavItems: [] }),
}));

vi.mock('../contexts/ThemeContext', () => ({
  __esModule: true,
  useTheme: () => ({
    themePack: { id: 'default' },
    availableThemePacks: [],
    setThemePack: vi.fn(),
    importTheme: vi.fn(() => ({ success: true })),
    removeTheme: vi.fn(),
  }),
}));

vi.mock('../contexts/HotkeyContext', () => ({
  __esModule: true,
  useHotkeys: () => ({ applyPreset: vi.fn() }),
}));

vi.mock('../contexts/EditorManagerContext', () => ({
  __esModule: true,
  useEditorManager: () => ({
    paneSizes: {},
    updatePaneSize: vi.fn(),
    isAutoSaveEnabled: false,
    whitespaceRenderingMode: 'boundary',
    isFormatOnSaveEnabled: false,
  }),
}));

vi.mock('../contexts/NotificationContext', () => ({
  __esModule: true,
  NotificationProvider: ({ children }) => children,
  useNotifications: () => ({ addNotification: () => {} }),
  useLog: () => vi.fn(),
}));

vi.mock('../config/mode', () => ({
  isCloud: false,
  supportsSettings: true,
  supportsLocalTerminal: false,
  supportsGit: true,
  supportsWorkspaceSwitching: false,
}));

vi.mock('./SettingsPanel', () => ({ default: () => createElement('div', { className: 'mock-settings' }) }));
vi.mock('./FileTree', () => ({ default: () => createElement('div', { className: 'mock-filetree' }) }));
vi.mock('./SearchView', () => ({ default: () => createElement('div', { className: 'mock-search' }) }));
vi.mock('./GitSidebarPanel', () => ({ default: () => createElement('div', { className: 'mock-git' }) }));
vi.mock('./AgentChangesPanel', () => ({ default: () => createElement('div', { className: 'mock-changes' }) }));
vi.mock('./SproutLogo', () => ({ default: () => createElement('svg', { className: 'mock-logo' }) }));
vi.mock('./LocationSwitcher', () => ({ default: () => createElement('div', { className: 'mock-location' }) }));
vi.mock('./ResizeHandle', () => ({ default: () => createElement('div', { className: 'mock-resize-handle' }) }));

vi.mock('./SidebarFilesSection', () => ({
  default: vi.fn(() => createElement('div', { className: 'mock-files-section' })),
}));
vi.mock('./SidebarGitSection', () => ({
  default: vi.fn(() => createElement('div', { className: 'mock-git-section' })),
}));
vi.mock('./SidebarLogsPane', () => ({
  default: vi.fn(() => createElement('div', { className: 'mock-logs' })),
}));
vi.mock('./SidebarSettingsSection', () => ({
  default: vi.fn(() => createElement('div', { className: 'mock-settings-section' })),
}));
vi.mock('./AutomationsPanel', () => ({
  default: vi.fn(() => createElement('div', { className: 'mock-automations' })),
}));

vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: vi.fn(() => ({
      getProviders: vi.fn().mockResolvedValue({
        providers: [{ id: 'openai', name: 'OpenAI', models: ['gpt-4'] }],
        current_provider: 'openai',
        current_model: 'gpt-4',
      }),
      getSettings: vi.fn().mockResolvedValue({}),
    })),
  },
}));

vi.mock('../hooks/useSidebarEventHandlers', () => ({ useSidebarEventHandlers: vi.fn() }));
vi.mock('../hooks/useSidebarModel', () => ({
  useSidebarModel: () => ({
    selectedProvider: 'openai',
    selectedModelState: 'gpt-4',
    selectedPersonaState: '',
    personas: [],
    isLoadingPersonas: false,
    providers: [{ id: 'openai', name: 'OpenAI', models: ['gpt-4'] }],
    isLoadingProviders: false,
    settings: null,
    settingsFocusTarget: null,
    finalSelectedModel: 'gpt-4',
    availableModelsState: ['gpt-4'],
    finalAvailableModels: ['gpt-4'],
    setSelectedProvider: vi.fn(),
    setSelectedModelState: vi.fn(),
    setSelectedPersonaState: vi.fn(),
    setSettings: vi.fn(),
    setSettingsFocusTarget: vi.fn(),
  }),
}));

vi.mock('../utils/log', () => ({ useLog: () => vi.fn(), debugLog: vi.fn() }));

// ---------------------------------------------------------------------------
// Import AFTER mocks are set up
// ---------------------------------------------------------------------------

import Sidebar from './Sidebar';

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  mockDesignPresent = false;
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

const minimalProps = {
  isConnected: true,
  isOpen: true,
  isMobile: false,
  provider: 'openai',
  model: 'gpt-4',
  selectedModel: 'gpt-4',
  currentView: 'chat',
  onViewChange: vi.fn(),
};

describe('Sidebar Design nav affordance', () => {
  it('hides the design nav item when the workspace has no design/ directory', () => {
    mockDesignPresent = false;
    act(() => {
      root.render(createElement(Sidebar, minimalProps));
    });
    expect(container.querySelector('[data-testid="sidebar-design-button"]')).toBeNull();
  });

  it('shows the design nav item when design/ exists', () => {
    mockDesignPresent = true;
    act(() => {
      root.render(createElement(Sidebar, minimalProps));
    });
    const btn = container.querySelector('[data-testid="sidebar-design-button"]');
    expect(btn).not.toBeNull();
    expect(btn.getAttribute('aria-label')).toBe('Design');
    expect(btn.getAttribute('role')).toBe('tab');
    expect(btn.getAttribute('aria-selected')).toBe('false');
  });

  it('clicking the design nav item calls onViewChange("design")', () => {
    mockDesignPresent = true;
    const onViewChange = vi.fn();
    act(() => {
      root.render(createElement(Sidebar, { ...minimalProps, onViewChange }));
    });
    const btn = container.querySelector('[data-testid="sidebar-design-button"]');
    act(() => {
      btn.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onViewChange).toHaveBeenCalledWith('design');
  });

  it('marks the design nav item active when currentView is "design"', () => {
    mockDesignPresent = true;
    act(() => {
      root.render(createElement(Sidebar, { ...minimalProps, currentView: 'design' }));
    });
    const btn = container.querySelector('[data-testid="sidebar-design-button"]');
    expect(btn.classList.contains('active')).toBe(true);
    expect(btn.getAttribute('aria-selected')).toBe('true');
  });
});
