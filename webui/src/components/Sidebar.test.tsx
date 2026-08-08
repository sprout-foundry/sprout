// @ts-nocheck
/**
 * Sidebar provider-selection behavior tests.
 *
 * Uses the same focused-mock pattern as Sidebar.platformNav.test.tsx and
 * Sidebar.costsNav.test.tsx: heavy child panels and the model/event-handler
 * hooks are mocked so the jsdom worker doesn't OOM. The real useSidebarModel
 * pulls ProviderCatalogContext + settings effects that churn under jsdom;
 * mocking it lets these tests verify the provider-selection wiring in the
 * Sidebar component itself.
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';

// ---------------------------------------------------------------------------
// Mocks — MUST be set up BEFORE importing Sidebar
// ---------------------------------------------------------------------------

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

vi.mock('../contexts/NotificationContext', () => ({
  __esModule: true,
  NotificationProvider: ({ children }) => children,
  useNotifications: () => ({ addNotification: () => {} }),
  useLog: () => vi.fn(),
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

vi.mock('../contexts/PlatformNavContext', () => ({
  __esModule: true,
  PlatformNavProvider: ({ children }) => children,
  usePlatformNav: () => ({
    platformNavItems: [],
  }),
}));

// Mock config/mode so capability flags are explicit (not adapter-dependent).
vi.mock('../config/mode', () => ({
  __esModule: true,
  isCloud: false,
  supportsSettings: true,
  supportsLocalTerminal: false,
  supportsGit: true,
  supportsWorkspaceSwitching: false,
}));

// Mock ApiService — never load the real ../services/api index (it re-exports
// gitApi/chatApi/sshApi/etc. and collecting that graph OOMs the worker).
vi.mock('../services/api', () => ({
  __esModule: true,
  ApiService: {
    getInstance: vi.fn(() => ({
      getProviders: vi.fn().mockResolvedValue({
        providers: [
          { id: 'openai', name: 'OpenAI', models: ['gpt-4o-mini'] },
          { id: 'anthropic', name: 'Anthropic', models: ['claude-3-7-sonnet'] },
        ],
        current_provider: 'openai',
        current_model: 'gpt-4o-mini',
      }),
      getSettings: vi.fn().mockResolvedValue({}),
    })),
  },
}));

// Mock utils/log
vi.mock('../utils/log', () => ({
  __esModule: true,
  useLog: () => vi.fn(),
  debugLog: vi.fn(),
}));

// Mock heavy child panels (avoid the lazy SettingsPanel graph entirely).
vi.mock('./SettingsPanel', () => ({ default: () => createElement('div', { className: 'mock-settings' }) }));
vi.mock('./FileTree', () => ({ default: () => createElement('div', { className: 'mock-filetree' }) }));
vi.mock('./SearchView', () => ({ default: () => createElement('div', { className: 'mock-search' }) }));
vi.mock('./GitSidebarPanel', () => ({ default: () => createElement('div', { className: 'mock-git' }) }));
vi.mock('./AgentChangesPanel', () => ({ default: () => createElement('div', { className: 'mock-changes' }) }));
vi.mock('./SproutLogo', () => ({ default: () => createElement('svg', { className: 'mock-logo' }) }));
vi.mock('./LocationSwitcher', () => ({ default: () => createElement('div', { className: 'mock-location-switcher' }) }));
vi.mock('./ResizeHandle', () => ({ default: () => createElement('div', { className: 'mock-resize-handle' }) }));
vi.mock('./SidebarFilesSection', () => ({ default: () => createElement('div', { className: 'mock-files-section' }) }));
vi.mock('./SidebarGitSection', () => ({ default: () => createElement('div', { className: 'mock-git-section' }) }));
vi.mock('./SidebarLogsPane', () => ({ default: () => createElement('div', { className: 'mock-logs' }) }));
vi.mock('./SidebarSettingsSection', () => ({
  default: ({ selectedProvider, providers, onProviderChange }: any) =>
    createElement(
      'div',
      { className: 'mock-settings-section' },
      createElement(
        'select',
        {
          id: 'provider-select',
          value: selectedProvider,
          onChange: (e: { target: { value: string } }) => onProviderChange?.(e.target.value),
          'data-testid': 'provider-select',
        },
        (providers || []).map((p: { id: string; name: string }) =>
          createElement('option', { key: p.id, value: p.id }, p.name),
        ),
      ),
    ),
}));
vi.mock('./AutomationsPanel', () => ({ default: () => createElement('div', { className: 'mock-automations' }) }));

// Mock hooks — the real useSidebarModel/useSidebarEventHandlers have effects
// that pull ProviderCatalogContext and churn under jsdom (OOM).
vi.mock('../hooks/useSidebarEventHandlers', () => ({
  useSidebarEventHandlers: vi.fn(),
}));
vi.mock('../hooks/useSidebarModel', () => ({
  useSidebarModel: vi.fn(),
}));

// ---------------------------------------------------------------------------
// Import AFTER mocks are set up
// ---------------------------------------------------------------------------

import Sidebar from './Sidebar';
import { useSidebarModel as useSidebarModelMock } from '../hooks/useSidebarModel';

function makeModelState(overrides = {}) {
  return {
    selectedProvider: 'openai',
    selectedModelState: 'gpt-4o-mini',
    selectedPersonaState: '',
    personas: [],
    isLoadingPersonas: false,
    providers: [
      { id: 'openai', name: 'OpenAI', models: ['gpt-4o-mini'] },
      { id: 'anthropic', name: 'Anthropic', models: ['claude-3-7-sonnet'] },
    ],
    isLoadingProviders: false,
    settings: null,
    settingsFocusTarget: null,
    finalSelectedModel: 'gpt-4o-mini',
    availableModelsState: ['gpt-4o-mini', 'claude-3-7-sonnet'],
    finalAvailableModels: ['gpt-4o-mini', 'claude-3-7-sonnet'],
    setSelectedProvider: vi.fn(),
    setSelectedModelState: vi.fn(),
    setSelectedPersonaState: vi.fn(),
    setSettings: vi.fn(),
    setSettingsFocusTarget: vi.fn(),
    ...overrides,
  };
}

const flushPromises = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

describe('Sidebar provider selection', () => {
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

  it('forwards a provider change from the settings section through the model hook', async () => {
    const onProviderChange = vi.fn();
    const setSelectedProvider = vi.fn();
    vi.mocked(useSidebarModelMock).mockReturnValue(makeModelState({ setSelectedProvider }));

    await act(async () => {
      root.render(
        <Sidebar
          isConnected={true}
          isOpen={true}
          selectedSection="settings"
          provider="openai"
          model="gpt-4o-mini"
          onProviderChange={onProviderChange}
        />,
      );
    });

    // The settings section is mocked with a provider <select>; changing it
    // must flow through modelState.setSelectedProvider AND the Sidebar's own
    // onProviderChange prop (the stale-fetch guard lives in the real hook,
    // but this verifies the wiring the guard sits behind).
    const providerSelect = container.querySelector('[data-testid="provider-select"]') as HTMLSelectElement;
    expect(providerSelect).not.toBeNull();

    await act(async () => {
      providerSelect.value = 'anthropic';
      providerSelect.dispatchEvent(new Event('change', { bubbles: true }));
    });

    expect(setSelectedProvider).toHaveBeenCalledWith('anthropic');
    expect(onProviderChange).toHaveBeenCalledWith('anthropic');
  });

  it('propagates a settings tab click to onSectionChange', async () => {
    const onSectionChange = vi.fn();
    vi.mocked(useSidebarModelMock).mockReturnValue(makeModelState());

    await act(async () => {
      root.render(
        <Sidebar
          isConnected={true}
          isOpen={true}
          selectedSection="git"
          provider="openai"
          model="gpt-4o-mini"
          onSectionChange={onSectionChange}
        />,
      );
    });

    await act(async () => {
      const settingsButton = container.querySelector('button[aria-label="Settings"]') as HTMLButtonElement;
      settingsButton.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(onSectionChange).toHaveBeenCalledWith('settings');
  });
});
