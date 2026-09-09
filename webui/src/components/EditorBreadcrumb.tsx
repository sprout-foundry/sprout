import { ChevronRight } from 'lucide-react';
import { useMemo, useCallback, memo, useSyncExternalStore } from 'react';
import type { KeyboardEvent } from 'react';
import { getWorkspaceCwd, subscribeWorkspaceCwd } from '../services/workspaceCwd';
import { type SymbolInfo as BreadcrumbSymbol, type SymbolKind, KIND_ICONS } from '../utils/symbolUtils';
import './EditorBreadcrumb.css';

// ── Workspace cwd snapshot (external store) ─────────────────────────────

function useWorkspaceCwd(): string {
  return useSyncExternalStore(subscribeWorkspaceCwd, getWorkspaceCwd, () => '');
}

// ── Path collapsing ──────────────────────────────────────────────────────

/**
 * Collapse the middle of a long segment list into an ellipsis item, keeping
 * the first segment (workspace root context) and the last two (the file's
 * immediate directory + name). Single leading collapse keeps enough context
 * to know WHERE you are without showing the whole machine path.
 *
 * Examples:
 *   ['Users','alanp','dev','sprout-foundry','sprout','webui','src','components','App.tsx']
 *     → ['Users', '…', 'src', 'components', 'App.tsx']   (when over budget)
 *   ['webui','src','components','App.tsx']                → unchanged (fits)
 */
function collapseSegments(parts: string[], maxSegments = 5): { visible: string[]; collapsed: boolean } {
  if (parts.length <= maxSegments) return { visible: parts, collapsed: false };
  const head = parts.slice(0, 1);
  const tail = parts.slice(-2);
  const visible = [...head, '\u2026', ...tail];
  return { visible, collapsed: true };
}

/** Strip a known workspace prefix so the breadcrumb shows project-relative
 * segments instead of the full machine path. Returns the input when no
 * prefix matches (paths outside the workspace keep their full form, but
 * the middle-collapse still applies). */
function relativizePath(filePath: string, cwd: string): string {
  if (!cwd) return filePath;
  const norm = (p: string) => p.replace(/\/+$/, '');
  const c = norm(cwd);
  if (c && (filePath === c || filePath.startsWith(c + '/'))) {
    return filePath.slice(c.length + 1) || '/';
  }
  return filePath;
}

// ── Types ────────────────────────────────────────────────────────────────

// BreadcrumbSymbol is re-exported as an alias for SymbolInfo (from
// symbolUtils) so that existing consumers that import
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

function EditorBreadcrumb({
  filePath,
  onNavigate,
  symbols,
  onNavigateToSymbol,
}: EditorBreadcrumbProps): JSX.Element | null {
  // ── Workspace cwd + file path segments ───────────────────────────────

  const cwd = useWorkspaceCwd();

  const segments = useMemo(() => {
    // Don't show breadcrumbs for virtual workspace paths
    if (filePath.startsWith('__workspace/')) return null;
    // Don't show breadcrumbs for empty or plain filenames without directory parts
    if (!filePath || !filePath.includes('/')) return null;

    // Display relative to the workspace when possible — the full machine
    // path is noise (and post-LSP-fix buffer paths are absolute).
    const displayPath = relativizePath(filePath, cwd);
    const parts = displayPath.split('/').filter(Boolean);
    if (parts.length < 2) return null;
    return parts;
  }, [filePath, cwd]);

  // ── Symbol segments ──────────────────────────────────────────────────

  const hasSymbols = symbols && symbols.length > 0;

  // ── Path click handler ───────────────────────────────────────────────

  // Collapse AFTER relativizing; navigation rebuilds the path from the
  // ORIGINAL (absolute) filePath so reveal-in-explorer still gets a real
  // filesystem path even though the display path is shortened.
  // Keep the leading-slash marker so rebuilt prefixes stay absolute when
  // the source path was absolute (reveal-in-explorer needs a real path).
  const originalParts = useMemo(() => {
    const isAbs = filePath.startsWith('/');
    const parts = filePath.split('/').filter(Boolean);
    return isAbs ? ['/' + parts[0], ...parts.slice(1)] : parts;
  }, [filePath]);
  const { visible: displayParts } = useMemo(() => collapseSegments(segments ?? []), [segments]);

  const handleClick = useCallback(
    (displayIndex: number) => {
      if (!segments || !onNavigate) return;
      const label = displayParts[displayIndex];
      if (label === '\u2026') return; // collapsed middle — not navigable
      // Map the displayed label back to its original segment index.
      let origIndex = -1;
      if (displayIndex === 0) {
        origIndex = 0;
      } else {
        const offset = originalParts.length - displayParts.length; // segments dropped by the collapse
        origIndex = displayIndex + Math.max(offset, 0);
        if (displayIndex === displayParts.length - 1) origIndex = originalParts.length - 1;
      }
      if (origIndex < 0 || origIndex >= originalParts.length) return;
      const isLast = origIndex === originalParts.length - 1;
      if (isLast && !hasSymbols) return;
      const path = originalParts.slice(0, origIndex + 1).join('/');
      onNavigate(path);
    },
    [segments, displayParts, originalParts, onNavigate, hasSymbols],
  );

  // Allow keyboard activation (Enter/Space) on breadcrumb buttons
  const handleKeyDown = useCallback(
    (e: KeyboardEvent, index: number) => {
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
    (e: KeyboardEvent, line: number) => {
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
          displayParts.map((segment, index) => {
            const isEllipsis = segment === '…';
            const isCurrent = !isEllipsis && index === displayParts.length - 1;
            const origIndex = isEllipsis
              ? -1
              : index === 0
                ? 0
                : index === displayParts.length - 1
                  ? originalParts.length - 1
                  : index + Math.max(originalParts.length - displayParts.length, 0);
            const path = origIndex >= 0 ? originalParts.slice(0, origIndex + 1).join('/') : filePath;
            return (
              <li key={`path-${index}`} className="breadcrumb-item">
                {index > 0 && (
                  <span className="breadcrumb-separator" aria-hidden="true">
                    <ChevronRight size={12} />
                  </span>
                )}
                {isEllipsis ? (
                  <span className="breadcrumb-segment breadcrumb-segment-ellipsis" aria-hidden="true">
                    …
                  </span>
                ) : isCurrent && !hasSymbols ? (
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
}

export default memo(EditorBreadcrumb);
