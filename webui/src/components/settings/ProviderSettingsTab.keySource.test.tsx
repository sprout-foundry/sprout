/**
 * Pins the custom-provider API key source choice:
 *   - A pasted API key is the default for new providers.
 *   - Only the chosen source's input is shown, and switching clears the
 *     other field so it can never be submitted alongside.
 *   - Env-var mode requires a variable name before saving.
 *   - Editing a provider opens on the source it already uses.
 */

import { fireEvent, render, screen } from '@testing-library/react';
import { createElement, useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import ProviderSettingsTab from './ProviderSettingsTab';

interface HarnessProps {
  mode: 'add' | 'edit';
  initialEnvVar?: string;
  initialApiKey?: string;
  customProviders?: Record<string, unknown>;
  handleAddProvider?: () => Promise<void>;
  handleUpdateProvider?: () => Promise<void>;
  onState?: (state: { envVar: string; apiKey: string }) => void;
}

function Harness({
  mode,
  initialEnvVar = '',
  initialApiKey = '',
  customProviders = {},
  handleAddProvider = vi.fn().mockResolvedValue(undefined),
  handleUpdateProvider = vi.fn().mockResolvedValue(undefined),
  onState,
}: HarnessProps) {
  const [envVar, setEnvVar] = useState(initialEnvVar);
  const [apiKey, setApiKey] = useState(initialApiKey);
  const [editing, setEditing] = useState<{ mode: 'add' | 'edit'; originalName?: string } | null>(
    mode === 'edit' ? null : { mode: 'add' },
  );
  onState?.({ envVar, apiKey });
  return createElement(ProviderSettingsTab, {
    settings: { custom_providers: customProviders } as any,
    editingProvider: editing,
    providerName: 'gw',
    providerApiBase: 'https://gw.example/v1',
    providerModelName: '',
    providerContextSize: 32768,
    providerEnvVar: envVar,
    providerApiKey: apiKey,
    providerSupportsVision: false,
    providerVisionModel: '',
    providerBillingType: 'pay_per_token',
    providerModelContextSizes: '',
    loadingProviderInfo: false,
    currentProviderInfo: null,
    setEditingProvider: setEditing,
    setProviderName: vi.fn(),
    setProviderApiBase: vi.fn(),
    setProviderModelName: vi.fn(),
    setProviderContextSize: vi.fn(),
    setProviderEnvVar: setEnvVar,
    setProviderApiKey: setApiKey,
    setProviderSupportsVision: vi.fn(),
    setProviderVisionModel: vi.fn(),
    setProviderBillingType: vi.fn(),
    setProviderModelContextSizes: vi.fn(),
    resetProviderForm: vi.fn(),
    handleAddProvider,
    handleUpdateProvider,
    handleDeleteProvider: vi.fn().mockResolvedValue(undefined),
  });
}

describe('ProviderSettingsTab — API key source', () => {
  it('defaults new providers to a pasted API key and hides the env var input', () => {
    render(createElement(Harness, { mode: 'add' }));

    expect(screen.getByRole('radio', { name: /API key/ })).toBeChecked();
    expect(screen.getByRole('radio', { name: /Environment variable/ })).not.toBeChecked();
    expect(screen.getByLabelText('API key')).toHaveAttribute('type', 'password');
    expect(screen.queryByLabelText('Environment variable name')).toBeNull();
  });

  it('switching to env var clears a pasted key, and switching back clears the env var', () => {
    let state = { envVar: '', apiKey: '' };
    render(
      createElement(Harness, {
        mode: 'add',
        initialApiKey: 'sk-pasted',
        onState: (s) => {
          state = s;
        },
      }),
    );

    fireEvent.click(screen.getByRole('radio', { name: /Environment variable/ }));
    expect(state.apiKey).toBe('');
    expect(screen.queryByLabelText('API key')).toBeNull();

    fireEvent.change(screen.getByLabelText('Environment variable name'), { target: { value: 'GW_KEY' } });
    expect(state.envVar).toBe('GW_KEY');

    fireEvent.click(screen.getByRole('radio', { name: /API key/ }));
    expect(state.envVar).toBe('');
  });

  it('requires a variable name before saving in env var mode', () => {
    const handleAddProvider = vi.fn().mockResolvedValue(undefined);
    render(createElement(Harness, { mode: 'add', handleAddProvider }));

    fireEvent.click(screen.getByRole('radio', { name: /Environment variable/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));

    expect(handleAddProvider).not.toHaveBeenCalled();
    expect(screen.getByRole('alert')).toHaveTextContent('Enter the environment variable name');

    fireEvent.change(screen.getByLabelText('Environment variable name'), { target: { value: 'GW_KEY' } });
    expect(screen.queryByRole('alert')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    expect(handleAddProvider).toHaveBeenCalledTimes(1);
  });

  it('saves in API key mode without requiring a key (existing providers keep theirs)', () => {
    const handleAddProvider = vi.fn().mockResolvedValue(undefined);
    render(createElement(Harness, { mode: 'add', handleAddProvider }));

    fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    expect(handleAddProvider).toHaveBeenCalledTimes(1);
  });

  it('opens an env-var provider for editing in env var mode', () => {
    render(
      createElement(Harness, {
        mode: 'edit',
        customProviders: { gw: { endpoint: 'https://gw.example/v1', env_var: 'GW_KEY' } },
      }),
    );

    fireEvent.click(screen.getByTitle('Edit provider'));
    expect(screen.getByRole('radio', { name: /Environment variable/ })).toBeChecked();
  });

  it('opens a stored-key provider for editing in API key mode with a keep-existing hint', () => {
    render(
      createElement(Harness, {
        mode: 'edit',
        customProviders: { gw: { endpoint: 'https://gw.example/v1' } },
      }),
    );

    fireEvent.click(screen.getByTitle('Edit provider'));
    expect(screen.getByRole('radio', { name: /API key/ })).toBeChecked();
    expect(screen.getByLabelText('API key')).toHaveAttribute('placeholder', 'Leave blank to keep the saved key');
  });
});
