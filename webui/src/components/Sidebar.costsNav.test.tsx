// @ts-nocheck
/**
 * Integration test: verifies that the Costs button appears in the Sidebar
 * icon rail and triggers onViewChange('costs') when clicked.
 *
 * Focused test using lightweight mocks (same pattern as Sidebar.platformNav.test.tsx).
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';

// ---------------------------------------------------------------------------
// Mocks — MUST be set up BEFORE importing Sidebar
// ---------------------------------------------------------------------------

// Mock PlatformNavContext — no cloud nav items for this test
vi.mock('../contexts/PlatformNavContext', () => ({
  __esModule: true,
  PlatformNavProvider: ({ children }) => children,
  usePlatformNav: () => ({
    platformNavItems: [],
  }),
}));

// Mock ThemeContext
vi.mock('../contexts/ThemeContext', () => {
  return {
    __esModule: true,
    useTheme: () => ({
      themePack: { id: 'default' },
      availableThemePacks: [],
      setThemePack: vi.fn(),
      importTheme: vi.fn(() => ({ success: true })),
      removeTheme: vi.fn(),
    }),
  };
});

// Mock HotkeyContext
vi.mock('../contexts/HotkeyContext', () => {
  return {
    __esModule: true,
    useHotkeys: () => ({
      applyPreset: vi.fn(),
    }),
  };
});

// Mock EditorManagerContext
vi.mock('../contexts/EditorManagerContext', () => {
  return {
    __esModule: true,
    useEditorManager: () => ({
      paneSizes: {},
      updatePaneSize: vi.fn(),
      isAutoSaveEnabled: false,
      whitespaceRenderingMode: 'boundary',
      isFormatOnSaveEnabled: false,
    }),
  };
});

// Mock NotificationContext
vi.mock('../contexts/NotificationContext', () => {
  return {
    __esModule: true,
    NotificationProvider: ({ children }) => children,
    useNotifications: () => ({ addNotification: () => {} }),
    useLog: () => vi.fn(),
  };
});

// Mock config/mode
vi.mock('../config/mode', () => {
  return {
    isCloud: false,
    supportsSettings: true,
    supportsLocalTerminal: false,
  };
});

// Mock heavy sub-components
vi.mock('./SettingsPanel', () => ({ default: () => createElement('div', { className: 'mock-settings' }) }));
vi.mock('./FileTree', () => ({ default: () => createElement('div', { className: 'mock-filetree' }) }));
vi.mock('./SearchView', () => ({ default: () => createElement('div', { className: 'mock-search' }) }));
vi.mock('./GitSidebarPanel', () => ({ default: () => createElement('div', { className: 'mock-git' }) }));
vi.mock('./AgentChangesPanel', () => ({ default: () => createElement('div', { className: 'mock-changes' }) }));
vi.mock('./SproutLogo', () => ({ default: () => createElement('svg', { className: 'mock-logo' }) }));
vi.mock('./LocationSwitcher', () => ({ default: () => createElement('div', { className: 'mock-location-switcher' }) }));
vi.mock('./ResizeHandle', () => ({ default: () => createElement('div', { className: 'mock-resize-handle' }) }));

// Mock sidebar section components
vi.mock('./SidebarFilesSection', () => {
  return {
    default: vi.fn(({ onFileClick }) => createElement('div', { className: 'mock-files-section' })),
  };
});
vi.mock('./SidebarGitSection', () => {
  return {
    default: vi.fn(({ onSectionChange }) => createElement('div', { className: 'mock-git-section' })),
  };
});
vi.mock('./SidebarLogsPane', () => {
  return {
    default: vi.fn(() => createElement('div', { className: 'mock-logs' })),
  };
});
vi.mock('./SidebarSettingsSection', () => {
  return {
    default: vi.fn(() => createElement('div', { className: 'mock-settings-section' })),
  };
});

// Mock AutomationsPanel
vi.mock('./AutomationsPanel', () => {
  return {
    default: vi.fn(() => createElement('div', { className: 'mock-automations' })),
  };
});

// Mock ApiService
vi.mock('../services/api', () => {
  return {
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
  };
});

// Mock hooks
vi.mock('../hooks/useSidebarEventHandlers', () => {
  return {
    useSidebarEventHandlers: vi.fn(),
  };
});
vi.mock('../hooks/useSidebarModel', () => {
  return {
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
  };
});

// Mock utils/log
vi.mock('../utils/log', () => {
  return {
    useLog: () => vi.fn(),
    debugLog: vi.fn(),
  };
});

// ---------------------------------------------------------------------------
// Import AFTER mocks are set up
// ---------------------------------------------------------------------------

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
});

/** Minimal Sidebar props for testing the costs nav button */
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

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('Sidebar Costs Navigation', () => {
  it('renders the costs button with data-testid="sidebar-costs-button"', () => {
    act(() => {
      root.render(createElement(Sidebar, minimalProps));
    });

    const costsBtn = container.querySelector('[data-testid="sidebar-costs-button"]');
    expect(costsBtn).not.toBeNull();
  });

  it('renders the costs button with aria-label="Costs"', () => {
    act(() => {
      root.render(createElement(Sidebar, minimalProps));
    });

    const costsBtn = container.querySelector('[data-testid="sidebar-costs-button"]');
    expect(costsBtn).not.toBeNull();
    expect(costsBtn!.getAttribute('aria-label')).toBe('Costs');
  });

  it('renders the costs button with title="Costs"', () => {
    act(() => {
      root.render(createElement(Sidebar, minimalProps));
    });

    const costsBtn = container.querySelector('[data-testid="sidebar-costs-button"]');
    expect(costsBtn).not.toBeNull();
    expect(costsBtn!.getAttribute('title')).toBe('Costs');
  });

  it('clicking the costs button calls onViewChange with "costs"', () => {
    const onViewChange = vi.fn();

    act(() => {
      root.render(createElement(Sidebar, { ...minimalProps, onViewChange }));
    });

    const costsBtn = container.querySelector('[data-testid="sidebar-costs-button"]');
    expect(costsBtn).not.toBeNull();

    act(() => {
      costsBtn!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(onViewChange).toHaveBeenCalledWith('costs');
  });

  it('highlights the costs button as active when currentView is "costs"', () => {
    act(() => {
      root.render(createElement(Sidebar, { ...minimalProps, currentView: 'costs' }));
    });

    const costsBtn = container.querySelector('[data-testid="sidebar-costs-button"]');
    expect(costsBtn).not.toBeNull();
    expect(costsBtn!.classList.contains('active')).toBe(true);
    expect(costsBtn!.getAttribute('aria-selected')).toBe('true');
  });

  it('does not highlight the costs button when currentView is "chat"', () => {
    act(() => {
      root.render(createElement(Sidebar, { ...minimalProps, currentView: 'chat' }));
    });

    const costsBtn = container.querySelector('[data-testid="sidebar-costs-button"]');
    expect(costsBtn).not.toBeNull();
    expect(costsBtn!.classList.contains('active')).toBe(false);
    expect(costsBtn!.getAttribute('aria-selected')).toBe('false');
  });

  it('costs button has role="tab"', () => {
    act(() => {
      root.render(createElement(Sidebar, minimalProps));
    });

    const costsBtn = container.querySelector('[data-testid="sidebar-costs-button"]');
    expect(costsBtn).not.toBeNull();
    expect(costsBtn!.getAttribute('role')).toBe('tab');
  });

  it('costs button has rail-icon CSS class', () => {
    act(() => {
      root.render(createElement(Sidebar, minimalProps));
    });

    const costsBtn = container.querySelector('[data-testid="sidebar-costs-button"]');
    expect(costsBtn).not.toBeNull();
    expect(costsBtn!.classList.contains('rail-icon')).toBe(true);
  });
});
