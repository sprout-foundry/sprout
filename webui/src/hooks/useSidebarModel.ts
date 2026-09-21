import { useState, useEffect, useRef, useMemo } from 'react';
import { supportsSettings } from '../config/mode';
import { useProviderCatalog } from '../contexts/ProviderCatalogContext';
import { ApiService, type ProviderOption, type SproutSettings } from '../services/api';
import { debugLog } from '../utils/log';

interface UseSidebarModelParams {
  isConnected: boolean;
  provider?: string;
  model?: string;
  selectedModel?: string;
  stats?: {
    persona?: string;
  };
  onProviderChange?: (provider: string) => void;
  onModelChange?: (model: string) => void;
}

export interface UseSidebarModelReturn {
  // State
  selectedProvider: string;
  selectedModelState: string;
  providers: ProviderOption[];
  isLoadingProviders: boolean;
  settings: SproutSettings | null;
  settingsFocusTarget: 'provider' | null;
  // Computed values
  finalSelectedModel: string;
  availableModelsState: string[];
  finalAvailableModels: string[];
  // Setters
  setSelectedProvider: (provider: string) => void;
  setSelectedModelState: (model: string) => void;
  setSettings: (settings: SproutSettings | null) => void;
  setSettingsFocusTarget: (target: 'provider' | null) => void;
}

export function useSidebarModel({
  isConnected,
  provider,
  model,
  selectedModel,
  onProviderChange: _onProviderChange,
  onModelChange,
}: UseSidebarModelParams): UseSidebarModelReturn {
  const apiService = ApiService.getInstance();

  const catalog = useProviderCatalog();
  const [selectedProvider, setSelectedProvider] = useState(provider || '');
  const [selectedModelState, setSelectedModelState] = useState(model || selectedModel || '');
  // Providers now live in ProviderCatalogContext (single source of truth).
  const providers = catalog.providers;
  const isLoadingProviders = catalog.isLoading;
  const hasHydratedProviderStateRef = useRef(false);
  const [settings, setSettings] = useState<SproutSettings | null>(null);
  const [settingsFocusTarget, setSettingsFocusTarget] = useState<'provider' | null>(null);

  // Load settings on mount / connection
  useEffect(() => {
    if (!isConnected || !supportsSettings) return;
    let cancelled = false;
    apiService
      .getSettings()
      .then((s) => {
        if (!cancelled) setSettings(s);
      })
      .catch((err) => {
        debugLog('Failed to load settings:', err);
      });
    return () => {
      cancelled = true;
    };
  }, [isConnected, apiService]);

  const finalSelectedModel = selectedModel || selectedModelState;

  // Compute available models from providers and selectedProvider
  const availableModelsState = useMemo(() => {
    const providerData = providers.find((p) => p.id === selectedProvider);
    return providerData?.models || [];
  }, [providers, selectedProvider]);

  const finalAvailableModels = availableModelsState;

  // Hydrate the local selection from the shared catalog once it has loaded.
  // The catalog's `currentProvider`/`currentModel` come from /api/providers
  // (the same endpoint the old per-hook fetch used) so this preserves the
  // existing "prefer prop, then daemon-reported current, then first entry"
  // precedence. The fetch itself now lives in ProviderCatalogContext.
  useEffect(() => {
    if (providers.length === 0) return;
    if (hasHydratedProviderStateRef.current) return;

    const initialProvider =
      provider && provider !== 'unknown' ? provider : catalog.currentProvider || providers[0]?.id || '';
    if (initialProvider) {
      setSelectedProvider(initialProvider);
    }

    const initialModel =
      model && model !== 'unknown'
        ? model
        : selectedModel && selectedModel !== 'unknown'
          ? selectedModel
          : catalog.currentModel || '';
    if (initialModel) {
      setSelectedModelState(initialModel);
    }

    hasHydratedProviderStateRef.current = true;
  }, [providers, catalog.currentProvider, catalog.currentModel]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    // Always sync with the provider prop from App, even if it's empty or 'unknown'
    // This ensures the Sidebar reflects the actual backend state after errors/rollbacks
    if (provider !== undefined) {
      setSelectedProvider(provider);
    }
  }, [provider]);

  useEffect(() => {
    // Always sync with the model prop from App, even if it's empty or 'unknown'
    // This ensures the Sidebar reflects the actual backend state after errors/rollbacks
    if (model !== undefined) {
      setSelectedModelState(model);
    }
  }, [model]);

  useEffect(() => {
    if (!selectedProvider) {
      if (providers.length > 0) {
        setSelectedProvider(providers[0].id);
      }
      return;
    }

    const providerExists = providers.some((item) => item.id === selectedProvider);
    if (!providerExists && providers.length > 0) {
      setSelectedProvider(providers[0].id);
    }
  }, [providers, selectedProvider]);

  useEffect(() => {
    if (!selectedProvider) {
      return;
    }

    const providerData = providers.find((item) => item.id === selectedProvider);
    if (!providerData || providerData.models.length === 0) {
      return;
    }

    if (!providerData.models.includes(finalSelectedModel)) {
      setSelectedModelState(providerData.models[0]);
    }
  }, [providers, selectedProvider, finalSelectedModel]);

  return {
    selectedProvider,
    selectedModelState,
    providers,
    isLoadingProviders,
    settings,
    settingsFocusTarget,
    finalSelectedModel,
    availableModelsState,
    finalAvailableModels,
    setSelectedProvider,
    setSelectedModelState,
    setSettings,
    setSettingsFocusTarget,
  };
}
