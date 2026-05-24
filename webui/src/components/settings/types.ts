import type { SproutSettings, ProviderOption, ApiService } from '../../services/api';
import type { SubagentTypeInfo } from '../../services/api/types';

/**
 * Shared context passed to domain-specific mutation hooks.
 * Contains common services and state needed by all mutation operations.
 */
export interface MutationContext {
  /** API service instance */
  api: ReturnType<typeof ApiService.getInstance>;
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
  /** Refresh the provider catalog. Called by provider CRUD mutations so
   *  newly added/updated/deleted custom providers appear immediately in
   *  the ProviderSettingsTab and SubagentSettingsTab dropdowns. */
  refreshProviderCatalog?: () => void;
}

/** @deprecated Use SubagentTypeInfo from services/api/types */
export type SubagentTypeEntry = SubagentTypeInfo;

/**
 * Legacy flat tab IDs — kept for backward compatibility with useSettingsState.
 * New code should use SettingsSection / SettingsSubsection from SECTION_GROUPS.
 */
export type SettingsSubTab =
  | 'general'
  | 'security'
  | 'credentials'
  | 'performance'
  | 'subagents'
  | 'commit-review'
  | 'pdf-ocr'
  | 'mcp'
  | 'providers'
  | 'skills'
  | 'embeddings'
  | 'roles';

export interface EditorPreferences {
  autoSaveEnabled: boolean;
  whitespaceRenderingMode: 'none' | 'boundary' | 'all';
  formatOnSaveEnabled?: boolean;
}

/** Props for rendering provider/model/persona selectors inside a section body */
export interface AgentConfigProps {
  selectedProvider: string;
  selectedModel: string;
  selectedPersona: string;
  providers: Array<{ id: string; name: string }>;
  availableModels: string[];
  personas: Array<{ id: string; name: string }>;
  isLoadingProviders: boolean;
  isLoadingPersonas: boolean;
  isConnected: boolean;
  onProviderChange: (val: string) => void;
  onModelChange: (val: string) => void;
  onPersonaChange: (val: string) => void;
}

export interface SettingsPanelProps {
  settings: SproutSettings | null;
  onSettingsChanged: (settings: SproutSettings) => void;
  /** Callback to open the provider setup/onboarding dialog */
  onRequestProviderSetup?: () => void;
  editorPreferences?: EditorPreferences | null;
  onEditorPreferenceChanged?: (key: string, value: unknown) => void;
  /** Provider/model/persona data for the Agent section */
  agentConfig?: AgentConfigProps | null;
}

/* ─── Hierarchical section model (SP-017) ──────────────────── */

export type SettingsSection = 'agent' | 'workspace' | 'environment' | 'editor';

export type SettingsSubsection =
  // Agent (session scope)
  | 'agent-general'
  | 'agent-behavior'
  | 'agent-subagents'
  | 'agent-skills'
  | 'agent-roles'
  // Workspace (workspace scope)
  | 'workspace-embeddings'
  | 'workspace-mcp'
  // Environment (global scope)
  | 'env-providers'
  | 'env-performance'
  | 'env-commit-review'
  | 'env-ocr'
  // Editor
  | 'editor-preferences';

export interface SectionDef {
  id: SettingsSection;
  label: string;
  scope: 'session' | 'workspace' | 'global' | 'runtime';
  description: string;
  subsections: { id: SettingsSubsection; label: string }[];
}

export const SECTION_GROUPS: SectionDef[] = [
  {
    id: 'agent',
    label: 'Agent',
    scope: 'session',
    description: 'How the agent behaves this session',
    subsections: [
      { id: 'agent-general', label: 'General' },
      { id: 'agent-behavior', label: 'Security' },
      { id: 'agent-subagents', label: 'Subagents' },
      { id: 'agent-skills', label: 'Skills' },
      { id: 'agent-roles', label: 'Roles' },
    ],
  },
  {
    id: 'workspace',
    label: 'Workspace',
    scope: 'workspace',
    description: 'Settings for this project directory',
    subsections: [
      { id: 'workspace-embeddings', label: 'Embeddings' },
      { id: 'workspace-mcp', label: 'MCP Servers' },
    ],
  },
  {
    id: 'environment',
    label: 'Environment',
    scope: 'global',
    description: 'Global infrastructure config (~/.config/sprout)',
    subsections: [
      { id: 'env-providers', label: 'Providers' },
      { id: 'env-performance', label: 'Performance' },
      { id: 'env-commit-review', label: 'Commit & Review' },
      { id: 'env-ocr', label: 'PDF OCR' },
    ],
  },
  {
    id: 'editor',
    label: 'Editor',
    scope: 'runtime',
    description: 'Editor display preferences (this session only)',
    subsections: [{ id: 'editor-preferences', label: 'Display' }],
  },
];

/** Map a subsection ID to its parent section. */
export function getSectionForSubsection(subsectionId: SettingsSubsection): SectionDef | undefined {
  return SECTION_GROUPS.find((s) => s.subsections.some((sub) => sub.id === subsectionId));
}

/** Derive configViewLayer from a section scope. */
export function scopeToLayer(scope: SectionDef['scope']): 'session' | 'workspace' | 'global' {
  if (scope === 'session' || scope === 'runtime') return 'session';
  if (scope === 'workspace') return 'workspace';
  return 'global';
}

/**
 * Map a subsection ID to the legacy SettingsSubTab used by useSettingsState.
 * This keeps the internal fetch effects in useSettingsState working.
 */
export function subsectionToLegacyTab(subsectionId: SettingsSubsection): SettingsSubTab {
  const map: Record<SettingsSubsection, SettingsSubTab> = {
    'agent-general': 'general',
    'agent-behavior': 'security',
    'agent-subagents': 'subagents',
    'agent-skills': 'skills',
    'agent-roles': 'roles',
    'workspace-embeddings': 'embeddings',
    'workspace-mcp': 'mcp',
    'env-providers': 'providers',
    'env-performance': 'performance',
    'env-commit-review': 'commit-review',
    'env-ocr': 'pdf-ocr',
    'editor-preferences': 'general',
  };
  return map[subsectionId];
}

export type { SproutSettings, ProviderOption };
