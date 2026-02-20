/**
 * Provider System Types
 *
 * Defines the architecture for managing view-specific content in the sidebar
 * and other UI components in an extensible, maintainable, data-driven way.
 */

import { ReactNode } from 'react';

/** Generic action that can be performed by providers */
export interface Action {
  type: string;
  payload?: any;
}

/** Result of handling an action */
export interface ActionResult {
  success: boolean;
  error?: string;
  data?: any;
}

/** Context provided to content providers */
export interface ProviderContext {
  // Application state
  isConnected: boolean;
  currentView: string;

  // Callbacks for common actions
  onFileClick?: (filePath: string) => void;
  onModelChange?: (model: string) => void;

  // Data accessors (not hardcoded - providers fetch what they need)
  recentFiles: Array<{ path: string; modified: boolean }>;
  recentLogs: any[];
  stats?: {
    queryCount: number;
    filesModified: number;
  };
}

/** Data source configuration - not hardcoded content */
export interface DataSource {
  type: 'api' | 'state' | 'websocket';
  endpoint?: string;
  eventType?: string;
  transform?: (data: any) => any;
}

/** Section definition - data-driven, not hardcoded */
export interface SidebarSection {
  id: string;
  dataSource: DataSource;
  renderItem: (item: any, context: ProviderContext) => ReactNode;
  title?: (data: any) => string;
  emptyMessage?: string;
  order?: number;
}

/**
 * Base interface for all content providers
 *
 * Providers should define WHAT data they need, not WHAT content to show.
 * The actual rendering is data-driven based on the data source.
 */
export interface ContentProvider {
  /** Unique identifier for this provider */
  readonly id: string;

  /** View type this provider handles */
  readonly viewType: string;

  /** Human-readable name */
  readonly name: string;

  /**
   * Get the sidebar section definitions for this view
   * Returns DATA SOURCES, not hardcoded content
   * @param context - Provider context with app state and callbacks
   * @returns Array of section definitions with data sources
   */
  getSections(context: ProviderContext): SidebarSection[];

  /**
   * Handle an action triggered from the UI
   * @param action - Action to handle
   * @param context - Provider context
   * @returns Action result
   */
  handleAction?(action: Action, context: ProviderContext): ActionResult;

  /**
   * Optional subscription to provider state changes
   * @param listener - Callback to invoke when provider state changes
   * @returns Unsubscribe function
   */
  subscribe?(listener: () => void): () => void;

  /**
   * Optional cleanup when provider is unregistered
   */
  cleanup?(): void;
}
