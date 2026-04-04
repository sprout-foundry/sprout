import React, { useMemo, useCallback } from 'react';
import { ChevronRight } from 'lucide-react';
import { type SymbolInfo as BreadcrumbSymbol, type SymbolKind, KIND_ICONS } from './GoToSymbolOverlay';
import './EditorBreadcrumb.css';

// ── Types ────────────────────────────────────────────────────────────────

// BreadcrumbSymbol is re-exported as an alias for SymbolInfo (from
// GoToSymbolOverlay) so that existing consumers that import
// `BreadcrumbSymbol` from this module continue to compile without changes.
export type { BreadcrumbSymbol };

interface EditorBreadcrumbProps {
  filePath: string;
  onNavigate?: (path: string) => void;
  symbols?: BreadcrumbSymbol[];
  onNavigateToSymbol?: (line: number) => void;
}

// ── Kind icon lookup (safe against unknown kinds) ────────────────────────

const getKindIcon = (kind: SymbolKind): string => (KIND_ICONS as Record<string, string>)[kind] || '?';

// ── Component ────────────────────────────────────────────────────────────

const EditorBreadcrumb: React.FC<EditorBreadcrumbProps> = ({ filePath, onNavigate, symbols, onNavigateToSymbol }) => {
  // ── File path segments ───────────────────────────────────────────────

  const segments = useMemo(() => {
    // Don't show breadcrumbs for virtual workspace paths
    if (filePath.startsWith('__workspace/')) return null;
    // Don't show breadcrumbs for empty or plain filenames without directory parts
    if (!filePath || !filePath.includes('/')) return null;

    const parts = filePath.split('/').filter(Boolean);
    if (parts.length < 2) return null;
    return parts;
  }, [filePath]);

  // ── Symbol segments ──────────────────────────────────────────────────

  const hasSymbols = symbols && symbols.length > 0;

  // ── Path click handler ───────────────────────────────────────────────

  const handleClick = useCallback(
    (index: number) => {
      if (!segments || !onNavigate) return;
      // The last path segment is "current" (non-clickable) only when there
      // are no symbol breadcrumbs following it.
      if (index === segments.length - 1 && !hasSymbols) return;
      const path = segments.slice(0, index + 1).join('/');
      onNavigate(path);
    },
    [segments, onNavigate, hasSymbols],
  );

  // Allow keyboard activation (Enter/Space) on breadcrumb buttons
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent, index: number) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        handleClick(index);
      }
    },
    [handleClick],
  );

  // ── Symbol click handler ─────────────────────────────────────────────

  const handleSymbolClick = useCallback(
    (line: number) => {
      if (onNavigateToSymbol) {
        onNavigateToSymbol(line);
      }
    },
    [onNavigateToSymbol],
  );

  const handleSymbolKeyDown = useCallback(
    (e: React.KeyboardEvent, line: number) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        handleSymbolClick(line);
      }
    },
    [handleSymbolClick],
  );

  // ── Render guard ─────────────────────────────────────────────────────

  // Show nothing if there's neither path segments nor symbols
  if (!segments && !hasSymbols) return null;

  return (
    <nav className="editor-breadcrumb" aria-label="Breadcrumb">
      <ol className="breadcrumb-list">
        {/* ── Path segments ── */}
        {segments &&
          segments.map((segment, index) => {
            const isCurrent = index === segments.length - 1;
            const path = segments.slice(0, index + 1).join('/');
            return (
              <li key={`path-${index}`} className="breadcrumb-item">
                {index > 0 && (
                  <span className="breadcrumb-separator" aria-hidden="true">
                    <ChevronRight size={12} />
                  </span>
                )}
                {isCurrent && !hasSymbols ? (
                  <span className="breadcrumb-segment breadcrumb-segment-current" aria-current="page">
                    {segment}
                  </span>
                ) : (
                  <button
                    className="breadcrumb-segment"
                    onClick={() => handleClick(index)}
                    onKeyDown={(e) => handleKeyDown(e, index)}
                    title={path}
                    type="button"
                  >
                    {segment}
                  </button>
                )}
              </li>
            );
          })}

        {/* ── Symbol separator (dot separator between path and symbols) ── */}
        {segments && hasSymbols && (
          <li className="breadcrumb-item">
            <span className="breadcrumb-symbol-section-separator" aria-hidden="true">
              <ChevronRight size={12} />
            </span>
          </li>
        )}

        {/* ── Symbol segments ── */}
        {symbols &&
          symbols.length > 0 &&
          symbols.map((sym, index) => {
            const isCurrent = index === symbols.length - 1;
            const icon = getKindIcon(sym.kind);
            return (
              <li key={`sym-${sym.kind}-${sym.name}-${sym.line}`} className="breadcrumb-item">
                {index > 0 && (
                  <span className="breadcrumb-separator" aria-hidden="true">
                    <ChevronRight size={12} />
                  </span>
                )}
                {isCurrent ? (
                  <span
                    className="breadcrumb-segment breadcrumb-symbol breadcrumb-segment-current"
                    aria-current="page"
                    title={`${sym.kind} ${sym.name}:${sym.line}`}
                  >
                    <span className="breadcrumb-symbol-icon">{icon}</span>
                    {sym.name}
                  </span>
                ) : (
                  <button
                    className="breadcrumb-segment breadcrumb-symbol"
                    onClick={() => handleSymbolClick(sym.line)}
                    onKeyDown={(e) => handleSymbolKeyDown(e, sym.line)}
                    title={`${sym.kind} ${sym.name}:${sym.line}`}
                    type="button"
                  >
                    <span className="breadcrumb-symbol-icon">{icon}</span>
                    {sym.name}
                  </button>
                )}
              </li>
            );
          })}
      </ol>
    </nav>
  );
};

export default EditorBreadcrumb;
