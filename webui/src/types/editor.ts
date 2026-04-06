export type EditorBufferKind = 'file' | 'chat' | 'diff' | 'review' | 'welcome';

export interface EditorBuffer {
  id: string;
  kind: EditorBufferKind;
  file: {
    name: string;
    path: string;
    isDir: boolean;
    size: number;
    modified: number;
    ext?: string;
  };
  content: string;
  originalContent: string; // Content when file was loaded/reset
  cursorPosition: {
    line: number;
    column: number;
  };
  scrollPosition: {
    top: number;
    left: number;
  };
  isModified: boolean;
  isActive: boolean; // Currently displayed in an editor pane
  paneId?: string | null; // Which pane is displaying this buffer
  isPinned?: boolean;
  isClosable?: boolean;
  externallyModified?: boolean;
  diskContent?: string | null;
  metadata?: Record<string, unknown>;
  languageOverride?: string | null; // Language mode override (null = auto-detect by extension)
}

export type PaneLayout = 'single' | 'split-vertical' | 'split-horizontal' | 'split-grid';

export interface EditorPane {
  id: string;
  bufferId: string | null; // null = empty pane
  isActive: boolean;
  position?: 'primary' | 'secondary' | 'tertiary' | 'quaternary';
  dimensions?: {
    width: number | string;
    height: number | string;
  };
}

export interface PaneSize {
  [paneId: string]: number; // Size in percentage for pane sizing
}

export interface EditorState {
  activeBufferId: string | null;
  buffers: Map<string, EditorBuffer>;
  panes: EditorPane[];
  paneLayout: PaneLayout;
  activePaneId: string | null;
}
