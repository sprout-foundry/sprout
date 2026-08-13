/**
 * Plugin Registry — singleton that stores installed plugins.
 *
 * Provides getters for all registered views, panels, and settings tabs
 * (flattened from all plugins). Allows registration at any time and
 * dispatches a custom event `sprout:plugins-changed` when plugins are
 * added or removed so React components can re-render.
 */

import type { PluginView, PluginPanel, PluginSettingsTab, SproutPlugin } from '../types/plugin';

/** Custom event dispatched when the plugin registry changes. */
const PLUGINS_CHANGED_EVENT = 'sprout:plugins-changed';

/** Dispatch a custom event to notify React consumers of registry changes. */
function dispatchChanged(): void {
  if (typeof window === 'undefined') return;
  window.dispatchEvent(new CustomEvent(PLUGINS_CHANGED_EVENT));
}

/**
 * Internal registry state.
 */
class PluginRegistryImpl {
  private plugins = new Map<string, SproutPlugin>();

  /** Register a plugin. Replaces any existing plugin with the same ID. */
  register(plugin: SproutPlugin): void {
    this.plugins.set(plugin.id, plugin);
    dispatchChanged();
  }

  /** Unregister a plugin by ID. */
  unregister(pluginId: string): void {
    this.plugins.delete(pluginId);
    dispatchChanged();
  }

  /** Get a specific plugin by ID, or undefined if not registered. */
  get(pluginId: string): SproutPlugin | undefined {
    return this.plugins.get(pluginId);
  }

  /** All registered plugin IDs. */
  getPluginIds(): readonly string[] {
    return Array.from(this.plugins.keys());
  }

  /** All registered views, flattened from all plugins and sorted by order. */
  getPluginViews(): readonly PluginView[] {
    const views: PluginView[] = [];
    for (const plugin of this.plugins.values()) {
      if (plugin.views) {
        views.push(...plugin.views);
      }
    }
    return views.sort((a, b) => (a.order ?? Infinity) - (b.order ?? Infinity));
  }

  /** All registered sidebar panels, flattened from all plugins. */
  getPluginPanels(): readonly PluginPanel[] {
    const panels: PluginPanel[] = [];
    for (const plugin of this.plugins.values()) {
      if (plugin.panels) {
        panels.push(...plugin.panels);
      }
    }
    return panels;
  }

  /** All registered settings tabs, flattened from all plugins. */
  getPluginSettingsTabs(): readonly PluginSettingsTab[] {
    const tabs: PluginSettingsTab[] = [];
    for (const plugin of this.plugins.values()) {
      if (plugin.settingsTabs) {
        tabs.push(...plugin.settingsTabs);
      }
    }
    return tabs;
  }

  /** All registered plugin view IDs (used by persistence validator). */
  getPluginViewIds(): readonly string[] {
    const ids: string[] = [];
    for (const plugin of this.plugins.values()) {
      if (plugin.views) {
        ids.push(...plugin.views.map((v) => v.id));
      }
    }
    return ids;
  }
}

/** Singleton plugin registry instance. */
const pluginRegistry = new PluginRegistryImpl();

export { pluginRegistry, PLUGINS_CHANGED_EVENT };

/**
 * Get all registered plugin view IDs.
 * Used by the persistence validator to accept plugin view IDs.
 */
export function getPluginViewIds(): readonly string[] {
  return pluginRegistry.getPluginViewIds();
}

/**
 * Get all registered plugin views.
 */
export function getPluginViews(): readonly PluginView[] {
  return pluginRegistry.getPluginViews();
}

/**
 * Get all registered plugin panels.
 */
export function getPluginPanels(): readonly PluginPanel[] {
  return pluginRegistry.getPluginPanels();
}

/**
 * Get all registered plugin settings tabs.
 */
export function getPluginSettingsTabs(): readonly PluginSettingsTab[] {
  return pluginRegistry.getPluginSettingsTabs();
}

// ── External plugin registration bridge ──────────────────────────────
// Allow externally-loaded IIFE plugin bundles to register themselves.
// Supports two mechanisms:
// 1. Global function: window.__sproutRegisterPlugin(plugin)
// 2. Custom event: document.addEventListener('sprout:register-plugin', ...)
if (typeof window !== 'undefined') {
  (window as unknown as { __sproutRegisterPlugin?: (p: SproutPlugin) => void }).__sproutRegisterPlugin = (
    plugin: SproutPlugin,
  ) => {
    pluginRegistry.register(plugin);
  };
}

if (typeof document !== 'undefined') {
  document.addEventListener('sprout:register-plugin', (event: Event) => {
    const customEvent = event as CustomEvent<SproutPlugin>;
    if (customEvent.detail && typeof customEvent.detail === 'object' && 'id' in customEvent.detail) {
      pluginRegistry.register(customEvent.detail);
    }
  });
}
