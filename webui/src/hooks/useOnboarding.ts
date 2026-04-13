import { useState, useEffect, useCallback, useMemo } from 'react';
import type { OnboardingState } from '../types/app';
import type { OnboardingProviderOption } from '../services/api';
import { ApiService } from '../services/api';
import { notificationBus } from '../services/notificationBus';
import { debugLog } from '../utils/log';

export interface WindowsOnboardingGuidance {
  tone: string;
  title: string;
  body: string;
  checklist: string[];
  canInstallWsl: boolean;
  canInstallGitBash: boolean;
}

export interface UseOnboardingReturn {
  /** Full onboarding state object */
  onboarding: OnboardingState;
  /** Provider object matching the currently selected provider id */
  selectedProvider: OnboardingProviderOption | null;
  /** Providers flagged as recommended */
  recommendedProviders: OnboardingProviderOption[];
  /** Providers NOT flagged as recommended */
  advancedProviders: OnboardingProviderOption[];
  /** Windows-specific guidance panel, or null when not applicable */
  windowsGuidance: WindowsOnboardingGuidance | null;
  /** Re-fetch onboarding status from the backend (used on mount). */
  refreshStatus: () => Promise<void>;
  /** Refresh the provider list without changing dialog open state (used by dialog Refresh button). */
  refreshProviderList: () => Promise<void>;
  /** Callback: change the selected provider id */
  onProviderChange: (providerID: string) => void;
  /**
   * Callback: complete onboarding.
   * Receives a updater function so the parent can apply provider/model
   * changes to its own state.
   */
  onComplete: (applyAppState: (values: { provider: string; model: string }) => void) => Promise<void>;
  /** Callback: skip onboarding and use as editor-only mode */
  onSkip: () => Promise<void>;
  /** Force-open the onboarding dialog (for re-entering setup from editor-only mode) */
  openProviderSetup: () => Promise<void>;
  /** Callback: install WSL via the desktop bridge */
  onInstallWsl: () => Promise<void>;
  /** Callback: install Git for Windows via the desktop bridge */
  onInstallGitBash: () => Promise<void>;
  /** Partially update the onboarding state (value or updater function). */
  updateOnboarding: (patch: Partial<OnboardingState> | ((prev: OnboardingState) => OnboardingState)) => void;
}

