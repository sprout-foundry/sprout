import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useEditorManager } from '../../contexts/EditorManagerContext';
import { useHotkeys } from '../../contexts/HotkeyContext';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import { ApiService } from '../../services/api';
import { getWorkspaceSymbols } from '../../services/api/editorApi';
import {
  loadRecentFiles,
  pruneRecentFiles,
  recordRecentFile as recordRecentFileInStore,
  type RecentFileEntry,
} from '../../services/paletteRecents';
import { fuzzyFilter } from '../../utils/fuzzyMatch';
import { useLog } from '../../utils/log';
import { extractSymbols } from '../../utils/symbolUtils';
import type { CommandDef, FileResult, SymbolResult } from './CommandPalette';
import { visibleCommands } from './constants';
import useFileIndex from './useFileIndex';

export interface UseCommandPaletteSourcesOptions {
  /** Whether the palette is open; the project folder file index is built only while it is. */
  isOpen: boolean;
  /** Open a project folder file, optionally at a line. */
  openFile: (path: string, options?: { line?: number }) => unknown;
}

export interface CommandPaletteSources {
  /** The palette's commands, each with the user's current keybinding. */
  commands: CommandDef[];
  isLoading: boolean;
  recentFiles: RecentFileEntry[];
  projectFolder: string;
  onSearchFiles: (query: string) => Promise<FileResult[]>;
  onSearchSymbols: (query: string) => SymbolResult[];
  onSearchWorkspaceSymbols: (query: string) => Promise<SymbolResult[]>;
  onOpenFileAtLine: (filePath: string, line: number) => void;
}

/**
 * The command palette's data: the command list, the project folder file index and
 * its fuzzy search, the per-workspace recent files, and symbol search in the
 * active buffer and across the project folder. The standalone app's palette and the
 * composed project folder's palette unit both read it, so the two list and search
 * the same things; each supplies its own open state and file opener.
 */
