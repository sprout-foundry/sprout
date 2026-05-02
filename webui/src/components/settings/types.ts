import type { SproutSettings, ProviderOption } from '../../services/api';

export interface SubagentTypeEntry {
  id: string;
  name: string;
  description: string;
  provider: string;
  model: string;
  system_prompt: string;
  system_prompt_text?: string;
  allowed_tools: string[];
  aliases: string[];
  enabled: boolean;
}

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
