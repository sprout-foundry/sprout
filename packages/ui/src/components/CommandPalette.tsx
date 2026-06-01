/**
 * CommandPalette component for @sprout/ui
 *
 * A searchable command palette supporting commands, files, and symbols.
 * Fully props-driven — data fetching is handled by the host application.
 */
import { useState, useEffect, useRef, useCallback, useMemo, useId } from 'react';
import type { ChangeEvent, KeyboardEvent } from 'react';
import { Command as CommandIcon, FileText, Hash, Search } from 'lucide-react';
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
  /**
   * Invoked when the user activates a command result. Return `false` to keep
   * the palette open (useful for commands that change the palette's own
   * state, e.g. switching to "files" mode). Any other return value (or void)
   * triggers the default auto-close behavior.
   */
  onExecuteCommand?: (commandId: string) => void | boolean;
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

  // Resolve raw query to (search-mode, cleaned-query). The first character
  // can override mode: `>` → commands, `@` or `#` → symbols. Otherwise
  // inherit the active mode (with `all` treated as "search everything").
  const { q: searchQuery, searchMode } = useMemo<{ q: string; searchMode: PaletteMode }>(() => {
    if (query.startsWith('>')) return { q: query.slice(1).trim(), searchMode: 'commands' };
    if (query.startsWith('@') || query.startsWith('#')) {
      return { q: query.slice(1).trim(), searchMode: 'symbols' };
    }
    return { q: query, searchMode: mode === 'all' ? 'all' : mode };
  }, [query, mode]);

  // Track the currently-selected result by its stable key so we can restore
  // selection when the result list filters as the user types.
  const selectedKeyRef = useRef<string | null>(null);

  // Race-safe debounced file search. Each call gets a token; only the most
  // recent token may write to state. Without this guard, async backends
  // produce flicker / wrong-result-for-current-query during fast typing.
  const fileSearchTokenRef = useRef(0);
  useEffect(() => {
    if (!searchQuery || !onSearchFiles || (searchMode !== 'all' && searchMode !== 'files')) {
      setFileResults([]);
      return;
    }
    const token = ++fileSearchTokenRef.current;
    const timer = setTimeout(() => {
      onSearchFiles(searchQuery)
        .then((results) => {
          if (token === fileSearchTokenRef.current) setFileResults(results);
        })
        .catch(() => {
          if (token === fileSearchTokenRef.current) setFileResults([]);
        });
    }, 150);
    return () => clearTimeout(timer);
  }, [searchQuery, searchMode, onSearchFiles]);

  // Build results
  const results = useMemo(() => {
    if (!query) return [];

    const q = searchQuery;

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
        const fileScore = fuzzyScore(q, file.path);
        items.push({
          kind: 'file',
          filePath: file.path,
          fileName: file.name,
          fileDirectory: dir,
          highlightedLabel: highlightMatches(file.name, fileScore.matches),
          score: fileScore.score,
          key: `file-${file.path}`,
        });
      }
    }

    // Symbols
    if ((searchMode === 'all' || searchMode === 'symbols') && onSearchSymbols) {
      const symbols = onSearchSymbols(q);
      for (const sym of symbols) {
        const symScore = fuzzyScore(q, sym.name);
        items.push({
          kind: 'symbol',
          highlightedLabel: highlightMatches(sym.name, symScore.matches),
          score: symScore.score,
          symbolLine: sym.line,
          symbolKind: sym.kind,
          scopePath: sym.scopePath,
          key: `sym-${sym.name}-${sym.line}`,
        });
      }
    }

    return items.sort((a, b) => b.score - a.score).slice(0, 50);
  }, [query, searchQuery, searchMode, commands, fileResults, onSearchSymbols]);

  // Remember which item the user has selected (by its stable key).
  useEffect(() => {
    selectedKeyRef.current = results[selectedIndex]?.key ?? null;
  }, [selectedIndex, results]);

  // When the result list changes, try to keep the previously-selected item
  // highlighted. If it's no longer in the list, clamp to 0. This makes
  // refining a query feel non-jarring: typing one more char doesn't yank
  // the user back to the top of the list.
  useEffect(() => {
    if (results.length === 0) return;
    const key = selectedKeyRef.current;
    if (key) {
      const idx = results.findIndex((r) => r.key === key);
      if (idx >= 0) {
        if (idx !== selectedIndex) setSelectedIndex(idx);
        return;
      }
    }
    if (selectedIndex >= results.length) setSelectedIndex(0);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [results]);

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

  // Keep the selected item visible as the user navigates with arrows or
  // PageUp/PageDown. Without this, the selection rectangle scrolls off
  // screen and the user loses track of where they are. Guarded for jsdom
  // (which doesn't implement scrollIntoView).
  useEffect(() => {
    if (!listRef.current || results.length === 0) return;
    const selectedEl = listRef.current.querySelector<HTMLElement>(
      `#command-palette-result-${selectedIndex}`,
    );
    if (selectedEl && typeof selectedEl.scrollIntoView === 'function') {
      selectedEl.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    }
  }, [selectedIndex, results.length]);

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
      if (e.key === 'PageDown') {
        e.preventDefault();
        setSelectedIndex((prev) => Math.min(prev + 10, results.length - 1));
        return;
      }
      if (e.key === 'PageUp') {
        e.preventDefault();
        setSelectedIndex((prev) => Math.max(prev - 10, 0));
        return;
      }
      if (e.key === 'Enter' && results.length > 0) {
        e.preventDefault();
        const result = results[selectedIndex];
        if (!result) return;

        if (result.kind === 'command' && result.commandId) {
          const keepOpen = onExecuteCommand?.(result.commandId) === false;
          if (keepOpen) return;
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
          <Search size={14} className="command-palette-input-icon" aria-hidden="true" />
          <input
            ref={inputRef}
            className="command-palette-input"
            value={query}
            onChange={(e: ChangeEvent<HTMLInputElement>) => {
              setQuery(e.target.value);
            }}
            onKeyDown={handleKeyDown}
            placeholder={
              mode === 'files'
                ? 'Search files…'
                : mode === 'symbols'
                  ? 'Search symbols…'
                  : '> for commands, @ for symbols, type to find files'
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
          aria-live="polite"
        >
          {results.length === 0 && query && (
            <div className="command-palette-empty">
              <div className="command-palette-empty-title">
                No results for “{searchQuery || query}”
              </div>
              <div className="command-palette-empty-hint">
                {searchMode === 'commands'
                  ? 'Try a different command name'
                  : searchMode === 'symbols'
                    ? 'No matching symbols in the active file'
                    : 'Type > for commands, @ for symbols, or check spelling'}
              </div>
            </div>
          )}
          {results.length === 0 && !query && (
            <div className="command-palette-empty command-palette-empty--idle">
              <div className="command-palette-empty-title">Search files, symbols, and commands</div>
              <div className="command-palette-empty-hint">
                Type to find files · <kbd>&gt;</kbd> commands · <kbd>@</kbd> symbols
              </div>
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
                  const keepOpen = onExecuteCommand?.(result.commandId) === false;
                  if (keepOpen) return;
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
                  {result.kind === 'command' ? (
                    <CommandIcon size={12} />
                  ) : result.kind === 'file' ? (
                    <FileText size={12} />
                  ) : (
                    <Hash size={12} />
                  )}
                </span>
                <span dangerouslySetInnerHTML={{ __html: result.highlightedLabel }} />
              </span>
              {result.kind === 'file' && result.fileDirectory && (
                <span className="command-palette-item-path">{result.fileDirectory}</span>
              )}
              {result.kind === 'symbol' && result.scopePath && (
                <span className="command-palette-item-path command-palette-item-path--symbol">
                  {result.scopePath}
                </span>
              )}
              {result.kind === 'command' && (
                <span className="command-palette-item-category">
                  {commands.find((c) => c.id === result.commandId)?.category}
                </span>
              )}
            </div>
          ))}
        </div>
        <div className="command-palette-footer" aria-hidden="true">
          <span className="command-palette-hint">
            <kbd>↑</kbd>
            <kbd>↓</kbd>
            <span>navigate</span>
          </span>
          <span className="command-palette-hint">
            <kbd>↵</kbd>
            <span>open</span>
          </span>
          <span className="command-palette-hint">
            <kbd>Esc</kbd>
            <span>close</span>
          </span>
          {results.length > 0 && (
            <span className="command-palette-hint command-palette-hint--count">
              {selectedIndex + 1} / {results.length}
            </span>
          )}
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
