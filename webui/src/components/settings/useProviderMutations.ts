import { useCallback } from 'react';
import type { CustomProviderConfig } from '../../services/api/types';
import { debugLog } from '../../utils/log';
import type { MutationContext } from './types';

interface ProviderMutationParams {
  // Shared context
  ctx: MutationContext;
  // Provider form state
  editingProvider: { mode: 'add' | 'edit'; originalName?: string } | null;
  setEditingProvider: (v: { mode: 'add' | 'edit'; originalName?: string } | null) => void;
  providerName: string;
  setProviderName: (v: string) => void;
  providerApiBase: string;
  setProviderApiBase: (v: string) => void;
  providerModelName: string;
  setProviderModelName: (v: string) => void;
  providerContextSize: number;
  setProviderContextSize: (v: number) => void;
  providerEnvVar: string;
  setProviderEnvVar: (v: string) => void;
  providerSupportsVision: boolean;
  setProviderSupportsVision: (v: boolean) => void;
  providerVisionModel: string;
  setProviderVisionModel: (v: string) => void;
  providerModelContextSizes: string;
  setProviderModelContextSizes: (v: string) => void;
}

/** Parse a comma-separated "model:size" string into a Record
 * (e.g. "gpt-4o:128000,claude:200000" → { gpt-4o: 128000, claude: 200000 }). */
function parseModelContextSizes(raw: string): Record<string, number> | undefined {
  const trimmed = raw.trim();
  if (!trimmed) return undefined;
  const entries = trimmed.split(',').map((pair) => {
    const sep = pair.lastIndexOf(':');
    if (sep === -1) return null;
    const model = pair.slice(0, sep).trim();
    const size = Number(pair.slice(sep + 1).trim());
    return model && Number.isFinite(size) ? ([model, size] as const) : null;
  });
  const valid = entries.filter((e): e is readonly [string, number] => e !== null);
  return valid.length > 0 ? Object.fromEntries(valid) : undefined;
}

export function useProviderMutations(params: ProviderMutationParams) {
  const {
    ctx,
    editingProvider,
    setEditingProvider,
    providerName,
    setProviderName,
    providerApiBase,
    setProviderApiBase,
    providerModelName,
    setProviderModelName,
    providerContextSize,
    setProviderContextSize,
    providerEnvVar,
    setProviderEnvVar,
    providerSupportsVision,
    setProviderSupportsVision,
    providerVisionModel,
    setProviderVisionModel,
    providerModelContextSizes,
    setProviderModelContextSizes,
  } = params;

  const resetProviderForm = useCallback(() => {
    setEditingProvider(null);
    setProviderName('');
    setProviderApiBase('');
    setProviderModelName('');
    setProviderContextSize(0);
    setProviderEnvVar('');
    setProviderSupportsVision(false);
    setProviderVisionModel('');
    setProviderModelContextSizes('');
  }, [
    setEditingProvider,
    setProviderName,
    setProviderApiBase,
    setProviderModelName,
    setProviderContextSize,
    setProviderEnvVar,
    setProviderSupportsVision,
    setProviderVisionModel,
    setProviderModelContextSizes,
  ]);

  const handleAddProvider = useCallback(async () => {
    if (!providerName.trim()) {
      ctx.addNotification('error', 'Providers', 'Provider name is required', 5000);
      return;
    }
    ctx.setSavingKey('provider-add');
    try {
      const modelContextSizes = parseModelContextSizes(providerModelContextSizes);
      const provider: CustomProviderConfig = {
        name: providerName.trim(),
        endpoint: providerApiBase.trim(),
        model_name: providerModelName.trim(),
        context_size: providerContextSize,
        requires_api_key: true,
        env_var: providerEnvVar.trim() || undefined,
        supports_vision: providerSupportsVision || undefined,
        vision_model: providerVisionModel.trim() || undefined,
        ...(modelContextSizes ? { model_context_sizes: modelContextSizes } : {}),
      };
      await ctx.api.addCustomProvider(provider);
      ctx.addNotification('success', 'Providers', `Provider "${providerName}" added`, 3000);
      resetProviderForm();
      ctx.refreshProviderCatalog?.();
    } catch (err) {
      debugLog('[SettingsPanel] failed to add provider:', err);
      ctx.addNotification('error', 'Providers', 'Failed to add provider', 5000);
    } finally {
      ctx.setSavingKey(null);
    }
  }, [
    ctx,
    providerName,
    providerApiBase,
    providerModelName,
    providerContextSize,
    providerEnvVar,
    providerSupportsVision,
    providerVisionModel,
    providerModelContextSizes,
    resetProviderForm,
  ]);

  const handleUpdateProvider = useCallback(async () => {
    if (!editingProvider?.originalName) return;
    ctx.setSavingKey(`provider-${editingProvider.originalName}`);
    try {
      const modelContextSizes = parseModelContextSizes(providerModelContextSizes);
      const provider: CustomProviderConfig = {
        name: providerName.trim(),
        endpoint: providerApiBase.trim(),
        model_name: providerModelName.trim(),
        context_size: providerContextSize,
        requires_api_key: true,
        env_var: providerEnvVar.trim() || undefined,
        supports_vision: providerSupportsVision || undefined,
        vision_model: providerVisionModel.trim() || undefined,
        ...(modelContextSizes ? { model_context_sizes: modelContextSizes } : {}),
      };
      await ctx.api.updateCustomProvider(editingProvider.originalName, provider);
      ctx.addNotification('success', 'Providers', `Provider "${editingProvider.originalName}" updated`, 3000);
      resetProviderForm();
      ctx.refreshProviderCatalog?.();
    } catch (err) {
      debugLog('[SettingsPanel] failed to update provider:', err);
      ctx.addNotification('error', 'Providers', 'Failed to update provider', 5000);
    } finally {
      ctx.setSavingKey(null);
    }
  }, [
    ctx,
    editingProvider,
    providerName,
    providerApiBase,
    providerModelName,
    providerContextSize,
    providerEnvVar,
    providerSupportsVision,
    providerVisionModel,
    providerModelContextSizes,
    resetProviderForm,
  ]);

  const handleDeleteProvider = useCallback(
    async (name: string) => {
      ctx.setSavingKey(`provider-delete-${name}`);
      try {
        await ctx.api.deleteCustomProvider(name);
        ctx.addNotification('success', 'Providers', `Provider "${name}" deleted`, 3000);
        if (editingProvider?.originalName === name) {
          resetProviderForm();
        }
        ctx.refreshProviderCatalog?.();
      } catch (err) {
        debugLog('[SettingsPanel] failed to delete provider:', err);
        ctx.addNotification('error', 'Providers', 'Failed to delete provider', 5000);
      } finally {
        ctx.setSavingKey(null);
      }
    },
    [ctx, editingProvider, resetProviderForm],
  );

  return {
    resetProviderForm,
    handleAddProvider,
    handleUpdateProvider,
    handleDeleteProvider,
  };
}
