/**
 * Plugin Context — React context that subscribes to the plugin registry.
 *
 * Provides registered plugin views, panels, and settings tabs to consumers.
 * Listens for the `sprout:plugins-changed` custom event to trigger re-renders
 * when plugins are registered or unregistered after the initial mount.
 */

import { createContext, useContext, useEffect, useState, useMemo, type ReactNode } from 'react';
import type { PluginView, PluginPanel, PluginSettingsTab } from '../types/plugin';
import { getPluginViews, getPluginPanels, getPluginSettingsTabs, PLUGINS_CHANGED_EVENT } from '../services/pluginRegistry';

interface PluginContextValue {
  /** All registered plugin views (sorted by order). */
  pluginViews: readonly PluginView[];
  /** All registered plugin panels. */
  pluginPanels: readonly PluginPanel[];
  /** All registered plugin settings tabs. */
  pluginSettingsTabs: readonly PluginSettingsTab[];
}

const PluginContext = createContext<PluginContextValue>({
  pluginViews: [],
  pluginPanels: [],
  pluginSettingsTabs: [],
});

/**
 * Hook to access the plugin context. Returns empty arrays when used
 * outside the provider (defensive default).
 */
export function usePlugins(): PluginContextValue {
  return useContext(PluginContext);
}

/**
 * Provider component that wraps the app and subscribes to the plugin registry.
 */
export function PluginContextProvider({ children }: { children: ReactNode }): JSX.Element {
  // Tick state to force re-read from the registry on each event.
  const [, setTick] = useState(0);

  useEffect(() => {
    const handler = () => setTick((t) => t + 1);
    if (typeof window !== 'undefined') {
      window.addEventListener(PLUGINS_CHANGED_EVENT, handler);
    }
    return () => {
      if (typeof window !== 'undefined') {
        window.removeEventListener(PLUGINS_CHANGED_EVENT, handler);
      }
    };
  }, []);

  const value = useMemo<PluginContextValue>(() => {
    return {
      pluginViews: getPluginViews(),
      pluginPanels: getPluginPanels(),
      pluginSettingsTabs: getPluginSettingsTabs(),
    };
  }, [setTick]);

  return <PluginContext.Provider value={value}>{children}</PluginContext.Provider>;
}

export { PluginContext };
