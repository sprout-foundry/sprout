/**
 * CommandPalette component for @sprout/ui
 *
 * A searchable command palette supporting commands, files, and symbols.
 * Fully props-driven — data fetching is handled by the host application.
 */
import { useState, useEffect, useRef, useCallback, useMemo, useId } from 'react';
import type { ChangeEvent, KeyboardEvent } from 'react';
import { fuzzyScore, highlightMatches } from '../utils/fuzzyMatch';
import { debugLog } from '../utils/log';
import './CommandPalette.css';

export type PaletteMode = 'all' | 'files' | 'symbols' | 'commands';

export interface CommandDef {
  id: string;
  label: string;
  category: string;
}

export interface FileResult {
  name: string;
  path: string;
  type: string;
}

export interface SymbolResult {
  name: string;
  kind: string;
  line: number;
  scopePath?: string;
}

type ResultKind = 'command' | 'file' | 'symbol' | 'header';

interface PaletteResult {
  kind: ResultKind;
  commandId?: string;
  commandLabel?: string;
  filePath?: string;
  fileName?: string;
  fileDirectory?: string;
  highlightedLabel: string;
  score: number;
  symbolLine?: number;
  symbolKind?: string;
  scopePath?: string;
  /** Stable unique key for React rendering */
  key: string;
}

export interface CommandPaletteProps {
  isOpen: boolean;
  onClose: () => void;
  onOpenFile: (filePath: string) => void;
  onExecuteCommand?: (commandId: string) => void;
  /** Mode to open with */
  initialMode?: PaletteMode;
  /** Navigate to a line in the active editor */
  onNavigateToLine?: (line: number) => void;
  /** Webui-specific: toggle sidebar */
  onToggleSidebar?: () => void;
  /** Webui-specific: toggle terminal */
  onToggleTerminal?: () => void;
  /** Webui-specific: open hotkeys config */
  onOpenHotkeysConfig?: () => void;

  // Data providers
  /** Available commands */
  commands?: CommandDef[];
  /** Callback to search files */
  onSearchFiles?: (query: string) => Promise<FileResult[]>;
  /** Callback to search symbols */
  onSearchSymbols?: (query: string) => SymbolResult[];
  /** Active buffer content for local symbol search */
  activeBufferContent?: string;
  /** Active buffer file extension */
  activeBufferFileExtension?: string;
  /** Workspace root path */
  workspaceRoot?: string;
}

