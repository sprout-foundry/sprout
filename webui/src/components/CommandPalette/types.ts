import type { SymbolKind } from '../../utils/symbolUtils';

// ── Public Props ────────────────────────────────────────────────────────────

export type PaletteMode = 'all' | 'files' | 'symbols' | 'commands';

export interface CommandPaletteProps {
  isOpen: boolean;
  onClose: () => void;
  onOpenFile: (filePath: string) => void;
  onToggleSidebar: () => void;
  onToggleTerminal: () => void;
  onOpenHotkeysConfig: () => void;
  /** Mode to open with (e.g. Cmd+P → 'files', Cmd+Shift+O → 'symbols') */
  initialMode?: PaletteMode;
  /** Navigate to a line in the active editor (for symbol results) */
  onNavigateToLine?: (line: number) => void;
  /** Content of the active buffer (for symbol extraction) */
  activeBufferContent?: string;
  /** File extension of the active buffer (for symbol extraction) */
  activeBufferFileExtension?: string;
}

// ── Internal Data Types ────────────────────────────────────────────────────

export interface FileResult {
  name: string;
  path: string;
  type: string;
}

export interface CommandDef {
  id: string;
  label: string;
  category: string;
}

export type ResultKind = 'command' | 'file' | 'symbol' | 'commands-header' | 'files-header' | 'symbols-header';

export interface PaletteResult {
  kind: ResultKind;
  commandId?: string;
  commandLabel?: string;
  filePath?: string;
  fileName?: string;
  fileDirectory?: string;
  secondaryHighlightedLabel?: string;
  highlightedLabel: string;
  score: number;
  symbolLine?: number;
  symbolKind?: SymbolKind;
  scopePath?: string;
}
