/**
 * The editor unit: the open files, the active file, and opening, closing and
 * saving them, behind one provider.
 *
 * The buffers themselves live in the buffer manager that `SproutProviders`
 * mounts (`EditorManagerProvider`); every editor surface reads the same
 * store. This unit is the host-facing side of it: files addressed by path
 * rather than by buffer id, and the "open this path (at this line)" plumbing
 * the app root used to keep to itself. The standalone app mounts it inside
 * its provider stack and routes its own file opens through it; a composed
 * `SproutWorkspace` mounts it in own mode, so an arranged `editor` view and
 * the file tree share one editor.
 *
 * It must be mounted under `SproutProviders` (it reads the buffer manager).
 */

import type { FileInfo, FileTreeProps } from '@sprout/ui';
import { createContext, useCallback, useContext, useMemo, type ReactNode } from 'react';
import { useEditorManager } from '../contexts/EditorManagerContext';
import { getWorkspaceRootPath, resolveEditorFilePath } from '../services/lspClientService';
import type { EditorBuffer } from '../types/editor';

/** A file open in the editor. */
export interface WorkspaceEditorFile {
  /** The buffer id the editor tracks the file under. */
  bufferId: string;
  /** The file's path as the editor holds it (absolute when the workspace root is known). */
  path: string;
  /** The path relative to the workspace root — the form the file tree uses. */
  workspacePath: string;
  /** The file name. */
  name: string;
  /** Whether the buffer has unsaved edits. */
  isModified: boolean;
}

export interface OpenWorkspaceFileOptions {
  /** A 1-based line to move the cursor to once the file is shown. */
  line?: number;
}

export interface WorkspaceEditorValue {
  /** The files open in the editor, in tab order. */
  openFiles: WorkspaceEditorFile[];
  /** The file the editor is showing, or null when it shows none. */
  activeFile: WorkspaceEditorFile | null;
  /**
   * Open a file (or switch to it when already open) and make it the active
   * one. Returns the buffer id.
   */
  openFile: (path: string, options?: OpenWorkspaceFileOptions) => string;
  /** Close an open file. Unknown paths are ignored. */
  closeFile: (path: string) => Promise<void>;
  /**
   * Save an open file — the active one when `path` is omitted. An explicit
   * save, so it also resolves a pending external-change conflict in favour of
   * the buffer.
   */
  saveFile: (path?: string) => Promise<void>;
}

/** What an arranged workspace's views take from the editor unit. */
export interface WorkspaceEditorProps {
  /** Tree selection wired to the editor: selecting a file opens it; the active file is selected. */
  fileTree: Pick<FileTreeProps, 'onFileSelect' | 'selectedFile'>;
  /** Open a path, optionally at a line — the shape `AgentChangesPanel`'s `onFileClick` takes. */
  onFileClick: (path: string, line?: number) => void;
}

export interface WorkspaceEditorProviderProps {
  children: ReactNode;
}

const WorkspaceEditorContext = createContext<WorkspaceEditorValue | null>(null);

function fileNameOf(path: string): string {
  return path.split('/').filter(Boolean).pop() || path;
}

function extensionOf(name: string): string {
  const index = name.lastIndexOf('.');
  return index > 0 ? name.slice(index) : '';
}

/** The tree's form of an editor path: relative to the workspace root when it is known. */
function toWorkspacePath(path: string): string {
  const root = getWorkspaceRootPath();
  if (!root) return path;
  const prefix = `${root.replace(/\/+$/, '')}/`;
  return path.startsWith(prefix) ? path.slice(prefix.length) : path;
}

function toEditorFile(buffer: EditorBuffer): WorkspaceEditorFile {
  return {
    bufferId: buffer.id,
    path: buffer.file.path,
    workspacePath: toWorkspacePath(buffer.file.path),
    name: buffer.file.name,
    isModified: buffer.isModified,
  };
}

/** How long to wait for a freshly opened buffer's editor to mount before moving its cursor. */
const GOTO_LINE_DELAY_MS = 100;

export function WorkspaceEditorProvider({ children }: WorkspaceEditorProviderProps): JSX.Element {
  const { buffers, buffersRef, activeBufferId, openFile: openBuffer, closeBuffer, saveBuffer } = useEditorManager();

  const findBufferId = useCallback(
    (path: string): string | null => {
      const target = resolveEditorFilePath(path);
      for (const [id, buffer] of buffersRef.current.entries()) {
        if (buffer.kind === 'file' && resolveEditorFilePath(buffer.file.path) === target) return id;
      }
      return null;
    },
    [buffersRef],
  );

  const openFile = useCallback(
    (path: string, options?: OpenWorkspaceFileOptions) => {
      const name = fileNameOf(path);
      const bufferId = openBuffer({ path, name, isDir: false, size: 0, modified: 0, ext: extensionOf(name) });
      const line = options?.line;
      if (typeof line === 'number') {
        // The goto-line event is dropped by an editor that has not mounted the
        // buffer yet, so it waits for the pane to render it.
        setTimeout(() => {
          document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line } }));
        }, GOTO_LINE_DELAY_MS);
      }
      return bufferId;
    },
    [openBuffer],
  );

  const closeFile = useCallback(
    async (path: string) => {
      const id = findBufferId(path);
      if (id) await closeBuffer(id);
    },
    [findBufferId, closeBuffer],
  );

  const saveFile = useCallback(
    async (path?: string) => {
      const id = path === undefined ? activeBufferId : findBufferId(path);
      if (id) await saveBuffer(id, { force: true });
    },
    [activeBufferId, findBufferId, saveBuffer],
  );

  const openFiles = useMemo(() => {
    const files: WorkspaceEditorFile[] = [];
    for (const buffer of buffers.values()) {
      if (buffer.kind === 'file') files.push(toEditorFile(buffer));
    }
    return files;
  }, [buffers]);

  const activeFile = useMemo(() => {
    const buffer = activeBufferId ? buffers.get(activeBufferId) : undefined;
    return buffer?.kind === 'file' ? toEditorFile(buffer) : null;
  }, [buffers, activeBufferId]);

  const value = useMemo<WorkspaceEditorValue>(
    () => ({ openFiles, activeFile, openFile, closeFile, saveFile }),
    [openFiles, activeFile, openFile, closeFile, saveFile],
  );

  return <WorkspaceEditorContext.Provider value={value}>{children}</WorkspaceEditorContext.Provider>;
}

WorkspaceEditorProvider.displayName = 'WorkspaceEditorProvider';

/** Read the editor unit. Throws outside a `WorkspaceEditorProvider`. */
export function useWorkspaceEditor(): WorkspaceEditorValue {
  const value = useContext(WorkspaceEditorContext);
  if (!value) {
    throw new Error('useWorkspaceEditor must be used within a WorkspaceEditorProvider');
  }
  return value;
}

/** The editor unit's props for the views arranged around it (the file tree, the changes panel). */
export function useWorkspaceEditorProps(): WorkspaceEditorProps {
  const { openFile, activeFile } = useWorkspaceEditor();
  const onFileSelect = useCallback(
    (file: FileInfo) => {
      if (!file.isDir) openFile(file.path);
    },
    [openFile],
  );
  const onFileClick = useCallback(
    (path: string, line?: number) => {
      openFile(path, { line });
    },
    [openFile],
  );
  const selectedFile = activeFile?.workspacePath;
  return useMemo(
    () => ({ fileTree: { onFileSelect, selectedFile }, onFileClick }),
    [onFileSelect, selectedFile, onFileClick],
  );
}
