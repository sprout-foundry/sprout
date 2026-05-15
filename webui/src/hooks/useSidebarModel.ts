import { useState, useEffect, useRef, useMemo } from 'react';
import { ApiService, type ProviderOption, type SproutSettings } from '../services/api';
import { useLog, debugLog } from '../utils/log';
import { supportsSettings } from '../config/mode';

interface UseSidebarModelParams {
  isConnected: boolean;
  provider?: string;
  model?: string;
  selectedModel?: string;
  selectedPersona?: string;
  stats?: {
    persona?: string;
  };
  onProviderChange?: (provider: string) => void;
  onModelChange?: (model: string) => void;
  onPersonaChange?: (persona: string) => void;
}

export interface UseSidebarModelReturn {
  // State
  selectedProvider: string;
  selectedModelState: string;
  selectedPersonaState: string;
  personas: { id: string; name: string; enabled: boolean }[];
  isLoadingPersonas: boolean;
  providers: ProviderOption[];
  isLoadingProviders: boolean;
  settings: SproutSettings | null;
  settingsFocusTarget: 'persona' | 'provider' | null;
  // Computed values
  finalSelectedModel: string;
  availableModelsState: string[];
  finalAvailableModels: string[];
  // Setters
  setSelectedProvider: (provider: string) => void;
  setSelectedModelState: (model: string) => void;
  setSelectedPersonaState: (persona: string) => void;
  setSettings: (settings: SproutSettings | null) => void;
  setSettingsFocusTarget: (target: 'persona' | 'provider' | null) => void;
}

export function useSidebarModel({
  isConnected,
  provider,
  model,
  selectedModel,
  selectedPersona,
  stats,
  onProviderChange,
  onModelChange,
  onPersonaChange,
}: UseSidebarModelParams): UseSidebarModelReturn {
  const log = useLog();
  const apiService = ApiService.getInstance();

  const [selectedProvider, setSelectedProvider] = useState(provider || '');
  const [selectedModelState, setSelectedModelState] = useState(model || selectedModel || '');
  const [selectedPersonaState, setSelectedPersonaState] = useState<string>(
    selectedPersona || stats?.persona || 'orchestrator',
  );
  const [personas, setPersonas] = useState<{ id: string; name: string; enabled: boolean }[]>([]);
  const [isLoadingPersonas, setIsLoadingPersonas] = useState(false);
  const [providers, setProviders] = useState<ProviderOption[]>([]);
  const [isLoadingProviders, setIsLoadingProviders] = useState(false);
  const hasHydratedProviderStateRef = useRef(false);
  const [settings, setSettings] = useState<SproutSettings | null>(null);
  const [settingsFocusTarget, setSettingsFocusTarget] = useState<'persona' | 'provider' | null>(null);

  // Sync persona state when stats change (e.g., from another client's persona change)
  useEffect(() => {
    if (stats?.persona && stats.persona !== selectedPersonaState) {
      setSelectedPersonaState(stats.persona);
    }
  }, [stats?.persona, selectedPersonaState]);

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

  useEffect(() => {
    if (!isConnected || !supportsSettings) return;

    const fetchProviders = async () => {
      setIsLoadingProviders(true);
      try {
        const data = await apiService.getProviders();
        if (data.providers && data.providers.length > 0) {
          setProviders(data.providers);
          if (!hasHydratedProviderStateRef.current) {
            const initialProvider =
              provider && provider !== 'unknown' ? provider : data.current_provider || data.providers[0]?.id || '';
            if (initialProvider) {
              setSelectedProvider(initialProvider);
            }

            const initialModel =
              model && model !== 'unknown'
                ? model
                : selectedModel && selectedModel !== 'unknown'
                  ? selectedModel
                  : data.current_model || '';
            if (initialModel) {
              setSelectedModelState(initialModel);
            }

            hasHydratedProviderStateRef.current = true;
          }
        }
      } catch (error) {
        log.error(`Failed to fetch providers: ${error instanceof Error ? error.message : String(error)}`, {
          title: 'Provider Load Error',
        });
      } finally {
        setIsLoadingProviders(false);
      }
    };

    fetchProviders();
  }, [apiService, isConnected]); // eslint-disable-line react-hooks/exhaustive-deps

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

  // Load personas from the backend
  useEffect(() => {
    if (!isConnected || !supportsSettings) return;

    const fetchPersonas = async () => {
      setIsLoadingPersonas(true);
      try {
        const data = await apiService.getSubagentTypes();
        const enabledPersonas = Object.values(data.subagent_types)
          .filter((p) => p.enabled && p.id && p.name) // Skip empty/corrupted entries
          .map((p) => ({
            id: p.id,
            name: p.name || p.id,
            enabled: p.enabled,
          }));

        // Always add orchestrator as an option (it's the default)
        const allPersonas = [
          { id: 'orchestrator', name: 'Orchestrator', enabled: true },
          ...enabledPersonas.filter((p) => p.id !== 'orchestrator'),
        ];

        setPersonas(allPersonas);
      } catch (error) {
        log.error(`Failed to fetch personas: ${error instanceof Error ? error.message : String(error)}`, {
          title: 'Persona Load Error',
        });
        // Fallback to just orchestrator
        setPersonas([{ id: 'orchestrator', name: 'Orchestrator', enabled: true }]);
      } finally {
        setIsLoadingPersonas(false);
      }
    };

    fetchPersonas();
  }, [apiService, isConnected, log]); // eslint-disable-line react-hooks/exhaustive-deps

  return {
    selectedProvider,
    selectedModelState,
    selectedPersonaState,
    personas,
    isLoadingPersonas,
    providers,
    isLoadingProviders,
    settings,
    settingsFocusTarget,
    finalSelectedModel,
    availableModelsState,
    finalAvailableModels,
    setSelectedProvider,
    setSelectedModelState,
    setSelectedPersonaState,
    setSettings,
    setSettingsFocusTarget,
  };
}
