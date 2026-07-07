import { useState, useRef, useEffect, useMemo, useCallback } from 'react';
import { useNotifications } from '../../contexts/NotificationContext';
import { useProviderCatalog } from '../../contexts/ProviderCatalogContext';
import { ApiService, type SproutSettings, type ProviderOption } from '../../services/api';
import type { SubagentTypeInfo } from '../../services/api/types';
import { debugLog } from '../../utils/log';
import { getNestedValue } from './settingsHelpers';
import type { SettingsSubTab } from './types';

interface UseSettingsStateReturn {
  activeSubTab: SettingsSubTab;
  setActiveSubTab: (v: SettingsSubTab) => void;
  /** Force a re-fetch of the current provider/model info shown in the
   *  Providers tab — used after the inline switcher persists changes. */
  refreshCurrentProviderInfo: () => void;
  /** Force a re-fetch of the provider catalog (subagentProviders) — used
   *  after the ProviderSettingsTab CRUD form adds a custom provider so
   *  the new entry appears in the dropdowns immediately. */
  refreshSubagentProviders: () => void;
  configViewLayer: 'session' | 'workspace' | 'global';
  setConfigViewLayer: (v: 'session' | 'workspace' | 'global') => void;
  layerLoading: string | null;
  setLayerLoading: (v: string | null) => void;
  layerData: Record<string, unknown> | null;
  setLayerData: (v: Record<string, unknown> | null) => void;
  layerError: string | null;
  setLayerError: (v: string | null) => void;
  creatingWorkspaceConfig: boolean;
  setCreatingWorkspaceConfig: (v: boolean) => void;
  provenanceSources: Record<string, string>;
  setProvenanceSources: (v: Record<string, string>) => void;
  displaySettingsRef: React.MutableRefObject<SproutSettings | null>;
  /** Provider catalog — proxied from ProviderCatalogContext so every tab
   *  reads from the same source as the Sidebar dropdown and status bar. */
  subagentProviders: ProviderOption[];
  subagentTypes: Record<string, SubagentTypeInfo>;
  setSubagentTypes: (
    v:
      | Record<string, SubagentTypeInfo>
      | ((prev: Record<string, SubagentTypeInfo>) => Record<string, SubagentTypeInfo>),
  ) => void;
  currentProviderInfo: { provider: string; model: string; hasCredential: boolean } | null;
  setCurrentProviderInfo: (v: { provider: string; model: string; hasCredential: boolean } | null) => void;
  loadingProviderInfo: boolean;
  setLoadingProviderInfo: (v: boolean) => void;
  /** Same provider catalog, exposed under the legacy name the Commit & Review
   *  tab destructures. Kept as a separate field to avoid touching the tab's
   *  prop interface. */
  commitReviewProviders: ProviderOption[];
  // MCP server state
  editingServer: { mode: 'add' | 'edit'; originalName?: string } | null;
  setEditingServer: (v: { mode: 'add' | 'edit'; originalName?: string } | null) => void;
  serverName: string;
  setServerName: (v: string) => void;
  serverCommand: string;
  setServerCommand: (v: string) => void;
  serverArgs: string;
  setServerArgs: (v: string) => void;
  serverEnvVars: Array<{ key: string; value: string }>;
  setServerEnvVars: (v: Array<{ key: string; value: string }>) => void;
  newEnvKey: string;
  setNewEnvKey: (v: string) => void;
  newEnvValue: string;
  setNewEnvValue: (v: string) => void;
  // Credential management state
  credentialServer: string | null;
  setCredentialServer: (v: string | null) => void;
  credentialEntries: Array<{ key: string; value: string; status: string }>;
  setCredentialEntries: (
    v:
      | Array<{ key: string; value: string; status: string }>
      | ((
          prev: Array<{ key: string; value: string; status: string }>,
        ) => Array<{ key: string; value: string; status: string }>),
  ) => void;
  credentialLoading: boolean;
  setCredentialLoading: (v: boolean) => void;
  newCredentialKey: string;
  setNewCredentialKey: (v: string) => void;
  newCredentialValue: string;
  setNewCredentialValue: (v: string) => void;
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
  providerApiKey: string;
  setProviderApiKey: (v: string) => void;
  providerSupportsVision: boolean;
  setProviderSupportsVision: (v: boolean) => void;
  providerVisionModel: string;
  setProviderVisionModel: (v: string) => void;
  providerBillingType: 'pay_per_token' | 'subscription' | 'free';
  setProviderBillingType: (v: 'pay_per_token' | 'subscription' | 'free') => void;
  providerModelContextSizes: string;
  setProviderModelContextSizes: (v: string) => void;
  // Drafts
  textDrafts: Record<string, string>;
  setTextDrafts: (v: Record<string, string> | ((prev: Record<string, string>) => Record<string, string>)) => void;
  textSaveTimersRef: React.MutableRefObject<Record<string, ReturnType<typeof setTimeout>>>;
  // Services
  api: ReturnType<typeof ApiService.getInstance>;
  addNotification: ReturnType<typeof useNotifications>['addNotification'];
}

