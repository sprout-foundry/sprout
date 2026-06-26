// @ts-nocheck
/**
 * Integration test: verifies that PlatformNav items (tasks, billing, team)
 * appear in the Sidebar icon rail when the cloud adapter is installed.
 *
 * Tests the full chain:
 *   CloudAdapter.install(platformNavItems) → PlatformNavProvider → Sidebar icon rail DOM
 *
 * This is a focused test that mocks heavy dependencies (EditorManager, API, etc.)
 * to avoid the OOM issues seen in the full Sidebar.test.tsx.
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import type { PlatformNavItem } from '../services/apiAdapter';

// ---------------------------------------------------------------------------
// Mocks — MUST be set up BEFORE importing Sidebar or PlatformNavContext
// ---------------------------------------------------------------------------

// Cloud nav items matching bootstrapAdapter.ts
const CLOUD_NAV_ITEMS: PlatformNavItem[] = [
  { id: 'tasks', label: 'Tasks', href: '/tasks', icon: 'list-checks', order: 1 },
  { id: 'billing', label: 'Billing', href: '/billing', icon: 'credit-card', order: 2 },
  { id: 'team', label: 'Team', href: '/team', icon: 'users', order: 3 },
];

// Mock apiAdapter — provides the cloud adapter with nav items
vi.mock('../services/apiAdapter', () => ({
  getAdapter: vi.fn(() => ({
    name: 'foundry-cloud',
    platformNavItems: CLOUD_NAV_ITEMS,
  })),
  installAdapter: vi.fn(),
  hasAdapter: vi.fn(() => true),
  requiresBackendHealthCheck: vi.fn(() => true),
}));

// Mock PlatformNavContext — uses the mocked apiAdapter above
vi.mock('../contexts/PlatformNavContext', () => ({
  __esModule: true,
  PlatformNavProvider: ({ children }) => children,
  usePlatformNav: () => ({
    platformNavItems: CLOUD_NAV_ITEMS,
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

/** Minimal Sidebar props for testing platform nav items */
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

