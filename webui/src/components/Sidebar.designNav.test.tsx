// @ts-nocheck
/**
 * SP-140-5 — the Design mode's rail in the sidebar icon rail.
 *
 * Repurposed from the pre-SP-140-5 design-nav-button test: that button is
 * gone — Design is reached from the top-left mode switcher, and while the
 * Design mode is active the shell passes that mode's rail component
 * (`modeRail` prop; see workspaces/rail.ts) and the Sidebar renders it in
 * place of the Code section tabs. What this file pins is that seam: the
 * rail component renders its mode's entries, an entry selection fires
 * `onModeSectionChange`, and a mode without a rail (Code) renders no rail
 * at all.
 *
 * Same lightweight-mock pattern as Sidebar.costsNav.test.tsx (Sidebar's
 * context hooks throw without their providers).
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';

// ---------------------------------------------------------------------------
// Mocks — MUST be set up BEFORE importing Sidebar
// ---------------------------------------------------------------------------

vi.mock('../contexts/PlatformNavContext', () => ({
  __esModule: true,
  PlatformNavProvider: ({ children }) => children,
  usePlatformNav: () => ({
    platformNavItems: [],
  }),
}));

// The probe is irrelevant to the rail seam; report absent so no design
// surface is expected.
vi.mock('./design/useDesignPresence', () => ({
  __esModule: true,
  useDesignPresence: () => ({ present: false, loading: false }),
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
  useHotkeys: () => ({
    applyPreset: vi.fn(),
  }),
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
  __esModule: true,
  isCloud: false,
  supportsSettings: true,
  supportsLocalTerminal: false,
  supportsGit: true,
  supportsWorkspaceSwitching: false,
}));

// Mock leaf components to keep the render cheap.
vi.mock('./SproutLogo', () => ({ default: () => createElement('svg', { className: 'mock-logo' }) }));
vi.mock('./LocationSwitcher', () => ({
  default: () => createElement('div', { className: 'mock-location-switcher' }),
}));
vi.mock('./ResizeHandle', () => ({
  default: () => createElement('div', { className: 'mock-resize-handle' }),
}));
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
  __esModule: true,
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

vi.mock('../hooks/useSidebarEventHandlers', () => ({
  __esModule: true,
  useSidebarEventHandlers: vi.fn(),
}));

vi.mock('../hooks/useSidebarModel', () => ({
  __esModule: true,
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

vi.mock('../utils/log', () => ({
  __esModule: true,
  useLog: () => vi.fn(),
  debugLog: vi.fn(),
}));

// ---------------------------------------------------------------------------
// Import AFTER mocks are set up
// ---------------------------------------------------------------------------

import DesignRail from './design/DesignRail';
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

const codeMode = { id: 'code', label: 'Code' };
const designMode = { id: 'design', label: 'Design' };
// The public seam is the `modeRail` prop: the shell computes the active
// mode's rail (AppContent: `workspaceMode.id === 'design' ? DesignRail :
// undefined`) and hands the component down. Tests exercise that seam
// directly rather than the mode registry's shape.
const designRailProps = { modeRail: DesignRail };

/** Render Sidebar with the given extra props and return the container. */
function renderSidebar(extraProps = {}) {
  act(() => {
    root.render(createElement(Sidebar, { ...minimalProps, ...extraProps }));
  });
  return container;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('Sidebar mode rail (SP-140-5)', () => {
  it('renders no mode rail while a mode without a rail (Code) is active', () => {
    renderSidebar({ modes: [codeMode], activeModeId: 'code' });

    expect(container.querySelector('[data-testid="sidebar-mode-rail"]')).toBeNull();
    // The section rail itself is still there — the mode rail is an addition,
    // not a replacement.
    expect(container.querySelector('[data-testid="sidebar-icon-rail"]')).not.toBeNull();
  });

  it('renders the active mode rail with its entries, current section marked', () => {
    renderSidebar({
      modes: [codeMode, designMode],
      activeModeId: 'design',
      modeSection: 'flows',
      ...designRailProps,
    });

    const rail = container.querySelector('[data-testid="sidebar-mode-rail"]');
    expect(rail).not.toBeNull();
    expect(rail.querySelectorAll('[role="tab"]')).toHaveLength(3);

    const flows = container.querySelector('[data-testid="design-rail-flows"]');
    const screens = container.querySelector('[data-testid="design-rail-screens"]');
    const tokens = container.querySelector('[data-testid="design-rail-tokens"]');
    expect(flows).not.toBeNull();
    expect(screens).not.toBeNull();
    expect(tokens).not.toBeNull();
    expect(flows!.getAttribute('aria-selected')).toBe('true');
    expect(screens!.getAttribute('aria-selected')).toBe('false');
    expect(tokens!.getAttribute('aria-selected')).toBe('false');
  });

  it('fires onModeSectionChange when a rail entry is picked', () => {
    const onModeSectionChange = vi.fn();
    renderSidebar({
      modes: [codeMode, designMode],
      activeModeId: 'design',
      modeSection: 'flows',
      onModeSectionChange,
      ...designRailProps,
    });

    act(() => {
      container
        .querySelector('[data-testid="design-rail-screens"]')!
        .dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(onModeSectionChange).toHaveBeenCalledTimes(1);
    expect(onModeSectionChange).toHaveBeenCalledWith('screens');
  });

  it('a rail selection leaves the section state untouched', () => {
    const onSectionChange = vi.fn();
    renderSidebar({
      modes: [codeMode, designMode],
      activeModeId: 'design',
      modeSection: 'flows',
      selectedSection: 'git',
      onSectionChange,
      ...designRailProps,
    });

    act(() => {
      container
        .querySelector('[data-testid="design-rail-tokens"]')!
        .dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    // The Design rail drives the design surface's section, not the sidebar's.
    expect(onSectionChange).not.toHaveBeenCalled();
    // The content pane belongs to the mode while its rail is active: the
    // stale Code-section selection renders no Code section pane.
    expect(container.querySelector('.mock-git-section')).toBeNull();
    expect(container.querySelector('.mock-files-section')).toBeNull();
  });

  it('still renders global sections while a mode rail is active', () => {
    renderSidebar({
      modes: [codeMode, designMode],
      activeModeId: 'design',
      modeSection: 'flows',
      selectedSection: 'logs',
      ...designRailProps,
    });

    // Logs is addressed by the shared rail below the mode rail, not by the
    // rail's own entries, so its pane keeps rendering in Design mode.
    expect(container.querySelector('.mock-logs')).not.toBeNull();
    expect(container.querySelector('.mock-git-section')).toBeNull();
  });

  it('a mode-section pick releases a global pane back to the mode', () => {
    const onSectionChange = vi.fn();
    renderSidebar({
      modes: [codeMode, designMode],
      activeModeId: 'design',
      modeSection: 'flows',
      selectedSection: 'logs',
      onSectionChange,
      ...designRailProps,
    });

    // Clicking a mode-rail entry clears the global selection: the pane
    // returns to the mode's assets instead of sticking on Logs.
    act(() => {
      container
        .querySelector('[data-testid="design-rail-screens"]')!
        .dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onSectionChange).toHaveBeenCalledWith('');
  });
});
