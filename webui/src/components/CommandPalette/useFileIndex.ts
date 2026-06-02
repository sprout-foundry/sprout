import { useEffect, useRef, useState } from 'react';
import type { ApiService } from '../../services/api';
import { clientFetch } from '../../services/clientSession';
import { debugLog, type useLog } from '../../utils/log';
import { MAX_INDEXED_FILES, MAX_INDEXED_DIRECTORIES, SKIP_DIRECTORIES, MAX_DIRECTORY_DEPTH } from './constants';
import type { FileResult } from './CommandPalette';

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

// Files only change when the workspace does, so we cache the BFS result and
// invalidate it after this TTL so users picking up new files don't need to
// reload the page. 60s is short enough to feel fresh, long enough to skip
// redundant work during rapid palette opens.
const INDEX_TTL_MS = 60_000;

function useFileIndex(options: UseFileIndexOptions): UseFileIndexResult {
  const { apiService, isOpen, log } = options;

  const [allFiles, setAllFiles] = useState<FileResult[]>([]);
  const [workspaceRoot, setWorkspaceRoot] = useState('');
  const [isLoadingFiles, setIsLoadingFiles] = useState(false);
  // Cache fingerprint: skip re-indexing if the workspace root matches and
  // the index is still fresh. Reset on workspace change.
  const lastIndexedAtRef = useRef<number>(0);
  const lastIndexedRootRef = useRef<string>('');

  useEffect(() => {
    if (!isOpen) return;
    let cancelled = false;

    const run = async () => {
      // Resolve workspace root first so we can decide whether the cache is
      // still valid for it.
      let resolvedRoot = '';
      try {
        const workspace = await apiService.getWorkspace();
        if (cancelled) return;
        resolvedRoot = String(workspace.workspace_root || '').trim();
        setWorkspaceRoot(resolvedRoot);
      } catch (err) {
        if (!cancelled) setWorkspaceRoot('');
        debugLog('[FileIndex] Failed to fetch workspace root:', err);
      }

      const isFresh =
        resolvedRoot &&
        resolvedRoot === lastIndexedRootRef.current &&
        Date.now() - lastIndexedAtRef.current < INDEX_TTL_MS;
      if (isFresh) return;

      setIsLoadingFiles(true);
      try {
        const queue: Array<{ path: string; depth: number }> = [{ path: '.', depth: 0 }];
        const indexedFiles: FileResult[] = [];
        const visited = new Set<string>();
        let visitedDirs = 0;

        while (queue.length > 0 && indexedFiles.length < MAX_INDEXED_FILES && visitedDirs < MAX_INDEXED_DIRECTORIES) {
          if (cancelled) return;
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

        if (!cancelled) {
          setAllFiles(indexedFiles);
          lastIndexedAtRef.current = Date.now();
          lastIndexedRootRef.current = resolvedRoot;
        }
      } catch (err) {
        log.error(`Failed to browse files: ${err instanceof Error ? err.message : String(err)}`, {
          title: 'File Browse Error',
        });
      } finally {
        if (!cancelled) setIsLoadingFiles(false);
      }
    };

    void run();

    return () => {
      cancelled = true;
    };
  }, [apiService, isOpen, log]); // eslint-disable-line react-hooks/exhaustive-deps

  return { allFiles, workspaceRoot, isLoadingFiles };
}

export default useFileIndex;
