import './CommandPalette.css';
import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import type { ChangeEvent, KeyboardEvent, MouseEvent } from 'react';
import { useHotkeys } from '../contexts/HotkeyContext';
import { showThemedConfirm } from './ThemedDialog';
import { clientFetch } from '../services/clientSession';
import { ApiService } from '../services/api';
import { clearLayoutSnapshot } from '../services/layoutPersistence';
import { fuzzyFilter, highlightMatches } from '../utils/fuzzyMatch';
import type { FuzzyResult } from '../utils/fuzzyMatch';
import { useLog, debugLog } from '../utils/log';
import {
  extractSymbols,
  buildScopePaths,
  KIND_ICONS,
  type SymbolInfo,
  type SymbolKind,
} from '../utils/symbolUtils';
import { supportsLocalTerminal } from '../config/mode';

// ── Types ────────────────────────────────────────────────────────────────

export type PaletteMode = 'all' | 'files' | 'symbols' | 'commands';

interface CommandPaletteProps {
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

interface FileResult {
  name: string;
  path: string;
  type: string;
}

interface CommandDef {
  id: string;
  label: string;
  category: string;
}

type ResultKind = 'command' | 'file' | 'symbol' | 'commands-header' | 'files-header' | 'symbols-header';

interface PaletteResult {
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

// ── Command definitions ────────────────────────────────────────────────────

const COMMAND_DEFINITIONS: CommandDef[] = [
  { id: 'quick_open', label: 'Go to File...', category: 'File' },
  { id: 'new_file', label: 'New File', category: 'File' },
  { id: 'save_file', label: 'Save File', category: 'File' },
  { id: 'save_all_files', label: 'Save All Files', category: 'File' },
  { id: 'close_editor', label: 'Close Editor', category: 'File' },
  { id: 'close_all_editors', label: 'Close All Editors', category: 'File' },
  { id: 'close_other_editors', label: 'Close Other Editors', category: 'File' },
  { id: 'toggle_pin_tab', label: 'Toggle Pin Tab', category: 'File' },
  { id: 'command_palette', label: 'Show All Commands', category: 'General' },
  { id: 'toggle_explorer', label: 'Toggle File Explorer', category: 'View' },
  { id: 'toggle_sidebar', label: 'Toggle Sidebar', category: 'View' },
  { id: 'toggle_terminal', label: 'Toggle Terminal', category: 'View' },
  { id: 'split_editor_vertical', label: 'Split Editor Vertical', category: 'View' },
  { id: 'split_editor_horizontal', label: 'Split Editor Horizontal', category: 'View' },
  { id: 'split_editor_grid', label: 'Split Editor Grid', category: 'View' },
  { id: 'focus_split_1', label: 'Focus Editor Split 1', category: 'View' },
  { id: 'focus_split_2', label: 'Focus Editor Split 2', category: 'View' },
  { id: 'focus_split_3', label: 'Focus Editor Split 3', category: 'View' },
  { id: 'split_terminal_vertical', label: 'Split Terminal Vertical', category: 'View' },
  { id: 'split_terminal_horizontal', label: 'Split Terminal Horizontal', category: 'View' },
  { id: 'editor_toggle_word_wrap', label: 'Toggle Word Wrap', category: 'View' },
  { id: 'toggle_minimap', label: 'Toggle Minimap', category: 'View' },
  { id: 'editor_cycle_whitespace_rendering', label: 'Cycle Whitespace Rendering', category: 'View' },
  { id: 'toggle_linked_scroll', label: 'Toggle Linked Scrolling', category: 'View' },
  { id: 'reset_saved_layout', label: 'Reset Saved Layout', category: 'View' },
  { id: 'focus_next_tab', label: 'Focus Next Tab', category: 'Navigation' },
  { id: 'focus_prev_tab', label: 'Focus Previous Tab', category: 'Navigation' },
  { id: 'switch_to_chat', label: 'Switch to Chat', category: 'Navigation' },
  { id: 'switch_to_editor', label: 'Switch to Editor', category: 'Navigation' },
  { id: 'switch_to_git', label: 'Switch to Git', category: 'Navigation' },
  { id: 'open_hotkeys_config', label: 'Edit Keyboard Shortcuts', category: 'Preferences' },
  { id: 'format_document', label: 'Format Document', category: 'Editor' },
  { id: 'editor_find_all_references', label: 'Find All References', category: 'Editor' },
  { id: 'editor_workspace_symbol', label: 'Go to Symbol in Workspace', category: 'Editor' },
  { id: 'editor_goto_symbol', label: 'Go to Symbol in File', category: 'Editor' },
];

const VISIBLE_COMMANDS = COMMAND_DEFINITIONS.filter(cmd => {
  if (!supportsLocalTerminal && (cmd.id === 'toggle_terminal' || cmd.id === 'split_terminal_vertical' || cmd.id === 'split_terminal_horizontal')) {
    return false;
  }
  return true;
});

// ── File browsing constants ────────────────────────────────────────────────

const MAX_FILE_RESULTS = 100;
const MAX_INDEXED_FILES = 12000;
const MAX_INDEXED_DIRECTORIES = 3000;
const SKIP_DIRECTORIES = new Set(['.git', 'node_modules', '.next', 'dist', 'build', '__pycache__', '.venv', 'vendor']);
const MAX_DIRECTORY_DEPTH = 8;

const MAX_SYMBOL_RESULTS = 100;

// ── Mode tab definitions ─────────────────────────────────────────────────

const MODE_TABS: { mode: PaletteMode; label: string }[] = [
  { mode: 'all', label: 'All' },
  { mode: 'files', label: 'Files' },
  { mode: 'symbols', label: 'Symbols' },
  { mode: 'commands', label: 'Commands' },
];

// ── Prefix-based auto-detection ──────────────────────────────────────────

const COMMAND_PREFIX = '>';
const SYMBOL_PREFIXES = ['@', '#'];

function parsePrefixAndQuery(raw: string): { prefix: PaletteMode | null; query: string } {
  if (raw.startsWith(COMMAND_PREFIX)) {
    return { prefix: 'commands', query: raw.slice(COMMAND_PREFIX.length) };
  }
  for (const p of SYMBOL_PREFIXES) {
    if (raw.startsWith(p)) {
      return { prefix: 'symbols', query: raw.slice(p.length) };
    }
  }
  return { prefix: null, query: raw };
}

// ── Navigable result kinds (skip headers in arrow-key nav) ──────────────

const NAVIGABLE_KINDS = new Set<ResultKind>(['command', 'file', 'symbol']);

// ── Path helpers (exported for tests) ────────────────────────────────────

function normalizePathSeparators(value: string): string {
  return value.replace(/\\/g, '/');
}

export function toWorkspaceRelativePath(filePath: string, workspaceRoot: string): string {
  const normalizedPath = normalizePathSeparators(filePath).replace(/^\.\//, '');
  const normalizedRoot = normalizePathSeparators(workspaceRoot).replace(/\/+$/, '');
  if (!normalizedRoot) return normalizedPath;
  if (normalizedPath === normalizedRoot) return '';
  const prefix = `${normalizedRoot}/`;
  if (normalizedPath.startsWith(prefix)) return normalizedPath.slice(prefix.length);
  return normalizedPath;
}

export function getDirectoryName(relativePath: string): string {
  const normalizedPath = normalizePathSeparators(relativePath);
  const lastSlash = normalizedPath.lastIndexOf('/');
  if (lastSlash <= 0) return '';
  return normalizedPath.slice(0, lastSlash);
}

// ── Component ──────────────────────────────────────────────────────────────

function CommandPalette({
  isOpen,
  onClose,
  onOpenFile,
  onToggleSidebar,
  onToggleTerminal,
  onOpenHotkeysConfig,
  initialMode = 'all',
  onNavigateToLine,
  activeBufferContent,
  activeBufferFileExtension,
}: CommandPaletteProps): JSX.Element | null {
  const { hotkeyForCommand } = useHotkeys();
  const log = useLog();
  const apiService = ApiService.getInstance();

  const [query, setQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [isLoadingFiles, setIsLoadingFiles] = useState(false);
  const [allFiles, setAllFiles] = useState<FileResult[]>([]);
  const [workspaceRoot, setWorkspaceRoot] = useState('');
  const [mode, setMode] = useState<PaletteMode>('all');
  const savedInitialMode = useRef<PaletteMode>(initialMode);

  const inputRef = useRef<HTMLInputElement>(null);
  const resultsRef = useRef<HTMLDivElement>(null);
  const prevOpenRef = useRef(false);

  // ── Lifecycle: reset on open/close ────────────────────────────────────

  useEffect(() => {
    if (isOpen && !prevOpenRef.current) {
      setMode(initialMode);
      savedInitialMode.current = initialMode;
      setQuery('');
      setSelectedIndex(0);
    } else if (!isOpen && prevOpenRef.current) {
      setQuery('');
      setSelectedIndex(0);
      setAllFiles([]);
      setMode('all');
    }
    prevOpenRef.current = isOpen;
  }, [isOpen, initialMode]);

  // ── Auto-focus ────────────────────────────────────────────────────────

  useEffect(() => {
    if (isOpen && inputRef.current) {
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [isOpen]);

  // ── Pre-fetch files when palette opens ────────────────────────────────

  useEffect(() => {
    if (!isOpen) return;
    let cancelled = false;

    apiService
      .getWorkspace()
      .then((workspace) => {
        if (!cancelled) setWorkspaceRoot(String(workspace.workspace_root || '').trim());
      })
      .catch(() => {
        if (!cancelled) setWorkspaceRoot('');
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

  // ── Extract symbols from active buffer (memoised) ────────────────────

  const allSymbols = useMemo(() => {
    if (!activeBufferContent) return [];
    return extractSymbols(activeBufferContent, activeBufferFileExtension);
  }, [activeBufferContent, activeBufferFileExtension]);

  const scopePaths = useMemo(
    () => buildScopePaths(activeBufferContent || '', activeBufferFileExtension, allSymbols),
    [activeBufferContent, activeBufferFileExtension, allSymbols],
  );

  // ── Resolve effective mode: prefix takes priority, then current mode ──

  const { effectiveMode, searchQuery } = useMemo(() => {
    if (!query) return { effectiveMode: mode, searchQuery: '' };
    const parsed = parsePrefixAndQuery(query);
    if (parsed.prefix) return { effectiveMode: parsed.prefix, searchQuery: parsed.query };
    return { effectiveMode: mode, searchQuery: query };
  }, [query, mode]);

  // ── Build unified results ─────────────────────────────────────────────

  const results = useMemo((): PaletteResult[] => {
    const trimmed = searchQuery.trim();
    const em = effectiveMode;

    if (!trimmed) {
      // No query — show category-grouped commands or mode default
      if (em === 'files') return [];
      if (em === 'symbols') {
        // Show all symbols
        const items: PaletteResult[] = [];
        for (const sym of allSymbols) {
          const icon = KIND_ICONS[sym.kind] || '?';
          const scope = scopePaths.get(sym.line);
          items.push({
            kind: 'symbol',
            highlightedLabel: sym.name,
            score: 0,
            symbolLine: sym.line,
            symbolKind: sym.kind,
            scopePath: scope,
          });
        }
        return items;
      }
      if (em === 'commands') {
        const items: PaletteResult[] = [];
        let lastCat = '';
        for (const cmd of VISIBLE_COMMANDS) {
          if (cmd.category !== lastCat) {
            lastCat = cmd.category;
            items.push({ kind: 'commands-header', highlightedLabel: cmd.category, score: 0 });
          }
          items.push({
            kind: 'command',
            commandId: cmd.id,
            commandLabel: cmd.label,
            highlightedLabel: cmd.label,
            score: 0,
          });
        }
        return items;
      }
      // 'all' — show all commands as the homepage
      const items: PaletteResult[] = [];
      let lastCat = '';
      for (const cmd of VISIBLE_COMMANDS) {
        if (cmd.category !== lastCat) {
          lastCat = cmd.category;
          items.push({ kind: 'commands-header', highlightedLabel: cmd.category, score: 0 });
        }
        items.push({
          kind: 'command',
          commandId: cmd.id,
          commandLabel: cmd.label,
          highlightedLabel: cmd.label,
          score: 0,
        });
      }
      return items;
    }

    const items: PaletteResult[] = [];

    // ── Commands ─────────────────────────────────────────────────────────
    if (em === 'all' || em === 'commands') {
      const cmdResults: FuzzyResult<CommandDef>[] = fuzzyFilter(trimmed, VISIBLE_COMMANDS, (c) => c.label, 50);
      if (cmdResults.length > 0) {
        items.push({ kind: 'commands-header', highlightedLabel: 'Commands', score: Infinity });
        for (const r of cmdResults) {
          items.push({
            kind: 'command',
            commandId: r.item.id,
            commandLabel: r.item.label,
            highlightedLabel: highlightMatches(r.item.label, r.matches),
            score: r.score,
          });
        }
      }
    }

    // ── Files ────────────────────────────────────────────────────────────
    if ((em === 'all' || em === 'files') && allFiles.length > 0) {
      const fileResults: FuzzyResult<FileResult>[] = fuzzyFilter(
        trimmed,
        allFiles,
        (f) => toWorkspaceRelativePath(f.path, workspaceRoot),
        MAX_FILE_RESULTS,
      );
      if (fileResults.length > 0) {
        items.push({ kind: 'files-header', highlightedLabel: 'Files', score: -1 });
        for (const r of fileResults) {
          const relativePath = toWorkspaceRelativePath(r.item.path, workspaceRoot);
          const directoryPath = getDirectoryName(relativePath);
          items.push({
            kind: 'file',
            filePath: r.item.path,
            fileName: r.item.name,
            fileDirectory: directoryPath,
            highlightedLabel: highlightMatches(r.item.name, []),
            secondaryHighlightedLabel: directoryPath ? highlightMatches(directoryPath, []) : '',
            score: r.score,
          });
        }
      }
    }

    // ── Symbols ──────────────────────────────────────────────────────────
    if ((em === 'all' || em === 'symbols') && allSymbols.length > 0) {
      const symResults: FuzzyResult<SymbolInfo>[] = fuzzyFilter(trimmed, allSymbols, (s) => s.name, MAX_SYMBOL_RESULTS);
      if (symResults.length > 0) {
        items.push({ kind: 'symbols-header', highlightedLabel: `Symbols (${activeBufferFileExtension || 'current file'})`, score: -2 });
        for (const r of symResults) {
          const scope = scopePaths.get(r.item.line);
          items.push({
            kind: 'symbol',
            highlightedLabel: highlightMatches(r.item.name, r.matches),
            score: r.score,
            symbolLine: r.item.line,
            symbolKind: r.item.kind,
            scopePath: scope,
          });
        }
      }
    }

    return items;
  }, [searchQuery, effectiveMode, allFiles, allSymbols, scopePaths, workspaceRoot, activeBufferFileExtension]);

  // ── Navigable items (skip headers) ────────────────────────────────────

  const navigableItems = useMemo(() => results.filter((r) => NAVIGABLE_KINDS.has(r.kind)), [results]);

  // ── Reset selected index when query or mode changes ───────────────────

  useEffect(() => {
    setSelectedIndex(0);
  }, [searchQuery, effectiveMode]);

  // ── Scroll selected item into view ────────────────────────────────────

  useEffect(() => {
    const container = resultsRef.current;
    if (!container) return;
    const selected = container.querySelector('[data-selected="true"]');
    if (selected) selected.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  }, [selectedIndex]);

  // ── Execute a command by id ───────────────────────────────────────────

  const executeCommand = useCallback(
    async (commandId: string) => {
      switch (commandId) {
        case 'command_palette':
          break;
        case 'quick_open':
          setMode('files');
          setQuery('');
          inputRef.current?.focus();
          return;
        case 'toggle_explorer':
        case 'toggle_sidebar':
          onToggleSidebar();
          break;
        case 'toggle_terminal':
          onToggleTerminal();
          break;
        case 'new_file':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'new_file' } }));
          break;
        case 'switch_to_chat':
        case 'switch_to_editor':
        case 'switch_to_git':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId } }));
          break;
        case 'open_hotkeys_config':
          onOpenHotkeysConfig();
          break;
        case 'split_editor_vertical':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'split_editor_vertical' } }));
          break;
        case 'split_editor_horizontal':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'split_editor_horizontal' } }));
          break;
        case 'split_editor_grid':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'split_editor_grid' } }));
          break;
        case 'focus_split_1':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'focus_split_1' } }));
          break;
        case 'focus_split_2':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'focus_split_2' } }));
          break;
        case 'focus_split_3':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'focus_split_3' } }));
          break;
        case 'split_terminal_vertical':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'split_terminal_vertical' } }));
          break;
        case 'split_terminal_horizontal':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'split_terminal_horizontal' } }));
          break;
        case 'editor_toggle_word_wrap':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'editor_toggle_word_wrap' } }));
          break;
        case 'toggle_linked_scroll':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'toggle_linked_scroll' } }));
          break;
        case 'toggle_minimap':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'toggle_minimap' } }));
          break;
        case 'editor_cycle_whitespace_rendering':
          window.dispatchEvent(new CustomEvent('editor-cycle-whitespace-rendering'));
          break;
        case 'close_editor':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'close_editor' } }));
          break;
        case 'save_file':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'save_file' } }));
          break;
        case 'save_all_files':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'save_all_files' } }));
          break;
        case 'close_all_editors':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'close_all_editors' } }));
          break;
        case 'close_other_editors':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'close_other_editors' } }));
          break;
        case 'toggle_pin_tab':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'toggle_pin_tab' } }));
          break;
        case 'focus_next_tab':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'focus_next_tab' } }));
          break;
        case 'focus_prev_tab':
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'focus_prev_tab' } }));
          break;
        case 'reset_saved_layout': {
          if (!(await showThemedConfirm('Reset all saved layout settings? This cannot be undone.', { type: 'danger' }))) break;
          clearLayoutSnapshot();
          const keys = [
            'sprout.editor.paneLayout', 'sprout.editor.paneSizes', 'sprout-terminal-height',
            'sprout-terminal-expanded', 'sprout-sidebar-collapsed', 'sprout-sidebar-width',
            'sprout.contextPanel.width', 'sprout.contextPanel.collapsed', 'editor:minimap-enabled',
            'filetree-show-ignored',
          ];
          for (const key of keys) {
            try { window.localStorage.removeItem(key); } catch (err) { debugLog('[resetPreferences] localStorage.removeItem failed:', err); }
          }
          window.location.reload();
          return;
        }
        case 'format_document':
          document.dispatchEvent(new CustomEvent('editor-format-document'));
          break;
        case 'editor_find_all_references':
          document.dispatchEvent(new CustomEvent('editor-find-all-references'));
          break;
        case 'editor_workspace_symbol':
          document.dispatchEvent(new CustomEvent('editor-go-to-workspace-symbol'));
          break;
        case 'editor_goto_symbol':
          setMode('symbols');
          setQuery('');
          inputRef.current?.focus();
          return;
        default:
          break;
      }
      onClose();
    },
    [onClose, onToggleSidebar, onToggleTerminal, onOpenHotkeysConfig],
  );

  // ── Execute the currently selected item ───────────────────────────────

  const executeSelected = useCallback(() => {
    const item = navigableItems[selectedIndex];
    if (!item) return;
    if (item.kind === 'command' && item.commandId) {
      executeCommand(item.commandId);
    } else if (item.kind === 'file' && item.filePath) {
      onOpenFile(item.filePath);
      onClose();
    } else if (item.kind === 'symbol' && item.symbolLine !== undefined && onNavigateToLine) {
      onNavigateToLine(item.symbolLine);
      onClose();
    }
  }, [navigableItems, selectedIndex, executeCommand, onOpenFile, onNavigateToLine, onClose]);

  // ── Map result index → navigable index ───────────────────────────────

  const toNavigableIndex = useCallback(
    (resultIndex: number) => {
      let count = 0;
      for (let i = 0; i < results.length; i++) {
        if (NAVIGABLE_KINDS.has(results[i].kind)) {
          if (i === resultIndex) return count;
          count++;
        }
      }
      return 0;
    },
    [results],
  );

  // ── Map selectedIndex → flat result index ────────────────────────────

  const selectedFlatIndex = useMemo(() => {
    let navCount = 0;
    for (let i = 0; i < results.length; i++) {
      if (NAVIGABLE_KINDS.has(results[i].kind)) {
        if (navCount === selectedIndex) return i;
        navCount++;
      }
    }
    return -1;
  }, [results, selectedIndex]);

  // ── Handle text input ─────────────────────────────────────────────────

  const handleInputChange = useCallback((e: ChangeEvent<HTMLInputElement>) => {
    setQuery(e.target.value);
  }, []);

  // ── Handle keyboard navigation ────────────────────────────────────────

  const cycleMode = useCallback((direction: 1 | -1) => {
    setMode((prev) => {
      const idx = MODE_TABS.findIndex((t) => t.mode === prev);
      const next = (idx + direction + MODE_TABS.length) % MODE_TABS.length;
      return MODE_TABS[next].mode;
    });
  }, []);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey) {
        e.preventDefault();
        e.stopPropagation();
      }

      switch (e.key) {
        case 'Escape':
          e.preventDefault();
          onClose();
          break;
        case 'ArrowDown':
          e.preventDefault();
          setSelectedIndex((prev) => Math.min(prev + 1, Math.max(navigableItems.length - 1, 0)));
          break;
        case 'ArrowUp':
          e.preventDefault();
          setSelectedIndex((prev) => Math.max(prev - 1, 0));
          break;
        case 'Enter':
          e.preventDefault();
          e.stopPropagation();
          executeSelected();
          break;
        case 'Tab':
          e.preventDefault();
          cycleMode(e.shiftKey ? -1 : 1);
          break;
        case 'Backspace': {
          if (!query && mode !== savedInitialMode.current) {
            e.preventDefault();
            setMode(savedInitialMode.current);
          }
          break;
        }
      }
    },
    [navigableItems.length, query, mode, cycleMode, executeSelected, onClose],
  );

  const handleOverlayClick = useCallback(
    (e: MouseEvent) => {
      if (e.target === e.currentTarget) onClose();
    },
    [onClose],
  );

  if (!isOpen) return null;

  const hasQuery = searchQuery.length > 0;

  const placeholderText =
    effectiveMode === 'files'
      ? 'Search files by name…'
      : effectiveMode === 'symbols'
        ? 'Search symbols in current file…'
        : effectiveMode === 'commands'
          ? 'Type a command…'
          : 'Search commands, files & symbols…';

  const prefixIcon =
    effectiveMode === 'commands'
      ? '>'
      : effectiveMode === 'symbols'
        ? '@'
        : '';

  const hintLabel =
    !hasQuery && mode !== 'all'
      ? `${mode.charAt(0).toUpperCase() + mode.slice(1)} mode · Tab to cycle · Backspace to reset`
      : !hasQuery
        ? '> commands · @ symbols · Tab to cycle modes'
        : '';

  return (
    <div className="command-palette-overlay" onClick={handleOverlayClick} role="presentation">
      <div className="command-palette-container" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label="Command Palette">
        {/* Mode tab bar */}
        <div className="command-palette-mode-bar">
          {MODE_TABS.map((tab) => (
            <button
              key={tab.mode}
              className={`command-palette-mode-tab ${mode === tab.mode ? ' active' : ''}`}
              onClick={() => setMode(tab.mode)}
            >
              {tab.label}
            </button>
          ))}
        </div>

        {/* Input */}
        <div className="command-palette-input-wrapper">
          {prefixIcon && <span className="command-palette-prefix">{prefixIcon}</span>}
          <input
            ref={inputRef}
            type="text"
            className="command-palette-input"
            value={query}
            onChange={handleInputChange}
            onKeyDown={handleKeyDown}
            placeholder={placeholderText}
            autoComplete="off"
            autoCorrect="off"
            autoCapitalize="off"
            spellCheck={false}
          />
        </div>

        {/* Results */}
        <div className="command-palette-results" ref={resultsRef} role="listbox" aria-label="Palette results">
          {isLoadingFiles && !hasQuery && <div className="command-palette-loading">Loading files…</div>}

          {results.length === 0 && !hasQuery && !isLoadingFiles && effectiveMode === 'files' && (
            <div className="command-palette-hint">Type to search files</div>
          )}

          {results.length === 0 && hasQuery && !isLoadingFiles && (
            <div className="command-palette-empty">
              {effectiveMode === 'symbols' && !allSymbols.length
                ? 'No symbols in current file'
                : effectiveMode === 'files'
                  ? 'No matching files'
                  : `No matching ${effectiveMode === 'all' ? 'commands, files or symbols' : effectiveMode}`}
            </div>
          )}

          {results.map((item, index) => {
            // ── Header ────────────────────────────────────────────────────
            if (item.kind === 'commands-header' || item.kind === 'files-header' || item.kind === 'symbols-header') {
              return (
                <div key={`${item.kind}-${index}`} className="command-palette-category">
                  {item.highlightedLabel}
                </div>
              );
            }

            const isSelected = index === selectedFlatIndex;

            // ── Command item ──────────────────────────────────────────────
            if (item.kind === 'command') {
              const cmdId = item.commandId;
              if (!cmdId) return null;
              const shortcut = hotkeyForCommand(cmdId);
              return (
                <div
                  key={`cmd-${item.commandId}`}
                  data-selected={isSelected}
                  className={`command-palette-item ${isSelected ? 'command-palette-selected' : ''}`}
                  onClick={() => executeCommand(cmdId)}
                  onMouseEnter={() => setSelectedIndex(toNavigableIndex(index))}
                >
                  <span className="command-palette-label" dangerouslySetInnerHTML={{ __html: item.highlightedLabel }} />
                  {shortcut && <span className="command-palette-shortcut">{shortcut}</span>}
                </div>
              );
            }

            // ── File item ─────────────────────────────────────────────────
            if (item.kind === 'file') {
              const filePath = item.filePath;
              if (!filePath) return null;
              return (
                <div
                  key={`file-${filePath}`}
                  data-selected={isSelected}
                  className={`command-palette-item ${isSelected ? 'command-palette-selected' : ''}`}
                  onClick={() => { onOpenFile(filePath); onClose(); }}
                  onMouseEnter={() => setSelectedIndex(toNavigableIndex(index))}
                >
                  <span className="command-palette-file-icon">📄</span>
                  <span className="command-palette-file-meta">
                    <span className="command-palette-file-name" dangerouslySetInnerHTML={{ __html: item.highlightedLabel }} />
                    {item.fileDirectory && (
                      <span className="command-palette-file-path" title={item.fileDirectory} dangerouslySetInnerHTML={{ __html: item.secondaryHighlightedLabel || item.fileDirectory }} />
                    )}
                  </span>
                </div>
              );
            }

            // ── Symbol item ───────────────────────────────────────────────
            if (item.kind === 'symbol') {
              const icon = item.symbolKind ? KIND_ICONS[item.symbolKind] || '?' : '?';
              return (
                <div
                  key={`sym-${item.highlightedLabel}-${item.symbolLine}`}
                  data-selected={isSelected}
                  className={`command-palette-item command-palette-symbol-item ${isSelected ? 'command-palette-selected' : ''}`}
                  onClick={() => {
                    if (item.symbolLine !== undefined && onNavigateToLine) {
                      onNavigateToLine(item.symbolLine);
                      onClose();
                    }
                  }}
                  onMouseEnter={() => setSelectedIndex(toNavigableIndex(index))}
                >
                  <span className={`command-palette-symbol-icon goto-symbol-kind goto-symbol-kind-${item.symbolKind || 'function'}`}>{icon}</span>
                  <span className="command-palette-file-meta">
                    <span className="command-palette-file-name" dangerouslySetInnerHTML={{ __html: item.highlightedLabel }} />
                    {item.scopePath && (
                      <span className="command-palette-symbol-scope">{item.scopePath}</span>
                    )}
                  </span>
                  {item.symbolLine !== undefined && (
                    <span className="command-palette-symbol-line">:{item.symbolLine}</span>
                  )}
                </div>
              );
            }

            return null;
          })}

          {!hasQuery && hintLabel && <div className="command-palette-hint">{hintLabel}</div>}
        </div>
      </div>
    </div>
  );
}

export default CommandPalette;