function CommandPalette({
  isOpen,
  onClose,
  onOpenFile,
  onExecuteCommand,
  initialMode = 'all',
  onNavigateToLine,
  commands = [],
  onSearchFiles,
  onSearchSymbols,
  activeBufferContent,
  activeBufferFileExtension,
  workspaceRoot,
}: CommandPaletteProps): JSX.Element | null {
  const [query, setQuery] = useState('');
  const [mode, setMode] = useState<PaletteMode>(initialMode);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [fileResults, setFileResults] = useState<FileResult[]>([]);
  const [ariaAnnouncement, setAriaAnnouncement] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const resultsListId = useId();

  // Reset on open
  useEffect(() => {
    if (isOpen) {
      setQuery('');
      setMode(initialMode);
      setSelectedIndex(0);
      setFileResults([]);
      setTimeout(() => inputRef.current?.focus(), 50);
    }
  }, [isOpen, initialMode]);

  // Search files when query changes
  useEffect(() => {
    if (!query || !onSearchFiles) {
      setFileResults([]);
      return;
    }
    const timer = setTimeout(() => {
      onSearchFiles(query).then(setFileResults).catch(() => setFileResults([]));
    }, 150);
    return () => clearTimeout(timer);
  }, [query, onSearchFiles]);

  // Build results
  const results = useMemo(() => {
    if (!query) return [];

    const q = query.startsWith('>') ? query.slice(1).trim() : query;
    const searchMode = query.startsWith('>') ? 'commands' : mode === 'all' ? 'all' : mode;

    const items: PaletteResult[] = [];

    // Commands
    if (searchMode === 'all' || searchMode === 'commands') {
      for (const cmd of commands) {
        const result = fuzzyScore(q, cmd.label);
        if (result.score > 0.3) {
          items.push({
            kind: 'command',
            commandId: cmd.id,
            commandLabel: cmd.label,
            highlightedLabel: highlightMatches(cmd.label, result.matches),
            score: result.score,
            key: `cmd-${cmd.id}`,
          });
        }
      }
    }

    // Files
    if ((searchMode === 'all' || searchMode === 'files') && fileResults.length > 0) {
      for (const file of fileResults) {
        const dir = file.path.substring(0, file.path.lastIndexOf('/'));
        items.push({
          kind: 'file',
          filePath: file.path,
          fileName: file.name,
          fileDirectory: dir,
          highlightedLabel: highlightMatches(file.name, fuzzyScore(query, file.path).matches),
          score: fuzzyScore(query, file.path).score,
          key: `file-${file.path}`,
        });
      }
    }

    // Symbols
    if ((searchMode === 'all' || searchMode === 'symbols') && onSearchSymbols) {
      const symbols = onSearchSymbols(query);
      for (const sym of symbols) {
        items.push({
          kind: 'symbol',
          highlightedLabel: highlightMatches(sym.name, fuzzyScore(query, sym.name).matches),
          score: fuzzyScore(query, sym.name).score,
          symbolLine: sym.line,
          symbolKind: sym.kind,
          scopePath: sym.scopePath,
          key: `sym-${sym.name}-${sym.line}`,
        });
      }
    }

    return items.sort((a, b) => b.score - a.score).slice(0, 50);
  }, [query, mode, commands, fileResults, onSearchSymbols]);

  // Announce selection changes when navigating with arrow keys.
  // Only depend on selectedIndex, not results, to avoid firing on every result recomputation.
  useEffect(() => {
    // Only announce selection if we have results and a valid index
    if (results.length === 0 || selectedIndex >= results.length) {
      return;
    }
    const item = results[selectedIndex];
    // Strip HTML from highlightedLabel for screen reader
    const plainLabel = item.highlightedLabel.replace(/<[^>]*>/g, '');
    const label = item.kind === 'command' && item.commandLabel
      ? item.commandLabel
      : item.kind === 'file' && item.fileName
        ? item.fileName
        : plainLabel;
    setAriaAnnouncement(`${selectedIndex + 1} of ${results.length}, ${label}`);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedIndex]);

  // Debounce result-count announcements so rapid keystrokes don't queue up speech
  useEffect(() => {
    const timer = setTimeout(() => {
      if (!query) {
        setAriaAnnouncement('');
      } else if (results.length === 0) {
        setAriaAnnouncement('No results found');
      } else {
        setAriaAnnouncement(`${results.length} result${results.length === 1 ? '' : 's'} available`);
      }
    }, 300);
    return () => clearTimeout(timer);
  }, [results.length, query]);

  // Keyboard navigation
  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLInputElement>) => {
      if (e.key === 'Escape') {
        onClose();
        return;
      }
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setSelectedIndex((prev) => Math.min(prev + 1, results.length - 1));
        return;
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault();
        setSelectedIndex((prev) => Math.max(prev - 1, 0));
        return;
      }
      if (e.key === 'Home') {
        e.preventDefault();
        setSelectedIndex(0);
        return;
      }
      if (e.key === 'End') {
        e.preventDefault();
        setSelectedIndex(results.length - 1);
        return;
      }
      if (e.key === 'Enter' && results.length > 0) {
        e.preventDefault();
        const result = results[selectedIndex];
        if (!result) return;

        if (result.kind === 'command' && result.commandId) {
          onExecuteCommand?.(result.commandId);
        } else if (result.kind === 'file' && result.filePath) {
          onOpenFile(result.filePath);
        } else if (result.kind === 'symbol' && result.symbolLine != null) {
          onNavigateToLine?.(result.symbolLine);
        }
        onClose();
      }
    },
    [results, selectedIndex, onClose, onOpenFile, onExecuteCommand, onNavigateToLine],
  );

  if (!isOpen) return null;

  return (
    <div
      className="command-palette-overlay"
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-label="Command palette"
    >
      <div className="command-palette" onClick={(e) => e.stopPropagation()}>
        <div className="command-palette-input-row">
          <input
            ref={inputRef}
            className="command-palette-input"
            value={query}
            onChange={(e: ChangeEvent<HTMLInputElement>) => {
              setQuery(e.target.value);
              setSelectedIndex(0);
            }}
            onKeyDown={handleKeyDown}
            placeholder={
              mode === 'files'
                ? 'Search files by name...'
                : mode === 'symbols'
                  ? 'Search symbols...'
                  : 'Type a command or search...'
            }
            role="combobox"
            aria-haspopup="listbox"
            aria-expanded={true}
            aria-controls={resultsListId}
            aria-activedescendant={
              results.length > 0 && selectedIndex < results.length
                ? `command-palette-result-${selectedIndex}`
                : undefined
            }
            aria-autocomplete="list"
            autoFocus
          />
        </div>
        <div
          className="command-palette-results"
          ref={listRef}
          id={resultsListId}
          role="listbox"
          aria-label="Search results"
        >
          {results.length === 0 && query && (
            <div className="command-palette-empty">
              No results found
            </div>
          )}
          {results.map((result, index) => (
            <div
              id={`command-palette-result-${index}`}
              key={result.key}
              className={`command-palette-item ${index === selectedIndex ? 'selected' : ''}`}
              role="option"
              aria-selected={index === selectedIndex}
              aria-setsize={results.length}
              aria-posinset={index + 1}
              onClick={() => {
                if (result.kind === 'command' && result.commandId) {
                  onExecuteCommand?.(result.commandId);
                } else if (result.kind === 'file' && result.filePath) {
                  onOpenFile(result.filePath);
                } else if (result.kind === 'symbol' && result.symbolLine != null) {
                  onNavigateToLine?.(result.symbolLine);
                }
                onClose();
              }}
            >
              <span className="command-palette-item-label">
                <span className={`result-kind-badge ${result.kind}`} aria-hidden="true">
                  {result.kind === 'command' ? '⌘' : result.kind === 'file' ? '📄' : 'ƒ'}
                </span>
                <span dangerouslySetInnerHTML={{ __html: result.highlightedLabel }} />
              </span>
              {result.kind === 'file' && result.fileDirectory && (
                <span className="command-palette-item-path">{result.fileDirectory}</span>
              )}
              {result.kind === 'command' && (
                <span className="command-palette-item-category">
                  {commands.find((c) => c.id === result.commandId)?.category}
                </span>
              )}
            </div>
          ))}
        </div>
        <div
          className="command-palette-sr-only"
          aria-live="polite"
          aria-atomic="true"
          role="status"
        >
          {ariaAnnouncement}
        </div>
      </div>
    </div>
  );
}

export default CommandPalette;
