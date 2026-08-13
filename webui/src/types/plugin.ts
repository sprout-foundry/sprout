/**
 * Plugin API types for the Sprout WebUI.
 *
 * These types define the plugin contract that allows external code
 * (e.g., the proprietary platform) to register React views, sidebar
 * panels, and settings tabs that render inside the webui's layout.
 */

import type { ComponentType } from 'react';

// ── Plugin Views ──────────────────────────────────────────────────────

/**
 * A main view that takes over the editor workspace area.
 * Registered by plugins to provide full-page experiences (e.g. dashboard, billing).
 */
export interface PluginView {
  /** Unique view ID (e.g. 'billing', 'dashboard') */
  readonly id: string;
  /** Display label for nav */
  readonly label: string;
  /** Icon name from the lucide icon map (same names PlatformNavItem uses) */
  readonly icon?: string;
  /** Sort order in the nav rail */
  readonly order?: number;
  /** The React component to render when this view is active */
  readonly component: ComponentType<PluginViewProps>;
}

/**
 * Props passed to a plugin view component.
 */
export interface PluginViewProps {
  /** Called when the view wants to navigate back to chat */
  readonly onBack: () => void;
  /** Called when the view wants to navigate to another view */
  readonly onNavigate: (viewId: string) => void;
}

// ── Plugin Panels ─────────────────────────────────────────────────────

/**
 * A sidebar panel that renders in the sidebar content pane alongside
 * the built-in panels (files, git, search, settings).
 */
export interface PluginPanel {
  /** Unique panel ID (e.g. 'tasks-panel') */
  readonly id: string;
  /** Display label for the sidebar tab */
  readonly label: string;
  /** Icon name from the lucide icon map */
  readonly icon?: string;
  /** The React component to render in the sidebar content pane */
  readonly component: ComponentType;
}

// ── Plugin Settings Tabs ──────────────────────────────────────────────

/**
 * A tab in the settings panel.
 */
export interface PluginSettingsTab {
  readonly id: string;
  readonly label: string;
  readonly component: ComponentType;
}

// ── Plugin Definition ─────────────────────────────────────────────────

/**
 * A plugin definition. Plugins register views, panels, and settings tabs.
 * The platform repo will implement a plugin that registers dashboard,
 * billing, tasks, workspaces, and team as views.
 */
export interface SproutPlugin {
  /** Unique plugin ID */
  readonly id: string;
  /** Main views that take over the editor area */
  readonly views?: readonly PluginView[];
  /** Sidebar panels */
  readonly panels?: readonly PluginPanel[];
  /** Settings tabs */
  readonly settingsTabs?: readonly PluginSettingsTab[];
}

/**
 * Registration function exported by a plugin entry point.
 * Returns a SproutPlugin definition.
 */
export type PluginRegistration = () => SproutPlugin | Promise<SproutPlugin>;
