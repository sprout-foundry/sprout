import type { SproutSettings, ProviderOption } from '../../services/api';
import type { SubagentTypeInfo } from '../../services/api/types';

/**
 * Shared context passed to domain-specific mutation hooks.
 * Contains common services and state needed by all mutation operations.
 */
export interface MutationContext {
  /** API service instance */
  api: ReturnType<typeof import('../../services/api').ApiService.getInstance>;
  /** Reference to current settings (kept up-to-date by useSettingsState) */
  settingsRef: React.MutableRefObject<SproutSettings | null>;
  /** Callback to notify parent component of settings changes */
  onSettingsChanged: (settings: SproutSettings) => void;
  /** Function to show toast notifications */
  addNotification: (type: 'success' | 'error' | 'info', title: string, message: string, duration?: number) => string;
  /** Current settings layer ('session' | 'workspace' | 'global') */
  configViewLayer: 'session' | 'workspace' | 'global';
  /** Callback to update provenance sources (for session layer) */
  setProvenanceSources: (v: Record<string, string>) => void;
  /** Setter for savingKey (to update loading indicators) */
  setSavingKey: (key: string | null) => void;
}

/** @deprecated Use SubagentTypeInfo from services/api/types */
export type SubagentTypeEntry = SubagentTypeInfo;

export type SettingsSubTab = 'general' | 'security' | 'credentials' | 'performance' | 'subagents' | 'commit-review' | 'pdf-ocr' | 'mcp' | 'providers' | 'skills';

export interface EditorPreferences {
  autoSaveEnabled: boolean;
  whitespaceRenderingMode: 'none' | 'boundary' | 'all';
  formatOnSaveEnabled?: boolean;
}

export interface SettingsPanelProps {
  settings: SproutSettings | null;
  onSettingsChanged: (settings: SproutSettings) => void;
  /** Callback to open the provider setup/onboarding dialog */
  onRequestProviderSetup?: () => void;
  editorPreferences?: EditorPreferences | null;
  onEditorPreferenceChanged?: (key: string, value: unknown) => void;
}

export const SUB_TABS: { id: SettingsSubTab; label: string }[] = [
  { id: 'general', label: 'General' },
  { id: 'security', label: 'Security' },
  { id: 'credentials', label: 'Credentials' },
  { id: 'performance', label: 'Perf' },
  { id: 'subagents', label: 'Subagents' },
  { id: 'commit-review', label: 'Commit & Review' },
  { id: 'pdf-ocr', label: 'OCR' },
  { id: 'mcp', label: 'MCP' },
  { id: 'providers', label: 'Providers' },
  { id: 'skills', label: 'Skills' },
];

export type { SproutSettings, ProviderOption };
