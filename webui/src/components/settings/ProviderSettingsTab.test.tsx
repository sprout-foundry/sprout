/**
 * Tests for the inline primary-provider/model switcher added to
 * ProviderSettingsTab. Pins:
 *   - Inline dropdowns render when both `availableProviders` and
 *     `updateSetting` are provided.
 *   - Read-only legacy view renders otherwise (fallback path).
 *   - Provider change calls api.updateSettings with layer="global" and
 *     fires onPrimaryProviderChanged so the parent re-fetches the
 *     "Current Provider" panel.
 *   - Model dropdown is disabled when no provider is selected.
 */

import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi } from 'vitest';
import ProviderSettingsTab from './ProviderSettingsTab';

// Mock the ApiService used by persistPrimary so we can capture the call
// without going through the network. The component imports the static
// getInstance() helper, so we mock the module.
const updateSettingsMock = vi.fn().mockResolvedValue(undefined);
vi.mock('../../services/api', async () => {
  const actual = await vi.importActual<any>('../../services/api');
  return {
    ...actual,
    ApiService: {
      getInstance: () => ({ updateSettings: updateSettingsMock }),
    },
  };
});

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  // @ts-expect-error
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  updateSettingsMock.mockClear();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

const baseProps = {
  settings: { custom_providers: {} } as any,
  editingProvider: null,
  providerName: '',
  providerApiBase: '',
  providerModelName: '',
  providerContextSize: 32768,
  providerEnvVar: '',
  providerSupportsVision: false,
  providerVisionModel: '',
  providerModelContextSizes: '',
  loadingProviderInfo: false,
  currentProviderInfo: {
    provider: 'anthropic',
    model: 'claude-haiku-4-5',
    hasCredential: true,
  },
  setEditingProvider: vi.fn(),
  setProviderName: vi.fn(),
  setProviderApiBase: vi.fn(),
  setProviderModelName: vi.fn(),
  setProviderContextSize: vi.fn(),
  setProviderEnvVar: vi.fn(),
  setProviderSupportsVision: vi.fn(),
  setProviderVisionModel: vi.fn(),
  setProviderModelContextSizes: vi.fn(),
  resetProviderForm: vi.fn(),
  handleAddProvider: vi.fn().mockResolvedValue(undefined),
  handleUpdateProvider: vi.fn().mockResolvedValue(undefined),
  handleDeleteProvider: vi.fn().mockResolvedValue(undefined),
};

const providersCatalog = [
  { id: 'anthropic', name: 'Anthropic', models: ['claude-haiku-4-5', 'claude-sonnet-4-6'] },
  { id: 'openai', name: 'OpenAI', models: ['gpt-5', 'gpt-5-mini'] },
];

describe('ProviderSettingsTab — inline switcher', () => {
  it('renders inline provider+model dropdowns when catalog and updateSetting are provided', () => {
    act(() => {
      root.render(
        createElement(ProviderSettingsTab, {
          ...baseProps,
          availableProviders: providersCatalog,
          updateSetting: vi.fn(),
        }),
      );
    });

    expect(container.querySelector('#primary-provider-select')).not.toBeNull();
    expect(container.querySelector('#primary-model-select')).not.toBeNull();
    // Legacy read-only "Provider: ..." rows must not coexist with the dropdowns.
    const labels = Array.from(container.querySelectorAll('.label')).map((el) => el.textContent);
    expect(labels).not.toContain('Provider:');
    expect(labels).not.toContain('Model:');
  });

  it('falls back to read-only legacy view when no catalog is supplied', () => {
    act(() => {
      root.render(
        createElement(ProviderSettingsTab, {
          ...baseProps,
          // no availableProviders, no updateSetting → legacy
        }),
      );
    });

    expect(container.querySelector('#primary-provider-select')).toBeNull();
    expect(container.querySelector('#primary-model-select')).toBeNull();
    // Legacy view shows the static read-only rows.
    const labels = Array.from(container.querySelectorAll('.label')).map((el) => el.textContent);
    expect(labels).toContain('Provider:');
    expect(labels).toContain('Model:');
  });

  it('preselects the current provider and model in the dropdowns', () => {
    act(() => {
      root.render(
        createElement(ProviderSettingsTab, {
          ...baseProps,
          availableProviders: providersCatalog,
          updateSetting: vi.fn(),
        }),
      );
    });

    const providerSelect = container.querySelector('#primary-provider-select') as HTMLSelectElement;
    const modelSelect = container.querySelector('#primary-model-select') as HTMLSelectElement;
    expect(providerSelect.value).toBe('anthropic');
    expect(modelSelect.value).toBe('claude-haiku-4-5');
  });

  it('disables the model dropdown when no provider is selected', () => {
    act(() => {
      root.render(
        createElement(ProviderSettingsTab, {
          ...baseProps,
          availableProviders: providersCatalog,
          updateSetting: vi.fn(),
          currentProviderInfo: { provider: '', model: '', hasCredential: false },
        }),
      );
    });

    const modelSelect = container.querySelector('#primary-model-select') as HTMLSelectElement;
    expect(modelSelect.disabled).toBe(true);
  });

  it('persists primary provider change via api.updateSettings(layer="global")', async () => {
    const onChanged = vi.fn();
    act(() => {
      root.render(
        createElement(ProviderSettingsTab, {
          ...baseProps,
          availableProviders: providersCatalog,
          updateSetting: vi.fn(),
          onPrimaryProviderChanged: onChanged,
        }),
      );
    });

    const providerSelect = container.querySelector('#primary-provider-select') as HTMLSelectElement;
    await act(async () => {
      providerSelect.value = 'openai';
      providerSelect.dispatchEvent(new Event('change', { bubbles: true }));
    });

    expect(updateSettingsMock).toHaveBeenCalledWith({ provider: 'openai' }, 'global');
    expect(onChanged).toHaveBeenCalled();
  });

  it('persists primary model change via api.updateSettings(layer="global")', async () => {
    const onChanged = vi.fn();
    act(() => {
      root.render(
        createElement(ProviderSettingsTab, {
          ...baseProps,
          availableProviders: providersCatalog,
          updateSetting: vi.fn(),
          onPrimaryProviderChanged: onChanged,
        }),
      );
    });

    const modelSelect = container.querySelector('#primary-model-select') as HTMLSelectElement;
    await act(async () => {
      modelSelect.value = 'claude-sonnet-4-6';
      modelSelect.dispatchEvent(new Event('change', { bubbles: true }));
    });

    expect(updateSettingsMock).toHaveBeenCalledWith({ model: 'claude-sonnet-4-6' }, 'global');
    expect(onChanged).toHaveBeenCalled();
  });

  it('shows the current model as a synthetic option when it is not in the provider catalog (handles unlisted/custom models)', () => {
    act(() => {
      root.render(
        createElement(ProviderSettingsTab, {
          ...baseProps,
          availableProviders: providersCatalog,
          updateSetting: vi.fn(),
          currentProviderInfo: {
            provider: 'anthropic',
            model: 'claude-experimental-beta', // not in the catalog
            hasCredential: true,
          },
        }),
      );
    });

    const modelSelect = container.querySelector('#primary-model-select') as HTMLSelectElement;
    // The current model should still be selectable so the dropdown doesn't
    // render an empty/unknown value when the user has a model the catalog
    // doesn't yet know about.
    expect(modelSelect.value).toBe('claude-experimental-beta');
  });
});
