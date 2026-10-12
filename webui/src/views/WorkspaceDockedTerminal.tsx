import { useEffect, useState } from 'react';
import { TerminalCollapseButton, TerminalCollapsedStrip } from '../components/TerminalDockControls';
import {
  TERMINAL_HEIGHT_STORAGE_KEY,
  clampTerminalHeight,
  defaultTerminalHeight,
  parseTerminalHeight,
} from '../components/terminalPref';
import { TerminalWorkbench } from '../components/TerminalWorkbench';
import { usePersistedNumber } from '../hooks/usePersistedPref';
import { useTerminalWorkbench } from '../hooks/useTerminalWorkbench';
import { useVerticalDragResize } from '../hooks/useVerticalDragResize';
import { useCommandHandler } from '../services/commandHandlers';
import type { WorkspaceTerminalBackend } from './WorkspaceTerminalView';
import '../components/Terminal.css';

const TERMINAL_COMMANDS: readonly string[] = ['toggle_terminal'];
const PTY_TERMINAL_COMMANDS: readonly string[] = [
  'toggle_terminal',
  'split_terminal_vertical',
  'split_terminal_horizontal',
  'clear_terminal',
  'kill_terminal',
];
const NO_COMMANDS: readonly string[] = [];

export interface WorkspaceDockedTerminalProps {
  backend: Exclude<WorkspaceTerminalBackend, 'none'>;
  expanded: boolean;
  onExpandedChange: (expanded: boolean) => void;
}

/**
 * The composed space shell's terminal: the app's docked terminal (the same
 * workbench — tabs, splits, shell picker, background sessions — the same
 * persisted height, resize handle and collapse controls), docked at the
 * bottom of the workspace in flow rather than fixed to the viewport, so it
 * never covers the host page around the workspace.
 *
 * As in the app, a PTY terminal collapses to a strip that expands it, and the
 * in-browser (WASM) shell is out of the way until opened (Ctrl+`, the palette,
 * or the sidebar's Terminal entry). The workbench stays mounted while
 * collapsed, so sessions survive a toggle.
 */
export function WorkspaceDockedTerminal({
  backend,
  expanded,
  onExpandedChange,
}: WorkspaceDockedTerminalProps): JSX.Element {
  const [height, setHeight] = usePersistedNumber(
    TERMINAL_HEIGHT_STORAGE_KEY,
    defaultTerminalHeight(),
    parseTerminalHeight,
    clampTerminalHeight,
  );
  const [isResizingVertical, setIsResizingVertical] = useState(false);
  const [hasActivated, setHasActivated] = useState(expanded);
  const workbench = useTerminalWorkbench({ isExpanded: expanded, terminalHeight: height, setIsResizingVertical });
  const startResize = useVerticalDragResize({ currentHeight: height, onResize: setHeight });

  useEffect(() => {
    if (!expanded) return;
    setHasActivated(true);
    // The panes refit to the size they are shown at.
    window.dispatchEvent(new CustomEvent('sprout-terminal-expand'));
  }, [expanded]);

  const hidden = !expanded && backend === 'wasm';
  const className = [
    'sprout-workspace-shell__terminal',
    expanded ? 'sprout-workspace-shell__terminal--expanded' : '',
    isResizingVertical ? 'resizing' : '',
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <div
      className={className}
      style={expanded ? { height: `${height}px` } : undefined}
      hidden={hidden}
      data-testid="workspace-docked-terminal"
      data-backend={backend}
      data-expanded={String(expanded)}
    >
      <div
        className={`terminal-container ${expanded ? 'expanded' : 'collapsed'}${isResizingVertical ? ' resizing' : ''}`}
      >
        {expanded && (
          <div
            className="terminal-resize-handle"
            onPointerDown={startResize}
            onKeyDown={(e) => {
              if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return;
              e.preventDefault();
              const step = e.shiftKey ? 50 : 10;
              setHeight((h) => h + (e.key === 'ArrowUp' ? step : -step));
            }}
            tabIndex={0}
            role="separator"
            aria-orientation="horizontal"
            aria-label="Resize terminal"
            aria-valuenow={Math.round(height)}
            title="Drag to resize terminal"
          />
        )}
        {!expanded && backend === 'pty' && (
          <TerminalCollapsedStrip
            onExpand={() => onExpandedChange(true)}
            sessionName={workbench.focusedSession?.name}
            totalSessions={workbench.totalSessions}
          />
        )}
        {/* `isConnected` selects the pane transport: the PTY socket, or the
            in-browser WASM shell. */}
        <TerminalWorkbench
          workbench={workbench}
          isConnected={backend === 'pty'}
          isExpanded={expanded}
          isActive={hasActivated || expanded}
          trailingActions={<TerminalCollapseButton onCollapse={() => onExpandedChange(false)} expanded={expanded} />}
        />
      </div>
    </div>
  );
}

/**
 * Handle `toggle_terminal` (Ctrl+` and the palette's Toggle Terminal) — and,
 * for a PTY terminal, the split/clear/kill commands the app's hotkey handler
 * forwards to it — registered so the scoped hotkeys, the menu and the palette
 * offer them from inside the workspace. Nothing registers without a terminal.
 */
export function useToggleTerminalCommand(
  setExpanded: (update: (expanded: boolean) => boolean) => void,
  backend: WorkspaceTerminalBackend,
): void {
  useCommandHandler(backend === 'pty' ? PTY_TERMINAL_COMMANDS : backend === 'wasm' ? TERMINAL_COMMANDS : NO_COMMANDS);
  const enabled = backend !== 'none';
  useEffect(() => {
    if (!enabled) return;
    const handle = (event: Event) => {
      if ((event as CustomEvent<{ commandId?: string }>).detail?.commandId !== 'toggle_terminal') return;
      setExpanded((expanded) => !expanded);
    };
    window.addEventListener('sprout:hotkey', handle);
    return () => window.removeEventListener('sprout:hotkey', handle);
  }, [enabled, setExpanded]);
}
