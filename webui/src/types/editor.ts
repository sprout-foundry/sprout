export interface EditorBuffer {
  id: string;
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
}

export type PaneLayout = 'single' | 'split-vertical' | 'split-horizontal' | 'split-grid';

export interface EditorPane {
  id: string;
  bufferId: string | null; // null = empty pane
  isActive: boolean;
  position?: 'primary' | 'secondary' | 'tertiary';
  dimensions?: {
    width: number | string;
    height: number | string;
  };
}

export interface EditorState {
  activeBufferId: string | null;
  buffers: Map<string, EditorBuffer>;
  panes: EditorPane[];
  paneLayout: PaneLayout;
  activePaneId: string | null;
}