export function useCommandPaletteSources({ isOpen, openFile }: UseCommandPaletteSourcesOptions): CommandPaletteSources {
  const apiService = ApiService.getInstance();
  const sproutFetch = useSproutFetch();
  const { buffers, activeBufferId } = useEditorManager();
  const currentBuffer = activeBufferId ? buffers.get(activeBufferId) : null;

  // Pre-index project folder files only while the palette is open. The fuzzy
  // filter then runs against this in-memory list for instant results.
  const paletteLog = useLog();
  // Bake the user's current keybinding next to each command so the palette
  // doubles as a shortcut cheatsheet. `hotkeyForCommand` already handles
  // Mac vs non-Mac modifier substitution (Cmd ↔ Ctrl).
  const { hotkeyForCommand } = useHotkeys();
  const paletteCommands = useMemo(
    () => visibleCommands().map((cmd) => ({ ...cmd, shortcut: hotkeyForCommand(cmd.id) ?? undefined })),
    [hotkeyForCommand],
  );
  const {
    allFiles: paletteAllFiles,
    isLoadingFiles: paletteIsLoading,
    projectFolder: paletteProjectFolder,
  } = useFileIndex({
    apiService,
    isOpen,
    log: paletteLog,
  });
  const handlePaletteSearchFiles = useCallback(
    async (query: string) => {
      if (!query) return [];
      const matches = fuzzyFilter(query, paletteAllFiles, (f) => f.path, 50);
      return matches.map((m) => ({ name: m.item.name, path: m.item.path, type: m.item.type }));
    },
    [paletteAllFiles],
  );
  // Most-recently-opened files for the palette's idle landing list.
  // Persisted across sessions, keyed PER WORKSPACE (paletteRecents v2): the
  // old v1 store was one browser-global list of absolute paths, so files
  // from the previous folder stayed in the palette after a project folder switch
  // and clicking one opened a path in the wrong project folder. v2 buckets by
  // root with workspace-relative paths — a cross-workspace leak is
  // structurally impossible.
  const [paletteRecentFiles, setPaletteRecentFiles] = useState<RecentFileEntry[]>([]);
  // Load the current project folder's bucket when the root is known (and reload
  // when it changes — worktree switches arrive without a remount).
  useEffect(() => {
    setPaletteRecentFiles(loadRecentFiles(paletteProjectFolder));
  }, [paletteProjectFolder]);
  const recordRecentFile = useCallback(
    (file: RecentFileEntry) => {
      setPaletteRecentFiles(recordRecentFileInStore(paletteProjectFolder, file));
    },
    [paletteProjectFolder],
  );
  // Watch the editor manager's buffer set so *any* path that opens a file
  // (palette, file tree, hotkey, drag-drop, layout restore…) contributes to
  // the recents MRU. Skip non-file kinds (welcome/chat/diff/etc.) and the
  // synthetic __workspace/ paths used by virtual buffers.
  const seenFileBufferIdsRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    const seen = seenFileBufferIdsRef.current;
    for (const [id, buffer] of buffers.entries()) {
      if (seen.has(id)) continue;
      seen.add(id);
      if (buffer.kind !== 'file') continue;
      const path = buffer.file?.path;
      if (!path || path.startsWith('__workspace/')) continue;
      recordRecentFile({
        name: buffer.file.name,
        path,
        type: 'file',
      });
    }
  }, [buffers, recordRecentFile]);
  // A freshly-built index tells us which paths actually exist in this
  // project folder; drop stale recents (deleted files, renamed paths) against it.
  // Runs only after a bulk load, keyed by root so it re-arms on switches.
  const lastPruneRootRef = useRef<string>('');
  useEffect(() => {
    if (!paletteProjectFolder || lastPruneRootRef.current === paletteProjectFolder) return;
    if (paletteIsLoading) return;
    if (paletteAllFiles.length === 0) return;
    lastPruneRootRef.current = paletteProjectFolder;
    const known = new Set(paletteAllFiles.map((f) => f.path));
    setPaletteRecentFiles((prev) => {
      const next = pruneRecentFiles(paletteProjectFolder, (p) => known.has(p));
      return next === prev ? prev : next;
    });
  }, [paletteProjectFolder, paletteAllFiles, paletteIsLoading]);
  const handlePaletteSearchSymbols = useCallback(
    (query: string) => {
      const content = currentBuffer?.content;
      if (!content) return [];
      const ext = currentBuffer?.file?.ext || '';
      const langId = ext.startsWith('.') ? ext.slice(1) : ext;
      // extractSymbols is internally cached by content hash, so repeated
      // calls for the same buffer are cheap.
      const symbols = extractSymbols(content, langId);
      const top = query.trim()
        ? fuzzyFilter(query, symbols, (s) => s.name, 100).map((m) => m.item)
        : symbols.slice(0, 100);
      return top.map((s) => ({ name: s.name, kind: s.kind, line: s.line }));
    },
    [currentBuffer],
  );
  // Workspace-wide LSP symbol search. Surfaces symbols from any indexed
  // file so the user can jump to a definition without first opening the
  // file. The shared palette handles debouncing + race-safety.
  const handlePaletteSearchWorkspaceSymbols = useCallback(
    async (query: string) => {
      const q = query.trim();
      if (!q) return [];
      try {
        const response = await getWorkspaceSymbols(sproutFetch, q);
        const out: Array<{ name: string; kind: string; line: number; filePath: string }> = [];
        for (const file of response.files ?? []) {
          for (const sym of file.symbols ?? []) {
            if (sym.line == null) continue;
            out.push({
              name: sym.name,
              kind: sym.kind || 'symbol',
              line: sym.line,
              filePath: file.file,
            });
            if (out.length >= 100) return out;
          }
        }
        return out;
      } catch {
        return [];
      }
    },
    [sproutFetch],
  );
  // Open a file at a specific line. Used by project folder symbol clicks: the
  // user picks a symbol that lives elsewhere; we load that file then fire
  // the goto-line event after the editor has mounted the buffer.
  const handlePaletteOpenFileAtLine = useCallback(
    (filePath: string, line: number) => {
      openFile(filePath, { line });
    },
    [openFile],
  );

  return {
    commands: paletteCommands,
    isLoading: paletteIsLoading,
    recentFiles: paletteRecentFiles,
    projectFolder: paletteProjectFolder,
    onSearchFiles: handlePaletteSearchFiles,
    onSearchSymbols: handlePaletteSearchSymbols,
    onSearchWorkspaceSymbols: handlePaletteSearchWorkspaceSymbols,
    onOpenFileAtLine: handlePaletteOpenFileAtLine,
  };
}
