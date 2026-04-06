import './CommandPalette.css';
import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import type { ChangeEvent, KeyboardEvent, MouseEvent } from 'react';
import { useHotkeys } from '../contexts/HotkeyContext';
import { clientFetch } from '../services/clientSession';
import { clearLayoutSnapshot } from '../services/layoutPersistence';
import { fuzzyFilter, highlightMatches } from '../utils/fuzzyMatch';
import type { FuzzyResult } from '../utils/fuzzyMatch';
import { useLog, debugLog } from '../utils/log';

interface CommandPaletteProps {
  isOpen: boolean;
  onClose: () => void;
  onOpenFile: (filePath: string) => void;
  onToggleSidebar: () => void;
  onToggleTerminal: () => void;
  onOpenHotkeysConfig: () => void;
}

interface FileResult {
  name: string;
  path: string;
  type: string;
}

// ── Command definitions ────────────────────────────────────────────────────

interface CommandDef {
  id: string;
  label: string;
  category: string;
}

const COMMAND_DEFINITIONS: CommandDef[] = [
  // File
  { id: 'quick_open', label: 'Go to File...', category: 'File' },
  { id: 'new_file', label: 'New File', category: 'File' },
  { id: 'save_file', label: 'Save File', category: 'File' },
  { id: 'save_all_files', label: 'Save All Files', category: 'File' },
  { id: 'close_editor', label: 'Close Editor', category: 'File' },
  { id: 'close_all_editors', label: 'Close All Editors', category: 'File' },
  { id: 'close_other_editors', label: 'Close Other Editors', category: 'File' },
  // View
  { id: 'command_palette', label: 'Show All Commands', category: 'General' },
  { id: 'toggle_explorer', label: 'Toggle File Explorer', category: 'View' },
  { id: 'toggle_sidebar', label: 'Toggle Sidebar', category: 'View' },
  { id: 'toggle_terminal', label: 'Toggle Terminal', category: 'View' },
  { id: 'split_editor_vertical', label: 'Split Editor Vertical', category: 'View' },
  { id: 'split_editor_horizontal', label: 'Split Editor Horizontal', category: 'View' },
  { id: 'split_editor_grid', label: 'Split Editor Grid', category: 'View' },
  { id: 'split_terminal_vertical', label: 'Split Terminal Vertical', category: 'View' },
  { id: 'split_terminal_horizontal', label: 'Split Terminal Horizontal', category: 'View' },
  { id: 'editor_toggle_word_wrap', label: 'Toggle Word Wrap', category: 'View' },
  { id: 'toggle_minimap', label: 'Toggle Minimap', category: 'View' },
  { id: 'toggle_linked_scroll', label: 'Toggle Linked Scrolling', category: 'View' },
  { id: 'reset_saved_layout', label: 'Reset Saved Layout', category: 'View' },
  // Navigation
  { id: 'focus_next_tab', label: 'Focus Next Tab', category: 'Navigation' },
  { id: 'focus_prev_tab', label: 'Focus Previous Tab', category: 'Navigation' },
  { id: 'switch_to_chat', label: 'Switch to Chat', category: 'Navigation' },
  { id: 'switch_to_editor', label: 'Switch to Editor', category: 'Navigation' },
  { id: 'switch_to_git', label: 'Switch to Git', category: 'Navigation' },
  // Preferences
  { id: 'open_hotkeys_config', label: 'Edit Keyboard Shortcuts', category: 'Preferences' },
];

// ── File browsing constants ────────────────────────────────────────────────

const MAX_FILE_RESULTS = 100;
const MAX_INDEXED_FILES = 12000;
const MAX_INDEXED_DIRECTORIES = 3000;
const SKIP_DIRECTORIES = new Set(['.git', 'node_modules', '.ledit', '.next', 'dist', 'build']);

// ── Unified result types ───────────────────────────────────────────────────

type ResultKind = 'command' | 'file' | 'commands-header' | 'files-header';

interface PaletteResult {
  kind: ResultKind;
  /** For command results — the command id. */
  commandId?: string;
  /** For command results — display label. */
  commandLabel?: string;
  /** For file results — the file path. */
  filePath?: string;
  /** For file results — file name. */
  fileName?: string;
  /** Highlighted HTML for the primary label. */
  highlightedLabel: string;
  /** Score from fuzzy matcher (lower = worse). */
  score: number;
}

// ── Component ──────────────────────────────────────────────────────────────

