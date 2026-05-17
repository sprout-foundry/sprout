import '../CommandPalette.css';
import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import type { ChangeEvent, KeyboardEvent, MouseEvent } from 'react';
import { useHotkeys } from '../../contexts/HotkeyContext';
import { ApiService } from '../../services/api';
import { useLog } from '../../utils/log';
import { extractSymbols, buildScopePaths, KIND_ICONS } from '../../utils/symbolUtils';
import { MODE_TABS } from './constants';
import type { CommandPaletteProps, PaletteMode, PaletteResult } from './types';
import useCommandExecutor from './useCommandExecutor';
import useFileIndex from './useFileIndex';
import usePaletteResults from './usePaletteResults';

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
  const [mode, setMode] = useState<PaletteMode>('all');
  const savedInitialMode = useRef<PaletteMode>(initialMode);
  const inputRef = useRef<HTMLInputElement>(null);
  const resultsRef = useRef<HTMLDivElement>(null);
  const prevOpenRef = useRef(false);

  // Refs to avoid stale closures in handleKeyDown
  const selectedIndexRef = useRef(0);
  const navigableItemsRef = useRef<PaletteResult[]>([]);

  // ── File index ─────────────────────────────────────────────────────────

  const { allFiles, workspaceRoot, isLoadingFiles } = useFileIndex({
    apiService,
    isOpen,
    log,
  });

  // ── Symbols ────────────────────────────────────────────────────────────

  const allSymbols = useMemo(() => {
    if (!activeBufferContent) return [];
    return extractSymbols(activeBufferContent, activeBufferFileExtension);
  }, [activeBufferContent, activeBufferFileExtension]);

  const scopePaths = useMemo(
    () => buildScopePaths(activeBufferContent || '', activeBufferFileExtension, allSymbols),
    [activeBufferContent, activeBufferFileExtension, allSymbols],
  );

  // ── Palette results ────────────────────────────────────────────────────

  const { results, navigableItems, selectedFlatIndex, effectiveMode, searchQuery, toNavigableIndex } =
    usePaletteResults({
      query,
      mode,
      allFiles,
      allSymbols,
      scopePaths,
      workspaceRoot,
      activeBufferFileExtension,
      selectedIndex,
    });

  // Sync refs for handleKeyDown closure
  selectedIndexRef.current = selectedIndex;
  navigableItemsRef.current = navigableItems;

  // ── Command executor ───────────────────────────────────────────────────

  const { executeCommand, executeSelected } = useCommandExecutor({
    onClose,
    onToggleSidebar,
    onToggleTerminal,
    onOpenHotkeysConfig,
    setMode,
    setQuery,
    inputRef,
  });

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
    }
    prevOpenRef.current = isOpen;
  }, [isOpen, initialMode]);

  // ── Auto-focus ────────────────────────────────────────────────────────

  useEffect(() => {
    if (isOpen && inputRef.current) {
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [isOpen]);

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

  // ── Cycle mode ────────────────────────────────────────────────────────

  const cycleMode = useCallback((direction: 1 | -1) => {
    setMode((prev) => {
      const idx = MODE_TABS.findIndex((t) => t.mode === prev);
      const next = (idx + direction + MODE_TABS.length) % MODE_TABS.length;
      return MODE_TABS[next].mode;
    });
  }, []);

  // ── Handle text input ─────────────────────────────────────────────────

  const handleInputChange = useCallback((e: ChangeEvent<HTMLInputElement>) => {
    setQuery(e.target.value);
  }, []);

  // ── Handle keyboard navigation ────────────────────────────────────────

  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLInputElement>) => {
      if (e.ctrlKey || e.metaKey) {
        e.preventDefault();
        e.stopPropagation();
      }

      const navItems = navigableItemsRef.current;
      const selIdx = selectedIndexRef.current;

      switch (e.key) {
        case 'Escape':
          e.preventDefault();
          onClose();
          break;
        case 'ArrowDown':
          e.preventDefault();
          setSelectedIndex((prev) => Math.min(prev + 1, Math.max(navItems.length - 1, 0)));
          break;
        case 'ArrowUp':
          e.preventDefault();
          setSelectedIndex((prev) => Math.max(prev - 1, 0));
          break;
        case 'Enter':
          e.preventDefault();
          e.stopPropagation();
          executeSelected(navItems[selIdx], onOpenFile, onNavigateToLine);
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
    [query, mode, cycleMode, executeSelected, onOpenFile, onNavigateToLine, onClose],
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

  const prefixIcon = effectiveMode === 'commands' ? '>' : effectiveMode === 'symbols' ? '@' : '';

  const hintLabel =
    !hasQuery && mode !== 'all'
      ? `${mode.charAt(0).toUpperCase() + mode.slice(1)} mode · Tab to cycle · Backspace to reset`
      : !hasQuery
        ? '> commands · @ symbols · Tab to cycle modes'
        : '';

  return (
    <div className="command-palette-overlay" onClick={handleOverlayClick} role="presentation">
      <div
        className="command-palette-container"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="Command Palette"
      >
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
                  onClick={() => {
                    onOpenFile(filePath);
                    onClose();
                  }}
                  onMouseEnter={() => setSelectedIndex(toNavigableIndex(index))}
                >
                  <span className="command-palette-file-icon">📄</span>
                  <span className="command-palette-file-meta">
                    <span
                      className="command-palette-file-name"
                      dangerouslySetInnerHTML={{ __html: item.highlightedLabel }}
                    />
                    {item.fileDirectory && (
                      <span
                        className="command-palette-file-path"
                        title={item.fileDirectory}
                        dangerouslySetInnerHTML={{ __html: item.secondaryHighlightedLabel || item.fileDirectory }}
                      />
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
                  <span
                    className={`command-palette-symbol-icon goto-symbol-kind goto-symbol-kind-${item.symbolKind || 'function'}`}
                  >
                    {icon}
                  </span>
                  <span className="command-palette-file-meta">
                    <span
                      className="command-palette-file-name"
                      dangerouslySetInnerHTML={{ __html: item.highlightedLabel }}
                    />
                    {item.scopePath && <span className="command-palette-symbol-scope">{item.scopePath}</span>}
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
