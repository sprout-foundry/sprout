import React, { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { FileCode } from 'lucide-react';
import type { KeyboardEvent as ReactKeyboardEvent, MouseEvent as ReactMouseEvent } from 'react';
import './FindAllReferencesOverlay.css';

// ── Types ────────────────────────────────────────────────────────────────

export interface ReferenceInfo {
  filePath: string;
  line: number;
  startCol: number;
  endCol: number;
  lineText: string;
}

interface FindAllReferencesOverlayProps {
  visible: boolean;
  symbolName: string;
  references: ReferenceInfo[];
  onSelectReference: (filePath: string, line: number) => void;
  onClose: () => void;
}

// ── Helpers ────────────────────────────────────────────────────────────────

/** Format file path for display — shows last 2 components for brevity. */
function formatFilePath(filePath: string | null): string {
  if (!filePath) return '';
  const parts = filePath.split('/');
  if (parts.length <= 2) return filePath;
  return '...' + parts.slice(-2).join('/');
}

/** Highlight the symbol range in a line of code. */
function highlightSymbol(lineText: string, startCol: number, endCol: number): React.ReactNode {
  if (startCol < 1 || startCol > lineText.length || endCol < startCol || endCol > lineText.length + 1) {
    return lineText;
  }
  const before = lineText.slice(0, startCol - 1);
  const symbol = lineText.slice(startCol - 1, endCol);
  const after = lineText.slice(endCol);

  return (
    <>
      {before}
      <mark className="find-refs-symbol">{symbol}</mark>
      {after}
    </>
  );
}

// ── Component ────────────────────────────────────────────────────────────────

/**
 * Custom equality check for FindAllReferencesOverlay.
 * The `onSelectReference` and `onClose` callbacks are likely recreated by the
 * parent on every render, so we compare them explicitly along with the
 * primitive/array props.
 */
export function areFindAllReferencesPropsEqual(
  prev: FindAllReferencesOverlayProps,
  next: FindAllReferencesOverlayProps,
): boolean {
  if (prev.visible !== next.visible) return false;
  if (prev.symbolName !== next.symbolName) return false;
  if (prev.references !== next.references) return false;
  if (prev.onSelectReference !== next.onSelectReference) return false;
  if (prev.onClose !== next.onClose) return false;
  return true;
}

const FindAllReferencesOverlayImpl = ({
  visible,
  symbolName,
  references,
  onSelectReference,
  onClose,
}: FindAllReferencesOverlayProps): JSX.Element | null => {
  const [selectedIndex, setSelectedIndex] = useState(0);

  const listRef = useRef<HTMLDivElement>(null);
  const prevVisibleRef = useRef(false);

  // ── Reset state when visibility changes ───────────────────────────────

  useEffect(() => {
    if (visible && !prevVisibleRef.current) {
      setSelectedIndex(0);
    }
    prevVisibleRef.current = visible;
  }, [visible]);

  // ── Group references by file, current file first ──────────────────────

  const groupedReferences = useMemo(() => {
    if (!references.length) return [];

    // Determine the "current file" — the backend sorts it first
    const currentFile = references[0]?.filePath || null;

    // Group by filePath, preserving backend order (current file first)
    const groups = new Map<string, ReferenceInfo[]>();
    const fileOrder: string[] = [];

    for (const ref of references) {
      if (!groups.has(ref.filePath)) {
        groups.set(ref.filePath, []);
        fileOrder.push(ref.filePath);
      }
      groups.get(ref.filePath)!.push(ref);
    }

    return fileOrder.map((fp) => ({
      filePath: fp,
      isCurrentFile: fp === currentFile,
      refs: groups.get(fp) || [],
    }));
  }, [references]);

  // ── Flatten for keyboard navigation ───────────────────────────────────

  const flatRefs = useMemo(() => {
    const flat: ReferenceInfo[] = [];
    for (const group of groupedReferences) {
      for (const ref of group.refs) {
        flat.push(ref);
      }
    }
    return flat;
  }, [groupedReferences]);

  // ── Reset selected index when results change ──────────────────────────

  useEffect(() => {
    setSelectedIndex(0);
  }, [references]);

  // ── Scroll selected item into view ────────────────────────────────────

  useEffect(() => {
    const container = listRef.current;
    if (!container) return;
    const selected = container.querySelector('[data-selected="true"]');
    if (selected) {
      selected.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
  }, [selectedIndex]);

  // ── Handle keyboard navigation ────────────────────────────────────────

  const handleKeyDown = useCallback(
    (e: ReactKeyboardEvent) => {
      e.stopPropagation();
      const itemCount = flatRefs.length;

      switch (e.key) {
        case 'Escape':
          e.preventDefault();
          onClose();
          break;

        case 'ArrowDown':
          e.preventDefault();
          setSelectedIndex((prev) => Math.min(prev + 1, Math.max(itemCount - 1, 0)));
          break;

        case 'ArrowUp':
          e.preventDefault();
          setSelectedIndex((prev) => Math.max(prev - 1, 0));
          break;

        case 'Enter':
          e.preventDefault();
          if (flatRefs[selectedIndex]) {
            const ref = flatRefs[selectedIndex];
            onSelectReference(ref.filePath, ref.line);
            onClose();
          }
          break;
      }
    },
    [flatRefs, selectedIndex, onClose, onSelectReference],
  );

  // ── Handle item click ─────────────────────────────────────────────────

  const handleItemClick = useCallback(
    (ref: ReferenceInfo) => {
      onSelectReference(ref.filePath, ref.line);
      onClose();
    },
    [onSelectReference, onClose],
  );

  // ── Handle mouse enter on item ────────────────────────────────────────

  const handleItemMouseEnter = useCallback((index: number) => {
    setSelectedIndex(index);
  }, []);

  // ── Stop mousedown from stealing focus ─────────────────────────────────

  const handleMouseDown = useCallback((e: ReactMouseEvent) => {
    e.preventDefault();
  }, []);

  // ── Render ─────────────────────────────────────────────────────────────

  if (!visible) return null;

  const isLoading = !symbolName && references.length === 0;
  const isEmpty = references.length === 0 && !isLoading;
  const isSearching = isLoading && !symbolName;

  return (
    <div className="find-refs-overlay">
      {/* Header */}
      <div className="find-refs-header">
        {symbolName ? (
          <>
            <span className="find-refs-symbol-name">{symbolName}</span>
            <span className="find-refs-count">
              {references.length} reference{references.length !== 1 ? 's' : ''}
            </span>
          </>
        ) : (
          <span className="find-refs-searching">Searching...</span>
        )}
      </div>

      {/* Reference list */}
      <div
        className="find-refs-list"
        ref={listRef}
        onMouseDown={handleMouseDown}
        role="listbox"
        aria-label={symbolName ? `References to ${symbolName}` : 'References'}
        aria-activedescendant={flatRefs[selectedIndex] ? `ref-item-${selectedIndex}` : undefined}
      >
        {isEmpty && <div className="find-refs-empty">No references found</div>}

        {isSearching && <div className="find-refs-empty">Searching for references...</div>}

        {!isEmpty &&
          !isSearching &&
          groupedReferences.map((group) => {
            if (!group.refs.length) return null;

            return (
              <div key={group.filePath}>
                {/* Group header — show file name for every file group */}
                <div className="find-refs-group-header">
                  <FileCode size={12} />
                  <span>{formatFilePath(group.filePath)}</span>
                </div>

                {/* References in this group */}
                {group.refs.map((ref, refIndex) => {
                  // Calculate the flat index for keyboard navigation
                  let flatIndex = 0;
                  for (const g of groupedReferences) {
                    if (g.filePath === group.filePath) break;
                    flatIndex += g.refs.length;
                  }
                  flatIndex += refIndex;

                  const isActive = flatIndex === selectedIndex;

                  return (
                    <div
                      key={`${ref.filePath}:${ref.line}:${ref.startCol}`}
                      id={`ref-item-${flatIndex}`}
                      data-selected={isActive}
                      role="option"
                      aria-selected={isActive}
                      tabIndex={-1}
                      className={`find-refs-item${isActive ? ' find-refs-item-active' : ''}`}
                      onClick={() => handleItemClick(ref)}
                      onMouseEnter={() => handleItemMouseEnter(flatIndex)}
                    >
                      <span className="find-refs-line-text">
                        {highlightSymbol(ref.lineText, ref.startCol, ref.endCol)}
                      </span>
                      <span className="find-refs-line-num">:{ref.line}</span>
                    </div>
                  );
                })}
              </div>
            );
          })}
      </div>
    </div>
  );
}

export const FindAllReferencesOverlay = React.memo(FindAllReferencesOverlayImpl, areFindAllReferencesPropsEqual);

export default FindAllReferencesOverlay;