function CommandPalette({
  isOpen,
  onClose,
  onOpenFile,
  onToggleSidebar,
  onToggleTerminal,
  onOpenHotkeysConfig,
}: CommandPaletteProps): JSX.Element | null {
  const { hotkeyForCommand } = useHotkeys();
  const log = useLog();

  // State
  const [query, setQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [isLoadingFiles, setIsLoadingFiles] = useState(false);
  const [allFiles, setAllFiles] = useState<FileResult[]>([]);
  const [prefersCommandsOnly, setPrefersCommandsOnly] = useState(false);
  const [prefersFilesOnly, setPrefersFilesOnly] = useState(false);

  const inputRef = useRef<HTMLInputElement>(null);
  const resultsRef = useRef<HTMLDivElement>(null);
  const prevOpenRef = useRef(false);

  // ── Lifecycle: reset on open/close ────────────────────────────────────

  useEffect(() => {
    if (isOpen && !prevOpenRef.current) {
      setQuery('');
      setSelectedIndex(0);
      setPrefersCommandsOnly(false);
      setPrefersFilesOnly(false);
    } else if (!isOpen && prevOpenRef.current) {
      setQuery('');
      setSelectedIndex(0);
      setAllFiles([]);
      setPrefersCommandsOnly(false);
      setPrefersFilesOnly(false);
    }
    prevOpenRef.current = isOpen;
  }, [isOpen]);

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

    const doFetch = async () => {
      setIsLoadingFiles(true);
      try {
        const queue: string[] = ['.'];
        const indexedFiles: FileResult[] = [];
        const visited = new Set<string>();
        let visitedDirs = 0;

        while (queue.length > 0 && indexedFiles.length < MAX_INDEXED_FILES && visitedDirs < MAX_INDEXED_DIRECTORIES) {
          const dir = queue.shift();
          if (!dir || visited.has(dir)) continue;
          visited.add(dir);
          visitedDirs += 1;

          const response = await clientFetch(`/api/browse?path=${encodeURIComponent(dir)}`);
          if (!response.ok) continue;

          const data = await response.json();
          const entries = Array.isArray(data.files) ? data.files : [];

          for (const entry of entries) {
            const entryPath = String(entry.path || '');
            const entryName = String(entry.name || entryPath.split('/').pop() || '');
            const entryType = String(entry.type || 'file');

            if (!entryPath || !entryName) continue;

            if (entryType === 'directory') {
              if (!SKIP_DIRECTORIES.has(entryName)) {
                queue.push(entryPath);
              }
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
  }, [isOpen, log]); // eslint-disable-line react-hooks/exhaustive-deps

  // ── Build unified results: commands first, then files ─────────────────

  const results = useMemo((): PaletteResult[] => {
    const trimmed = query.trim();

    if (!trimmed) {
      // No query — show all commands as a shortcut list
      const items: PaletteResult[] = [];
      let lastCat = '';

      for (const cmd of COMMAND_DEFINITIONS) {
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

    // ── Fuzzy-match commands ─────────────────────────────────────────────
    if (!prefersFilesOnly) {
      const cmdResults: FuzzyResult<CommandDef>[] = fuzzyFilter(trimmed, COMMAND_DEFINITIONS, (cmd) => cmd.label, 50);

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

    // ── Fuzzy-match files ────────────────────────────────────────────────
    if (!prefersCommandsOnly && allFiles.length > 0) {
      const fileResults: FuzzyResult<FileResult>[] = fuzzyFilter(trimmed, allFiles, (f) => f.path, MAX_FILE_RESULTS);

      if (fileResults.length > 0) {
        items.push({ kind: 'files-header', highlightedLabel: 'Files', score: -1 });
        for (const r of fileResults) {
          items.push({
            kind: 'file',
            filePath: r.item.path,
            fileName: r.item.name,
            highlightedLabel: highlightMatches(r.item.path, r.matches),
            score: r.score,
          });
        }
      }
    }

    return items;
  }, [query, allFiles, prefersCommandsOnly, prefersFilesOnly]);

  // ── Navigable items (skip headers for arrow-key selection) ────────────

  const navigableItems = useMemo(() => results.filter((r) => r.kind === 'command' || r.kind === 'file'), [results]);

  // ── Reset selected index when results or query change ─────────────────

  useEffect(() => {
    setSelectedIndex(0);
  }, [query, prefersCommandsOnly, prefersFilesOnly]);

  // ── Scroll selected item into view ────────────────────────────────────

  useEffect(() => {
    const container = resultsRef.current;
    if (!container) return;
    const selected = container.querySelector('[data-selected="true"]');
    if (selected) {
      selected.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
  }, [selectedIndex]);

  // ── Execute a command by id ───────────────────────────────────────────

  const executeCommand = useCallback(
    (commandId: string) => {
      switch (commandId) {
        case 'command_palette':
          break; // already open
        case 'quick_open':
          setPrefersFilesOnly(true);
          setPrefersCommandsOnly(false);
          return; // keep palette open and refocus
        case 'toggle_explorer':
        case 'toggle_sidebar':
          onToggleSidebar();
          break;
        case 'toggle_terminal':
          onToggleTerminal();
          break;
        case 'new_file':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: 'new_file' } }));
          break;
        case 'switch_to_chat':
        case 'switch_to_editor':
        case 'switch_to_git':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId } }));
          break;
        case 'open_hotkeys_config':
          onOpenHotkeysConfig();
          break;
        case 'split_editor_vertical':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: 'split_editor_vertical' } }));
          break;
        case 'split_editor_horizontal':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: 'split_editor_horizontal' } }));
          break;
        case 'split_editor_grid':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: 'split_editor_grid' } }));
          break;
        case 'split_terminal_vertical':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: 'split_terminal_vertical' } }));
          break;
        case 'split_terminal_horizontal':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: 'split_terminal_horizontal' } }));
          break;
        case 'editor_toggle_word_wrap':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: 'editor_toggle_word_wrap' } }));
          break;
        case 'toggle_linked_scroll':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: 'toggle_linked_scroll' } }));
          break;
        case 'toggle_minimap':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: 'toggle_minimap' } }));
          break;
        case 'close_editor':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: 'close_editor' } }));
          break;
        case 'save_file':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: 'save_file' } }));
          break;
        case 'save_all_files':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: 'save_all_files' } }));
          break;
        case 'close_all_editors':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: 'close_all_editors' } }));
          break;
        case 'close_other_editors':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: 'close_other_editors' } }));
          break;
        case 'focus_next_tab':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: 'focus_next_tab' } }));
          break;
        case 'focus_prev_tab':
          window.dispatchEvent(new CustomEvent('ledit:hotkey', { detail: { commandId: 'focus_prev_tab' } }));
          break;
        case 'reset_saved_layout': {
          if (!window.confirm('Reset all saved layout settings? This cannot be undone.')) break;
          clearLayoutSnapshot();
          const keys = [
            'ledit.editor.paneLayout',
            'ledit.editor.paneSizes',
            'ledit-terminal-height',
            'ledit-terminal-expanded',
            'ledit-sidebar-collapsed',
            'ledit-sidebar-width',
            'ledit.contextPanel.width',
            'ledit.contextPanel.collapsed',
            'editor:minimap-enabled',
            'filetree-show-ignored',
          ];
          for (const key of keys) {
            try {
              window.localStorage.removeItem(key);
            } catch (err) {
              debugLog('[resetPreferences] localStorage.removeItem failed:', err);
            }
          }
          window.location.reload();
          return; // page is reloading — skip onClose()
        }
        default:
          break;
      }
      onClose();
    },
    [onClose, onToggleSidebar, onToggleTerminal, onOpenHotkeysConfig],
  );

  // ── Select & execute the currently selected item ──────────────────────

  const executeSelected = useCallback(() => {
    const item = navigableItems[selectedIndex];
    if (!item) return;

    if (item.kind === 'command' && item.commandId) {
      executeCommand(item.commandId);
    } else if (item.kind === 'file' && item.filePath) {
      onOpenFile(item.filePath);
      onClose();
    }
  }, [navigableItems, selectedIndex, executeCommand, onOpenFile, onClose]);

  // ── Determine the navigable index for a given result index ────────────

  const toNavigableIndex = useCallback(
    (resultIndex: number) => {
      let count = 0;
      for (let i = 0; i < results.length; i++) {
        if (results[i].kind === 'command' || results[i].kind === 'file') {
          if (i === resultIndex) return count;
          count++;
        }
      }
      return 0;
    },
    [results],
  );

  // ── Handle text input ─────────────────────────────────────────────────

  const handleInputChange = useCallback((e: ChangeEvent<HTMLInputElement>) => {
    setQuery(e.target.value);
  }, []);

  // ── Handle keyboard navigation ────────────────────────────────────────

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      // Prevent browser defaults for Ctrl/Cmd+key
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
          if (e.shiftKey) {
            // Shift+Tab → show only commands
            setPrefersCommandsOnly(true);
            setPrefersFilesOnly(false);
          } else if (prefersFilesOnly) {
            // Tab when in files-only → back to unified
            setPrefersFilesOnly(false);
          } else {
            // Tab → show only files
            setPrefersFilesOnly(true);
            setPrefersCommandsOnly(false);
          }
          break;

        case 'Backspace': {
          if (!query && (prefersCommandsOnly || prefersFilesOnly)) {
            e.preventDefault();
            setPrefersCommandsOnly(false);
            setPrefersFilesOnly(false);
          }
          break;
        }
      }
    },
    [navigableItems.length, query, prefersCommandsOnly, prefersFilesOnly, executeSelected, onClose],
  );

  // ── Handle overlay click ──────────────────────────────────────────────

  const handleOverlayClick = useCallback(
    (e: MouseEvent) => {
      if (e.target === e.currentTarget) onClose();
    },
    [onClose],
  );

  // ── Map selectedIndex to flat results index (for highlighting) ───────
  const selectedFlatIndex = useMemo(() => {
    let navCount = 0;
    for (let i = 0; i < results.length; i++) {
      if (results[i].kind === 'command' || results[i].kind === 'file') {
        if (navCount === selectedIndex) return i;
        navCount++;
      }
    }
    return -1;
  }, [results, selectedIndex]);

  if (!isOpen) return null;

  const hasQuery = query.trim().length > 0;

  // ── Active filter indicator ───────────────────────────────────────────

  const filterLabel = prefersCommandsOnly
    ? 'Commands only · Tab for files · Backspace to reset'
    : prefersFilesOnly
      ? 'Files only · Tab or Shift+Tab to reset'
      : '⌘+P · Tab to filter · Esc to close';

  return (
    <div className="command-palette-overlay" onClick={handleOverlayClick}>
      <div className="command-palette-container" onClick={(e) => e.stopPropagation()}>
        {/* Input */}
        <div className="command-palette-input-wrapper">
          <span className="command-palette-prefix">⌘</span>
          <input
            ref={inputRef}
            type="text"
            className="command-palette-input"
            value={query}
            onChange={handleInputChange}
            onKeyDown={handleKeyDown}
            placeholder="Search commands & files…"
            autoComplete="off"
            autoCorrect="off"
            autoCapitalize="off"
            spellCheck={false}
          />
        </div>

        {/* Filter indicator */}
        {(prefersCommandsOnly || prefersFilesOnly) && (
          <div className="command-palette-filter-bar">
            {prefersCommandsOnly && (
              <button
                className="filter-chip active"
                onClick={() => {
                  setPrefersCommandsOnly(false);
                }}
              >
                Commands
              </button>
            )}
            {prefersFilesOnly && (
              <button
                className="filter-chip active"
                onClick={() => {
                  setPrefersFilesOnly(false);
                }}
              >
                Files
              </button>
            )}
            <button
              className="filter-chip"
              onClick={() => {
                setPrefersCommandsOnly(false);
                setPrefersFilesOnly(false);
              }}
            >
              All
            </button>
          </div>
        )}

        {/* Results */}
        <div className="command-palette-results" ref={resultsRef}>
          {isLoadingFiles && !hasQuery && <div className="command-palette-loading">Loading files…</div>}

          {results.length === 0 && hasQuery && !isLoadingFiles && (
            <div className="command-palette-empty">No matching commands or files</div>
          )}

          {results.map((item, index) => {
            if (item.kind === 'commands-header' || item.kind === 'files-header') {
              return (
                <div key={`${item.kind}-${index}-${item.highlightedLabel}`} className="command-palette-category">
                  {item.highlightedLabel}
                </div>
              );
            }

            const isSelected = index === selectedFlatIndex;

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

            const filePath = item.filePath;
            if (!filePath) return null;
            return (
              <div
                key={`file-${filePath}`}
                data-selected={isSelected}
                className={`command-palette-item ${isSelected ? 'command-palette-selected' : ''}`}
                onClick={() => {
                  onOpenFile(filePath);
                  onClose();
                }}
                onMouseEnter={() => setSelectedIndex(toNavigableIndex(index))}
              >
                <span className="command-palette-file-icon">📄</span>
                <span
                  className="command-palette-file-path"
                  dangerouslySetInnerHTML={{ __html: item.highlightedLabel }}
                />
              </div>
            );
          })}

          {!hasQuery && <div className="command-palette-hint">{filterLabel}</div>}
        </div>
      </div>
    </div>
  );
};

export default CommandPalette;
