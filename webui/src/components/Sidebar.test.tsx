// @ts-nocheck

import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react';
import Sidebar from './Sidebar';
import { ApiService } from '../services/api';

jest.mock('./SettingsPanel', () => () => <div data-testid="settings-panel" />);
jest.mock('./FileTree', () => () => <div data-testid="file-tree" />);
jest.mock('./SearchView', () => () => <div data-testid="search-view" />);
jest.mock('./GitSidebarPanel', () => () => <div data-testid="git-panel" />);
jest.mock('./RevisionListPanel', () => () => <div data-testid="revision-panel" />);
jest.mock('./LeditLogo', () => () => <div data-testid="ledit-logo" />);
jest.mock('./LocationSwitcher', () => () => <div data-testid="location-switcher" />);
jest.mock('./ResizeHandle', () => () => null);
jest.mock('../contexts/ThemeContext', () => ({
  useTheme: () => ({
    themePack: { id: 'default' },
    availableThemePacks: [],
    setThemePack: jest.fn(),
    importTheme: jest.fn(() => ({ success: true })),
    removeTheme: jest.fn(),
  }),
}));
jest.mock('../contexts/HotkeyContext', () => ({
  useHotkeys: () => ({
    applyPreset: jest.fn(),
  }),
}));
jest.mock('../services/api', () => {
  const actual = jest.requireActual('../services/api');
  return {
    ...actual,
    ApiService: {
      getInstance: jest.fn(),
    },
  };
});

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
      getSettings: jest.fn().mockResolvedValue({}),
      getProviders: jest.fn(),
    };
    (ApiService.getInstance as jest.Mock).mockReturnValue(apiServiceMock);
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
    jest.restoreAllMocks();
    jest.clearAllMocks();
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

    const onProviderChange = jest.fn();

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
