import React, { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { useHotkeys } from '../contexts/HotkeyContext';
import { ApiService } from '../services/api';
import './CommandPalette.css';

interface CommandPaletteProps {
  isOpen: boolean;
  onClose: () => void;
  onOpenFile: (filePath: string) => void;
  onViewChange: (view: 'chat' | 'editor' | 'git' | 'logs') => void;
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

const apiService = ApiService.getInstance();

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
  // Navigation
  { id: 'switch_to_chat', label: 'Switch to Chat', category: 'Navigation' },
  { id: 'switch_to_editor', label: 'Switch to Editor', category: 'Navigation' },
  { id: 'switch_to_git', label: 'Switch to Git', category: 'Navigation' },
  { id: 'switch_to_logs', label: 'Switch to Logs', category: 'Navigation' },
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
  const browseTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
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
    return allFiles.filter(f =>
      f.name.toLowerCase().includes(lower) ||
      f.path.toLowerCase().includes(lower)
    );
  }, [mode, searchQuery, allFiles]);

  // Fetch file list when switching to file mode
  useEffect(() => {
    if (mode !== 'file') return;

    const doFetch = async () => {
      setIsLoadingFiles(true);
      try {
        const response = await fetch('/api/browse?path=.');
        if (response.ok) {
          const data = await response.json();
          const files: FileResult[] = (data.files || []).map((f: any) => ({
            name: f.name || f.path?.split('/').pop() || '',
            path: f.path || '',
            type: f.type || 'file',
          }));
          setAllFiles(files);
        }
      } catch (err) {
        console.error('Failed to browse files:', err);
      } finally {
        setIsLoadingFiles(false);
      }
    };
    doFetch();
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
        onViewChange('editor');
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
      case 'switch_to_logs':
        onViewChange('logs');
        break;
      case 'open_hotkeys_config':
        onOpenHotkeysConfig();
        break;
      default:
        break;
    }
    onClose();
  }, [onClose, onViewChange, onToggleSidebar, onToggleTerminal, onOpenHotkeysConfig]);

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
        if (mode === 'command') {
          const item = flatFilteredItems[selectedIndex];
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
  }, [mode, flatFilteredItems, filteredFiles, selectedIndex, rawInput, executeCommand, onClose, onOpenFile]);

  // Handle overlay click to close
  const handleOverlayClick = useCallback((e: React.MouseEvent) => {
    if (e.target === e.currentTarget) {
      onClose();
    }
  }, [onClose]);

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

  if (!isOpen) return null;

  const displayValue = mode === 'command' ? rawInput : rawInput;
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