function useOnboarding(): UseOnboardingReturn {
  const [onboarding, setOnboarding] = useState<OnboardingState>({
    checking: true,
    open: false,
    reason: '',
    providers: [],
    environment: null,
    provider: '',
    model: '',
    apiKey: '',
    showAllProviders: false,
    submitting: false,
    platformActionMessage: null,
    error: null,
    initialModelSet: false,
    keyError: false,
    validationSuccess: false,
    validationModelCount: 0,
    isReonboarding: false,
  });

  const apiService = ApiService.getInstance();

  // Stable reference so consumers can pass it around without breaking memoisation.
  const updateOnboarding = useCallback(
    (patch: Partial<OnboardingState> | ((prev: OnboardingState) => OnboardingState)) => {
      setOnboarding((prev) => (typeof patch === 'function' ? patch(prev) : { ...prev, ...patch }));
    },
    [],
  );

  const refreshStatus = useCallback(async () => {
    setOnboarding((prev) => ({ ...prev, checking: true, error: null }));
    try {
      const status = await apiService.getOnboardingStatus();
      const providers = Array.isArray(status.providers) ? status.providers : [];
      const preferredProvider =
        status.current_provider || providers.find((p) => p.recommended)?.id || providers[0]?.id || '';
      const providerInfo = providers.find((p) => p.id === preferredProvider) || providers[0];
      const preferredModel = status.current_model || providerInfo?.recommended_model || providerInfo?.models?.[0] || '';
      setOnboarding({
        checking: false,
        open: !!status.setup_required,
        reason: status.reason || '',
        providers,
        environment: status.environment || null,
        provider: preferredProvider,
        model: preferredModel,
        apiKey: '',
        showAllProviders: false,
        submitting: false,
        platformActionMessage: null,
        error: null,
        initialModelSet: true,
        keyError: false,
        validationSuccess: false,
        validationModelCount: 0,
        isReonboarding: false,
      });
    } catch (error) {
      debugLog('[useOnboarding] Failed to refresh setup status:', error);
      setOnboarding((prev) => ({
        ...prev,
        checking: false,
        open: true,
        showAllProviders: false,
        platformActionMessage: null,
        error: error instanceof Error ? error.message : 'Failed to check setup status',
        initialModelSet: false,
        validationSuccess: false,
        validationModelCount: 0,
        isReonboarding: false,
      }));
    }
  }, [apiService]);

  /**
   * Refresh the provider list without changing dialog visibility or
   * isReonboarding. Used by the dialog's "Refresh" button in both
   * first-run and re-onboarding modes.
   */
  const refreshProviderList = useCallback(async () => {
    setOnboarding((prev) => ({ ...prev, checking: true, error: null }));
    try {
      const status = await apiService.getOnboardingStatus();
      const providers = Array.isArray(status.providers) ? status.providers : [];
      const preferredProvider =
        status.current_provider || providers.find((p) => p.recommended)?.id || providers[0]?.id || '';
      const providerInfo = providers.find((p) => p.id === preferredProvider) || providers[0];
      const preferredModel = status.current_model || providerInfo?.recommended_model || providerInfo?.models?.[0] || '';
      setOnboarding((prev) => ({
        ...prev,
        checking: false,
        providers,
        provider: prev.provider || preferredProvider,
        model: prev.model || preferredModel,
      }));
    } catch (error) {
      debugLog('[useOnboarding] Failed to refresh provider list:', error);
      setOnboarding((prev) => ({
        ...prev,
        checking: false,
        error: error instanceof Error ? error.message : 'Failed to refresh provider list',
      }));
    }
  }, [apiService]);

  // Refresh onboarding status on mount
  useEffect(() => {
    refreshStatus().catch((err) => {
      debugLog('[useOnboarding] Failed to refresh setup status:', err);
      notificationBus.notify('warning', 'Onboarding', 'Failed to refresh setup status: ' + String(err));
    });
  }, [refreshStatus]);

  const selectedProvider = useMemo(() => {
    return onboarding.providers.find((p) => p.id === onboarding.provider) || null;
  }, [onboarding.provider, onboarding.providers]);

  const recommendedProviders = useMemo(() => {
    return onboarding.providers.filter((p) => p.recommended);
  }, [onboarding.providers]);

  const advancedProviders = useMemo(() => {
    return onboarding.providers.filter((p) => !p.recommended);
  }, [onboarding.providers]);

  const windowsGuidance = useMemo((): WindowsOnboardingGuidance | null => {
    const env = onboarding.environment;
    if (!env) {
      return null;
    }

    const isWindowsHost = env.host_platform === 'windows' || env.runtime_platform === 'windows';
    if (!isWindowsHost) {
      return null;
    }

    if (env.backend_mode === 'wsl') {
      return {
        tone: 'success',
        title: 'WSL mode is already active',
        body: 'This window is already using a WSL backend, which is the recommended setup for terminals, shell tools, and repo workflows on Windows.',
        checklist: [
          'Keep repos inside the WSL filesystem when practical.',
          'Use native Windows mode only when you specifically need Windows-only tools.',
          env.has_git_bash
            ? 'Git Bash is also available as a native Windows fallback.'
            : 'Git Bash is optional and only needed if you plan to use the native Windows backend.',
        ],
        canInstallWsl: false,
        canInstallGitBash: !env.has_git_bash,
      };
    }

    return {
      tone: env.has_wsl ? 'warning' : 'info',
      title: env.has_wsl
        ? 'Recommended: use WSL for the best Windows experience'
        : 'Recommended: install WSL before relying on shell-heavy workflows',
      body: env.has_wsl
        ? 'Native Windows mode can handle some tasks, but this app is built around Unix-style terminal behavior. WSL is the intended path.'
        : 'This app expects Unix-style shell and terminal behavior. WSL gives the best compatibility for chat tools, shell commands, and git workflows.',
      checklist: [
        env.has_wsl
          ? 'Reopen the project through the WSL-backed desktop mode when possible.'
          : 'Install WSL with an Ubuntu distro, then reopen the project through the WSL-backed desktop mode.',
        env.has_git_bash
          ? 'Git Bash is installed and can help with native Windows shell commands.'
          : 'Install Git for Windows if you want Git Bash as a native-Windows fallback for shell commands.',
        'Expect the native Windows backend to be less complete than the WSL path for terminal behavior.',
      ],
      canInstallWsl: !env.has_wsl,
      canInstallGitBash: !env.has_git_bash,
    };
  }, [onboarding.environment]);

  const onProviderChange = useCallback((providerID: string) => {
    setOnboarding((prev) => {
      const provider = prev.providers.find((p) => p.id === providerID);
      return {
        ...prev,
        provider: providerID,
        model: provider?.recommended_model || provider?.models?.[0] || '',
        apiKey: '',
        error: null,
        keyError: false,
        initialModelSet: true,
        validationSuccess: false,
        validationModelCount: 0,
      };
    });
  }, []);

  const onComplete = useCallback(
    async (applyAppState: (values: { provider: string; model: string }) => void) => {
      if (!onboarding.provider) {
        setOnboarding((prev) => ({ ...prev, error: 'Select a provider first.', keyError: false }));
        return;
      }
      if (selectedProvider?.requires_api_key && !selectedProvider.has_credential && !onboarding.apiKey.trim()) {
        setOnboarding((prev) => ({ ...prev, error: 'API key is required for this provider.', keyError: false }));
        return;
      }

      setOnboarding((prev) => ({ ...prev, submitting: true, error: null, keyError: false }));
      try {
        const response = await apiService.completeOnboarding({
          provider: onboarding.provider,
          model: onboarding.model || undefined,
          api_key: onboarding.apiKey.trim() || undefined,
        });
        // Apply the resolved provider/model to the parent app state
        applyAppState({
          provider: response.provider || onboarding.provider,
          model: response.model || onboarding.model,
        });
        if (response.validation?.tested) {
          // Show brief success feedback before closing the dialog.
          setOnboarding((prev) => ({
            ...prev,
            submitting: false,
            validationSuccess: true,
            validationModelCount: response.validation?.model_count || 0,
          }));
          setTimeout(() => {
            setOnboarding((prev) => ({
              ...prev,
              open: false,
              validationSuccess: false,
              validationModelCount: 0,
              apiKey: '',
            }));
          }, 1500);
        } else {
          setOnboarding((prev) => ({
            ...prev,
            open: false,
            submitting: false,
            apiKey: '',
            keyError: false,
          }));
        }
      } catch (error) {
        debugLog('[useOnboarding] Failed to complete setup:', error);
        // Detect API key validation failures from structured error codes.
        const isKeyError = (error as Error & { code?: string })?.code === 'api_key_invalid'
          || (error instanceof Error && /api key.*(?:invalid|failed|validation)/i.test(error.message));
        setOnboarding((prev) => ({
          ...prev,
          submitting: false,
          error: error instanceof Error ? error.message : 'Failed to complete setup',
          keyError: !!isKeyError,
          validationSuccess: false,
          validationModelCount: 0,
        }));
      }
    },
    [apiService, onboarding.apiKey, onboarding.model, onboarding.provider, selectedProvider],
  );

  const onSkip = useCallback(async () => {
    setOnboarding((prev) => ({ ...prev, submitting: true, error: null }));
    try {
      await apiService.skipOnboarding();
      setOnboarding((prev) => ({
        ...prev,
        open: false,
        submitting: false,
        provider: 'editor',
      }));
    } catch (error) {
      debugLog('[useOnboarding] Failed to skip setup:', error);
      setOnboarding((prev) => ({
        ...prev,
        submitting: false,
        error: error instanceof Error ? error.message : 'Failed to skip setup',
      }));
    }
  }, [apiService]);

  const openProviderSetup = useCallback(async () => {
    setOnboarding((prev) => ({ ...prev, checking: true, error: null }));
    try {
      const status = await apiService.getOnboardingStatus();
      const providers = Array.isArray(status.providers) ? status.providers : [];
      const preferredProvider =
        status.current_provider || providers.find((p) => p.recommended)?.id || providers[0]?.id || '';
      const providerInfo = providers.find((p) => p.id === preferredProvider) || providers[0];
      const preferredModel = status.current_model || providerInfo?.recommended_model || providerInfo?.models?.[0] || '';
      setOnboarding({
        checking: false,
        open: true,
        reason: status.reason || '',
        providers,
        environment: status.environment || null,
        provider: preferredProvider,
        model: preferredModel,
        apiKey: '',
        showAllProviders: false,
        submitting: false,
        platformActionMessage: null,
        error: null,
        initialModelSet: true,
        keyError: false,
        validationSuccess: false,
        validationModelCount: 0,
        isReonboarding: true,
      });
    } catch (error) {
      debugLog('[useOnboarding] Failed to open provider setup:', error);
      setOnboarding((prev) => ({
        ...prev,
        checking: false,
        open: true,
        showAllProviders: false,
        platformActionMessage: null,
        error: error instanceof Error ? error.message : 'Failed to load provider setup',
        keyError: false,
        validationSuccess: false,
        validationModelCount: 0,
        isReonboarding: true,
      }));
    }
  }, [apiService]);

  const onInstallWsl = useCallback(async () => {
    const desktopBridge = (
      window as unknown as Record<string, Record<string, (...args: unknown[]) => Promise<Record<string, unknown>>>>
    ).leditDesktop;
    if (!desktopBridge?.installWsl) {
      setOnboarding((prev) => ({
        ...prev,
        platformActionMessage: 'WSL installation is only available from the desktop app.',
      }));
      return;
    }
    const result = await desktopBridge.installWsl();
    const msg = result?.message != null ? String(result.message) : null;
    setOnboarding((prev) => ({ ...prev, platformActionMessage: msg || 'Started WSL setup.' }));
  }, []);

  const onInstallGitBash = useCallback(async () => {
    const desktopBridge = (
      window as unknown as Record<string, Record<string, (...args: unknown[]) => Promise<Record<string, unknown>>>>
    ).leditDesktop;
    if (!desktopBridge?.installGitForWindows) {
      setOnboarding((prev) => ({
        ...prev,
        platformActionMessage: 'Git Bash installation is only available from the desktop app.',
      }));
      return;
    }
    const result = await desktopBridge.installGitForWindows();
    const msg = result?.message != null ? String(result.message) : null;
    setOnboarding((prev) => ({
      ...prev,
      platformActionMessage: msg || 'Started Git for Windows setup.',
    }));
  }, []);

  return {
    onboarding,
    selectedProvider,
    recommendedProviders,
    advancedProviders,
    windowsGuidance,
    refreshStatus,
    refreshProviderList,
    onProviderChange,
    onComplete,
    onSkip,
    openProviderSetup,
    onInstallWsl,
    onInstallGitBash,
    updateOnboarding,
  };
}

export default useOnboarding;
