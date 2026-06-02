/**
 * CommandPalette component for @sprout/ui
 *
 * A searchable command palette supporting commands, files, and symbols.
 * Fully props-driven — data fetching is handled by the host application.
 */
import { Fragment, useState, useEffect, useRef, useCallback, useMemo, useId } from 'react';
import type { ChangeEvent, KeyboardEvent } from 'react';
import { Command as CommandIcon, FileText, Hash, Loader2, Search, X } from 'lucide-react';
import { fuzzyScore, highlightMatches } from '../utils/fuzzyMatch';
import { debugLog } from '../utils/log';
import './CommandPalette.css';

export type PaletteMode = 'all' | 'files' | 'symbols' | 'commands';

export interface CommandDef {
  id: string;
  label: string;
  category: string;
  /** Pre-formatted keyboard shortcut to show next to the command (e.g.
   *  "Cmd+Shift+P"). Optional — when omitted, no kbd is rendered. */
  shortcut?: string;
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

const KIND_LABEL: Record<'command' | 'file' | 'symbol', string> = {
  command: 'Commands',
  file: 'Files',
  symbol: 'Symbols',
};

const MODE_ORDER: PaletteMode[] = ['all', 'files', 'symbols', 'commands'];

/** Split a `Cmd+Shift+P` style shortcut into individual chip strings.
 *  Replaces common modifier names with single-char glyphs so the chips
 *  read compactly in the row. */
function splitShortcut(shortcut: string): string[] {
  return shortcut
    .split('+')
    .map((s) => s.trim())
    .filter(Boolean)
    .map((part) => {
      const lower = part.toLowerCase();
      if (lower === 'cmd' || lower === 'meta' || lower === 'command') return '⌘';
      if (lower === 'shift') return '⇧';
      if (lower === 'alt' || lower === 'option') return '⌥';
      if (lower === 'ctrl' || lower === 'control') return '⌃';
      if (lower === 'enter' || lower === 'return') return '↵';
      if (lower === 'esc' || lower === 'escape') return 'Esc';
      if (lower === 'space') return '␣';
      if (lower === 'tab') return '⇥';
      if (lower === 'backspace') return '⌫';
      return part.toUpperCase();
    });
}

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
  /** Recently-opened files, shown as the landing list when the user opens
   *  the palette in `all` or `files` mode with an empty query. Order is
   *  preserved (most-recent first). */
  recentFiles?: FileResult[];
  /** True while the host is indexing files (or otherwise warming up data).
   *  Surfaces a spinner so the user knows results may be temporarily empty. */
  isLoading?: boolean;
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
  recentFiles,
  isLoading = false,
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

  // O(1) command-id → CommandDef lookup, recomputed only when the command
  // list changes. Previously the render loop did `commands.find(...)` per
  // visible command, which is O(n × visible_results) on every render.
  const commandIndex = useMemo(() => {
    const m = new Map<string, CommandDef>();
    for (const cmd of commands) m.set(cmd.id, cmd);
    return m;
  }, [commands]);

