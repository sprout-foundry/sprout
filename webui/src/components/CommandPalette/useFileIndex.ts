import { useEffect, useRef, useState } from 'react';
import type { ApiService } from '../../services/api';
import { clientFetch } from '../../services/clientSession';
import { debugLog, type useLog } from '../../utils/log';
import type { FileResult } from './CommandPalette';
import { MAX_INDEXED_FILES, MAX_INDEXED_DIRECTORIES, SKIP_DIRECTORIES, MAX_DIRECTORY_DEPTH } from './constants';

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

// Files only change when the workspace does, so we cache the index result and
// invalidate it after this TTL so users picking up new files don't need to
// reload the page. 60s is short enough to feel fresh, long enough to skip
// redundant work during rapid palette opens.
const INDEX_TTL_MS = 60_000;

interface FileIndexResponse {
  files?: Array<{ name?: unknown; path?: unknown; type?: unknown }>;
  truncated?: boolean;
  message?: string;
  workspace?: string;
}

/**
 * Fetch the whole workspace index in ONE request. The daemon's
 * /api/file-index walks the tree server-side; in cloud mode the same route
 * is served by the WASM shell's bulk walkFiles export. One round-trip
 * replaces the previous crawl of up to 3000 serial /api/browse calls.
 *
 * Rows arrive workspace-relative (small payload, no root duplicated per
 * row); they are joined with the response's workspace root here so the
 * result matches the absolute-path shape the /api/browse crawl produced —
 * buffers, readFileWithConsent, and the WASM sanitizePath all expect
 * absolute paths (sanitizePath would turn a bare relative path into a
 * root-absolute one and miss the file).
 */
async function fetchBulkIndex(fallbackRoot: string): Promise<FileResult[]> {
  const response = await clientFetch('/api/file-index');
  if (!response.ok) {
    throw new Error(`file-index ${response.status}`);
  }
  const data = (await response.json()) as FileIndexResponse;
  const root = String(data.workspace || fallbackRoot || '').replace(/\/+$/, '');
  const entries = Array.isArray(data.files) ? data.files : [];
  const files: FileResult[] = [];
  for (const entry of entries) {
    const relPath = String(entry.path || '').replace(/^\/+/, '');
    const entryName = String(entry.name || relPath.split('/').pop() || '');
    if (!relPath || !entryName) continue;
    const entryPath = root === '' || root === '/' ? `/${relPath}` : `${root}/${relPath}`;
    files.push({ name: entryName, path: entryPath, type: String(entry.type || 'file') });
  }
  return files;
}

/**
 * Fallback for servers predating /api/file-index: crawl one /api/browse
 * request per directory, breadth-first, up to the shared caps.
 */
async function crawlPerDirectory(): Promise<FileResult[]> {
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
  return indexedFiles;
}

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
      let needsSelection = false;
      try {
        const workspace = await apiService.getWorkspace();
        if (cancelled) return;
        resolvedRoot = String(workspace.workspace_root || '').trim();
        needsSelection = Boolean(workspace.needs_workspace_selection);
        setWorkspaceRoot(resolvedRoot);
      } catch (err) {
        if (!cancelled) setWorkspaceRoot('');
        debugLog('[FileIndex] Failed to fetch workspace root:', err);
      }

      // Workspace gate: when the daemon's resolved workspace is the home
      // directory awaiting selection, crawling it would walk TCC-protected
      // folders (Documents, Music, …) — and the server refuses those
      // listings anyway. Show an empty index.
      if (needsSelection) {
        if (!cancelled) {
          setAllFiles([]);
          lastIndexedAtRef.current = Date.now();
          lastIndexedRootRef.current = resolvedRoot;
        }
        return;
      }

      const isFresh =
        resolvedRoot &&
        resolvedRoot === lastIndexedRootRef.current &&
        Date.now() - lastIndexedAtRef.current < INDEX_TTL_MS;
      if (isFresh) return;

      setIsLoadingFiles(true);
      try {
        // One bulk request when the server (or WASM shell) supports it;
        // per-directory crawl only as the legacy fallback.
        let files: FileResult[];
        try {
          files = await fetchBulkIndex(resolvedRoot);
        } catch (bulkErr) {
          debugLog('[FileIndex] bulk index unavailable, crawling directories:', bulkErr);
          if (cancelled) return;
          files = await crawlPerDirectory();
        }

        if (cancelled) return;
        setAllFiles(files);
        lastIndexedAtRef.current = Date.now();
        lastIndexedRootRef.current = resolvedRoot;
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
