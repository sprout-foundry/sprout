import React, { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { useHotkeys } from '../contexts/HotkeyContext';
import { clientFetch } from '../services/clientSession';
import './CommandPalette.css';

interface CommandPaletteProps {
  isOpen: boolean;
  onClose: () => void;
  onOpenFile: (filePath: string) => void;
  onViewChange: (view: 'chat' | 'editor' | 'git') => void;
  onToggleSidebar: () => void;
  onToggleTerminal: () => void;
  onOpenHotkeysConfig: () => void;
}

interface FileResult {
  name: string;
  path: string;
  type: string;
}

interface FlatCommand {
  id: string;
  label: string;
  category: string;
  isCategoryHeader?: boolean;
}

const MAX_FILE_RESULTS = 200;
const MAX_INDEXED_FILES = 12000;
const MAX_INDEXED_DIRECTORIES = 3000;
const SKIP_DIRECTORIES = new Set([
  '.git',
  'node_modules',
  '.ledit',
  '.next',
  'dist',
  'build',
]);

// Command definitions — starting set
const COMMAND_DEFINITIONS: Array<{ id: string; label: string; category: string }> = [
  // File
  { id: 'quick_open', label: 'Go to File...', category: 'File' },
  { id: 'new_file', label: 'New File', category: 'File' },
  { id: 'save_file', label: 'Save File', category: 'File' },
  { id: 'save_all_files', label: 'Save All Files', category: 'File' },
  { id: 'close_editor', label: 'Close Editor', category: 'File' },
  // View
  { id: 'command_palette', label: 'Show All Commands', category: 'General' },
  { id: 'toggle_explorer', label: 'Toggle File Explorer', category: 'View' },
  { id: 'toggle_sidebar', label: 'Toggle Sidebar', category: 'View' },
  { id: 'toggle_terminal', label: 'Toggle Terminal', category: 'View' },
  { id: 'split_editor_vertical', label: 'Split Editor Vertical', category: 'View' },
  { id: 'split_editor_horizontal', label: 'Split Editor Horizontal', category: 'View' },
  { id: 'close_all_editors', label: 'Close All Editors', category: 'File' },
  { id: 'close_other_editors', label: 'Close Other Editors', category: 'File' },
  { id: 'focus_next_tab', label: 'Focus Next Tab', category: 'Navigation' },
  { id: 'focus_prev_tab', label: 'Focus Previous Tab', category: 'Navigation' },
  // Navigation
  { id: 'switch_to_chat', label: 'Switch to Chat', category: 'Navigation' },
  { id: 'switch_to_editor', label: 'Switch to Editor', category: 'Navigation' },
  { id: 'switch_to_git', label: 'Switch to Git', category: 'Navigation' },
  // Preferences
  { id: 'open_hotkeys_config', label: 'Edit Keyboard Shortcuts', category: 'Preferences' },
];

type Mode = 'command' | 'file';

const CommandPalette: React.FC<CommandPaletteProps> = ({
  isOpen,
  onClose,
  onOpenFile,
  onViewChange,
  onToggleSidebar,
  onToggleTerminal,
  onOpenHotkeysConfig,
}) => {
  const { hotkeyForCommand } = useHotkeys();

  // State
  const [rawInput, setRawInput] = useState('');
  const [mode, setMode] = useState<Mode>('command');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [isLoadingFiles, setIsLoadingFiles] = useState(false);
  const [allFiles, setAllFiles] = useState<FileResult[]>([]);

  const inputRef = useRef<HTMLInputElement>(null);
  const resultsRef = useRef<HTMLDivElement>(null);
  const prevOpenRef = useRef(false);

  // Reset state when palette opens/closes
  useEffect(() => {
    if (isOpen && !prevOpenRef.current) {
      // Opened
      setRawInput('>');
      setMode('command');
      setSelectedIndex(0);
    } else if (!isOpen && prevOpenRef.current) {
      // Closed
      setRawInput('');
      setMode('command');
      setSelectedIndex(0);
      setAllFiles([]);
    }
    prevOpenRef.current = isOpen;
  }, [isOpen]);

  // Auto-focus input when palette opens
  useEffect(() => {
    if (isOpen && inputRef.current) {
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [isOpen]);

  // Determine search query (strip the > prefix in command mode)
  const searchQuery = mode === 'command'
    ? rawInput.startsWith('>') ? rawInput.slice(1).trim() : ''
    : rawInput.trim();

  // Filter commands by query
  const flatFilteredItems = useMemo(() => {
    if (mode !== 'command') return [];

    const lower = searchQuery.toLowerCase();
    const items: FlatCommand[] = [];
    const lastCategory = { value: '' };

    for (const cmd of COMMAND_DEFINITIONS) {
      if (lower && !cmd.label.toLowerCase().includes(lower)) continue;

      // Add category header if new category
      if (cmd.category !== lastCategory.value) {
        lastCategory.value = cmd.category;
        items.push({ id: `__cat__${cmd.category}`, label: cmd.category, category: cmd.category, isCategoryHeader: true });
      }
      items.push({ id: cmd.id, label: cmd.label, category: cmd.category });
    }
    return items;
  }, [mode, searchQuery]);

  // Filter files by query
  const filteredFiles = useMemo(() => {
    if (mode !== 'file' || !searchQuery) return [];
    const lower = searchQuery.toLowerCase();
    const getFileScore = (file: FileResult): number => {
      const name = file.name.toLowerCase();
      const path = file.path.toLowerCase();
      if (name === lower) return 1200;
      if (name.startsWith(lower)) return 1000;
      if (name.includes(lower)) return 700;
      if (path.endsWith(`/${lower}`)) return 650;
      if (path.includes(`/${lower}`)) return 540;
      if (path.includes(lower)) return 400;
      return -1;
    };

    return allFiles
      .map((file) => ({ file, score: getFileScore(file) }))
      .filter((entry) => entry.score >= 0)
      .sort((a, b) => {
        if (b.score !== a.score) return b.score - a.score;
        if (a.file.path.length !== b.file.path.length) return a.file.path.length - b.file.path.length;
        return a.file.path.localeCompare(b.file.path);
      })
      .slice(0, MAX_FILE_RESULTS)
      .map((entry) => entry.file);
  }, [mode, searchQuery, allFiles]);

  // Fetch file list when switching to file mode
  useEffect(() => {
    if (mode !== 'file') return;
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

            indexedFiles.push({
              name: entryName,
              path: entryPath,
              type: entryType,
            });

            if (indexedFiles.length >= MAX_INDEXED_FILES) {
              break;
            }
          }
        }

        if (!cancelled) {
          setAllFiles(indexedFiles);
        }
      } catch (err) {
        console.error('Failed to browse files:', err);
      } finally {
        if (!cancelled) {
          setIsLoadingFiles(false);
        }
      }
    };
    doFetch();

    return () => {
      cancelled = true;
    };
  }, [mode]);

  // Reset selected index when query changes
  useEffect(() => {
    setSelectedIndex(0);
  }, [searchQuery, mode]);

  // Scroll selected item into view
  useEffect(() => {
    const container = resultsRef.current;
    if (!container) return;
    const selected = container.querySelector('[data-selected="true"]');
    if (selected) {
      selected.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
  }, [selectedIndex]);

  // Execute a command by id
  const executeCommand = useCallback((commandId: string) => {
    switch (commandId) {
      case 'command_palette':
        // Already open — do nothing
        break;
      case 'quick_open':
        // Switch to file mode
        setRawInput('');
        setMode('file');
        return; // Don't close the palette
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
        onViewChange('chat');
        break;
      case 'switch_to_editor':
        onViewChange('editor');
        break;
      case 'switch_to_git':
        onViewChange('git');
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
      default:
        break;
    }
    onClose();
  }, [onClose, onViewChange, onToggleSidebar, onToggleTerminal, onOpenHotkeysConfig]);

  // Compute selected flat index: skip category headers
  const navigableIndex = useMemo(() => {
    if (mode === 'file') return selectedIndex;
    // Count non-category items up to selectedIndex
    let count = 0;
    for (let i = 0; i < flatFilteredItems.length; i++) {
      if (!flatFilteredItems[i].isCategoryHeader) {
        if (count === selectedIndex) return i;
        count++;
      }
    }
    return 0;
  }, [mode, selectedIndex, flatFilteredItems]);

  // Handle text input changes
  const handleInputChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const value = e.target.value;
    const newMode: Mode = value.startsWith('>') ? 'command' : 'file';
    setRawInput(value);
    setMode(newMode);
  }, []);

  // Handle keyboard navigation
  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    const totalItems = mode === 'command' ? flatFilteredItems.length : filteredFiles.length;

    // Prevent browser defaults for Ctrl/Cmd+key combinations so they don't
    // trigger browser actions (e.g. Ctrl+P = print, Ctrl+E = address bar).
    // The HotkeyContext global listener won't fire while an input is focused,
    // so we must intercept these here.
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
        setSelectedIndex(prev => Math.min(prev + 1, Math.max(totalItems - 1, 0)));
        break;

      case 'ArrowUp':
        e.preventDefault();
        setSelectedIndex(prev => Math.max(prev - 1, 0));
        break;

      case 'Enter':
        e.preventDefault();
        e.stopPropagation();
        if (mode === 'command') {
          const item = flatFilteredItems[navigableIndex];
          if (item && !item.isCategoryHeader) {
            executeCommand(item.id);
          }
        } else if (mode === 'file') {
          const file = filteredFiles[selectedIndex];
          if (file) {
            onOpenFile(file.path);
            onClose();
          }
        }
        break;

      case 'Tab':
        e.preventDefault();
        if (mode === 'command') {
          setRawInput('');
          setMode('file');
        } else {
          setRawInput('>');
          setMode('command');
        }
        break;

      case 'Backspace':
        // If in command mode and only ">" left, don't delete the prefix
        if (mode === 'command' && rawInput === '>') {
          e.preventDefault();
        }
        break;
    }
  }, [mode, flatFilteredItems, filteredFiles, selectedIndex, rawInput, executeCommand, onClose, onOpenFile, navigableIndex]);

  // Handle overlay click to close
  const handleOverlayClick = useCallback((e: React.MouseEvent) => {
    if (e.target === e.currentTarget) {
      onClose();
    }
  }, [onClose]);

  if (!isOpen) return null;

  const displayValue = rawInput;
  const placeholder = mode === 'command'
    ? 'Type a command...'
    : 'Search files by name...';

  return (
    <div className="command-palette-overlay" onClick={handleOverlayClick}>
      <div className="command-palette-container" onClick={(e) => e.stopPropagation()}>
        {/* Prefix indicator */}
        <div className="command-palette-input-wrapper">
          <span className="command-palette-prefix">
            {mode === 'command' ? '>' : ''}
          </span>
          <input
            ref={inputRef}
            type="text"
            className="command-palette-input"
            value={displayValue}
            onChange={handleInputChange}
            onKeyDown={handleKeyDown}
            placeholder={placeholder}
            autoComplete="off"
            autoCorrect="off"
            autoCapitalize="off"
            spellCheck={false}
          />
        </div>

        <div className="command-palette-results" ref={resultsRef}>
          {mode === 'command' ? (
            /* Command results */
            <>
              {flatFilteredItems.length === 0 && searchQuery && (
                <div className="command-palette-empty">No matching commands</div>
              )}
              {flatFilteredItems.map((item, index) => {
                if (item.isCategoryHeader) {
                  return (
                    <div key={item.id} className="command-palette-category">
                      {item.label}
                    </div>
                  );
                }
                const isSelected = index === navigableIndex;
                const shortcut = hotkeyForCommand(item.id);
                return (
                  <div
                    key={item.id}
                    data-selected={isSelected}
                    className={`command-palette-item ${isSelected ? 'command-palette-selected' : ''}`}
                    onClick={() => executeCommand(item.id)}
                    onMouseEnter={() => setSelectedIndex(
                      flatFilteredItems.slice(0, index).filter(i => !i.isCategoryHeader).length
                    )}
                  >
                    <span className="command-palette-label">{item.label}</span>
                    {shortcut && (
                      <span className="command-palette-shortcut">{shortcut}</span>
                    )}
                  </div>
                );
              })}
              {flatFilteredItems.length === 0 && !searchQuery && (
                <div className="command-palette-hint">
                  Type to search commands · Tab to switch to file mode · Esc to close
                </div>
              )}
            </>
          ) : (
            /* File results */
            <>
              {isLoadingFiles && (
                <div className="command-palette-empty">Loading files...</div>
              )}
              {!isLoadingFiles && filteredFiles.length === 0 && searchQuery && (
                <div className="command-palette-empty">No matching files</div>
              )}
              {filteredFiles.map((file, index) => (
                <div
                  key={file.path}
                  data-selected={index === selectedIndex}
                  className={`command-palette-item ${index === selectedIndex ? 'command-palette-selected' : ''}`}
                  onClick={() => {
                    onOpenFile(file.path);
                    onClose();
                  }}
                  onMouseEnter={() => setSelectedIndex(index)}
                >
                  <span className="command-palette-file-icon">
                    {file.type === 'directory' ? '📁 ' : '📄 '}
                  </span>
                  <span className="command-palette-file-path">{file.path}</span>
                </div>
              ))}
              {filteredFiles.length === 0 && !searchQuery && (
                <div className="command-palette-hint">
                  Type to search files · Tab to switch to command mode · Esc to close
                </div>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
};

export default CommandPalette;