  // Build results
  const results = useMemo(() => {
    const q = searchQuery;
    const isEmpty = q.trim().length === 0;
    const items: PaletteResult[] = [];

    // Commands — when explicitly in commands mode (via `>` prefix or
    // direct mode) AND the query is empty, list every available command so
    // the user can browse. Otherwise fuzzy-filter.
    if (searchMode === 'all' || searchMode === 'commands') {
      if (isEmpty) {
        if (searchMode === 'commands') {
          for (const cmd of commands) {
            items.push({
              kind: 'command',
              commandId: cmd.id,
              commandLabel: cmd.label,
              highlightedLabel: highlightMatches(cmd.label, []),
              score: 0,
              key: `cmd-${cmd.id}`,
            });
          }
        }
        // 'all' mode + empty query: skip commands so the landing screen is
        // dominated by recent files (lower friction than scrolling a long
        // command list).
      } else {
        for (const cmd of commands) {
          const result = fuzzyScore(q, cmd.label);
          if (result.score < 0.3) continue;
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

    // Files — rank by full path (folder context helps when names collide),
    // but highlight against the *basename* so the visible underlines line
    // up with the user's keystrokes. Empty-query landing surfaces the
    // recent-files list as the "where was I?" affordance.
    if (searchMode === 'all' || searchMode === 'files') {
      if (isEmpty) {
        const recents = recentFiles ?? [];
        // Decreasing score so most-recent stays on top after the group sort.
        recents.forEach((file, i) => {
          const dir = file.path.substring(0, file.path.lastIndexOf('/'));
          items.push({
            kind: 'file',
            filePath: file.path,
            fileName: file.name,
            fileDirectory: dir,
            highlightedLabel: highlightMatches(file.name, []),
            score: recents.length - i,
            key: `recent-${file.path}`,
          });
        });
      } else if (fileResults.length > 0) {
        for (const file of fileResults) {
          const dir = file.path.substring(0, file.path.lastIndexOf('/'));
          const pathScore = fuzzyScore(q, file.path);
          if (pathScore.score < 0.3) continue;
          const nameScore = fuzzyScore(q, file.name);
          items.push({
            kind: 'file',
            filePath: file.path,
            fileName: file.name,
            fileDirectory: dir,
            highlightedLabel: highlightMatches(file.name, nameScore.matches),
            score: pathScore.score,
            key: `file-${file.path}`,
          });
        }
      }
    }

    // Symbols — when in explicit symbols mode, show all symbols in the
    // active file even on an empty query so the palette doubles as a
    // file-outline browser. Otherwise fuzzy-filter.
    if (searchMode === 'all' || searchMode === 'symbols') {
      if (onSearchSymbols) {
        if (isEmpty) {
          if (searchMode === 'symbols') {
            const symbols = onSearchSymbols('');
            for (const sym of symbols) {
              items.push({
                kind: 'symbol',
                highlightedLabel: highlightMatches(sym.name, []),
                score: 0,
                symbolLine: sym.line,
                symbolKind: sym.kind,
                scopePath: sym.scopePath,
                key: `sym-${sym.name}-${sym.line}`,
              });
            }
          }
        } else {
          const symbols = onSearchSymbols(q);
          for (const sym of symbols) {
            const symScore = fuzzyScore(q, sym.name);
            if (symScore.score < 0.3) continue;
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
      }
    }

    // Group results by kind so the rendered list looks like
    //   Commands · Files · Symbols
    // instead of interleaved by raw score. Each group is internally sorted
    // by score, and groups are ordered by their top-scoring item — so if
    // the user's query matches a file very well it leads, with commands
    // tucked below. This also makes the per-group section headers
    // (rendered in JSX) sit in the correct place.
    const groups = new Map<string, PaletteResult[]>();
    for (const item of items) {
      if (!groups.has(item.kind)) groups.set(item.kind, []);
      groups.get(item.kind)!.push(item);
    }
    for (const arr of groups.values()) arr.sort((a, b) => b.score - a.score);
    const orderedKinds = Array.from(groups.keys()).sort((a, b) => {
      const aTop = groups.get(a)?.[0]?.score ?? 0;
      const bTop = groups.get(b)?.[0]?.score ?? 0;
      return bTop - aTop;
    });
    const out: PaletteResult[] = [];
    for (const kind of orderedKinds) {
      for (const item of groups.get(kind)!) {
        out.push(item);
        if (out.length >= 50) return out;
      }
    }
    return out;
  }, [searchQuery, searchMode, commands, fileResults, recentFiles, onSearchSymbols]);

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

  // Helper used by both Tab/Shift+Tab cycling and Cmd+digit shortcuts.
  // Switching modes strips any prefix override so the change is decisive
  // (otherwise typing `>` would silently outrank the mode switch).
  const switchMode = useCallback(
    (next: PaletteMode) => {
      setMode(next);
      setQuery((q) => {
        if (q.startsWith('>') || q.startsWith('@') || q.startsWith('#')) {
          return q.slice(1);
        }
        return q;
      });
    },
    [],
  );

  // Keyboard navigation
  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLInputElement>) => {
      if (e.key === 'Escape') {
        // Two-stage Esc: if the user has typed something, the first Esc
        // clears the query without closing. A second Esc (with empty input)
        // closes the palette.
        if (query) {
          e.preventDefault();
          setQuery('');
          return;
        }
        onClose();
        return;
      }
      // Cmd+1..4 (or Ctrl+1..4 on non-Mac) → jump directly to a mode tab.
      // Power-user shortcut equivalent to clicking the tab.
      if ((e.metaKey || e.ctrlKey) && /^[1-4]$/.test(e.key)) {
        e.preventDefault();
        switchMode(MODE_ORDER[Number(e.key) - 1]);
        return;
      }
      // Tab / Shift+Tab → cycle through modes. Without this, Tab would
      // escape the modal palette entirely, which is hostile in an overlay.
      if (e.key === 'Tab') {
        e.preventDefault();
        const idx = MODE_ORDER.indexOf(searchMode);
        const next = MODE_ORDER[(idx + (e.shiftKey ? -1 : 1) + MODE_ORDER.length) % MODE_ORDER.length];
        switchMode(next);
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
    [results, selectedIndex, query, searchMode, switchMode, onClose, onOpenFile, onExecuteCommand, onNavigateToLine],
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
        <div className="command-palette-mode-tabs" role="tablist" aria-label="Search mode">
          {(['all', 'files', 'symbols', 'commands'] as const).map((m) => (
            <button
              key={m}
              type="button"
              role="tab"
              aria-selected={searchMode === m}
              className={`command-palette-mode-tab ${searchMode === m ? 'active' : ''}`}
              tabIndex={-1}
              onClick={() => {
                // Strip any prefix override so an explicit click is decisive
                // (otherwise the prefix would silently outrank the tab).
                if (query.startsWith('>') || query.startsWith('@') || query.startsWith('#')) {
                  setQuery(query.slice(1));
                }
                setMode(m);
                inputRef.current?.focus();
              }}
            >
              {m.charAt(0).toUpperCase() + m.slice(1)}
            </button>
          ))}
        </div>
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
              searchMode === 'files'
                ? 'Search files…'
                : searchMode === 'symbols'
                  ? 'Search symbols…'
                  : searchMode === 'commands'
                    ? 'Search commands…'
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
          {isLoading && (
            <span
              className="command-palette-loading-badge"
              title="Indexing workspace files…"
              aria-label="Loading"
            >
              <Loader2 size={12} className="command-palette-spin" />
            </span>
          )}
          {query && (
            <button
              type="button"
              className="command-palette-clear"
              onClick={() => {
                setQuery('');
                inputRef.current?.focus();
              }}
              title="Clear (Esc)"
              aria-label="Clear search"
              tabIndex={-1}
            >
              <X size={12} />
            </button>
          )}
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
          {(() => {
            // Insert a section header before the first item of each kind.
            // The results array is already grouped (commands → files →
            // symbols, ordered by best score within each group).
            const isIdleFileGroup = !searchQuery.trim();
            let prevKind: string | null = null;
            return results.map((result, index) => {
              const showHeader = result.kind !== prevKind;
              prevKind = result.kind;
              // Files shown on empty query are recents — label accordingly
              // so the user understands why those particular files appear.
              const headerLabel =
                result.kind === 'file' && isIdleFileGroup ? 'Recent files' : KIND_LABEL[result.kind as 'command' | 'file' | 'symbol'];
              return (
                <Fragment key={result.key}>
                  {showHeader && (
                    <div className="command-palette-group-header" role="presentation">
                      {headerLabel}
                    </div>
                  )}
                  <div
                    id={`command-palette-result-${index}`}
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
                      <>
                        {result.commandId && commandIndex.get(result.commandId)?.shortcut && (
                          <span className="command-palette-item-shortcut" aria-hidden="true">
                            {splitShortcut(commandIndex.get(result.commandId)!.shortcut!).map((chip, i) => (
                              <kbd key={i}>{chip}</kbd>
                            ))}
                          </span>
                        )}
                        <span className="command-palette-item-category">
                          {result.commandId ? commandIndex.get(result.commandId)?.category : null}
                        </span>
                      </>
                    )}
                  </div>
                </Fragment>
              );
            });
          })()}
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
