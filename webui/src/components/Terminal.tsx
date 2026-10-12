import { useState, useEffect, useRef, useCallback } from 'react';
import './Terminal.css';
import { createPortal } from 'react-dom';
import { usePersistedNumber } from '../hooks/usePersistedPref';
import { useTerminalWorkbench } from '../hooks/useTerminalWorkbench';
import { type UIScale } from '../hooks/useUIScale';
import { useVerticalDragResize } from '../hooks/useVerticalDragResize';
import { TerminalCollapseButton, TerminalCollapsedStrip } from './TerminalDockControls';
import {
  TERMINAL_HEIGHT_STORAGE_KEY,
  defaultTerminalHeight,
  parseTerminalHeight,
  clampTerminalHeight,
} from './terminalPref';
import { TerminalWorkbench } from './TerminalWorkbench';

/** @deprecated Re-exported for backward compatibility with existing tests. */
export { nextActiveAfterClose } from '../hooks/useTerminalPanes';

interface TerminalProps {
  isConnected?: boolean;
  isExpanded?: boolean;
  onToggleExpand?: (expanded: boolean) => void;
  /** Collapsed means gone, not a strip: for hosts where the terminal is occasional. */
  hideWhenCollapsed?: boolean;
}

function Terminal({
  isConnected = true,
  isExpanded: externalIsExpanded = false,
  onToggleExpand,
  hideWhenCollapsed = false,
}: TerminalProps): JSX.Element {
  const getCollapsedHeight = useCallback(() => {
    if (typeof window === 'undefined') return 42;
    return window.innerWidth <= 768 ? 34 : 42;
  }, []);

  const [isExpanded, setIsExpanded] = useState(externalIsExpanded);
  const [hasActivated, setHasActivated] = useState(externalIsExpanded);
  const [terminalHeight, setTerminalHeight] = usePersistedNumber(
    TERMINAL_HEIGHT_STORAGE_KEY,
    defaultTerminalHeight(),
    parseTerminalHeight,
    clampTerminalHeight,
  );
  const [isResizingVertical, setIsResizingVertical] = useState(false);
  const [collapsedHeight, setCollapsedHeight] = useState(getCollapsedHeight);

  // Current UI tier factor (kept in a ref so the reserved-height effect
  // reads the live value without re-running on tier change — the CSS var
  // update on tier change is handled by the useUIScale effect's cascade).
  const uiScaleRef = useRef<UIScale>('default');
  useEffect(() => {
    try {
      const v = document.documentElement.getAttribute('data-ui-scale');
      if (v === 'compact' || v === 'large' || v === 'xlarge' || v === 'default') uiScaleRef.current = v;
    } catch {
      /* attribute read is safe everywhere */
    }
  }, []);

  const workbench = useTerminalWorkbench({ isExpanded, terminalHeight, setIsResizingVertical });
  const { focusedSession, totalSessions } = workbench;

  /* ---- Effects ---- */

  // Expand/collapse sync with external prop
  useEffect(() => {
    setIsExpanded(externalIsExpanded);
    if (externalIsExpanded) {
      setHasActivated(true);
      window.dispatchEvent(new CustomEvent('sprout-terminal-expand'));
    }
  }, [externalIsExpanded]);

  // CSS custom property for reserved height
  useEffect(() => {
    // LOGICAL px, not scaled: the consumer (.app padding-bottom) lives
    // INSIDE #root's scaled coordinate space — its padding is painted
    // by #root's transform. Emitting logical×scale here made the paint
    // scale² (a growing dead band between editor bottom and terminal
    // top). The terminal portal (outside #root, real viewport) reads
    // this too — it wants the LOGICAL value for its wrapper height and
    // applies the factor itself.
    const collapsed = hideWhenCollapsed ? 0 : collapsedHeight;
    const logical = isExpanded ? terminalHeight : collapsed;
    document.documentElement.style.setProperty('--sprout-terminal-reserved-height', `${logical}px`);
    return () => {
      document.documentElement.style.setProperty('--sprout-terminal-reserved-height', `${collapsed}px`);
    };
  }, [collapsedHeight, hideWhenCollapsed, isExpanded, terminalHeight]);

  // Responsive collapsed height
  useEffect(() => {
    if (typeof window === 'undefined') return undefined;
    const updateCollapsedHeight = () => {
      setCollapsedHeight(getCollapsedHeight());
    };
    updateCollapsedHeight();
    window.addEventListener('resize', updateCollapsedHeight);
    return () => window.removeEventListener('resize', updateCollapsedHeight);
  }, [getCollapsedHeight]);

  /* ---- Actions ---- */
  const toggleExpanded = useCallback(() => {
    setIsExpanded((prev) => {
      const next = !prev;
      if (next) {
        setHasActivated(true);
      }
      onToggleExpand?.(next);
      return next;
    });
  }, [onToggleExpand]);

  /* ---- Terminal height resize ---- */
  const handleVerticalResizeStart = useVerticalDragResize({
    currentHeight: terminalHeight,
    onResize: setTerminalHeight,
  });

  /* ---- Initial mount animation guard ---- */
  const hasMountedRef = useRef(false);
  useEffect(() => {
    if (!hasMountedRef.current) {
      hasMountedRef.current = true;
      const timer = setTimeout(() => {
        hasMountedRef.current = false;
      }, 300);
      return () => clearTimeout(timer);
    }
  }, []);

  /* ---- Render ---- */
  // Portaled to document.body: the terminal is VIEWPORT chrome (fixed to
  // the real viewport bottom), and must live OUTSIDE #root's UI-Size
  // transform. Inside it, three classes of breakage: fixed
  // positioning resolves against the scaled box (ghost offset from the
  // real viewport bottom), JS-set px heights mix logical/real numbers,
  // and xterm.js mis-measures char cells under ancestor transforms
  // (dead bands inside the console). Outside, the container pins to the
  // true viewport; the .terminal-portal wrapper applies the same paint
  // scale via --ui-scale so the terminal still reads at the UI size,
  // and the app's reserved-height var accounts for it (see Terminal.css).
  return createPortal(
    <div
      className={`terminal-portal${isExpanded ? ' terminal-portal--expanded' : ''}${hideWhenCollapsed && !isExpanded ? ' terminal-portal--hidden' : ''}`}
      style={{ ['--terminal-height' as string]: `${isExpanded ? terminalHeight : collapsedHeight}px` }}
    >
      <div
        className={[
          'terminal-container',
          isExpanded ? 'expanded' : 'collapsed',
          hasMountedRef.current ? 'initial-mount' : '',
          isResizingVertical ? 'resizing' : '',
        ]
          .filter(Boolean)
          .join(' ')}
        data-testid="terminal-container"
      >
        {isExpanded && (
          <div
            className="terminal-resize-handle"
            onPointerDown={handleVerticalResizeStart}
            onKeyDown={(e) => {
              // The terminal is docked at the bottom: ArrowUp makes it taller.
              if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return;
              e.preventDefault();
              const step = e.shiftKey ? 50 : 10;
              setTerminalHeight((h) => h + (e.key === 'ArrowUp' ? step : -step));
            }}
            tabIndex={0}
            role="separator"
            aria-orientation="horizontal"
            aria-label="Resize terminal"
            aria-valuenow={Math.round(terminalHeight)}
            title="Drag to resize terminal"
          />
        )}

        {!isExpanded && (
          <TerminalCollapsedStrip
            onExpand={toggleExpanded}
            sessionName={focusedSession?.name}
            totalSessions={totalSessions}
          />
        )}

        {/* Body */}
        <TerminalWorkbench
          workbench={workbench}
          isConnected={isConnected}
          isExpanded={isExpanded}
          isActive={hasActivated || isExpanded}
          trailingActions={<TerminalCollapseButton onCollapse={toggleExpanded} expanded={isExpanded} />}
        />
      </div>
    </div>,
    document.body,
  );
}

export default Terminal;
