/**
 * The file tree's data callbacks, implemented against the host transport.
 *
 * `FileTree` is a props-only primitive: it lists, creates, deletes and renames
 * through the callbacks it is handed. This hook is the one implementation of
 * those callbacks — the standalone app's sidebar and a composed workspace's
 * arranged `fileTree` view both use it — so a host gets a working tree from
 * the same code the app runs.
 *
 * Listing goes to `/api/files` through the adapter-aware `clientFetch` (the
 * daemon on a local host, the in-browser workspace's VFS on a WASM host, the
 * host's API base on a hosted one); create/delete/rename go to `/api/create`,
 * `/api/delete` and `/api/rename` on the same fetch. A native shell with the
 * files channel lists through its bridge instead.
 *
 * The hook needs no provider: a host can call it above `SproutWorkspace` to
 * build `viewProps.fileTree` itself. Inside the provider stack it also keeps
 * open editor buffers in step with deletes and renames.
 */

import type { FileInfo, FileTreeProps } from '@sprout/ui';
import { useCallback, useMemo } from 'react';
import { useOptionalBufferManager } from '../contexts/BufferManagerContext';
import * as filesApi from '../services/api/filesApi';
import { clientFetch } from '../services/clientSession';
import { detectSproutStudio, mapWorkspaceListing, nativeFsGate, workspaceListDepth } from '../services/nativeFs';
import { NATIVE_FS_ENABLED } from '../services/nativeFsStubs/nativeFsFlag';

export interface UseWorkspaceFileTreeOptions {
  /** Called when a file is selected in the tree. Omitted, selection does nothing. */
  onFileSelect?: (file: FileInfo) => void;
  /** The selected file path, for a controlled selection. */
  selectedFile?: string;
  /** The directory the tree roots at; defaults to the workspace root (`.`). */
  rootPath?: string;
  /** The workspace's absolute root, used when the tree copies absolute paths. */
  workspaceRoot?: string;
  /** The fetch the file calls go through; defaults to the adapter-aware `clientFetch`. */
  fetchFn?: typeof fetch;
}

/** The workspace file listing as `/api/files` returns it. */
interface ApiFileEntry {
  name: string;
  path: string;
  size?: number;
  modified?: number;
  mod_time?: number;
  isDir?: boolean;
  is_dir?: boolean;
  git_status?: string;
}

function toFileInfo(file: ApiFileEntry): FileInfo {
  const isDir = Boolean(file.isDir ?? file.is_dir);
  return {
    name: file.name,
    path: file.path,
    size: file.size || 0,
    modified: file.modified ?? file.mod_time ?? 0,
    isDir,
    ext: isDir ? '' : file.name.includes('.') ? `.${file.name.split('.').pop() || ''}` : '',
    gitStatus: (file.git_status || undefined) as FileInfo['gitStatus'],
  };
}

/** Directories first, ignored entries last, then by name. */
function compareEntries(a: FileInfo, b: FileInfo): number {
  if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
  if ((a.gitStatus === 'ignored') !== (b.gitStatus === 'ignored')) {
    return a.gitStatus === 'ignored' ? 1 : -1;
  }
  return a.name.localeCompare(b.name);
}

/** List one directory of the workspace. */
export async function fetchWorkspaceFiles(path: string, fetchFn: typeof fetch = clientFetch): Promise<FileInfo[]> {
  // The default build compiles NATIVE_FS_ENABLED to `false`, so the native
  // files channel is a dead branch there.
  if (NATIVE_FS_ENABLED) {
    const gate = await nativeFsGate();
    if (gate.active) {
      const bridge = detectSproutStudio();
      if (bridge) {
        const result = await bridge.listWorkspace(workspaceListDepth(path));
        return mapWorkspaceListing(result, path);
      }
    }
    // Gate active but bridge vanished (or failed): degrade to the client path.
  }
  const response = await fetchFn(`/api/files?path=${encodeURIComponent(path)}`);
  if (!response.ok) throw new Error(`Failed to fetch files: ${response.statusText}`);
  const data = await response.json();
  if (data.message !== 'success') throw new Error(data.message);
  return ((data.files || []) as ApiFileEntry[]).map(toFileInfo).sort(compareEntries);
}

function childPath(parentPath: string, name: string): string {
  return parentPath === '.' ? name : `${parentPath}/${name}`;
}

/** The `FileTreeProps` for a tree backed by the workspace's file API. */
export function useWorkspaceFileTree(options: UseWorkspaceFileTreeOptions = {}): FileTreeProps {
  const { onFileSelect, selectedFile, rootPath = '.', workspaceRoot, fetchFn = clientFetch } = options;
  const bufferManager = useOptionalBufferManager();

  const onFetchFiles = useCallback((path: string) => fetchWorkspaceFiles(path, fetchFn), [fetchFn]);
  const onCreateFile = useCallback(
    async (parentPath: string, name: string) => {
      await filesApi.createItem(fetchFn, childPath(parentPath, name), false);
    },
    [fetchFn],
  );
  const onCreateFolder = useCallback(
    async (parentPath: string, name: string) => {
      await filesApi.createItem(fetchFn, childPath(parentPath, name), true);
    },
    [fetchFn],
  );
  const onDeletePath = useCallback(
    async (path: string) => {
      await filesApi.deleteItem(fetchFn, path);
      bufferManager?.closeBuffersForDeletedPath(path);
    },
    [fetchFn, bufferManager],
  );
  const onRenamePath = useCallback(
    async (oldPath: string, newPath: string) => {
      await filesApi.renameItem(fetchFn, oldPath, newPath);
      bufferManager?.retargetBufferPaths(oldPath, newPath);
    },
    [fetchFn, bufferManager],
  );
  const handleFileSelect = useCallback((file: FileInfo) => onFileSelect?.(file), [onFileSelect]);

  return useMemo<FileTreeProps>(
    () => ({
      onFileSelect: handleFileSelect,
      selectedFile,
      rootPath,
      workspaceRoot,
      onFetchFiles,
      onCreateFile,
      onCreateFolder,
      onDeletePath,
      onRenamePath,
    }),
    [
      handleFileSelect,
      selectedFile,
      rootPath,
      workspaceRoot,
      onFetchFiles,
      onCreateFile,
      onCreateFolder,
      onDeletePath,
      onRenamePath,
    ],
  );
}
