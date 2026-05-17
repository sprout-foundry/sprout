import { useEffect, useState } from 'react';
import type { ApiService } from '../../services/api';
import { clientFetch } from '../../services/clientSession';
import { debugLog, type useLog } from '../../utils/log';
import { MAX_INDEXED_FILES, MAX_INDEXED_DIRECTORIES, SKIP_DIRECTORIES, MAX_DIRECTORY_DEPTH } from './constants';
import type { FileResult } from './types';

interface UseFileIndexResult {
  allFiles: FileResult[];
  workspaceRoot: string;
  isLoadingFiles: boolean;
}

interface UseFileIndexOptions {
  apiService: ApiService;
  isOpen: boolean;
  log: ReturnType<typeof useLog>;
}

function useFileIndex(options: UseFileIndexOptions): UseFileIndexResult {
  const { apiService, isOpen, log } = options;

  const [allFiles, setAllFiles] = useState<FileResult[]>([]);
  const [workspaceRoot, setWorkspaceRoot] = useState('');
  const [isLoadingFiles, setIsLoadingFiles] = useState(false);

  // Reset files when palette closes
  useEffect(() => {
    if (isOpen) return;
    setAllFiles([]);
  }, [isOpen]);

  useEffect(() => {
    if (!isOpen) return;
    let cancelled = false;

    apiService
      .getWorkspace()
      .then((workspace) => {
        if (!cancelled) setWorkspaceRoot(String(workspace.workspace_root || '').trim());
      })
      .catch((err) => {
        if (!cancelled) setWorkspaceRoot('');
        debugLog('[FileIndex] Failed to fetch workspace root:', err);
      });

    const doFetch = async () => {
      setIsLoadingFiles(true);
      try {
        const queue: Array<{ path: string; depth: number }> = [{ path: '.', depth: 0 }];
        const indexedFiles: FileResult[] = [];
        const visited = new Set<string>();
        let visitedDirs = 0;

        while (queue.length > 0 && indexedFiles.length < MAX_INDEXED_FILES && visitedDirs < MAX_INDEXED_DIRECTORIES) {
          const item = queue.shift();
          if (!item || visited.has(item.path)) continue;
          visited.add(item.path);
          visitedDirs += 1;

          if (item.depth > MAX_DIRECTORY_DEPTH) continue;

          const response = await clientFetch(`/api/browse?path=${encodeURIComponent(item.path)}&ignore=true`);
          if (!response.ok) continue;

          const data = await response.json();
          const entries = Array.isArray(data.files) ? data.files : [];

          for (const entry of entries) {
            const entryPath = String(entry.path || '');
            const entryName = String(entry.name || entryPath.split('/').pop() || '');
            const entryType = String(entry.type || 'file');
            if (!entryPath || !entryName) continue;

            if (entryType === 'directory') {
              if (!SKIP_DIRECTORIES.has(entryName)) queue.push({ path: entryPath, depth: item.depth + 1 });
              continue;
            }

            indexedFiles.push({ name: entryName, path: entryPath, type: entryType });
            if (indexedFiles.length >= MAX_INDEXED_FILES) break;
          }
        }

        if (!cancelled) setAllFiles(indexedFiles);
      } catch (err) {
        log.error(`Failed to browse files: ${err instanceof Error ? err.message : String(err)}`, {
          title: 'File Browse Error',
        });
      } finally {
        if (!cancelled) setIsLoadingFiles(false);
      }
    };
    doFetch();

    return () => {
      cancelled = true;
    };
  }, [apiService, isOpen, log]); // eslint-disable-line react-hooks/exhaustive-deps

  return { allFiles, workspaceRoot, isLoadingFiles };
}

export default useFileIndex;