describe('Sidebar PlatformNav Integration', () => {
  describe('cloud adapter with platform nav items', () => {
    it('renders a divider before platform nav items in the icon rail', () => {
      act(() => {
        root.render(createElement(Sidebar, minimalProps));
      });

      const divider = container.querySelector('.sidebar-icon-rail-divider');
      expect(divider).not.toBeNull();
    });

    it('renders a <nav aria-label="Platform navigation"> container', () => {
      act(() => {
        root.render(createElement(Sidebar, minimalProps));
      });

      const nav = container.querySelector('nav[aria-label="Platform navigation"]');
      expect(nav).not.toBeNull();
    });

    it('renders 3 platform nav item buttons (tasks, billing, team)', () => {
      act(() => {
        root.render(createElement(Sidebar, minimalProps));
      });

      const nav = container.querySelector('nav[aria-label="Platform navigation"]');
      expect(nav).not.toBeNull();

      const buttons = nav!.querySelectorAll('button[role="tab"]');
      expect(buttons.length).toBe(3);
    });

    it('renders Tasks button with correct aria-label and title', () => {
      act(() => {
        root.render(createElement(Sidebar, minimalProps));
      });

      const nav = container.querySelector('nav[aria-label="Platform navigation"]');
      expect(nav).not.toBeNull();

      const tasksBtn = nav!.querySelector('button[aria-label="Tasks"]');
      expect(tasksBtn).not.toBeNull();
      expect(tasksBtn!.getAttribute('title')).toBe('Tasks');
    });

    it('renders Billing button with correct aria-label and title', () => {
      act(() => {
        root.render(createElement(Sidebar, minimalProps));
      });

      const nav = container.querySelector('nav[aria-label="Platform navigation"]');
      expect(nav).not.toBeNull();

      const billingBtn = nav!.querySelector('button[aria-label="Billing"]');
      expect(billingBtn).not.toBeNull();
      expect(billingBtn!.getAttribute('title')).toBe('Billing');
    });

    it('renders Team button with correct aria-label and title', () => {
      act(() => {
        root.render(createElement(Sidebar, minimalProps));
      });

      const nav = container.querySelector('nav[aria-label="Platform navigation"]');
      expect(nav).not.toBeNull();

      const teamBtn = nav!.querySelector('button[aria-label="Team"]');
      expect(teamBtn).not.toBeNull();
      expect(teamBtn!.getAttribute('title')).toBe('Team');
    });

    it('renders buttons in order: tasks, billing, team', () => {
      act(() => {
        root.render(createElement(Sidebar, minimalProps));
      });

      const nav = container.querySelector('nav[aria-label="Platform navigation"]');
      expect(nav).not.toBeNull();

      const buttons = nav!.querySelectorAll('button[role="tab"]');
      expect(buttons[0].getAttribute('aria-label')).toBe('Tasks');
      expect(buttons[1].getAttribute('aria-label')).toBe('Billing');
      expect(buttons[2].getAttribute('aria-label')).toBe('Team');
    });

    it('platform nav buttons have rail-icon CSS class', () => {
      act(() => {
        root.render(createElement(Sidebar, minimalProps));
      });

      const nav = container.querySelector('nav[aria-label="Platform navigation"]');
      expect(nav).not.toBeNull();

      const buttons = nav!.querySelectorAll('button.rail-icon');
      expect(buttons.length).toBe(3);
    });

    it('clicking Tasks button calls onViewChange with "tasks"', () => {
      const onViewChange = vi.fn();

      act(() => {
        root.render(createElement(Sidebar, { ...minimalProps, onViewChange }));
      });

      const nav = container.querySelector('nav[aria-label="Platform navigation"]');
      const tasksBtn = nav!.querySelector('button[aria-label="Tasks"]');

      act(() => {
        tasksBtn!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });

      expect(onViewChange).toHaveBeenCalledWith('tasks');
    });

    it('clicking Billing button calls onViewChange with "billing"', () => {
      const onViewChange = vi.fn();

      act(() => {
        root.render(createElement(Sidebar, { ...minimalProps, onViewChange }));
      });

      const nav = container.querySelector('nav[aria-label="Platform navigation"]');
      const billingBtn = nav!.querySelector('button[aria-label="Billing"]');

      act(() => {
        billingBtn!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });

      expect(onViewChange).toHaveBeenCalledWith('billing');
    });

    it('clicking Team button calls onViewChange with "team"', () => {
      const onViewChange = vi.fn();

      act(() => {
        root.render(createElement(Sidebar, { ...minimalProps, onViewChange }));
      });

      const nav = container.querySelector('nav[aria-label="Platform navigation"]');
      const teamBtn = nav!.querySelector('button[aria-label="Team"]');

      act(() => {
        teamBtn!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
      });

      expect(onViewChange).toHaveBeenCalledWith('team');
    });

    it('highlights the active platform nav item with "active" class', () => {
      const onViewChange = vi.fn();

      act(() => {
        root.render(
          createElement(Sidebar, {
            ...minimalProps,
            currentView: 'billing',
            onViewChange,
          }),
        );
      });

      const nav = container.querySelector('nav[aria-label="Platform navigation"]');
      const buttons = nav!.querySelectorAll('button[role="tab"]');

      // Tasks should NOT be active
      expect(buttons[0].classList.contains('active')).toBe(false);
      expect(buttons[0].getAttribute('aria-selected')).toBe('false');

      // Billing SHOULD be active
      expect(buttons[1].classList.contains('active')).toBe(true);
      expect(buttons[1].getAttribute('aria-selected')).toBe('true');

      // Team should NOT be active
      expect(buttons[2].classList.contains('active')).toBe(false);
      expect(buttons[2].getAttribute('aria-selected')).toBe('false');
    });

    it('platform nav items are rendered between main sections and settings/logs', () => {
      act(() => {
        root.render(createElement(Sidebar, minimalProps));
      });

      const rail = container.querySelector('.sidebar-icon-rail');
      expect(rail).not.toBeNull();

      // Get all direct children of the icon rail
      const children = Array.from(rail!.children);

      // Find positions
      const mainTablistIdx = children.findIndex(
        (c) => c.getAttribute('role') === 'tablist' && c.querySelector('button[aria-label="Git"]'),
      );
      const platformNavIdx = children.findIndex(
        (c) => c.tagName === 'NAV' && c.getAttribute('aria-label') === 'Platform navigation',
      );
      const bottomTablistIdx = children.findIndex(
        (c) => c.getAttribute('role') === 'tablist' && c.querySelector('button[aria-label="Logs"]'),
      );

      // Platform nav should be after main tablist and before bottom tablist
      expect(mainTablistIdx >= 0);
      expect(platformNavIdx > mainTablistIdx);
      expect(bottomTablistIdx > platformNavIdx);
    });
  });
});
