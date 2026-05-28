// @ts-nocheck

import { act, createElement } from 'react';
import { createRoot } from 'react-dom/client';
import { ApiService } from '../services/api';
import Sidebar from './Sidebar';

vi.mock('./SettingsPanel', () => ({ default: () => null }));
vi.mock('./FileTree', () => ({ default: () => null }));
vi.mock('./SearchView', () => ({ default: () => null }));
vi.mock('./GitSidebarPanel', () => ({ default: () => null }));
vi.mock('./AgentChangesPanel', () => ({ default: () => null }));
vi.mock('./SproutLogo', () => ({ default: () => null }));
vi.mock('./LocationSwitcher', () => ({ default: () => null }));
vi.mock('./ResizeHandle', () => ({ default: () => null }));
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
  useHotkeys: () => ({
    applyPreset: vi.fn(),
  }),
}));
vi.mock('../services/api', () => {
  const actual = vi.importActual('../services/api');
  return {
    ...actual,
    ApiService: {
      getInstance: vi.fn(),
    },
  };
});

// Sidebar uses useLog() which requires NotificationContext.
// We provide a minimal mock with plain arrow functions (no vi.fn()) so Jest
// doesn't need to transform NotificationContext.tsx, avoiding a heavy module
// resolution cascade that causes OOM under Node 22 + Jest 27.
vi.mock('../contexts/NotificationContext', () => ({
  NotificationProvider: ({ children }) => children,
  useNotifications: () => ({ addNotification: () => {} }),
}));

// Sidebar uses useEditorManager for editor settings within the settings panel
vi.mock('../contexts/EditorManagerContext', () => ({
  useEditorManager: () => ({
    paneSizes: {},
    updatePaneSize: vi.fn(),
  }),
}));

// Sidebar uses usePlatformNav for cloud platform nav items
vi.mock('../contexts/PlatformNavContext', () => ({
  usePlatformNav: () => ({
    platformNavItems: [],
  }),
}));

const flushPromises = async () => {
  await act(async () => {
    await Promise.resolve();
  });
};

describe('Sidebar provider selection', () => {
  let container: HTMLDivElement;
  let root: any;
  let apiServiceMock: any;

  beforeAll(() => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  });

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);

    apiServiceMock = {
      getSettings: vi.fn().mockResolvedValue({}),
      getProviders: vi.fn(),
    };
    (ApiService.getInstance as vi.Mock).mockReturnValue(apiServiceMock);
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
    vi.restoreAllMocks();
    vi.clearAllMocks();
  });

  it('does not let a stale provider fetch overwrite a user selection', async () => {
    apiServiceMock.getProviders.mockResolvedValueOnce({
      providers: [
        { id: 'openai', name: 'OpenAI', models: ['gpt-4o-mini'] },
        { id: 'anthropic', name: 'Anthropic', models: ['claude-3-7-sonnet'] },
      ],
      current_provider: 'openai',
      current_model: 'gpt-4o-mini',
    });

    let resolveProviders: (value: any) => void = () => {};
    apiServiceMock.getProviders.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveProviders = resolve;
        }),
    );
    apiServiceMock.getProviders.mockResolvedValue({
      providers: [
        { id: 'openai', name: 'OpenAI', models: ['gpt-4o-mini'] },
        { id: 'anthropic', name: 'Anthropic', models: ['claude-3-7-sonnet'] },
      ],
      current_provider: 'openai',
      current_model: 'gpt-4o-mini',
    });

    const onProviderChange = vi.fn();

    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(
        <Sidebar
          isConnected={true}
          isOpen={true}
          provider="openai"
          model="gpt-4o-mini"
          onProviderChange={onProviderChange}
        />,
      );
    });

    await act(async () => {
      const settingsButton = container.querySelector('button[aria-label="Settings"]') as HTMLButtonElement;
      settingsButton.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    await flushPromises();

    const providerSelect = container.querySelector('#provider-select') as HTMLSelectElement;
    expect(providerSelect).not.toBeNull();

    await act(async () => {
      providerSelect.value = 'anthropic';
      providerSelect.dispatchEvent(new Event('change', { bubbles: true }));
    });

    expect(onProviderChange).toHaveBeenCalledWith('anthropic');
    expect(providerSelect.value).toBe('anthropic');

    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(
        <Sidebar
          isConnected={false}
          isOpen={true}
          provider="openai"
          model="gpt-4o-mini"
          onProviderChange={onProviderChange}
        />,
      );
    });

    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(
        <Sidebar
          isConnected={true}
          isOpen={true}
          provider="openai"
          model="gpt-4o-mini"
          onProviderChange={onProviderChange}
        />,
      );
    });

    await act(async () => {
      resolveProviders({
        providers: [
          { id: 'openai', name: 'OpenAI', models: ['gpt-4o-mini'] },
          { id: 'anthropic', name: 'Anthropic', models: ['claude-3-7-sonnet'] },
        ],
        current_provider: 'openai',
        current_model: 'gpt-4o-mini',
      });
    });

    await flushPromises();

    const updatedProviderSelect = container.querySelector('#provider-select') as HTMLSelectElement;
    expect(updatedProviderSelect.value).toBe('anthropic');
  });

  it('hydrates the initial provider from the backend when no explicit prop is set', async () => {
    apiServiceMock.getProviders.mockResolvedValue({
      providers: [
        { id: 'openai', name: 'OpenAI', models: ['gpt-4o-mini'] },
        { id: 'anthropic', name: 'Anthropic', models: ['claude-3-7-sonnet'] },
      ],
      current_provider: 'anthropic',
      current_model: 'claude-3-7-sonnet',
    });

    // eslint-disable-next-line testing-library/no-unnecessary-act
    await act(async () => {
      root.render(<Sidebar isConnected={true} isOpen={true} provider="" model="" />);
    });

    await act(async () => {
      const settingsButton = container.querySelector('button[aria-label="Settings"]') as HTMLButtonElement;
      settingsButton.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    await flushPromises();

    const providerSelect = container.querySelector('#provider-select') as HTMLSelectElement;
    const modelSelect = container.querySelector('#model-select') as HTMLSelectElement;
    expect(providerSelect.value).toBe('anthropic');
    expect(modelSelect.value).toBe('claude-3-7-sonnet');
  });
});