export function useSettingsState(
  settings: SproutSettings | null,
  onSettingsChanged: (settings: SproutSettings) => void,
  _onRequestProviderSetup?: () => void,
): UseSettingsStateReturn {
  const [activeSubTab, setActiveSubTab] = useState<SettingsSubTab>('general');
  const [textDrafts, setTextDrafts] = useState<Record<string, string>>({});

  const [configViewLayer, setConfigViewLayer] = useState<'session' | 'workspace' | 'global'>('session');
  const [layerLoading, setLayerLoading] = useState<string | null>(null);
  const [layerData, setLayerData] = useState<Record<string, unknown> | null>(null);
  const [layerError, setLayerError] = useState<string | null>(null);

  const displaySettingsRef = useRef<SproutSettings | null>(null);
  const [creatingWorkspaceConfig, setCreatingWorkspaceConfig] = useState(false);
  const [provenanceSources, setProvenanceSources] = useState<Record<string, string>>({});

  const { addNotification } = useNotifications();

  // MCP / Provider form state
  const [editingServer, setEditingServer] = useState<{
    mode: 'add' | 'edit';
    originalName?: string;
  } | null>(null);
  const [serverName, setServerName] = useState('');
  const [serverCommand, setServerCommand] = useState('');
  const [serverArgs, setServerArgs] = useState('');
  const [serverEnvVars, setServerEnvVars] = useState<Array<{ key: string; value: string }>>([]);
  const [newEnvKey, setNewEnvKey] = useState('');
  const [newEnvValue, setNewEnvValue] = useState('');

  // Credential management state
  const [credentialServer, setCredentialServer] = useState<string | null>(null);
  const [credentialEntries, setCredentialEntries] = useState<Array<{ key: string; value: string; status: string }>>([]);
  const [credentialLoading, setCredentialLoading] = useState(false);
  const [newCredentialKey, setNewCredentialKey] = useState('');
  const [newCredentialValue, setNewCredentialValue] = useState('');

  const [editingProvider, setEditingProvider] = useState<{
    mode: 'add' | 'edit';
    originalName?: string;
  } | null>(null);
  const [providerName, setProviderName] = useState('');
  const [providerApiBase, setProviderApiBase] = useState('');
  const [providerModelName, setProviderModelName] = useState('');
  const [providerContextSize, setProviderContextSize] = useState(32768);
  const [providerEnvVar, setProviderEnvVar] = useState('');
  const [providerApiKey, setProviderApiKey] = useState('');
  const [providerSupportsVision, setProviderSupportsVision] = useState(false);
  const [providerVisionModel, setProviderVisionModel] = useState('');
  const [providerBillingType, setProviderBillingType] = useState<'pay_per_token' | 'subscription' | 'free'>(
    'pay_per_token',
  );
  const [providerModelContextSizes, setProviderModelContextSizes] = useState<string>('');

  // Provider catalog is shared across the whole app — the Subagents,
  // Providers (primary chat), and Commit & Review tabs all read from the
  // same context as the Sidebar dropdown and bottom status bar. This used
  // to be a separate per-tab fetch of /api/settings/subagent-types, which
  // returned the same list but in a different order — guaranteeing visual
  // drift between Settings panes and the status bar.
  const catalog = useProviderCatalog();
  const subagentProviders = catalog.providers;
  const commitReviewProviders = catalog.providers;
  const [subagentTypes, setSubagentTypes] = useState<Record<string, SubagentTypeInfo>>({});

  // Current provider info for the Providers tab
  const [currentProviderInfo, setCurrentProviderInfo] = useState<{
    provider: string;
    model: string;
    hasCredential: boolean;
  } | null>(null);
  const [loadingProviderInfo, setLoadingProviderInfo] = useState(false);

  // eslint-disable-next-line react-hooks/exhaustive-deps -- singleton accessor is stable
  const api = useMemo(() => ApiService.getInstance(), []);
  const textSaveTimersRef = useRef<Record<string, ReturnType<typeof setTimeout>>>({});

  // Keep a ref to settings for async mutation callbacks.
  const settingsRef = useRef(settings);
  useEffect(() => {
    settingsRef.current = settings;
  }, [settings]);

  useEffect(() => {
    if (!settings) return;

    setTextDrafts((prev) => {
      let next = prev;
      let changed = false;

      Object.entries(prev).forEach(([key, draftValue]) => {
        const persistedValue = String(getNestedValue(settings, key) || '');
        if (draftValue === persistedValue) {
          if (next === prev) next = { ...prev };
          delete next[key];
          changed = true;
        }
      });

      return changed ? next : prev;
    });
  }, [settings]);

  // Fetch config layer data when layer changes
  useEffect(() => {
    if (configViewLayer !== 'session') {
      let cancelled = false;
      setLayerLoading(configViewLayer);
      setLayerError(null);
      api
        .getSettingsLayer(configViewLayer)
        .then((data) => {
          if (cancelled) return;
          setLayerData(data);
        })
        .catch((err) => {
          if (cancelled) return;
          console.error(`[SettingsPanel] failed to load ${configViewLayer} config:`, err);
          setLayerError(`Failed to load ${configViewLayer} config`);
          setLayerData(null);
        })
        .finally(() => {
          if (!cancelled) setLayerLoading(null);
        });
      return () => {
        cancelled = true;
      };
    } else {
      setLayerData(null);
      setLayerError(null);
      setLayerLoading(null);
      let cancelled = false;
      api
        .getSettingsProvenance()
        .then((data) => {
          if (!cancelled) setProvenanceSources(data.sources || {});
        })
        .catch((err) => {
          if (!cancelled) setProvenanceSources({});
          debugLog('[SettingsPanel] Failed to fetch settings provenance:', err);
        });
      return () => {
        cancelled = true;
      };
    }
  }, [activeSubTab, configViewLayer]);

  // Fetch subagent type / persona definitions when the subagents tab is
  // active. (The provider catalog is supplied by ProviderCatalogContext;
  // this fetch only needs the persona-specific fields.) Callers that just
  // need fresh providers should call refreshSubagentProviders below, which
  // refreshes the catalog rather than re-hitting this endpoint.
  useEffect(() => {
    if (activeSubTab !== 'subagents' && activeSubTab !== 'providers') return;
    let cancelled = false;
    (async () => {
      try {
        const data = await api.getSubagentTypes();
        if (cancelled) return;
        setSubagentTypes((data.subagent_types || {}) as Record<string, SubagentTypeInfo>);
      } catch (err) {
        debugLog('[SettingsPanel] failed to load subagent types:', err);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [activeSubTab, api]);

  // ProviderSettingsTab calls this after adding a custom provider so the
  // new entry appears in dropdowns immediately. Routing through the shared
  // catalog refreshes every consumer (status bar, Sidebar, all Settings
  // tabs) in one shot.
  const refreshSubagentProviders = catalog.refresh;

  // Fetch current provider info when providers tab is activated or
  // when refreshCurrentProviderInfo() is called externally (e.g. after
  // the inline primary-provider dropdown writes to global config).
  const [currentProviderRefreshTick, setCurrentProviderRefreshTick] = useState(0);
  const refreshCurrentProviderInfo = useCallback(() => {
    setCurrentProviderRefreshTick((n) => n + 1);
  }, []);
  useEffect(() => {
    if (activeSubTab !== 'providers') return;
    let cancelled = false;
    setLoadingProviderInfo(true);
    (async () => {
      try {
        const status = await api.getOnboardingStatus();
        if (cancelled) return;
        const providerEntry = (status.providers || []).find((p) => p.id === status.current_provider);
        setCurrentProviderInfo({
          provider: status.current_provider,
          model: status.current_model,
          hasCredential: providerEntry?.has_credential || false,
        });
      } catch (err) {
        debugLog('[SettingsPanel] failed to load provider info:', err);
      } finally {
        if (!cancelled) setLoadingProviderInfo(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [activeSubTab, api, currentProviderRefreshTick]);

  return {
    activeSubTab,
    setActiveSubTab,
    refreshCurrentProviderInfo,
    refreshSubagentProviders,
    configViewLayer,
    setConfigViewLayer,
    layerLoading,
    setLayerLoading,
    layerData,
    setLayerData,
    layerError,
    setLayerError,
    creatingWorkspaceConfig,
    setCreatingWorkspaceConfig,
    provenanceSources,
    setProvenanceSources,
    displaySettingsRef,
    subagentProviders,
    subagentTypes,
    setSubagentTypes,
    currentProviderInfo,
    setCurrentProviderInfo,
    loadingProviderInfo,
    setLoadingProviderInfo,
    commitReviewProviders,
    // MCP
    editingServer,
    setEditingServer,
    serverName,
    setServerName,
    serverCommand,
    setServerCommand,
    serverArgs,
    setServerArgs,
    serverEnvVars,
    setServerEnvVars,
    newEnvKey,
    setNewEnvKey,
    newEnvValue,
    setNewEnvValue,
    // Credentials
    credentialServer,
    setCredentialServer,
    credentialEntries,
    setCredentialEntries,
    credentialLoading,
    setCredentialLoading,
    newCredentialKey,
    setNewCredentialKey,
    newCredentialValue,
    setNewCredentialValue,
    // Provider
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
    providerApiKey,
    setProviderApiKey,
    providerSupportsVision,
    setProviderSupportsVision,
    providerVisionModel,
    setProviderVisionModel,
    providerBillingType,
    setProviderBillingType,
    providerModelContextSizes,
    setProviderModelContextSizes,
    // Drafts
    textDrafts,
    setTextDrafts,
    textSaveTimersRef,
    // Services
    api,
    addNotification,
  };
}
